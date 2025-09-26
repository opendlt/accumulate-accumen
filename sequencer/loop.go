package sequencer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/anchors"
	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
	"github.com/opendlt/accumulate-accumen/internal/config"
	"github.com/opendlt/accumulate-accumen/internal/l0"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/registry/dn"
	"github.com/opendlt/accumulate-accumen/types/json"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/client/signing"
	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

// SnapshotManager handles periodic snapshots and restoration
type SnapshotManager struct {
	config      *config.Config
	kvStore     state.KVStore
	snapshotDir string
	logger      *logz.Logger
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(cfg *config.Config, kvStore state.KVStore) *SnapshotManager {
	snapshotDir := filepath.Join(cfg.Storage.Path, "snapshots")

	return &SnapshotManager{
		config:      cfg,
		kvStore:     kvStore,
		snapshotDir: snapshotDir,
		logger:      logz.New(logz.INFO, "snapshot"),
	}
}

// ShouldTakeSnapshot returns true if a snapshot should be taken at the given height
func (sm *SnapshotManager) ShouldTakeSnapshot(height uint64) bool {
	if sm.config.Snapshots.EveryN <= 0 {
		return false
	}
	return height > 0 && height%uint64(sm.config.Snapshots.EveryN) == 0
}

// TakeSnapshot creates a snapshot at the given height
func (sm *SnapshotManager) TakeSnapshot(height uint64, appHash string) error {
	if err := os.MkdirAll(sm.snapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	snapshotPath := filepath.Join(sm.snapshotDir, fmt.Sprintf("%d.snap", height))

	// Create snapshot metadata
	meta := &state.SnapshotMeta{
		Height:  height,
		Time:    time.Now().UTC(),
		AppHash: appHash,
	}

	sm.logger.Info("Taking snapshot at height %d", height)
	start := time.Now()

	// Write the snapshot
	if err := state.WriteSnapshot(snapshotPath, sm.kvStore, meta); err != nil {
		// Clean up partial snapshot on failure
		os.Remove(snapshotPath)
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	duration := time.Since(start)
	sm.logger.Info("Snapshot completed: height=%d, keys=%d, duration=%v, path=%s",
		height, meta.NumKeys, duration, snapshotPath)

	// Clean up old snapshots
	if err := sm.cleanupOldSnapshots(); err != nil {
		sm.logger.Info("Warning: failed to cleanup old snapshots: %v", err)
	}

	return nil
}

// RestoreFromSnapshot attempts to restore state from the latest snapshot
func (sm *SnapshotManager) RestoreFromSnapshot() (*state.SnapshotMeta, error) {
	// Check if KV store is empty first
	isEmpty, err := sm.isKVStoreEmpty()
	if err != nil {
		return nil, fmt.Errorf("failed to check if KV store is empty: %w", err)
	}

	if !isEmpty {
		sm.logger.Info("KV store is not empty, skipping snapshot restore")
		return nil, nil
	}

	// Find the latest snapshot
	latestSnapshot, err := sm.findLatestSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to find latest snapshot: %w", err)
	}

	if latestSnapshot == "" {
		sm.logger.Info("No snapshots found, starting with empty state")
		return nil, nil
	}

	sm.logger.Info("Restoring from snapshot: %s", latestSnapshot)
	start := time.Now()

	// Read the snapshot
	meta, iterator, err := state.ReadSnapshot(latestSnapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}
	defer iterator.Close()

	// Restore key-value pairs
	var restoredKeys uint64
	for {
		key, value, err := iterator.Next()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("failed to read snapshot entry: %w", err)
		}

		if err := sm.kvStore.Set(key, value); err != nil {
			return nil, fmt.Errorf("failed to restore key-value pair: %w", err)
		}

		restoredKeys++
	}

	duration := time.Since(start)
	sm.logger.Info("Snapshot restored: height=%d, keys=%d, duration=%v",
		meta.Height, restoredKeys, duration)

	return meta, nil
}

// GenerateAppHash generates an application hash for the current state
func (sm *SnapshotManager) GenerateAppHash() (string, error) {
	hasher := sha256.New()

	// Iterate over all key-value pairs in sorted order for deterministic hash
	var keys [][]byte
	err := sm.kvStore.Iterate(func(key, value []byte) error {
		keys = append(keys, append([]byte(nil), key...)) // Copy key
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to collect keys: %w", err)
	}

	// Sort keys for deterministic hashing
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})

	// Hash each key-value pair
	for _, key := range keys {
		value, err := sm.kvStore.Get(key)
		if err != nil {
			return "", fmt.Errorf("failed to get value for key %s: %w", string(key), err)
		}

		hasher.Write(key)
		hasher.Write(value)
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// isKVStoreEmpty checks if the KV store has any data
func (sm *SnapshotManager) isKVStoreEmpty() (bool, error) {
	isEmpty := true
	err := sm.kvStore.Iterate(func(key, value []byte) error {
		isEmpty = false
		return fmt.Errorf("stop") // Stop iteration immediately
	})

	// If we got an error but it's our stop signal, the store is not empty
	if err != nil && err.Error() == "stop" {
		return false, nil
	}

	return isEmpty, err
}

// findLatestSnapshot finds the path to the latest snapshot file
func (sm *SnapshotManager) findLatestSnapshot() (string, error) {
	if _, err := os.Stat(sm.snapshotDir); os.IsNotExist(err) {
		return "", nil
	}

	entries, err := os.ReadDir(sm.snapshotDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot directory: %w", err)
	}

	var snapshots []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".snap") {
			snapshots = append(snapshots, entry.Name())
		}
	}

	if len(snapshots) == 0 {
		return "", nil
	}

	// Sort by height (filename is <height>.snap)
	sort.Slice(snapshots, func(i, j int) bool {
		// Extract height from filename
		heightI := extractHeightFromFilename(snapshots[i])
		heightJ := extractHeightFromFilename(snapshots[j])
		return heightI > heightJ // Descending order (latest first)
	})

	return filepath.Join(sm.snapshotDir, snapshots[0]), nil
}

// cleanupOldSnapshots removes old snapshots beyond the retention limit
func (sm *SnapshotManager) cleanupOldSnapshots() error {
	if sm.config.Snapshots.Retain <= 0 {
		return nil // No cleanup if retain is 0 or negative
	}

	entries, err := os.ReadDir(sm.snapshotDir)
	if err != nil {
		return fmt.Errorf("failed to read snapshot directory: %w", err)
	}

	var snapshots []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".snap") {
			snapshots = append(snapshots, entry.Name())
		}
	}

	if len(snapshots) <= sm.config.Snapshots.Retain {
		return nil // No cleanup needed
	}

	// Sort by height (descending)
	sort.Slice(snapshots, func(i, j int) bool {
		heightI := extractHeightFromFilename(snapshots[i])
		heightJ := extractHeightFromFilename(snapshots[j])
		return heightI > heightJ
	})

	// Remove old snapshots beyond retention limit
	for i := sm.config.Snapshots.Retain; i < len(snapshots); i++ {
		snapshotPath := filepath.Join(sm.snapshotDir, snapshots[i])
		if err := os.Remove(snapshotPath); err != nil {
			sm.logger.Info("Warning: failed to remove old snapshot %s: %v", snapshotPath, err)
		} else {
			sm.logger.Debug("Removed old snapshot: %s", snapshotPath)
		}
	}

	return nil
}

// extractHeightFromFilename extracts height from snapshot filename
func extractHeightFromFilename(filename string) uint64 {
	// Filename format is "<height>.snap"
	name := strings.TrimSuffix(filename, ".snap")
	var height uint64
	fmt.Sscanf(name, "%d", &height)
	return height
}

// SubmissionLoop manages the envelope submission pipeline
type SubmissionLoop struct {
	config       *config.Config
	outbox       *outputs.Outbox
	client       *v3.Client
	signer       signing.Signer
	logger       *logz.Logger
	submitterCfg *config.Submitter
}

// NewSubmissionLoop creates a new submission loop
func NewSubmissionLoop(cfg *config.Config, outbox *outputs.Outbox, client *v3.Client, signer signing.Signer) *SubmissionLoop {
	return &SubmissionLoop{
		config:       cfg,
		outbox:       outbox,
		client:       client,
		signer:       signer,
		logger:       logz.New(logz.INFO, "submission-loop"),
		submitterCfg: &cfg.Submitter,
	}
}

// Sequencer represents the main Accumen sequencer
type Sequencer struct {
	mu     sync.RWMutex
	config *Config

	// Core components
	mempool   *Mempool
	engine    *ExecutionEngine

	// Bridge components
	l0Client       *l0api.Client
	outputStager   *outputs.OutputStager
	submitter      *outputs.OutputSubmitter
	creditMgr      *pricing.CreditManager
	creditsManager *pricing.Manager // New credits manager for automatic top-ups
	submissionLoop *SubmissionLoop
	outbox         *outputs.Outbox

	// Registry client
	registryClient *dn.Client

	// Events manager for transaction confirmations
	eventsManager *l0api.Manager

	// Metadata and anchoring
	metadataBuilder *json.MetadataBuilder
	dnWriter        *anchors.DNWriter

	// Snapshot management
	snapshotManager *SnapshotManager

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

	// Create L0 round robin for endpoint management
	rrConfig := &l0.Config{
		Source:              l0.SourceStatic, // Default to static endpoints
		StaticURLs:          []string{config.Bridge.Client.Endpoint}, // Use bridge client endpoint
		WSPath:              "/ws",
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		MaxFailures:         3,
		BackoffDuration:     5 * time.Minute,
	}
	l0RR := l0.NewRoundRobin(rrConfig)
	if err := l0RR.Start(); err != nil {
		return nil, fmt.Errorf("failed to start L0 round robin: %w", err)
	}

	// Create pricing schedule provider
	scheduleProvider := pricing.NewScheduleProvider(
		func() (*pricing.Schedule, error) {
			// Use default schedule for now - in production this would load from DN
			return pricing.DefaultSchedule(), nil
		},
		5*time.Minute, // Refresh every 5 minutes
	)

	// Create L0 API querier
	l0Querier := l0api.NewQuerier(l0Client, nil)

	// Create credits manager for automatic top-ups
	creditsManager := pricing.NewManager(scheduleProvider, l0Querier, l0RR)

	// Create events manager if events are enabled
	var eventsManager *l0api.Manager
	if config.Bridge.EnableBridge { // Use bridge enabled flag instead
		managerConfig := &l0api.ManagerConfig{
			ServerURL:    config.Bridge.Client.Endpoint,
			ReconnectMin: 1 * time.Second,  // Default values
			ReconnectMax: 60 * time.Second,
		}
		eventsManager = l0api.NewManager(managerConfig)
	}

	sequencer := &Sequencer{
		config:           config,
		mempool:          mempool,
		engine:           engine,
		l0Client:         l0Client,
		outputStager:     outputStager,
		submitter:        submitter,
		creditMgr:        creditMgr,
		creditsManager:   creditsManager,
		registryClient:   registryClient,
		eventsManager:    eventsManager,
		metadataBuilder:  metadataBuilder,
		dnWriter:         dnWriter,
		blockHeight:      engine.GetCurrentHeight(),
		lastAnchorHeight: 0,
		anchorInterval:   10, // Anchor every 10 blocks by default
		stopChan:         make(chan struct{}),
	}

	return sequencer, nil
}

// SetSnapshotManager sets the snapshot manager for the sequencer
func (s *Sequencer) SetSnapshotManager(sm *SnapshotManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotManager = sm
}

// Start starts the sequencer
func (s *Sequencer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("sequencer is already running")
	}

	// Attempt to restore from snapshot if snapshot manager is configured
	if s.snapshotManager != nil {
		restoredMeta, err := s.snapshotManager.RestoreFromSnapshot()
		if err != nil {
			return fmt.Errorf("failed to restore from snapshot: %w", err)
		}
		if restoredMeta != nil {
			s.blockHeight = restoredMeta.Height
			fmt.Printf("Restored sequencer state from snapshot at height %d\n", restoredMeta.Height)
		}
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

	// Start events manager if enabled
	if s.eventsManager != nil {
		if err := s.eventsManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start events manager: %w", err)
		}
	}

	// Start block production timer
	s.blockTicker = time.NewTicker(s.config.BlockTime)

	// Start main sequencer loop
	go s.mainLoop(ctx)

	// Start credits management loop
	if s.creditsManager != nil {
		go s.creditsManagementLoop(ctx)
	}

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

	// Stop events manager
	if s.eventsManager != nil {
		s.eventsManager.Stop()
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

	// Check if we should take a snapshot
	if s.snapshotManager != nil && s.snapshotManager.ShouldTakeSnapshot(block.Header.Height) {
		appHash, err := s.snapshotManager.GenerateAppHash()
		if err != nil {
			fmt.Printf("Failed to generate app hash for snapshot: %v\n", err)
		} else {
			if err := s.snapshotManager.TakeSnapshot(block.Header.Height, appHash); err != nil {
				fmt.Printf("Failed to take snapshot at height %d: %v\n", block.Header.Height, err)
			}
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

			// Subscribe to confirmation if events manager is available
			if s.eventsManager != nil {
				statusCh, cancel := s.eventsManager.SubscribeTx(txid)

				// Monitor confirmation status in a separate goroutine
				go func() {
					defer cancel()

					// Use a timeout to avoid waiting indefinitely
					timeout := time.After(30 * time.Second)

					for {
						select {
						case status := <-statusCh:
							switch status.Status {
							case "confirmed":
								fmt.Printf("Transaction %s confirmed: %s\n", txID, txid)
								return
							case "failed":
								fmt.Printf("Transaction %s failed: %s (error: %s)\n", txID, txid, status.Error)
								return
							}
						case <-timeout:
							fmt.Printf("Transaction %s confirmation timeout: %s\n", txID, txid)
							return
						}
					}
				}()
			}
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

		// Subscribe to confirmation if events manager is available
		if s.eventsManager != nil {
			statusCh, cancel := s.eventsManager.SubscribeTx(txid)

			// Monitor confirmation status in a separate goroutine
			go func() {
				defer cancel()

				// Use a timeout to avoid waiting indefinitely
				timeout := time.After(60 * time.Second) // Longer timeout for anchors

				for {
					select {
					case status := <-statusCh:
						switch status.Status {
						case "confirmed":
							fmt.Printf("Anchor for block %d confirmed: %s\n", block.Header.Height, txid)
							return
						case "failed":
							fmt.Printf("Anchor for block %d failed: %s (error: %s)\n", block.Header.Height, txid, status.Error)
							return
						}
					case <-timeout:
						fmt.Printf("Anchor for block %d confirmation timeout: %s\n", block.Header.Height, txid)
						return
					}
				}
			}()
		}
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

// GetExecutionEngine returns the execution engine for query operations
func (s *Sequencer) GetExecutionEngine() *ExecutionEngine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.engine
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

// Start begins the submission loop
func (sl *SubmissionLoop) Start(ctx context.Context) error {
	sl.logger.Info("Starting submission loop with batch size %d", sl.submitterCfg.BatchSize)

	// Start the outbox worker goroutine
	go sl.OutboxWorker(ctx)

	return nil
}

// EnqueueEnvelope adds an envelope to the outbox for submission
func (sl *SubmissionLoop) EnqueueEnvelope(env *build.EnvelopeBuilder) (string, error) {
	id, err := sl.outbox.Enqueue(env)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue envelope: %w", err)
	}

	sl.logger.Debug("Enqueued envelope with ID: %s", id)
	return id, nil
}

// EnqueueEnvelopes adds multiple envelopes to the outbox for submission
func (sl *SubmissionLoop) EnqueueEnvelopes(envs []*build.EnvelopeBuilder) ([]string, error) {
	ids := make([]string, len(envs))

	for i, env := range envs {
		id, err := sl.outbox.Enqueue(env)
		if err != nil {
			return nil, fmt.Errorf("failed to enqueue envelope %d: %w", i, err)
		}
		ids[i] = id
	}

	sl.logger.Debug("Enqueued %d envelopes", len(envs))
	return ids, nil
}

// OutboxWorker continuously processes items from the outbox
func (sl *SubmissionLoop) OutboxWorker(ctx context.Context) {
	sl.logger.Info("Outbox worker started")
	defer sl.logger.Info("Outbox worker stopped")

	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sl.processBatch(ctx); err != nil {
				sl.logger.Info("Error processing batch: %v", err)
			}
		}
	}
}

// processBatch processes a batch of ready items from the outbox
func (sl *SubmissionLoop) processBatch(ctx context.Context) error {
	// Get ready items from outbox
	items, err := sl.outbox.DequeueReady(time.Now(), sl.submitterCfg.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to dequeue ready items: %w", err)
	}

	if len(items) == 0 {
		return nil // Nothing to process
	}

	sl.logger.Debug("Processing batch of %d items", len(items))

	// Process each item
	for _, item := range items {
		if err := sl.processItem(ctx, item); err != nil {
			sl.logger.Info("Failed to process item %s: %v", item.ID, err)
			// Item will be retried with backoff
		}
	}

	return nil
}

// processItem processes a single outbox item
func (sl *SubmissionLoop) processItem(ctx context.Context, item *outputs.OutboxItem) error {
	sl.logger.Debug("Processing item %s (attempt %d): %s", item.ID, item.Tries+1, item.OpSummary)

	// Deserialize envelope
	envelope, err := deserializeEnvelope(item.EnvBytes)
	if err != nil {
		// Permanent error - remove from queue
		sl.outbox.MarkDone(item.ID)
		return fmt.Errorf("failed to deserialize envelope: %w", err)
	}

	// Sign envelope if signer is available
	if sl.signer != nil {
		signedEnv, err := sl.signEnvelope(envelope)
		if err != nil {
			// Retry with backoff
			backoff := sl.calculateBackoff(item.Tries)
			sl.outbox.MarkRetry(item.ID, backoff)
			return fmt.Errorf("failed to sign envelope: %w", err)
		}
		envelope = signedEnv
	}

	// Submit envelope to L0 API
	if err := sl.submitEnvelope(ctx, envelope); err != nil {
		// Check if this is a permanent error
		if isPermanentError(err) {
			sl.logger.Info("Permanent error for item %s, removing from queue: %v", item.ID, err)
			sl.outbox.MarkDone(item.ID)
			return err
		}

		// Retry with backoff
		backoff := sl.calculateBackoff(item.Tries)
		sl.logger.Debug("Retrying item %s in %v (attempt %d)", item.ID, backoff, item.Tries+1)
		sl.outbox.MarkRetry(item.ID, backoff)
		return err
	}

	// Success - mark as done
	sl.logger.Debug("Successfully submitted item %s", item.ID)
	sl.outbox.MarkDone(item.ID)
	return nil
}

// signEnvelope signs an envelope using the configured signer
func (sl *SubmissionLoop) signEnvelope(envelope interface{}) (interface{}, error) {
	// TODO: Implement envelope signing with the signer
	// This is a placeholder - actual implementation would depend on
	// the envelope format and signing requirements
	sl.logger.Debug("Signing envelope (placeholder implementation)")
	return envelope, nil
}

// submitEnvelope submits an envelope to the L0 API client
func (sl *SubmissionLoop) submitEnvelope(ctx context.Context, envelope interface{}) error {
	// TODO: Implement envelope submission via L0 API client
	// This is a placeholder - actual implementation would depend on
	// the client API and envelope format
	sl.logger.Debug("Submitting envelope to L0 API (placeholder implementation)")

	// Simulate submission delay
	select {
	case <-time.After(10 * time.Millisecond):
		return nil // Success
	case <-ctx.Done():
		return ctx.Err()
	}
}

// calculateBackoff calculates exponential backoff duration
func (sl *SubmissionLoop) calculateBackoff(tries int) time.Duration {
	minBackoff := sl.submitterCfg.GetBackoffMinDuration()
	maxBackoff := sl.submitterCfg.GetBackoffMaxDuration()

	// Exponential backoff: min * 2^tries, capped at max
	backoff := time.Duration(float64(minBackoff) * math.Pow(2, float64(tries)))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}

// isPermanentError determines if an error is permanent and should not be retried
func isPermanentError(err error) bool {
	// TODO: Implement proper permanent error detection
	// This would check for specific error types that indicate
	// the request should not be retried (e.g., malformed data,
	// authentication errors, etc.)
	return false
}

// deserializeEnvelope deserializes envelope bytes
func deserializeEnvelope(data []byte) (interface{}, error) {
	// TODO: Implement proper envelope deserialization
	// This is a placeholder - actual implementation would depend on
	// the envelope format used
	if len(data) == 0 {
		return nil, fmt.Errorf("empty envelope data")
	}
	return data, nil // Placeholder return
}

// SubmissionStats contains statistics about the submission loop
type SubmissionStats struct {
	outputs.OutboxStats
	BatchSize  int    `json:"batch_size"`
	BackoffMin string `json:"backoff_min"`
	BackoffMax string `json:"backoff_max"`
}

// GetSubmissionStats returns statistics about the submission loop
func (sl *SubmissionLoop) GetSubmissionStats() (*SubmissionStats, error) {
	outboxStats, err := sl.outbox.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get outbox stats: %w", err)
	}

	return &SubmissionStats{
		OutboxStats: *outboxStats,
		BatchSize:   sl.submitterCfg.BatchSize,
		BackoffMin:  sl.submitterCfg.BackoffMin,
		BackoffMax:  sl.submitterCfg.BackoffMax,
	}, nil
}

// Cleanup performs maintenance on the submission pipeline
func (sl *SubmissionLoop) Cleanup() error {
	// Clean up old outbox items (older than 24 hours with many retries)
	return sl.outbox.Cleanup(24 * time.Hour)
}

// Stop gracefully stops the submission loop
func (sl *SubmissionLoop) Stop(ctx context.Context) error {
	sl.logger.Info("Stopping submission loop")

	// Perform final cleanup
	if err := sl.Cleanup(); err != nil {
		sl.logger.Info("Warning: Failed to cleanup during stop: %v", err)
	}

	return nil
}

// creditsManagementLoop runs periodic credits checks and top-ups for the controlling key page
func (s *Sequencer) creditsManagementLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second) // Check credits every minute
	defer ticker.Stop()

	fmt.Printf("Credits management loop started\n")

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			if err := s.ensureCredits(ctx); err != nil {
				fmt.Printf("Credits management error: %v\n", err)
			}
		}
	}
}

// ensureCredits checks and tops up credits for the controlling key page
func (s *Sequencer) ensureCredits(ctx context.Context) error {
	// For now, use hardcoded values since we don't have access to internal config
	// In a real implementation, these would come from configuration
	minBuffer := uint64(1000)   // Keep at least 1000 credits
	target := uint64(5000)      // Top up to 5000 credits
	fundingToken := "acc://acme.acme/tokens" // Default ACME token URL

	// Get the sequencer's key page URL from the DN writer configuration
	// This is a simplified approach - in production, this would be properly configured
	sequencerKeyPage := "acc://sequencer.acme/book/1" // Placeholder

	// Parse URLs
	keyPageURL, err := accutil.ParseURL(sequencerKeyPage)
	if err != nil {
		return fmt.Errorf("invalid key page URL: %w", err)
	}

	fundingTokenURL, err := accutil.ParseURL(fundingToken)
	if err != nil {
		return fmt.Errorf("invalid funding token URL: %w", err)
	}

	// Check if key page needs credits
	envelope, built, err := s.creditsManager.EnsureCredits(ctx, keyPageURL, target, fundingTokenURL)
	if err != nil {
		return fmt.Errorf("failed to check/ensure credits: %w", err)
	}

	// If a top-up transaction was built, submit it
	if built && envelope != nil {
		fmt.Printf("Submitting credits top-up transaction for key page %s\n", keyPageURL)

		// Submit the credits transaction via the submission loop if available
		if s.submissionLoop != nil {
			if _, err := s.submissionLoop.EnqueueEnvelope(envelope); err != nil {
				return fmt.Errorf("failed to enqueue credits top-up transaction: %w", err)
			}
			fmt.Printf("Credits top-up transaction enqueued successfully\n")
		} else {
			fmt.Printf("Warning: No submission loop available, credits top-up transaction not submitted\n")
		}
	}

	return nil
}