package anchors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"google.golang.org/protobuf/proto"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/types/proto/accumen"
)

// DNWriter handles writing transaction metadata and anchors to Accumulate Directory Network
type DNWriter struct {
	mu              sync.RWMutex
	config          *DNWriterConfig
	l0Client        *l0api.Client
	eventSubscriber *l0api.EventSubscriber
	stats           *WriterStats
}

// DNWriterConfig defines configuration for the DN writer
type DNWriterConfig struct {
	// L0 API configuration
	L0Endpoint string
	L0Timeout  time.Duration

	// Signing configuration
	SequencerKey string // Development signing key

	// Writer configuration
	MaxRetries int
	RetryDelay time.Duration

	// Execution confirmation
	WaitForExecution bool
	ExecutionTimeout time.Duration
}

// DefaultDNWriterConfig returns default configuration
func DefaultDNWriterConfig() *DNWriterConfig {
	return &DNWriterConfig{
		L0Endpoint:       "https://mainnet.accumulatenetwork.io/v3",
		L0Timeout:        30 * time.Second,
		SequencerKey:     "",
		MaxRetries:       3,
		RetryDelay:       time.Second,
		WaitForExecution: false,
		ExecutionTimeout: 30 * time.Second,
	}
}

// WriterStats contains statistics about the DN writer
type WriterStats struct {
	mu              sync.RWMutex
	MetadataWritten uint64
	AnchorsWritten  uint64
	BytesWritten    uint64
	Errors          uint64
	Retries         uint64
	AverageLatency  time.Duration
	LastWrite       time.Time
	TotalCost       uint64
}

// NewDNWriter creates a new DN writer
func NewDNWriter(config *DNWriterConfig) (*DNWriter, error) {
	if config == nil {
		config = DefaultDNWriterConfig()
	}

	// Create L0 client configuration
	clientConfig := &l0api.ClientConfig{
		Endpoint:     config.L0Endpoint,
		Timeout:      config.L0Timeout,
		SequencerKey: config.SequencerKey,
	}

	// Create L0 client
	l0Client, err := l0api.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create L0 client: %w", err)
	}

	writer := &DNWriter{
		config:   config,
		l0Client: l0Client,
		stats:    &WriterStats{},
	}

	// Create event subscriber if execution confirmation is enabled
	if config.WaitForExecution {
		eventSubscriber, err := l0api.NewEventSubscriber(config.L0Endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to create event subscriber: %w", err)
		}
		writer.eventSubscriber = eventSubscriber
	}

	return writer, nil
}

// WriteMetadata writes transaction metadata to DN via L0 WriteData
func (w *DNWriter) WriteMetadata(ctx context.Context, jsonBytes []byte, basePath string) (string, error) {
	startTime := time.Now()

	// Generate URL with date prefix and random suffix
	dataURL, err := w.generateMetadataURL(basePath)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to generate metadata URL: %w", err)
	}

	// Create WriteData transaction using builder
	envelope := l0api.BuildWriteData(dataURL, jsonBytes, "Accumen metadata", nil)

	// Submit the transaction
	txID, err := w.submitWithRetry(ctx, envelope)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to write metadata to DN: %w", err)
	}

	// Wait for execution if enabled
	if w.config.WaitForExecution && w.eventSubscriber != nil {
		if err := w.waitForExecution(ctx, txID); err != nil {
			w.recordError()
			return txID, fmt.Errorf("metadata submitted but execution failed: %w", err)
		}
	}

	// Record success
	w.recordMetadataSuccess(uint64(len(jsonBytes)), time.Since(startTime))

	return txID, nil
}

// WriteAnchor writes anchor blob to DN via L0 WriteData
func (w *DNWriter) WriteAnchor(ctx context.Context, blob []byte, basePath string) (string, error) {
	startTime := time.Now()

	// Generate URL with date prefix and random suffix
	anchorURL, err := w.generateAnchorURL(basePath)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to generate anchor URL: %w", err)
	}

	// Create WriteData transaction for anchor using builder
	envelope := l0api.BuildWriteData(anchorURL, blob, "Accumen anchor", nil)

	// Submit the transaction
	txID, err := w.submitWithRetry(ctx, envelope)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to write anchor to DN: %w", err)
	}

	// Wait for execution if enabled
	if w.config.WaitForExecution && w.eventSubscriber != nil {
		if err := w.waitForExecution(ctx, txID); err != nil {
			w.recordError()
			return txID, fmt.Errorf("anchor submitted but execution failed: %w", err)
		}
	}

	// Record success
	w.recordAnchorSuccess(uint64(len(blob)), time.Since(startTime))

	return txID, nil
}

// generateMetadataURL creates a URL for metadata with date prefix and random suffix
func (w *DNWriter) generateMetadataURL(basePath string) (*url.URL, error) {
	now := time.Now().UTC()
	datePrefix := fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day())

	// Generate random suffix
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random suffix: %w", err)
	}
	suffix := hex.EncodeToString(randomBytes)

	// Construct full path
	fullPath := fmt.Sprintf("%s/metadata/%s/%s", basePath, datePrefix, suffix)

	return url.Parse(fullPath)
}

// generateAnchorURL creates a URL for anchor with date prefix and random suffix
func (w *DNWriter) generateAnchorURL(basePath string) (*url.URL, error) {
	now := time.Now().UTC()
	datePrefix := fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day())

	// Generate random suffix
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random suffix: %w", err)
	}
	suffix := hex.EncodeToString(randomBytes)

	// Construct full path
	fullPath := fmt.Sprintf("%s/anchors/%s/%s", basePath, datePrefix, suffix)

	return url.Parse(fullPath)
}

// submitWithRetry submits a transaction with retry logic
func (w *DNWriter) submitWithRetry(ctx context.Context, envelope *build.EnvelopeBuilder) (string, error) {
	var lastErr error

	for attempt := 0; attempt <= w.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retrying
			select {
			case <-time.After(w.config.RetryDelay * time.Duration(attempt)):
			case <-ctx.Done():
				return "", ctx.Err()
			}
			w.recordRetry()
		}

		// Submit envelope
		txID, err := w.l0Client.SubmitEnvelope(ctx, envelope)
		if err == nil {
			return txID, nil
		}

		lastErr = err

		// Check if we should retry this error
		if !w.shouldRetry(err) {
			break
		}
	}

	return "", fmt.Errorf("failed after %d attempts: %w", w.config.MaxRetries, lastErr)
}

// waitForExecution waits for transaction execution confirmation via WebSocket
func (w *DNWriter) waitForExecution(ctx context.Context, txID string) error {
	if w.eventSubscriber == nil {
		return fmt.Errorf("event subscriber not available")
	}

	// Start event subscriber if not already started
	if !w.eventSubscriber.GetConnectionStatus() {
		if err := w.eventSubscriber.Start(ctx); err != nil {
			return fmt.Errorf("failed to start event subscriber: %w", err)
		}
	}

	// Wait for confirmation
	confirmation, err := w.eventSubscriber.WaitForConfirmation(ctx, txID, w.config.ExecutionTimeout)
	if err != nil {
		return fmt.Errorf("failed to get execution confirmation: %w", err)
	}

	// Check execution status
	if confirmation.Status == "failed" {
		return fmt.Errorf("transaction execution failed (code %d): %s", confirmation.Code, confirmation.Log)
	}

	if confirmation.Status != "executed" {
		return fmt.Errorf("unexpected transaction status: %s", confirmation.Status)
	}

	return nil
}

// shouldRetry determines if an error should trigger a retry
func (w *DNWriter) shouldRetry(err error) bool {
	// For now, retry all errors except context cancellation
	// In production, this would be more sophisticated
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}
	return true
}

// recordMetadataSuccess records successful metadata write statistics
func (w *DNWriter) recordMetadataSuccess(bytes uint64, duration time.Duration) {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()

	w.stats.MetadataWritten++
	w.stats.BytesWritten += bytes
	w.stats.LastWrite = time.Now()
	w.stats.AverageLatency = (w.stats.AverageLatency + duration) / 2
}

// recordAnchorSuccess records successful anchor write statistics
func (w *DNWriter) recordAnchorSuccess(bytes uint64, duration time.Duration) {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()

	w.stats.AnchorsWritten++
	w.stats.BytesWritten += bytes
	w.stats.LastWrite = time.Now()
	w.stats.AverageLatency = (w.stats.AverageLatency + duration) / 2
}

// recordError records error statistics
func (w *DNWriter) recordError() {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()
	w.stats.Errors++
}

// recordRetry records retry statistics
func (w *DNWriter) recordRetry() {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()
	w.stats.Retries++
}

// GetStats returns current writer statistics
func (w *DNWriter) GetStats() WriterStats {
	w.stats.mu.RLock()
	defer w.stats.mu.RUnlock()
	return *w.stats
}

// Close closes the DN writer and releases resources
func (w *DNWriter) Close() error {
	var err error

	// Close event subscriber if present
	if w.eventSubscriber != nil {
		if closeErr := w.eventSubscriber.Stop(); closeErr != nil {
			err = closeErr
		}
	}

	// Close L0 client
	if w.l0Client != nil {
		if closeErr := w.l0Client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}

// WriteBlockHeader writes a block header to DN for anchoring and follower verification
func (w *DNWriter) WriteBlockHeader(ctx context.Context, header accumen.BlockHeader, basePath string) (string, error) {
	startTime := time.Now()

	// Generate URL for block header anchor
	headerURL, err := w.generateBlockHeaderURL(basePath, header.Height)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to generate block header URL: %w", err)
	}

	// Serialize block header data for anchoring
	headerData, err := w.buildBlockHeaderData(header)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to build block header data: %w", err)
	}

	// Create WriteData transaction for block header using builder
	envelope := l0api.BuildWriteData(headerURL, headerData, "Accumen block header anchor", nil)

	// Submit the transaction
	txID, err := w.submitWithRetry(ctx, envelope)
	if err != nil {
		w.recordError()
		return "", fmt.Errorf("failed to write block header to DN: %w", err)
	}

	// Wait for execution if enabled
	if w.config.WaitForExecution && w.eventSubscriber != nil {
		if err := w.waitForExecution(ctx, txID); err != nil {
			w.recordError()
			return txID, fmt.Errorf("block header submitted but execution failed: %w", err)
		}
	}

	// Record success as anchor write
	w.recordAnchorSuccess(uint64(len(headerData)), time.Since(startTime))

	return txID, nil
}

// generateBlockHeaderURL creates a URL for block header anchor
func (w *DNWriter) generateBlockHeaderURL(basePath string, height uint64) (*url.URL, error) {
	now := time.Now().UTC()
	datePrefix := fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day())

	// Use block height as part of the path for deterministic URLs
	fullPath := fmt.Sprintf("%s/headers/%s/block-%d", basePath, datePrefix, height)

	return url.Parse(fullPath)
}

// buildBlockHeaderData constructs the block header anchor data with cross-link information
func (w *DNWriter) buildBlockHeaderData(header accumen.BlockHeader) ([]byte, error) {
	// Calculate header hash
	headerBytes, err := proto.Marshal(&header)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal header: %w", err)
	}
	headerHash := sha256.Sum256(headerBytes)

	// Convert timestamp from nanoseconds to time
	timestamp := time.Unix(0, header.Time).UTC()

	// Build header anchor structure with cross-link information
	anchor := map[string]interface{}{
		"version":     "1.0.0",
		"type":        "accumen_block_header",
		"blockHeight": header.Height,
		"headerHash":  hex.EncodeToString(headerHash[:]),
		"prevHash":    hex.EncodeToString(header.PrevHash),
		"txsRoot":     hex.EncodeToString(header.TxsRoot),
		"resultsRoot": hex.EncodeToString(header.ResultsRoot),
		"stateRoot":   hex.EncodeToString(header.StateRoot),
		"timestamp":   timestamp.Format(time.RFC3339),
		"crossLink": map[string]interface{}{
			"l1ChainId":    "accumen-l1",
			"l1BlockHash":  hex.EncodeToString(headerHash[:]),
			"l1Height":     header.Height,
			"followerNote": "L1 block header anchored to L0 for follower verification",
			"merkleProofs": map[string]interface{}{
				"txsRootProof":     hex.EncodeToString(header.TxsRoot),
				"resultsRootProof": hex.EncodeToString(header.ResultsRoot),
				"stateRootProof":   hex.EncodeToString(header.StateRoot),
			},
		},
		"verification": map[string]interface{}{
			"purpose":     "follower_sync",
			"description": "Block header anchor enables followers to verify L1 chain integrity",
			"usage":       "Followers can reconstruct and verify transaction chain using these merkle roots",
		},
	}

	// Convert to JSON bytes
	jsonBytes, err := w.encodeAnchorData(anchor)
	if err != nil {
		return nil, fmt.Errorf("failed to encode anchor data: %w", err)
	}

	return jsonBytes, nil
}

// encodeAnchorData encodes anchor data as JSON
func (w *DNWriter) encodeAnchorData(data map[string]interface{}) ([]byte, error) {
	// In a real implementation, this would use a proper JSON encoder
	// For now, we'll build JSON manually as a fallback
	headerHash := data["headerHash"].(string)
	prevHash := data["prevHash"].(string)
	txsRoot := data["txsRoot"].(string)
	resultsRoot := data["resultsRoot"].(string)
	stateRoot := data["stateRoot"].(string)
	timestamp := data["timestamp"].(string)
	height := data["blockHeight"].(uint64)

	jsonStr := fmt.Sprintf(`{
		"version": "1.0.0",
		"type": "accumen_block_header",
		"blockHeight": %d,
		"headerHash": "%s",
		"prevHash": "%s",
		"txsRoot": "%s",
		"resultsRoot": "%s",
		"stateRoot": "%s",
		"timestamp": "%s",
		"crossLink": {
			"l1ChainId": "accumen-l1",
			"l1BlockHash": "%s",
			"l1Height": %d,
			"followerNote": "L1 block header anchored to L0 for follower verification",
			"merkleProofs": {
				"txsRootProof": "%s",
				"resultsRootProof": "%s",
				"stateRootProof": "%s"
			}
		},
		"verification": {
			"purpose": "follower_sync",
			"description": "Block header anchor enables followers to verify L1 chain integrity",
			"usage": "Followers can reconstruct and verify transaction chain using these merkle roots"
		}
	}`,
		height,
		headerHash,
		prevHash,
		txsRoot,
		resultsRoot,
		stateRoot,
		timestamp,
		headerHash,
		height,
		txsRoot,
		resultsRoot,
		stateRoot,
	)

	return []byte(jsonStr), nil
}
