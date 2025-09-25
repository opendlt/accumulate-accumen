package sequencer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/anchors"
	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
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

	// Anchoring state
	lastAnchorHeight uint64
	anchorInterval   uint64

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
	DNWriterStats  *anchors.WriterStats      `json:"dn_writer_stats"`
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
	metadataBuilder := json.NewMetadataBuilder()

	// Create DN writer with sequencer key
	dnWriterConfig := anchors.DefaultDNWriterConfig()
	dnWriterConfig.SequencerKey = config.Bridge.Client.SequencerKey
	dnWriter, err := anchors.NewDNWriter(dnWriterConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create DN writer: %w", err)
	}

	sequencer := &Sequencer{
		config:           config,
		mempool:          mempool,
		engine:           engine,
		l0Client:         l0Client,
		outputStager:     outputStager,
		submitter:        submitter,
		creditMgr:        creditMgr,
		registryClient:   registryClient,
		metadataBuilder:  metadataBuilder,
		dnWriter:         dnWriter,
		blockHeight:      engine.GetCurrentHeight(),
		lastAnchorHeight: 0,
		anchorInterval:   10, // Anchor every 10 blocks by default
		stopChan:         make(chan struct{}),
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

	// Stop output submitter
	if err := s.submitter.Stop(); err != nil {
		return fmt.Errorf("failed to stop output submitter: %w", err)
	}

	// Close DN writer
	if err := s.dnWriter.Close(); err != nil {
		return fmt.Errorf("failed to close DN writer: %w", err)
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

	// Write metadata for each successful transaction
	if s.config.Bridge.EnableBridge {
		s.writeTransactionMetadata(ctx, block)

		// Check if it's time to write an anchor
		if s.shouldWriteAnchor(block.Header.Height) {
			s.writeAnchor(ctx, block)
		}
	}

	fmt.Printf("Produced block %d with %d transactions\n",
		block.Header.Height, len(block.Transactions))
}

// writeTransactionMetadata writes metadata for each successful transaction to DN
func (s *Sequencer) writeTransactionMetadata(ctx context.Context, block *Block) {
	basePath := "acc://accumen.acme"

	// Process each transaction result
	for i, result := range block.Results {
		if !result.Success {
			continue // Skip failed transactions
		}

		// Build metadata using the new builder
		tx := block.Transactions[i]
		args := json.MetadataArgs{
			ChainID:       "accumen-devnet-1", // TODO: get from config
			BlockHeight:   block.Header.Height,
			TxIndex:       i,
			TxHash:        []byte(tx.ID), // Convert string to bytes
			AppHash:       block.StateRoot,
			Time:          block.Header.Timestamp,
			ContractAddr:  tx.From, // Use transaction sender as contract address
			Entry:         "execute", // Default entry point
			Nonce:         []byte{byte(tx.Nonce)}, // Convert nonce to bytes
			GasUsed:       result.GasUsed,
			GasScheduleID: "v1.0.0", // TODO: get from config
			CreditsL0:     result.GasUsed / 1000 * 150, // 150 credits per 1k gas
			CreditsL1:     result.GasUsed / 100,  // 1 credit per 100 gas
			CreditsTotal:  result.GasUsed / 1000 * 150 + result.GasUsed / 100,
			AcmeBurnt:     fmt.Sprintf("0.%06d", result.GasUsed/1000), // Simple conversion
			L0Outputs:     s.convertStagedOpsToL0Outputs(result.StagedOps),
			Events:        s.convertRuntimeEventsToJSONEvents(result.Events),
		}

		// Build the metadata JSON
		jsonBytes, err := s.metadataBuilder.BuildMetadata(args)
		if err != nil {
			fmt.Printf("Failed to build metadata for tx %s: %v\n", result.TxID, err)
			continue
		}

		// Write metadata to DN asynchronously
		go func(jsonData []byte, txID string) {
			txid, err := s.dnWriter.WriteMetadata(ctx, jsonData, basePath)
			if err != nil {
				fmt.Printf("Failed to write metadata for tx %s: %v\n", txID, err)
				return
			}
			fmt.Printf("Metadata written for tx %s: %s\n", txID, txid)
		}(jsonBytes, result.TxID)
	}
}

// shouldWriteAnchor determines if an anchor should be written for this block
func (s *Sequencer) shouldWriteAnchor(blockHeight uint64) bool {
	return blockHeight > 0 && blockHeight%s.anchorInterval == 0
}

// writeAnchor writes an anchor blob to DN
func (s *Sequencer) writeAnchor(ctx context.Context, block *Block) {
	basePath := "acc://accumen.acme"

	// Build anchor blob with header hash, height, and merkle placeholders
	anchorData := s.buildAnchorBlob(block)

	// Write anchor to DN asynchronously
	go func() {
		txid, err := s.dnWriter.WriteAnchor(ctx, anchorData, basePath)
		if err != nil {
			fmt.Printf("Failed to write anchor for block %d: %v\n", block.Header.Height, err)
			return
		}

		// Update last anchor height
		s.mu.Lock()
		s.lastAnchorHeight = block.Header.Height
		s.mu.Unlock()

		fmt.Printf("Anchor written for block %d: %s\n", block.Header.Height, txid)
	}()
}

// buildAnchorBlob constructs the anchor blob data
func (s *Sequencer) buildAnchorBlob(block *Block) []byte {
	// Calculate header hash
	headerData := fmt.Sprintf("%d:%s:%d:%d",
		block.Header.Height,
		hex.EncodeToString(block.Header.PrevHash),
		block.Header.Timestamp.Unix(),
		block.Header.TxCount,
	)
	headerHash := sha256.Sum256([]byte(headerData))

	// Build anchor structure
	anchor := map[string]interface{}{
		"version":     "1.0.0",
		"type":        "accumen_anchor",
		"blockHeight": block.Header.Height,
		"headerHash":  hex.EncodeToString(headerHash[:]),
		"timestamp":   block.Header.Timestamp.UTC().Format(time.RFC3339),
		"txCount":     block.Header.TxCount,
		"gasUsed":     block.Header.GasUsed,
		"gasLimit":    block.Header.GasLimit,
		"stateRoot":   hex.EncodeToString(block.StateRoot),
		// Merkle proof placeholders - in production these would be real merkle proofs
		"merkleProof": map[string]interface{}{
			"root":   hex.EncodeToString(block.StateRoot),
			"leaves": len(block.Transactions),
			"depth":  s.calculateMerkleDepth(len(block.Transactions)),
		},
		"sequencer": block.Header.SequencerID,
		"signature": nil, // Will be filled by DN writer signing
	}

	// Convert to JSON
	jsonData, err := json.NewMetadataBuilder().PrettyJSON()
	if err != nil {
		// Fallback to simple encoding
		return []byte(fmt.Sprintf(`{
			"version": "1.0.0",
			"type": "accumen_anchor",
			"blockHeight": %d,
			"headerHash": "%s",
			"timestamp": "%s",
			"txCount": %d,
			"gasUsed": %d,
			"stateRoot": "%s",
			"sequencer": "%s"
		}`,
			block.Header.Height,
			hex.EncodeToString(headerHash[:]),
			block.Header.Timestamp.UTC().Format(time.RFC3339),
			block.Header.TxCount,
			block.Header.GasUsed,
			hex.EncodeToString(block.StateRoot),
			block.Header.SequencerID,
		))
	}

	return jsonData
}

// calculateMerkleDepth calculates the depth needed for a merkle tree with n leaves
func (s *Sequencer) calculateMerkleDepth(leaves int) int {
	if leaves <= 1 {
		return 0
	}
	depth := 0
	n := leaves
	for n > 1 {
		n = (n + 1) / 2
		depth++
	}
	return depth
}

// convertStagedOpsToL0Outputs converts runtime staged operations to JSON format
func (s *Sequencer) convertStagedOpsToL0Outputs(stagedOps []*runtime.StagedOp) []map[string]any {
	outputs := make([]map[string]any, len(stagedOps))

	for i, op := range stagedOps {
		output := map[string]any{
			"type":    op.Type,
			"account": op.Account,
		}

		switch op.Type {
		case "write_data":
			output["data"] = hex.EncodeToString(op.Data)
		case "send_tokens":
			output["from"] = op.From
			output["to"] = op.To
			output["amount"] = op.Amount
		case "update_auth":
			output["data"] = hex.EncodeToString(op.Data)
		}

		outputs[i] = output
	}

	return outputs
}

// convertRuntimeEventsToJSONEvents converts runtime events to JSON events format
func (s *Sequencer) convertRuntimeEventsToJSONEvents(events []*runtime.Event) []json.EventData {
	jsonEvents := make([]json.EventData, len(events))

	for i, event := range events {
		jsonEvents[i] = json.EventData{
			Key:   event.Type,
			Value: string(event.Data),
		}
	}

	return jsonEvents
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

// GetCurrentBlock returns the current block being processed
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

	stats := &SequencerStats{
		Running:        s.running,
		BlockHeight:    s.blockHeight,
		BlocksProduced: s.blocksProduced,
		TxsProcessed:   s.txsProcessed,
		Uptime:         uptime,
		MempoolStats:   s.mempool.GetStats(),
		ExecutionStats: s.engine.GetStats(),
		BridgeStats:    s.submitter.GetSubmissionStats(),
		DNWriterStats:  &s.dnWriter.GetStats(),
	}

	return stats
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
			fmt.Printf("METRICS: height=%d, blocks=%d, txs=%d, metadata_written=%d, anchors_written=%d, uptime=%v\n",
				stats.BlockHeight, stats.BlocksProduced, stats.TxsProcessed,
				stats.DNWriterStats.MetadataWritten, stats.DNWriterStats.AnchorsWritten, stats.Uptime)
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

		// Check DN writer stats for errors
		dnStats := s.dnWriter.GetStats()
		if dnStats.Errors > dnStats.MetadataWritten/10 { // More than 10% error rate
			return fmt.Errorf("DN writer has high error rate")
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

// SetAnchorInterval updates the anchor interval
func (s *Sequencer) SetAnchorInterval(interval uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anchorInterval = interval
}

// GetLastAnchorHeight returns the height of the last anchor
func (s *Sequencer) GetLastAnchorHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastAnchorHeight
}