package sequencer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/anchors"
	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/registry/dn"
	"github.com/opendlt/accumulate-accumen/types/json"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
)

// Sequencer represents the main Accumen sequencer
type Sequencer struct {
	mu     sync.RWMutex
	config *Config

	// Core components
	mempool   *Mempool
	engine    *ExecutionEngine

	// Bridge components
	l0Client     *l0api.Client
	outputStager *outputs.OutputStager
	submitter    *outputs.OutputSubmitter
	creditMgr    *pricing.CreditManager

	// Registry client
	registryClient *dn.Client

	// Metadata and anchoring
	metadataBuilder *json.MetadataBuilder
	dnWriter        *anchors.DNWriter

	// State
	running      bool
	currentBlock *Block
	blockHeight  uint64

	// Control channels
	stopChan     chan struct{}
	blockTicker  *time.Ticker

	// Metrics
	blocksProduced uint64
	txsProcessed   uint64
	startTime      time.Time
}

// SequencerStats contains sequencer statistics
type SequencerStats struct {
	Running        bool              `json:"running"`
	BlockHeight    uint64            `json:"block_height"`
	BlocksProduced uint64            `json:"blocks_produced"`
	TxsProcessed   uint64            `json:"txs_processed"`
	Uptime         time.Duration     `json:"uptime"`
	MempoolStats   *MempoolStats     `json:"mempool_stats"`
	ExecutionStats *ExecutionStats   `json:"execution_stats"`
	BridgeStats    *outputs.SubmissionStats `json:"bridge_stats"`
}

// NewSequencer creates a new Accumen sequencer
func NewSequencer(config *Config) (*Sequencer, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Create credit manager
	creditMgr := pricing.NewCreditManager(&config.Bridge.Pricing)

	// Create mempool
	mempool := NewMempool(config.Mempool, creditMgr)

	// Create execution engine
	engine, err := NewExecutionEngine(config.Execution)
	if err != nil {
		return nil, fmt.Errorf("failed to create execution engine: %w", err)
	}

	// Create L0 API client
	l0Client, err := l0api.NewClient(&config.Bridge.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to create L0 client: %w", err)
	}

	// Create output stager
	outputStager := outputs.NewOutputStager(&config.Bridge.Stager)

	// Create output submitter
	submitter := outputs.NewOutputSubmitter(l0Client, outputStager, creditMgr, &config.Bridge.Submitter)

	// Create registry client
	registryClient, err := dn.NewClient(l0Client, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Create metadata builder
	metadataBuilder := json.NewMetadataBuilder(json.DefaultBuilderConfig())

	// Create DN writer
	dnWriter, err := anchors.NewDNWriter(anchors.DefaultDNWriterConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create DN writer: %w", err)
	}

	sequencer := &Sequencer{
		config:         config,
		mempool:        mempool,
		engine:         engine,
		l0Client:       l0Client,
		outputStager:   outputStager,
		submitter:       submitter,
		creditMgr:       creditMgr,
		registryClient:  registryClient,
		metadataBuilder: metadataBuilder,
		dnWriter:        dnWriter,
		blockHeight:     engine.GetCurrentHeight(),
		stopChan:        make(chan struct{}),
	}

	return sequencer, nil
}

// Start starts the sequencer
func (s *Sequencer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("sequencer is already running")
	}

	s.running = true
	s.startTime = time.Now()

	// Start mempool
	if err := s.mempool.Start(ctx); err != nil {
		return fmt.Errorf("failed to start mempool: %w", err)
	}

	// Start output submitter if bridge is enabled
	if s.config.Bridge.EnableBridge {
		if err := s.submitter.Start(ctx); err != nil {
			return fmt.Errorf("failed to start output submitter: %w", err)
		}

		// Start DN writer
		if err := s.dnWriter.Start(ctx); err != nil {
			return fmt.Errorf("failed to start DN writer: %w", err)
		}
	}

	// Start block production timer
	s.blockTicker = time.NewTicker(s.config.BlockTime)

	// Start main sequencer loop
	go s.mainLoop(ctx)

	// Start metrics collection if enabled
	if s.config.MetricsAddr != "" {
		go s.metricsServer(ctx)
	}

	return nil
}

// Stop stops the sequencer
func (s *Sequencer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	// Stop block timer
	if s.blockTicker != nil {
		s.blockTicker.Stop()
	}

	// Signal stop
	close(s.stopChan)

	// Stop mempool
	if err := s.mempool.Stop(); err != nil {
		return fmt.Errorf("failed to stop mempool: %w", err)
	}

	// Stop output submitter and DN writer
	if err := s.submitter.Stop(); err != nil {
		return fmt.Errorf("failed to stop output submitter: %w", err)
	}

	if s.config.Bridge.EnableBridge {
		if err := s.dnWriter.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop DN writer: %w", err)
		}
	}

	// Close execution engine
	if err := s.engine.Close(ctx); err != nil {
		return fmt.Errorf("failed to close execution engine: %w", err)
	}

	return nil
}

// mainLoop runs the main sequencer loop
func (s *Sequencer) mainLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-s.blockTicker.C:
			s.produceBlock(ctx)
		}
	}
}

// produceBlock produces a new block
func (s *Sequencer) produceBlock(ctx context.Context) {
	// Get transactions from mempool
	txs := s.mempool.GetTopTransactions(s.config.MaxTransactions)
	if len(txs) == 0 {
		// No transactions to process, skip block
		return
	}

	// Execute transactions
	block, err := s.engine.ExecuteTransactions(ctx, txs)
	if err != nil {
		fmt.Printf("Failed to execute transactions: %v\n", err)
		return
	}

	// Remove executed transactions from mempool
	for _, tx := range block.Transactions {
		s.mempool.RemoveTransaction(tx.ID)
	}

	// Update sequencer state
	s.mu.Lock()
	s.currentBlock = block
	s.blockHeight = block.Header.Height
	s.blocksProduced++
	s.txsProcessed += uint64(len(block.Transactions))
	s.mu.Unlock()

	// Submit block to L0 bridge if enabled
	if s.config.Bridge.EnableBridge {
		s.submitBlockToL0(ctx, block)
		s.writeMetadataToL0(ctx, block)
	}

	fmt.Printf("Produced block %d with %d transactions\n",
		block.Header.Height, len(block.Transactions))
}

// submitBlockToL0 submits a block to the Accumulate L0 network
func (s *Sequencer) submitBlockToL0(ctx context.Context, block *Block) {
	// Create envelope for each successful transaction result
	for i, result := range block.Results {
		if !result.Success {
			continue // Skip failed transactions
		}

		// Create a simplified envelope (in practice, this would be more complex)
		// envelope := createEnvelopeFromResult(result, block.Transactions[i])

		// Stage the output for submission
		metadata := &outputs.OutputMetadata{
			SequencerID:   block.Header.SequencerID,
			BlockHeight:   block.Header.Height,
			OutputIndex:   uint64(i),
			EstimatedCost: result.GasUsed,
		}

		// For now, create a placeholder envelope
		// In practice, this would convert the execution result to a proper Accumulate transaction
		envelope := &messaging.Envelope{} // placeholder

		staged, err := s.outputStager.StageOutput(ctx, envelope, metadata)
		if err != nil {
			fmt.Printf("Failed to stage output for tx %s: %v\n", result.TxID, err)
			continue
		}

		// Mark as ready for submission
		s.outputStager.UpdateOutputStatus(staged.ID, outputs.StageStatusReady)
	}
}

// SubmitTransaction submits a transaction to the mempool
func (s *Sequencer) SubmitTransaction(tx *Transaction) error {
	if !s.IsRunning() {
		return fmt.Errorf("sequencer is not running")
	}

	// Validate transaction with execution engine
	if err := s.engine.ValidateTransaction(tx); err != nil {
		return fmt.Errorf("transaction validation failed: %w", err)
	}

	// Add to mempool
	return s.mempool.AddTransaction(tx)
}

// SimulateTransaction simulates a transaction without adding it to mempool
func (s *Sequencer) SimulateTransaction(ctx context.Context, tx *Transaction) (*ExecResult, error) {
	if !s.IsRunning() {
		return nil, fmt.Errorf("sequencer is not running")
	}

	return s.engine.SimulateTransaction(ctx, tx)
}

// GetTransaction retrieves a transaction from the mempool
func (s *Sequencer) GetTransaction(txID string) (*Transaction, error) {
	return s.mempool.GetTransaction(txID)
}

// GetBlock returns the current block being processed
func (s *Sequencer) GetCurrentBlock() *Block {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentBlock
}

// GetBlockHeight returns the current block height
func (s *Sequencer) GetBlockHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blockHeight
}

// IsRunning returns whether the sequencer is running
func (s *Sequencer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetStats returns sequencer statistics
func (s *Sequencer) GetStats() *SequencerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var uptime time.Duration
	if s.running {
		uptime = time.Since(s.startTime)
	}

	return &SequencerStats{
		Running:        s.running,
		BlockHeight:    s.blockHeight,
		BlocksProduced: s.blocksProduced,
		TxsProcessed:   s.txsProcessed,
		Uptime:         uptime,
		MempoolStats:   s.mempool.GetStats(),
		ExecutionStats: s.engine.GetStats(),
		BridgeStats:    s.submitter.GetSubmissionStats(),
	}
}

// metricsServer runs a metrics server if enabled
func (s *Sequencer) metricsServer(ctx context.Context) {
	// Simplified metrics server
	// In practice, this would expose Prometheus metrics or similar
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			stats := s.GetStats()
			fmt.Printf("METRICS: height=%d, blocks=%d, txs=%d, uptime=%v\n",
				stats.BlockHeight, stats.BlocksProduced, stats.TxsProcessed, stats.Uptime)
		}
	}
}

// HealthCheck performs a health check on the sequencer
func (s *Sequencer) HealthCheck(ctx context.Context) error {
	if !s.IsRunning() {
		return fmt.Errorf("sequencer is not running")
	}

	// Check mempool health
	if s.mempool.Size() > s.config.Mempool.MaxSize {
		return fmt.Errorf("mempool is full")
	}

	// Check execution engine health
	execStats := s.engine.GetStats()
	if execStats.QueueDepth > execStats.WorkerCount*10 {
		return fmt.Errorf("execution queue is backed up")
	}

	// Check bridge health if enabled
	if s.config.Bridge.EnableBridge {
		if err := s.submitter.HealthCheck(ctx); err != nil {
			return fmt.Errorf("bridge health check failed: %w", err)
		}
	}

	// Check registry connectivity
	if err := s.registryClient.ValidateRegistryAccess(); err != nil {
		return fmt.Errorf("registry access failed: %w", err)
	}

	return nil
}

// UpdateConfig updates the sequencer configuration
func (s *Sequencer) UpdateConfig(newConfig *Config) error {
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update configuration
	oldConfig := s.config
	s.config = newConfig

	// Update block timing if changed
	if oldConfig.BlockTime != newConfig.BlockTime && s.blockTicker != nil {
		s.blockTicker.Stop()
		s.blockTicker = time.NewTicker(newConfig.BlockTime)
	}

	return nil
}

// GetConfig returns a copy of the current configuration
func (s *Sequencer) GetConfig() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Clone()
}

// FlushMempool removes all transactions from the mempool
func (s *Sequencer) FlushMempool() error {
	return s.mempool.Flush()
}

// GetPendingTransactions returns all pending transactions in the mempool
func (s *Sequencer) GetPendingTransactions() []*Transaction {
	return s.mempool.GetAllTransactions()
}

// GetPendingTransactionsByAccount returns pending transactions for a specific account
func (s *Sequencer) GetPendingTransactionsByAccount(account string) ([]*Transaction, error) {
	return s.mempool.GetTransactionsByAccount(account)
}

// EstimateGas estimates gas required for a transaction
func (s *Sequencer) EstimateGas(ctx context.Context, tx *Transaction) (uint64, error) {
	result, err := s.engine.SimulateTransaction(ctx, tx)
	if err != nil {
		return 0, err
	}

	return result.GasUsed, nil
}

// GetRegistryStats returns statistics about the DN registry
func (s *Sequencer) GetRegistryStats() (*dn.RegistryStats, error) {
	return s.registryClient.GetRegistryStats()
}

// writeMetadataToL0 writes transaction metadata to the Accumulate L0 network
func (s *Sequencer) writeMetadataToL0(ctx context.Context, block *Block) {
	// Process each successful transaction result
	for i, result := range block.Results {
		if !result.Success {
			continue // Skip failed transactions
		}

		// Build metadata from receipt
		metadata, err := s.metadataBuilder.BuildFromReceipt(result.Receipt)
		if err != nil {
			fmt.Printf("Failed to build metadata for tx %s: %v\n", result.TxID, err)
			continue
		}

		// Write metadata to DN with appropriate priority
		priority := int64(100) // Default priority
		if result.Receipt.Priority > 0 {
			priority = int64(result.Receipt.Priority)
		}

		// Submit metadata asynchronously
		go func(meta *json.TransactionMetadata, prio int64, txID string) {
			response, err := s.dnWriter.WriteMetadata(ctx, meta, prio)
			if err != nil {
				fmt.Printf("Failed to write metadata for tx %s: %v\n", txID, err)
				return
			}

			if response.Success {
				fmt.Printf("Metadata written for tx %s: %s\n", txID, response.TxHash)
			} else {
				fmt.Printf("Metadata write failed for tx %s: %v\n", txID, response.Error)
			}
		}(metadata, priority, result.TxID)
	}
}