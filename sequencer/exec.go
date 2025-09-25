package sequencer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/gas"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/engine/state/contracts"
)

// Block represents a block of transactions
type Block struct {
	Header       *BlockHeader    `json:"header"`
	Transactions []*Transaction  `json:"transactions"`
	Results      []*ExecResult   `json:"results"`
	StateRoot    []byte          `json:"state_root"`
	Size         uint64          `json:"size"`
}

// BlockHeader contains block metadata
type BlockHeader struct {
	Height        uint64    `json:"height"`
	PrevHash      []byte    `json:"prev_hash"`
	Timestamp     time.Time `json:"timestamp"`
	SequencerID   string    `json:"sequencer_id"`
	TxCount       int       `json:"tx_count"`
	GasUsed       uint64    `json:"gas_used"`
	GasLimit      uint64    `json:"gas_limit"`
}

// ExecResult represents the result of executing a transaction
type ExecResult struct {
	TxID          string              `json:"tx_id"`
	Success       bool                `json:"success"`
	GasUsed       uint64              `json:"gas_used"`
	Receipt       *state.Receipt      `json:"receipt"`
	Error         string              `json:"error,omitempty"`
	ExecutionTime time.Duration       `json:"execution_time"`
	StateChanges  []state.StateChange `json:"state_changes"`
	StagedOps     []*runtime.StagedOp `json:"staged_ops"`
	Events        []*runtime.Event    `json:"events"`
}

// ScopeCache caches authority scopes for contracts
type ScopeCache struct {
	mu     sync.RWMutex
	cache  map[string]*scopeCacheEntry
	ttl    time.Duration
}

type scopeCacheEntry struct {
	scope     *outputs.AuthorityScope
	fetchedAt time.Time
}

// NewScopeCache creates a new scope cache with the specified TTL
func NewScopeCache(ttl time.Duration) *ScopeCache {
	return &ScopeCache{
		cache: make(map[string]*scopeCacheEntry),
		ttl:   ttl,
	}
}

// Get retrieves a cached scope or fetches a new one
func (sc *ScopeCache) Get(ctx context.Context, dnEndpoint, contractURL string) (*outputs.AuthorityScope, error) {
	sc.mu.RLock()
	if entry, exists := sc.cache[contractURL]; exists {
		if time.Since(entry.fetchedAt) < sc.ttl {
			sc.mu.RUnlock()
			return entry.scope, nil
		}
	}
	sc.mu.RUnlock()

	// Cache miss or expired, fetch new scope
	scope, err := outputs.FetchFromDN(ctx, dnEndpoint, contractURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch authority scope: %w", err)
	}

	// Update cache
	sc.mu.Lock()
	sc.cache[contractURL] = &scopeCacheEntry{
		scope:     scope,
		fetchedAt: time.Now(),
	}
	sc.mu.Unlock()

	return scope, nil
}

// ExecutionEngine handles transaction execution for the sequencer
type ExecutionEngine struct {
	mu               sync.RWMutex
	config           ExecutionConfig
	runtime          *runtime.Runtime
	kvStore          state.KVStore
	contractStore    *contracts.Store
	gasMeter         *gas.Meter
	scheduleProvider *pricing.ScheduleProvider

	// Authority scope management
	scopeCache    *ScopeCache
	limitTracker  *outputs.OperationLimitTracker
	dnEndpoint    string

	// State management
	currentHeight uint64
	stateRoot     []byte

	// Worker pool for parallel execution
	workers   []*worker
	workQueue chan *workItem

	// Statistics
	totalExecuted uint64
	totalGasUsed  uint64
	avgExecTime   time.Duration
}

// worker represents a worker in the execution pool
type worker struct {
	id      int
	engine  *ExecutionEngine
	runtime *runtime.Runtime
	kvStore state.KVStore
}

// workItem represents work to be done by a worker
type workItem struct {
	tx       *Transaction
	resultCh chan *ExecResult
}

// NewExecutionEngine creates a new execution engine
func NewExecutionEngine(config ExecutionConfig, kvStore state.KVStore, contractStore *contracts.Store, scheduleProvider *pricing.ScheduleProvider, dnEndpoint string) (*ExecutionEngine, error) {
	// Create WASM runtime
	wasmRuntime, err := runtime.NewRuntime(&config.Runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to create WASM runtime: %w", err)
	}

	// Create gas meter with initial schedule
	var gasConfig gas.Config
	if scheduleProvider != nil {
		schedule := scheduleProvider.Get()
		gasConfig = gas.Config{
			BaseGas:    config.Gas.BaseGas,
			GCR:        schedule.GetGCR(),
			HostCosts:  schedule.HostCosts,
			PerByteRead:  schedule.GetReadCost(),
			PerByteWrite: schedule.GetWriteCost(),
		}
	} else {
		gasConfig = config.Gas
	}

	gasMeter := gas.NewMeterWithConfig(gas.GasLimit(gasConfig.BaseGas*1000), &gasConfig)

	engine := &ExecutionEngine{
		config:           config,
		runtime:          wasmRuntime,
		kvStore:          kvStore,
		contractStore:    contractStore,
		gasMeter:         gasMeter,
		scheduleProvider: scheduleProvider,
		scopeCache:       NewScopeCache(30 * time.Second), // Cache scopes for 30 seconds
		limitTracker:     outputs.NewOperationLimitTracker(),
		dnEndpoint:       dnEndpoint,
		currentHeight:    0,
		stateRoot:        make([]byte, 32), // Genesis state root
		workQueue:        make(chan *workItem, config.WorkerCount*2),
	}

	// Initialize worker pool if parallel execution is enabled
	if config.EnableParallel {
		if err := engine.initWorkerPool(); err != nil {
			return nil, fmt.Errorf("failed to initialize worker pool: %w", err)
		}
	}

	return engine, nil
}

// initWorkerPool initializes the worker pool for parallel execution
func (e *ExecutionEngine) initWorkerPool() error {
	e.workers = make([]*worker, e.config.WorkerCount)

	for i := 0; i < e.config.WorkerCount; i++ {
		// Create worker runtime
		workerRuntime, err := runtime.NewRuntime(&e.config.Runtime)
		if err != nil {
			return fmt.Errorf("failed to create worker %d runtime: %w", i, err)
		}

		// Create worker state store
		workerKVStore := state.NewMemoryKVStore()

		worker := &worker{
			id:      i,
			engine:  e,
			runtime: workerRuntime,
			kvStore: workerKVStore,
		}

		e.workers[i] = worker

		// Start worker goroutine
		go worker.run()
	}

	return nil
}

// ExecuteTransactions executes a batch of transactions and returns a block
func (e *ExecutionEngine) ExecuteTransactions(ctx context.Context, txs []*Transaction) (*Block, error) {
	if len(txs) == 0 {
		return nil, fmt.Errorf("no transactions to execute")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	startTime := time.Now()

	// Create block header
	header := &BlockHeader{
		Height:      e.currentHeight + 1,
		PrevHash:    e.stateRoot,
		Timestamp:   time.Now(),
		SequencerID: "accumen-seq-1", // TODO: get from config
		TxCount:     len(txs),
		GasLimit:    e.gasMeter.GasLimit(),
	}

	// Execute transactions
	var results []*ExecResult
	var totalGasUsed uint64

	if e.config.EnableParallel {
		results = e.executeParallel(ctx, txs)
	} else {
		results = e.executeSequential(ctx, txs)
	}

	// Calculate total gas used
	for _, result := range results {
		totalGasUsed += result.GasUsed
	}

	header.GasUsed = totalGasUsed

	// Calculate new state root (simplified)
	newStateRoot := e.calculateStateRoot(results)

	// Create block
	block := &Block{
		Header:       header,
		Transactions: txs,
		Results:      results,
		StateRoot:    newStateRoot,
		Size:         e.calculateBlockSize(header, txs, results),
	}

	// Update engine state
	e.currentHeight = header.Height
	e.stateRoot = newStateRoot
	e.totalExecuted += uint64(len(txs))
	e.totalGasUsed += totalGasUsed

	// Reset operation limit tracker for next block
	e.limitTracker.Reset()

	// Update average execution time
	execTime := time.Since(startTime)
	if e.avgExecTime == 0 {
		e.avgExecTime = execTime
	} else {
		e.avgExecTime = (e.avgExecTime + execTime) / 2
	}

	return block, nil
}

// executeSequential executes transactions sequentially
func (e *ExecutionEngine) executeSequential(ctx context.Context, txs []*Transaction) []*ExecResult {
	results := make([]*ExecResult, len(txs))

	for i, tx := range txs {
		result := e.executeSingleTransaction(ctx, tx)
		results[i] = result
	}

	return results
}

// executeParallel executes transactions in parallel using worker pool
func (e *ExecutionEngine) executeParallel(ctx context.Context, txs []*Transaction) []*ExecResult {
	results := make([]*ExecResult, len(txs))
	resultChans := make([]chan *ExecResult, len(txs))

	// Submit work items to workers
	for i, tx := range txs {
		resultChan := make(chan *ExecResult, 1)
		resultChans[i] = resultChan

		workItem := &workItem{
			tx:       tx,
			resultCh: resultChan,
		}

		select {
		case e.workQueue <- workItem:
		case <-ctx.Done():
			// Context cancelled, create error result
			results[i] = &ExecResult{
				TxID:    tx.ID,
				Success: false,
				Error:   "execution cancelled",
			}
		}
	}

	// Collect results
	for i, resultChan := range resultChans {
		select {
		case result := <-resultChan:
			results[i] = result
		case <-ctx.Done():
			results[i] = &ExecResult{
				TxID:    txs[i].ID,
				Success: false,
				Error:   "execution timeout",
			}
		}
	}

	return results
}

// executeSingleTransaction executes a single transaction
func (e *ExecutionEngine) executeSingleTransaction(ctx context.Context, tx *Transaction) *ExecResult {
	startTime := time.Now()

	result := &ExecResult{
		TxID:          tx.ID,
		ExecutionTime: 0,
		StateChanges:  make([]state.StateChange, 0),
	}

	// Create gas meter for this transaction with current schedule
	var gasConfig gas.Config
	if e.scheduleProvider != nil {
		schedule := e.scheduleProvider.Get()
		gasConfig = gas.Config{
			BaseGas:      e.config.Gas.BaseGas,
			GCR:          schedule.GetGCR(),
			HostCosts:    schedule.HostCosts,
			PerByteRead:  schedule.GetReadCost(),
			PerByteWrite: schedule.GetWriteCost(),
		}
	} else {
		gasConfig = e.config.Gas
	}
	txGasMeter := gas.NewMeterWithConfig(gas.GasLimit(tx.GasLimit), &gasConfig)

	// Load WASM module for the contract
	if e.contractStore == nil {
		result.Success = false
		result.Error = "contract store not available"
		result.ExecutionTime = time.Since(startTime)
		return result
	}

	wasmBytes, wasmHash, err := e.contractStore.GetModule(tx.To)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("contract not found: %s - %v", tx.To, err)
		result.ExecutionTime = time.Since(startTime)
		return result
	}

	// Execute transaction using WASM runtime with the loaded module
	execResult, err := e.runtime.ExecuteContract(ctx, wasmBytes, wasmHash[:], "main", []uint64{}, e.kvStore)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.ExecutionTime = time.Since(startTime)
		return result
	}

	// Verify staged operations against authority scope if there are any
	if len(execResult.StagedOps) > 0 {
		// Fetch authority scope for this contract
		scope, err := e.scopeCache.Get(ctx, e.dnEndpoint, tx.To)
		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("failed to fetch authority scope: %v", err)
			result.ExecutionTime = time.Since(startTime)
			return result
		}

		// Verify all staged operations
		if err := outputs.VerifyAll(execResult.StagedOps, scope, e.limitTracker); err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("authority verification failed: %v", err)
			result.ExecutionTime = time.Since(startTime)
			return result
		}
	}

	// Process execution result
	if execResult.Success {
		result.Success = true
		result.GasUsed = execResult.GasUsed
		result.Receipt = execResult.Receipt

		// Calculate credits from gas used using current schedule
		if e.scheduleProvider != nil {
			schedule := e.scheduleProvider.Get()
			result.Receipt.CreditsUsed = schedule.CalculateCredits(result.GasUsed)
		}
		result.StagedOps = execResult.StagedOps
		result.Events = execResult.Events

		// Extract state changes from receipt
		if execResult.Receipt != nil {
			result.StateChanges = execResult.Receipt.StateChanges
		}
	} else {
		result.Success = false
		result.Error = execResult.Error.Error()
		result.GasUsed = execResult.GasUsed
		result.StagedOps = execResult.StagedOps
		result.Events = execResult.Events
	}

	result.ExecutionTime = time.Since(startTime)
	return result
}

// worker.run runs the worker loop
func (w *worker) run() {
	for workItem := range w.engine.workQueue {
		result := w.engine.executeSingleTransaction(context.Background(), workItem.tx)
		workItem.resultCh <- result
		close(workItem.resultCh)
	}
}

// calculateStateRoot calculates the state root hash for a block
func (e *ExecutionEngine) calculateStateRoot(results []*ExecResult) []byte {
	// Simplified state root calculation
	// In a real implementation, this would use a Merkle tree or similar
	stateRoot := make([]byte, 32)

	for i, result := range results {
		if result.Success {
			stateRoot[i%32] ^= byte(result.GasUsed)
		}
	}

	return stateRoot
}

// calculateBlockSize calculates the total size of a block
func (e *ExecutionEngine) calculateBlockSize(header *BlockHeader, txs []*Transaction, results []*ExecResult) uint64 {
	size := uint64(256) // Header size estimate

	for _, tx := range txs {
		size += tx.Size
	}

	for _, result := range results {
		size += uint64(len(result.Error)) + 64 // Result overhead
	}

	return size
}

// GetCurrentHeight returns the current block height
func (e *ExecutionEngine) GetCurrentHeight() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentHeight
}

// GetStateRoot returns the current state root
func (e *ExecutionEngine) GetStateRoot() []byte {
	e.mu.RLock()
	defer e.mu.RUnlock()

	root := make([]byte, len(e.stateRoot))
	copy(root, e.stateRoot)
	return root
}

// GetStats returns execution engine statistics
func (e *ExecutionEngine) GetStats() *ExecutionStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &ExecutionStats{
		CurrentHeight:   e.currentHeight,
		TotalExecuted:   e.totalExecuted,
		TotalGasUsed:    e.totalGasUsed,
		AverageExecTime: e.avgExecTime,
		WorkerCount:     len(e.workers),
		QueueDepth:      len(e.workQueue),
	}
}

// ExecutionStats contains execution engine statistics
type ExecutionStats struct {
	CurrentHeight   uint64        `json:"current_height"`
	TotalExecuted   uint64        `json:"total_executed"`
	TotalGasUsed    uint64        `json:"total_gas_used"`
	AverageExecTime time.Duration `json:"average_exec_time"`
	WorkerCount     int           `json:"worker_count"`
	QueueDepth      int           `json:"queue_depth"`
}

// ValidateTransaction validates a transaction before execution
func (e *ExecutionEngine) ValidateTransaction(tx *Transaction) error {
	// Check gas limit
	if tx.GasLimit == 0 {
		return fmt.Errorf("gas limit cannot be zero")
	}

	if tx.GasLimit > e.gasMeter.GasLimit() {
		return fmt.Errorf("gas limit %d exceeds maximum %d", tx.GasLimit, e.gasMeter.GasLimit())
	}

	// Check transaction size
	if tx.Size > 1024*1024 { // 1MB limit
		return fmt.Errorf("transaction size %d exceeds limit", tx.Size)
	}

	// Check signature
	if len(tx.Signature) == 0 {
		return fmt.Errorf("transaction signature missing")
	}

	return nil
}

// SimulateTransaction simulates transaction execution without applying state changes
func (e *ExecutionEngine) SimulateTransaction(ctx context.Context, tx *Transaction) (*ExecResult, error) {
	// Create temporary state store
	tempKVStore := state.NewMemoryKVStore()

	// Copy current state to temporary store (simplified)
	// In a real implementation, this would create a snapshot

	// Create temporary runtime
	tempRuntime, err := runtime.NewRuntime(&e.config.Runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to create simulation runtime: %w", err)
	}
	defer tempRuntime.Close(ctx)

	// Execute in simulation mode
	execResult, err := tempRuntime.Execute(ctx, "main", []uint64{}, tempKVStore)
	if err != nil {
		return &ExecResult{
			TxID:    tx.ID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	simResult := &ExecResult{
		TxID:      tx.ID,
		Success:   execResult.Success,
		GasUsed:   execResult.GasUsed,
		Receipt:   execResult.Receipt,
		StagedOps: execResult.StagedOps,
		Events:    execResult.Events,
	}

	// Extract state changes from receipt
	if execResult.Receipt != nil {
		simResult.StateChanges = execResult.Receipt.StateChanges
	}

	return simResult, nil
}

// Close shuts down the execution engine
func (e *ExecutionEngine) Close(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Close work queue
	close(e.workQueue)

	// Close main runtime
	if e.runtime != nil {
		if err := e.runtime.Close(ctx); err != nil {
			return fmt.Errorf("failed to close main runtime: %w", err)
		}
	}

	// Close worker runtimes
	for _, worker := range e.workers {
		if worker.runtime != nil {
			worker.runtime.Close(ctx)
		}
	}

	// Close state store
	if e.kvStore != nil {
		e.kvStore.Close()
	}

	return nil
}

// GetScheduleStats returns statistics about the pricing schedule
func (e *ExecutionEngine) GetScheduleStats() *pricing.ProviderStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.scheduleProvider != nil {
		stats := e.scheduleProvider.GetStats()
		return &stats
	}
	return nil
}

// GetCurrentSchedule returns the current pricing schedule
func (e *ExecutionEngine) GetCurrentSchedule() *pricing.Schedule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.scheduleProvider != nil {
		return e.scheduleProvider.Get()
	}
	return pricing.DefaultSchedule()
}