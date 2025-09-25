package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

// Indexer scans DN metadata and reconstructs L1 state
type Indexer struct {
	mu     sync.RWMutex
	config *Config
	client *v3.Client
	kvStore state.KVStore
	logger *logz.Logger

	// State tracking
	running        bool
	checkpoint     *Checkpoint
	stats          *Stats
	stopChan       chan struct{}
	scanTicker     *time.Ticker

	// Processing state
	lastProcessedKey string
	currentHeight    uint64
}

// Config defines configuration for the indexer
type Config struct {
	APIClient     *v3.Client
	KVStore       state.KVStore
	MetadataPath  string        // DN path for transaction metadata
	AnchorsPath   string        // DN path for anchors
	ScanInterval  time.Duration // How often to scan for new entries
	BatchSize     int           // Number of entries to process per batch
	StartFromKey  string        // Key to start scanning from (empty = beginning)
	MaxRetries    int           // Maximum retry attempts for failed operations
	RetryDelay    time.Duration // Delay between retries
}

// Checkpoint represents the current indexing position
type Checkpoint struct {
	Height       uint64    `json:"height"`
	LastKey      string    `json:"last_key"`
	LastUpdated  time.Time `json:"last_updated"`
	EntriesCount uint64    `json:"entries_count"`
}

// Stats contains indexing statistics
type Stats struct {
	mu               sync.RWMutex
	Running          bool          `json:"running"`
	CurrentHeight    uint64        `json:"current_height"`
	EntriesProcessed uint64        `json:"entries_processed"`
	Errors           uint64        `json:"errors"`
	Retries          uint64        `json:"retries"`
	LastScan         time.Time     `json:"last_scan"`
	LastEntry        string        `json:"last_entry"`
	ScanDuration     time.Duration `json:"scan_duration"`
	StartTime        time.Time     `json:"start_time"`
}

// MetadataEntry represents a transaction metadata entry from DN
type MetadataEntry struct {
	ChainID       string                 `json:"chainId"`
	BlockHeight   uint64                 `json:"blockHeight"`
	TxIndex       int                    `json:"txIndex"`
	TxHash        string                 `json:"txHash"`
	Time          time.Time              `json:"time"`
	ContractAddr  string                 `json:"contractAddr"`
	Entry         string                 `json:"entry"`
	GasUsed       uint64                 `json:"gasUsed"`
	CreditsL0     uint64                 `json:"creditsL0"`
	CreditsL1     uint64                 `json:"creditsL1"`
	L0Outputs     []map[string]any       `json:"l0Outputs"`
	Events        []map[string]any       `json:"events"`
}

// NewIndexer creates a new DN indexer
func NewIndexer(config *Config) (*Indexer, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.APIClient == nil {
		return nil, fmt.Errorf("API client is required")
	}

	if config.KVStore == nil {
		return nil, fmt.Errorf("KV store is required")
	}

	if config.ScanInterval == 0 {
		config.ScanInterval = 30 * time.Second
	}

	if config.BatchSize == 0 {
		config.BatchSize = 100
	}

	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}

	indexer := &Indexer{
		config:   config,
		client:   config.APIClient,
		kvStore:  config.KVStore,
		logger:   logz.New(logz.INFO, "indexer"),
		stats:    &Stats{StartTime: time.Now()},
		stopChan: make(chan struct{}),
	}

	// Load checkpoint from KV store
	if err := indexer.loadCheckpoint(); err != nil {
		indexer.logger.Info("Warning: Failed to load checkpoint: %v", err)
		// Initialize with defaults
		indexer.checkpoint = &Checkpoint{
			Height:      0,
			LastKey:     config.StartFromKey,
			LastUpdated: time.Now(),
		}
	}

	indexer.lastProcessedKey = indexer.checkpoint.LastKey
	indexer.currentHeight = indexer.checkpoint.Height

	return indexer, nil
}

// Start begins the indexing process
func (idx *Indexer) Start(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.running {
		return fmt.Errorf("indexer is already running")
	}

	idx.running = true
	idx.stats.Running = true
	idx.scanTicker = time.NewTicker(idx.config.ScanInterval)

	idx.logger.Info("Starting DN indexer")
	idx.logger.Info("Metadata path: %s", idx.config.MetadataPath)
	idx.logger.Info("Scan interval: %v", idx.config.ScanInterval)
	idx.logger.Info("Batch size: %d", idx.config.BatchSize)
	idx.logger.Info("Starting from key: %s", idx.lastProcessedKey)

	// Start main indexing loop
	go idx.indexingLoop(ctx)

	return nil
}

// Stop stops the indexing process
func (idx *Indexer) Stop(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if !idx.running {
		return nil
	}

	idx.logger.Info("Stopping DN indexer")
	idx.running = false
	idx.stats.Running = false

	// Stop ticker
	if idx.scanTicker != nil {
		idx.scanTicker.Stop()
	}

	// Signal stop
	close(idx.stopChan)

	// Save final checkpoint
	if err := idx.saveCheckpoint(); err != nil {
		idx.logger.Info("Warning: Failed to save final checkpoint: %v", err)
	}

	idx.logger.Info("DN indexer stopped")
	return nil
}

// indexingLoop runs the main indexing loop
func (idx *Indexer) indexingLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-idx.stopChan:
			return
		case <-idx.scanTicker.C:
			idx.performScan(ctx)
		}
	}
}

// performScan scans for new DN entries and processes them
func (idx *Indexer) performScan(ctx context.Context) {
	scanStart := time.Now()
	idx.logger.Debug("Starting DN scan from key: %s", idx.lastProcessedKey)

	// Update last scan time
	idx.stats.mu.Lock()
	idx.stats.LastScan = scanStart
	idx.stats.mu.Unlock()

	// Query DN for new entries
	entries, nextKey, err := idx.queryDNEntries(ctx, idx.lastProcessedKey, idx.config.BatchSize)
	if err != nil {
		idx.recordError()
		idx.logger.Info("Failed to query DN entries: %v", err)
		return
	}

	if len(entries) == 0 {
		idx.logger.Debug("No new entries found")
		idx.stats.mu.Lock()
		idx.stats.ScanDuration = time.Since(scanStart)
		idx.stats.mu.Unlock()
		return
	}

	idx.logger.Debug("Found %d new entries", len(entries))

	// Process each entry
	for _, entry := range entries {
		if err := idx.processEntry(ctx, entry); err != nil {
			idx.recordError()
			idx.logger.Info("Failed to process entry %s: %v", entry.TxHash, err)
			continue
		}

		idx.recordEntryProcessed(entry.TxHash)
	}

	// Update position
	idx.lastProcessedKey = nextKey
	idx.checkpoint.LastKey = nextKey
	idx.checkpoint.EntriesCount += uint64(len(entries))
	idx.checkpoint.LastUpdated = time.Now()

	// Save checkpoint periodically
	if idx.checkpoint.EntriesCount%100 == 0 {
		if err := idx.saveCheckpoint(); err != nil {
			idx.logger.Info("Warning: Failed to save checkpoint: %v", err)
		}
	}

	idx.stats.mu.Lock()
	idx.stats.ScanDuration = time.Since(scanStart)
	idx.stats.mu.Unlock()

	idx.logger.Debug("Scan completed in %v, processed %d entries", time.Since(scanStart), len(entries))
}

// queryDNEntries queries the DN for metadata entries
func (idx *Indexer) queryDNEntries(ctx context.Context, fromKey string, limit int) ([]*MetadataEntry, string, error) {
	// Parse the metadata path URL
	metadataURL, err := url.Parse(idx.config.MetadataPath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid metadata path: %w", err)
	}

	// TODO: Implement actual DN querying using v3 client
	// This is a placeholder implementation
	// In practice, this would use the Querier to scan data account ranges

	// Simulate querying DN entries with retry logic
	var entries []*MetadataEntry
	var nextKey string

	for attempt := 0; attempt <= idx.config.MaxRetries; attempt++ {
		if attempt > 0 {
			idx.recordRetry()
			time.Sleep(idx.config.RetryDelay * time.Duration(attempt))
		}

		// Placeholder: In real implementation, this would:
		// 1. Use v3 client to query data account entries
		// 2. Parse the directory structure under MetadataPath
		// 3. Read and decode JSON metadata entries
		// 4. Return entries in chronological order

		entries, nextKey, err = idx.simulateQueryDN(ctx, fromKey, limit)
		if err == nil {
			break
		}

		idx.logger.Debug("Query attempt %d failed: %v", attempt+1, err)
	}

	return entries, nextKey, err
}

// simulateQueryDN simulates querying DN entries (placeholder)
func (idx *Indexer) simulateQueryDN(ctx context.Context, fromKey string, limit int) ([]*MetadataEntry, string, error) {
	// Placeholder implementation - returns empty results
	// In real implementation, this would:
	// - Query the DN data account using the v3 client
	// - Parse directory entries under the metadata path
	// - Deserialize JSON metadata
	// - Return sorted entries

	idx.logger.Debug("Simulating DN query from key '%s' with limit %d", fromKey, limit)
	return []*MetadataEntry{}, fromKey, nil
}

// processEntry processes a single metadata entry and applies it to KV store
func (idx *Indexer) processEntry(ctx context.Context, entry *MetadataEntry) error {
	idx.logger.Debug("Processing entry: height=%d, tx=%s, contract=%s",
		entry.BlockHeight, entry.TxHash, entry.ContractAddr)

	// Apply the entry to reconstruct contract state
	if err := idx.applyEntry(entry); err != nil {
		return fmt.Errorf("failed to apply entry: %w", err)
	}

	// Update height if this entry is newer
	if entry.BlockHeight > idx.currentHeight {
		idx.currentHeight = entry.BlockHeight
		idx.checkpoint.Height = entry.BlockHeight
	}

	return nil
}

// applyEntry applies a metadata entry to reconstruct contract state
func (idx *Indexer) applyEntry(entry *MetadataEntry) error {
	// Reconstruct state by applying events
	for _, event := range entry.Events {
		if err := idx.applyEvent(entry.ContractAddr, event); err != nil {
			return fmt.Errorf("failed to apply event: %w", err)
		}
	}

	// Store transaction metadata for queries
	txKey := fmt.Sprintf("tx:%s", entry.TxHash)
	txData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	if err := idx.kvStore.Set([]byte(txKey), txData); err != nil {
		return fmt.Errorf("failed to store transaction data: %w", err)
	}

	return nil
}

// applyEvent applies a single event to update contract state
func (idx *Indexer) applyEvent(contractAddr string, event map[string]any) error {
	// Extract event data
	eventType, ok := event["key"].(string)
	if !ok {
		return fmt.Errorf("event missing type key")
	}

	eventValue, ok := event["value"].(string)
	if !ok {
		return fmt.Errorf("event missing value")
	}

	// Simulate state reconstruction by storing events as key-value pairs
	// In a real implementation, this would apply proper state transitions
	// based on the contract logic and event types
	stateKey := fmt.Sprintf("state:%s:%s", contractAddr, eventType)

	if err := idx.kvStore.Set([]byte(stateKey), []byte(eventValue)); err != nil {
		return fmt.Errorf("failed to update contract state: %w", err)
	}

	idx.logger.Debug("Applied event: contract=%s, type=%s, value=%s", contractAddr, eventType, eventValue)
	return nil
}

// loadCheckpoint loads the indexing checkpoint from KV store
func (idx *Indexer) loadCheckpoint() error {
	checkpointKey := []byte("indexer_checkpoint")
	data, err := idx.kvStore.Get(checkpointKey)
	if err != nil {
		return fmt.Errorf("failed to load checkpoint: %w", err)
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	idx.checkpoint = &checkpoint
	idx.logger.Info("Loaded checkpoint: height=%d, last_key=%s, entries=%d",
		checkpoint.Height, checkpoint.LastKey, checkpoint.EntriesCount)
	return nil
}

// saveCheckpoint saves the current indexing checkpoint to KV store
func (idx *Indexer) saveCheckpoint() error {
	checkpointKey := []byte("indexer_checkpoint")
	data, err := json.Marshal(idx.checkpoint)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	if err := idx.kvStore.Set(checkpointKey, data); err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	idx.logger.Debug("Saved checkpoint: height=%d, last_key=%s, entries=%d",
		idx.checkpoint.Height, idx.checkpoint.LastKey, idx.checkpoint.EntriesCount)
	return nil
}

// GetStats returns indexer statistics
func (idx *Indexer) GetStats() *Stats {
	idx.stats.mu.RLock()
	defer idx.stats.mu.RUnlock()

	// Create a copy to avoid concurrent access issues
	statsCopy := *idx.stats
	statsCopy.CurrentHeight = idx.currentHeight
	return &statsCopy
}

// GetCheckpoint returns the current checkpoint
func (idx *Indexer) GetCheckpoint() *Checkpoint {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.checkpoint == nil {
		return nil
	}

	// Return a copy
	checkpointCopy := *idx.checkpoint
	return &checkpointCopy
}

// recordError increments the error counter
func (idx *Indexer) recordError() {
	idx.stats.mu.Lock()
	defer idx.stats.mu.Unlock()
	idx.stats.Errors++
}

// recordRetry increments the retry counter
func (idx *Indexer) recordRetry() {
	idx.stats.mu.Lock()
	defer idx.stats.mu.Unlock()
	idx.stats.Retries++
}

// recordEntryProcessed records a successfully processed entry
func (idx *Indexer) recordEntryProcessed(txHash string) {
	idx.stats.mu.Lock()
	defer idx.stats.mu.Unlock()
	idx.stats.EntriesProcessed++
	idx.stats.LastEntry = txHash
}

// QueryTransaction queries for a specific transaction by hash
func (idx *Indexer) QueryTransaction(txHash string) (*MetadataEntry, error) {
	txKey := fmt.Sprintf("tx:%s", txHash)
	data, err := idx.kvStore.Get([]byte(txKey))
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	var entry MetadataEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	return &entry, nil
}

// QueryContractState queries contract state by address and key
func (idx *Indexer) QueryContractState(contractAddr, stateKey string) ([]byte, error) {
	fullKey := fmt.Sprintf("state:%s:%s", contractAddr, stateKey)
	value, err := idx.kvStore.Get([]byte(fullKey))
	if err != nil {
		return nil, fmt.Errorf("state not found: %w", err)
	}
	return value, nil
}

// ListContractKeys lists all state keys for a contract
func (idx *Indexer) ListContractKeys(contractAddr string) ([]string, error) {
	prefix := fmt.Sprintf("state:%s:", contractAddr)

	// Simple iteration (in practice, might need more efficient prefix scanning)
	var keys []string

	// TODO: Implement proper prefix iteration based on KV store capabilities
	// This is a simplified placeholder
	idx.logger.Debug("Listing keys for contract: %s", contractAddr)

	return keys, nil
}

// Reset resets the indexer state (useful for testing or full resync)
func (idx *Indexer) Reset() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.running {
		return fmt.Errorf("cannot reset while indexer is running")
	}

	// Clear checkpoint
	idx.checkpoint = &Checkpoint{
		Height:      0,
		LastKey:     idx.config.StartFromKey,
		LastUpdated: time.Now(),
	}

	// Save cleared checkpoint
	if err := idx.saveCheckpoint(); err != nil {
		return fmt.Errorf("failed to save reset checkpoint: %w", err)
	}

	// Reset stats
	idx.stats = &Stats{StartTime: time.Now()}

	idx.logger.Info("Indexer state reset")
	return nil
}