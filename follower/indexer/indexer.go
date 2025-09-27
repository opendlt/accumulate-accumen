package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
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

	// L1 transaction tracking
	l1HashMapping map[string]*L1Receipt // l1Hash -> receipt info
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

	// DN-specific fields for L1 discovery
	DNTxID        []byte                 `json:"dnTxId,omitempty"` // The DN WriteData transaction ID
	DNKey         string                 `json:"dnKey,omitempty"` // DN data entry key
	L1Hash        string                 `json:"l1Hash,omitempty"` // L1 hash from crosslink memo
}

// L1Receipt represents a receipt for an L1 transaction with DN references
type L1Receipt struct {
	L1Hash       string    `json:"l1Hash"`
	DNTxID       []byte    `json:"dnTxId"`
	DNKey        string    `json:"dnKey"`
	Contract     string    `json:"contract,omitempty"`
	Entry        string    `json:"entry,omitempty"`
	Metadata     *MetadataEntry `json:"metadata,omitempty"`
	AnchorTxIDs  [][]byte  `json:"anchorTxIds,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// DNEntry represents a single data entry found during DN scanning
type DNEntry struct {
	Key           string    `json:"key"`
	Data          []byte    `json:"data"`
	TxID          []byte    `json:"txId"` // WriteData transaction ID
	TxHeader      *api.TransactionQueryResponse `json:"txHeader,omitempty"`
	L1Hash        string    `json:"l1Hash,omitempty"` // Parsed from memo/metadata
	Contract      string    `json:"contract,omitempty"` // Parsed from metadata
	EntryMethod   string    `json:"entryMethod,omitempty"` // Parsed from metadata
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
		config:        config,
		client:        config.APIClient,
		kvStore:       config.KVStore,
		logger:        logz.New(logz.INFO, "indexer"),
		stats:         &Stats{StartTime: time.Now()},
		stopChan:      make(chan struct{}),
		l1HashMapping: make(map[string]*L1Receipt),
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

// queryDNEntries queries the DN for metadata entries and anchor data
func (idx *Indexer) queryDNEntries(ctx context.Context, fromKey string, limit int) ([]*MetadataEntry, string, error) {
	// Query both metadata and anchors paths
	var allEntries []*MetadataEntry
	var finalNextKey string

	// 1. Query metadata entries
	metadataEntries, metadataNextKey, err := idx.queryDNPath(ctx, idx.config.MetadataPath, fromKey, limit/2)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query metadata path: %w", err)
	}
	allEntries = append(allEntries, metadataEntries...)

	// 2. Query anchor entries if we have anchors path configured
	if idx.config.AnchorsPath != "" {
		anchorEntries, anchorNextKey, err := idx.queryDNPath(ctx, idx.config.AnchorsPath, fromKey, limit/2)
		if err != nil {
			idx.logger.Info("Warning: Failed to query anchors path: %v", err)
		} else {
			allEntries = append(allEntries, anchorEntries...)
			finalNextKey = anchorNextKey // Use anchor next key as final
		}
	}

	if finalNextKey == "" {
		finalNextKey = metadataNextKey
	}

	return allEntries, finalNextKey, nil
}

// queryDNPath queries a specific DN path for data entries
func (idx *Indexer) queryDNPath(ctx context.Context, dnPath string, fromKey string, limit int) ([]*MetadataEntry, string, error) {
	// Parse the DN path URL
	dnURL, err := url.Parse(dnPath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid DN path: %w", err)
	}

	// Query DN entries with retry logic
	var entries []*MetadataEntry
	var nextKey string

	for attempt := 0; attempt <= idx.config.MaxRetries; attempt++ {
		if attempt > 0 {
			idx.recordRetry()
			time.Sleep(idx.config.RetryDelay * time.Duration(attempt))
		}

		// Query the DN data account for entries
		entries, nextKey, err = idx.scanDNDataAccount(ctx, dnURL, fromKey, limit)
		if err == nil {
			break
		}

		idx.logger.Debug("Query attempt %d failed for %s: %v", attempt+1, dnPath, err)
	}

	return entries, nextKey, err
}

// scanDNDataAccount scans a DN data account for entries with WriteData transaction headers
func (idx *Indexer) scanDNDataAccount(ctx context.Context, dnURL *url.URL, fromKey string, limit int) ([]*MetadataEntry, string, error) {
	idx.logger.Debug("Scanning DN data account: %s from key '%s' with limit %d", dnURL, fromKey, limit)

	// Query the data account using v3 client
	// This queries both the data entries and their associated WriteData transaction headers
	dnEntries, err := idx.queryDataAccountEntries(ctx, dnURL, fromKey, limit)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query data account entries: %w", err)
	}

	var metadataEntries []*MetadataEntry
	var nextKey string

	// Process each DN entry
	for _, dnEntry := range dnEntries {
		// Parse L1 hash from WriteData transaction memo/metadata
		l1Hash, contract, entryMethod := idx.parseL1InfoFromTxHeader(dnEntry.TxHeader)
		if l1Hash == "" {
			idx.logger.Debug("No L1 hash found in DN entry %s, skipping", dnEntry.Key)
			continue
		}

		// Try to parse the data as L1 metadata JSON
		var metadata *MetadataEntry
		if len(dnEntry.Data) > 0 {
			if err := json.Unmarshal(dnEntry.Data, &metadata); err != nil {
				// If it's not JSON metadata, create a basic entry from DN info
				metadata = &MetadataEntry{
					TxHash:      l1Hash,
					L1Hash:      l1Hash,
					ContractAddr: contract,
					Entry:       entryMethod,
					DNTxID:      dnEntry.TxID,
					DNKey:       dnEntry.Key,
					Time:        time.Now(), // Fallback
				}
			} else {
				// Enhance parsed metadata with DN information
				metadata.DNTxID = dnEntry.TxID
				metadata.DNKey = dnEntry.Key
				metadata.L1Hash = l1Hash
				if metadata.ContractAddr == "" {
					metadata.ContractAddr = contract
				}
				if metadata.Entry == "" {
					metadata.Entry = entryMethod
				}
			}
		} else {
			// Empty data, create basic entry
			metadata = &MetadataEntry{
				TxHash:      l1Hash,
				L1Hash:      l1Hash,
				ContractAddr: contract,
				Entry:       entryMethod,
				DNTxID:      dnEntry.TxID,
				DNKey:       dnEntry.Key,
				Time:        time.Now(),
			}
		}

		// Store L1 hash mapping for receipt queries
		receipt := &L1Receipt{
			L1Hash:    l1Hash,
			DNTxID:    dnEntry.TxID,
			DNKey:     dnEntry.Key,
			Contract:  contract,
			Entry:     entryMethod,
			Metadata:  metadata,
			CreatedAt: time.Now(),
		}
		idx.l1HashMapping[l1Hash] = receipt

		metadataEntries = append(metadataEntries, metadata)
		nextKey = dnEntry.Key
	}

	idx.logger.Debug("Found %d entries with L1 hashes from DN scan", len(metadataEntries))
	return metadataEntries, nextKey, nil
}

// queryDataAccountEntries queries a data account for entries and their WriteData transaction headers
func (idx *Indexer) queryDataAccountEntries(ctx context.Context, dnURL *url.URL, fromKey string, limit int) ([]*DNEntry, error) {
	// For now, return empty results since this requires actual DN integration
	// In real implementation, this would:
	// 1. Use v3.Client.QueryDirectory to list data entries under dnURL
	// 2. For each entry, query both the data and the WriteData transaction that created it
	// 3. Parse the transaction header to extract memo and metadata

	idx.logger.Debug("Querying data account entries for %s (placeholder)", dnURL)
	return []*DNEntry{}, nil
}

// parseL1InfoFromTxHeader extracts L1 hash and contract info from WriteData transaction header
func (idx *Indexer) parseL1InfoFromTxHeader(txHeader *api.TransactionQueryResponse) (l1Hash, contract, entryMethod string) {
	if txHeader == nil || txHeader.Transaction == nil {
		return "", "", ""
	}

	tx := txHeader.Transaction

	// Parse memo for L1 hash using accutil.WithCrossLink format
	if tx.Header != nil && tx.Header.Memo != "" {
		l1Hash = idx.parseL1HashFromMemo(tx.Header.Memo)
	}

	// Parse metadata for contract and entry info
	if tx.Header != nil && len(tx.Header.Metadata) > 0 {
		contract, entryMethod = idx.parseContractInfoFromMetadata(tx.Header.Metadata)
	}

	return l1Hash, contract, entryMethod
}

// parseL1HashFromMemo extracts L1 hash from memo using accutil.WithCrossLink format
func (idx *Indexer) parseL1HashFromMemo(memo string) string {
	// Parse CrossLink format from accutil.WithCrossLink
	// Expected format: "crosslink:<l1Hash>"
	if strings.HasPrefix(memo, "crosslink:") {
		return strings.TrimPrefix(memo, "crosslink:")
	}

	// Also check for other common patterns
	if strings.HasPrefix(memo, "l1:") {
		return strings.TrimPrefix(memo, "l1:")
	}

	return ""
}

// parseContractInfoFromMetadata extracts contract and entry info from transaction metadata
func (idx *Indexer) parseContractInfoFromMetadata(metadata []byte) (contract, entryMethod string) {
	// Try to parse metadata as JSON
	var metadataMap map[string]interface{}
	if err := json.Unmarshal(metadata, &metadataMap); err != nil {
		return "", ""
	}

	// Look for contract address
	if contractVal, ok := metadataMap["contract"].(string); ok {
		contract = contractVal
	} else if contractVal, ok := metadataMap["contractAddr"].(string); ok {
		contract = contractVal
	}

	// Look for entry method
	if entryVal, ok := metadataMap["entry"].(string); ok {
		entryMethod = entryVal
	} else if entryVal, ok := metadataMap["method"].(string); ok {
		entryMethod = entryVal
	}

	return contract, entryMethod
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

	// Apply L0 outputs from metadata
	for _, l0Output := range entry.L0Outputs {
		if err := idx.applyL0Output(entry.ContractAddr, l0Output); err != nil {
			idx.logger.Info("Warning: Failed to apply L0 output: %v", err)
			// Continue processing other outputs
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

	// Store L1 hash mapping if available
	if entry.L1Hash != "" {
		receipt := &L1Receipt{
			L1Hash:    entry.L1Hash,
			DNTxID:    entry.DNTxID,
			DNKey:     entry.DNKey,
			Contract:  entry.ContractAddr,
			Entry:     entry.Entry,
			Metadata:  entry,
			CreatedAt: time.Now(),
		}
		idx.l1HashMapping[entry.L1Hash] = receipt
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

// applyL0Output applies a single L0 output to update state
func (idx *Indexer) applyL0Output(contractAddr string, l0Output map[string]any) error {
	// Extract L0 output data
	outputType, ok := l0Output["type"].(string)
	if !ok {
		return fmt.Errorf("L0 output missing type")
	}

	// Handle different types of L0 outputs
	switch outputType {
	case "WriteData":
		return idx.applyL0WriteData(contractAddr, l0Output)
	case "SendTokens":
		return idx.applyL0SendTokens(contractAddr, l0Output)
	case "AddCredits":
		return idx.applyL0AddCredits(contractAddr, l0Output)
	default:
		idx.logger.Debug("Unknown L0 output type: %s", outputType)
		return nil
	}
}

// applyL0WriteData applies a WriteData L0 output
func (idx *Indexer) applyL0WriteData(contractAddr string, l0Output map[string]any) error {
	// Extract WriteData fields
	target, ok := l0Output["target"].(string)
	if !ok {
		return fmt.Errorf("WriteData output missing target")
	}

	data, ok := l0Output["data"].([]byte)
	if !ok {
		// Try string format
		if dataStr, ok := l0Output["data"].(string); ok {
			data = []byte(dataStr)
		} else {
			return fmt.Errorf("WriteData output missing or invalid data")
		}
	}

	// Apply the write data operation
	stateKey := fmt.Sprintf("l0_data:%s:%s", contractAddr, target)
	if err := idx.kvStore.Set([]byte(stateKey), data); err != nil {
		return fmt.Errorf("failed to apply WriteData: %w", err)
	}

	idx.logger.Debug("Applied L0 WriteData: contract=%s, target=%s, size=%d", contractAddr, target, len(data))
	return nil
}

// applyL0SendTokens applies a SendTokens L0 output
func (idx *Indexer) applyL0SendTokens(contractAddr string, l0Output map[string]any) error {
	// Extract SendTokens fields
	to, ok := l0Output["to"].(string)
	if !ok {
		return fmt.Errorf("SendTokens output missing to field")
	}

	amount, ok := l0Output["amount"].(string)
	if !ok {
		return fmt.Errorf("SendTokens output missing amount field")
	}

	// Record the token transfer
	transferKey := fmt.Sprintf("l0_transfer:%s:%s", contractAddr, to)
	if err := idx.kvStore.Set([]byte(transferKey), []byte(amount)); err != nil {
		return fmt.Errorf("failed to apply SendTokens: %w", err)
	}

	idx.logger.Debug("Applied L0 SendTokens: contract=%s, to=%s, amount=%s", contractAddr, to, amount)
	return nil
}

// applyL0AddCredits applies an AddCredits L0 output
func (idx *Indexer) applyL0AddCredits(contractAddr string, l0Output map[string]any) error {
	// Extract AddCredits fields
	recipient, ok := l0Output["recipient"].(string)
	if !ok {
		return fmt.Errorf("AddCredits output missing recipient field")
	}

	amount, ok := l0Output["amount"].(string)
	if !ok {
		return fmt.Errorf("AddCredits output missing amount field")
	}

	// Record the credit addition
	creditKey := fmt.Sprintf("l0_credits:%s:%s", contractAddr, recipient)
	if err := idx.kvStore.Set([]byte(creditKey), []byte(amount)); err != nil {
		return fmt.Errorf("failed to apply AddCredits: %w", err)
	}

	idx.logger.Debug("Applied L0 AddCredits: contract=%s, recipient=%s, amount=%s", contractAddr, recipient, amount)
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

// GetL1Receipt returns the L1 receipt for a given L1 hash
func (idx *Indexer) GetL1Receipt(l1Hash string) (*L1Receipt, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	receipt, exists := idx.l1HashMapping[l1Hash]
	if !exists {
		return nil, fmt.Errorf("L1 receipt not found for hash: %s", l1Hash)
	}

	// Return a copy to avoid concurrent access issues
	receiptCopy := *receipt
	return &receiptCopy, nil
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

	// Clear L1 hash mappings
	idx.l1HashMapping = make(map[string]*L1Receipt)

	idx.logger.Info("Indexer state reset")
	return nil
}
