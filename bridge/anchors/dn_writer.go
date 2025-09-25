package anchors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/registry/dn"
	"github.com/opendlt/accumulate-accumen/types/json"
)

// DNWriter handles writing transaction metadata to Accumulate Directory Network
type DNWriter struct {
	mu         sync.RWMutex
	config     *DNWriterConfig
	dnClient   *dn.Client
	l0Client   *l0api.Client
	pricer     *pricing.Calculator
	builder    *json.MetadataBuilder
	stats      *WriterStats
	anchors    map[string]*Anchor
	queue      chan *WriteRequest
	workers    int
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// DNWriterConfig defines configuration for the DN writer
type DNWriterConfig struct {
	// Directory Network configuration
	DNEndpoint string
	DNTimeout  time.Duration

	// L0 API configuration
	L0Endpoint string
	L0Timeout  time.Duration

	// Writer configuration
	BatchSize    int
	FlushTimeout time.Duration
	MaxRetries   int
	RetryDelay   time.Duration

	// Worker configuration
	Workers     int
	QueueSize   int
	ConcretMode bool

	// Anchor configuration
	AnchorURL        string
	AnchorKeyBook    string
	AnchorSigningKey string

	// Metadata configuration
	MetadataBuilder *json.BuilderConfig

	// Enable compression
	EnableCompression bool
}

// DefaultDNWriterConfig returns default configuration
func DefaultDNWriterConfig() *DNWriterConfig {
	return &DNWriterConfig{
		DNEndpoint:        "https://mainnet.accumulatenetwork.io/v3",
		DNTimeout:         30 * time.Second,
		L0Endpoint:        "https://mainnet.accumulatenetwork.io/v3",
		L0Timeout:         30 * time.Second,
		BatchSize:         100,
		FlushTimeout:      5 * time.Second,
		MaxRetries:        3,
		RetryDelay:        time.Second,
		Workers:           4,
		QueueSize:         1000,
		ConcretMode:       false,
		AnchorURL:         "acc://accumen.acme/anchors",
		AnchorKeyBook:     "acc://accumen.acme/book",
		AnchorSigningKey:  "",
		MetadataBuilder:   json.DefaultBuilderConfig(),
		EnableCompression: true,
	}
}

// WriteRequest represents a request to write metadata
type WriteRequest struct {
	Metadata     *json.TransactionMetadata
	Priority     int64
	RetryCount   int
	SubmittedAt  time.Time
	ResponseChan chan *WriteResponse
}

// WriteResponse represents the response from a write operation
type WriteResponse struct {
	Success    bool
	TxHash     string
	Error      error
	Duration   time.Duration
	RetryCount int
}

// Anchor represents an anchor transaction
type Anchor struct {
	ID          string    `json:"id"`
	TxID        string    `json:"tx_id"`
	BlockHeight uint64    `json:"block_height"`
	Metadata    []byte    `json:"metadata"`
	Hash        string    `json:"hash"`
	Status      string    `json:"status"`
	Created     time.Time `json:"created"`
	Submitted   time.Time `json:"submitted"`
	Confirmed   time.Time `json:"confirmed"`
	L0TxHash    string    `json:"l0_tx_hash"`
	Cost        uint64    `json:"cost"`
	Retries     int       `json:"retries"`
}

// WriterStats contains statistics about the DN writer
type WriterStats struct {
	mu                 sync.RWMutex
	AnchorsWritten     uint64
	BytesWritten       uint64
	Errors             uint64
	Retries            uint64
	AverageLatency     time.Duration
	QueueSize          int
	ActiveWorkers      int
	LastWrite          time.Time
	TotalCost          uint64
	CompressionRatio   float64
	BatchesProcessed   uint64
	SuccessfulBatches  uint64
}

// NewDNWriter creates a new DN writer
func NewDNWriter(config *DNWriterConfig) (*DNWriter, error) {
	if config == nil {
		config = DefaultDNWriterConfig()
	}

	// Create DN client
	dnClient, err := dn.NewClient(&dn.ClientConfig{
		Endpoint: config.DNEndpoint,
		Timeout:  config.DNTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create DN client: %w", err)
	}

	// Create L0 client
	l0Client, err := l0api.NewClient(&l0api.ClientConfig{
		Endpoint: config.L0Endpoint,
		Timeout:  config.L0Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create L0 client: %w", err)
	}

	// Create pricing calculator
	pricer, err := pricing.NewCalculator(&pricing.Config{
		BasePrice:    100, // 1 credit base
		GasPrice:     1,   // 1 credit per gas unit
		SizePrice:    10,  // 10 credits per KB
		PriorityRate: 1.5, // 50% premium for high priority
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create pricing calculator: %w", err)
	}

	// Create metadata builder
	builder := json.NewMetadataBuilder(config.MetadataBuilder)

	writer := &DNWriter{
		config:   config,
		dnClient: dnClient,
		l0Client: l0Client,
		pricer:   pricer,
		builder:  builder,
		stats:    &WriterStats{},
		anchors:  make(map[string]*Anchor),
		queue:    make(chan *WriteRequest, config.QueueSize),
		workers:  config.Workers,
		stopCh:   make(chan struct{}),
	}

	return writer, nil
}

// Start starts the DN writer workers
func (w *DNWriter) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Start worker goroutines
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go w.worker(ctx, i)
	}

	return nil
}

// Stop stops the DN writer workers
func (w *DNWriter) Stop(ctx context.Context) error {
	close(w.stopCh)
	w.wg.Wait()
	return nil
}

// WriteMetadata queues metadata for writing to DN
func (w *DNWriter) WriteMetadata(ctx context.Context, metadata *json.TransactionMetadata, priority int64) (*WriteResponse, error) {
	if metadata == nil {
		return nil, fmt.Errorf("metadata cannot be nil")
	}

	// Validate metadata
	if err := w.builder.Validate(metadata); err != nil {
		return nil, fmt.Errorf("metadata validation failed: %w", err)
	}

	// Create write request
	req := &WriteRequest{
		Metadata:     metadata,
		Priority:     priority,
		RetryCount:   0,
		SubmittedAt:  time.Now(),
		ResponseChan: make(chan *WriteResponse, 1),
	}

	// Queue the request
	select {
	case w.queue <- req:
		// Request queued successfully
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("writer queue is full")
	}

	// Wait for response
	select {
	case resp := <-req.ResponseChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// worker processes write requests
func (w *DNWriter) worker(ctx context.Context, workerID int) {
	defer w.wg.Done()

	w.stats.mu.Lock()
	w.stats.ActiveWorkers++
	w.stats.mu.Unlock()

	defer func() {
		w.stats.mu.Lock()
		w.stats.ActiveWorkers--
		w.stats.mu.Unlock()
	}()

	batch := make([]*WriteRequest, 0, w.config.BatchSize)
	ticker := time.NewTicker(w.config.FlushTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Process remaining batch before shutting down
			if len(batch) > 0 {
				w.processBatch(ctx, batch)
			}
			return

		case <-w.stopCh:
			// Process remaining batch before shutting down
			if len(batch) > 0 {
				w.processBatch(ctx, batch)
			}
			return

		case req := <-w.queue:
			batch = append(batch, req)

			// Process batch if it's full
			if len(batch) >= w.config.BatchSize {
				w.processBatch(ctx, batch)
				batch = batch[:0] // Reset batch
			}

		case <-ticker.C:
			// Process batch on timeout
			if len(batch) > 0 {
				w.processBatch(ctx, batch)
				batch = batch[:0] // Reset batch
			}
		}
	}
}

// processBatch processes a batch of write requests
func (w *DNWriter) processBatch(ctx context.Context, batch []*WriteRequest) {
	startTime := time.Now()

	w.stats.mu.Lock()
	w.stats.BatchesProcessed++
	w.stats.mu.Unlock()

	// Group requests by priority for better processing
	highPriority := make([]*WriteRequest, 0)
	normalPriority := make([]*WriteRequest, 0)

	for _, req := range batch {
		if req.Priority > 500 { // High priority threshold
			highPriority = append(highPriority, req)
		} else {
			normalPriority = append(normalPriority, req)
		}
	}

	// Process high priority first
	if len(highPriority) > 0 {
		w.processRequests(ctx, highPriority)
	}

	// Process normal priority
	if len(normalPriority) > 0 {
		w.processRequests(ctx, normalPriority)
	}

	duration := time.Since(startTime)

	w.stats.mu.Lock()
	w.stats.SuccessfulBatches++
	w.stats.AverageLatency = (w.stats.AverageLatency + duration) / 2
	w.stats.mu.Unlock()
}

// processRequests processes individual write requests
func (w *DNWriter) processRequests(ctx context.Context, requests []*WriteRequest) {
	for _, req := range requests {
		response := w.processRequest(ctx, req)

		// Send response back
		select {
		case req.ResponseChan <- response:
		default:
			// Channel might be closed, ignore
		}
	}
}

// processRequest processes a single write request
func (w *DNWriter) processRequest(ctx context.Context, req *WriteRequest) *WriteResponse {
	startTime := time.Now()

	// Convert metadata to JSON
	metadataBytes, err := w.builder.ToJSON(req.Metadata)
	if err != nil {
		w.recordError()
		return &WriteResponse{
			Success:    false,
			Error:      fmt.Errorf("failed to serialize metadata: %w", err),
			Duration:   time.Since(startTime),
			RetryCount: req.RetryCount,
		}
	}

	// Compress if enabled
	if w.config.EnableCompression {
		// TODO: Implement compression
		// compressedBytes := compress(metadataBytes)
		// w.updateCompressionStats(len(metadataBytes), len(compressedBytes))
		// metadataBytes = compressedBytes
	}

	// Calculate cost
	cost, err := w.pricer.CalculateCost(&pricing.CostParams{
		GasUsed:  req.Metadata.ExecutionContext.GasUsed,
		DataSize: uint64(len(metadataBytes)),
		Priority: req.Priority,
	})
	if err != nil {
		w.recordError()
		return &WriteResponse{
			Success:    false,
			Error:      fmt.Errorf("failed to calculate cost: %w", err),
			Duration:   time.Since(startTime),
			RetryCount: req.RetryCount,
		}
	}

	// Create anchor
	anchor := &Anchor{
		ID:          fmt.Sprintf("anchor-%s-%d", req.Metadata.TxID, time.Now().UnixNano()),
		TxID:        req.Metadata.TxID,
		BlockHeight: req.Metadata.BlockHeight,
		Metadata:    metadataBytes,
		Status:      "pending",
		Created:     time.Now(),
		Cost:        cost,
	}

	// Submit to DN
	txHash, err := w.submitAnchor(ctx, anchor)
	if err != nil {
		// Retry logic
		if req.RetryCount < w.config.MaxRetries {
			time.Sleep(w.config.RetryDelay * time.Duration(req.RetryCount+1))
			req.RetryCount++
			w.recordRetry()
			return w.processRequest(ctx, req)
		}

		w.recordError()
		return &WriteResponse{
			Success:    false,
			Error:      fmt.Errorf("failed to submit anchor after %d retries: %w", req.RetryCount, err),
			Duration:   time.Since(startTime),
			RetryCount: req.RetryCount,
		}
	}

	// Update anchor
	anchor.L0TxHash = txHash
	anchor.Status = "submitted"
	anchor.Submitted = time.Now()

	// Store anchor
	w.mu.Lock()
	w.anchors[anchor.ID] = anchor
	w.mu.Unlock()

	// Record success
	w.recordSuccess(uint64(len(metadataBytes)), cost, time.Since(startTime))

	return &WriteResponse{
		Success:    true,
		TxHash:     txHash,
		Duration:   time.Since(startTime),
		RetryCount: req.RetryCount,
	}
}

// submitAnchor submits an anchor to the Accumulate network
func (w *DNWriter) submitAnchor(ctx context.Context, anchor *Anchor) (string, error) {
	// Parse anchor URL
	anchorURL, err := url.Parse(w.config.AnchorURL)
	if err != nil {
		return "", fmt.Errorf("invalid anchor URL: %w", err)
	}

	// Create anchor transaction data
	anchorData := map[string]interface{}{
		"type":         "accumen_metadata",
		"tx_id":        anchor.TxID,
		"block_height": anchor.BlockHeight,
		"metadata":     anchor.Metadata,
		"timestamp":    anchor.Created.Unix(),
		"cost":         anchor.Cost,
	}

	// Submit via DN client
	envelope, err := w.dnClient.PutAnchor(ctx, anchorURL, anchorData)
	if err != nil {
		return "", fmt.Errorf("failed to put anchor: %w", err)
	}

	return envelope.TxHash.String(), nil
}

// recordSuccess records successful write statistics
func (w *DNWriter) recordSuccess(bytes, cost uint64, duration time.Duration) {
	w.stats.mu.Lock()
	defer w.stats.mu.Unlock()

	w.stats.AnchorsWritten++
	w.stats.BytesWritten += bytes
	w.stats.TotalCost += cost
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

	// Update queue size
	w.stats.QueueSize = len(w.queue)

	return *w.stats
}

// GetAnchor retrieves an anchor by ID
func (w *DNWriter) GetAnchor(anchorID string) (*Anchor, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	anchor, exists := w.anchors[anchorID]
	return anchor, exists
}

// ListAnchors returns all anchors for a transaction
func (w *DNWriter) ListAnchors(txID string) []*Anchor {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var anchors []*Anchor
	for _, anchor := range w.anchors {
		if anchor.TxID == txID {
			anchors = append(anchors, anchor)
		}
	}

	return anchors
}

// UpdateAnchorStatus updates the status of an anchor
func (w *DNWriter) UpdateAnchorStatus(ctx context.Context, anchorID, status string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	anchor, exists := w.anchors[anchorID]
	if !exists {
		return fmt.Errorf("anchor %s not found", anchorID)
	}

	anchor.Status = status
	if status == "confirmed" {
		anchor.Confirmed = time.Now()
	}

	return nil
}

// Cleanup removes old anchors and releases resources
func (w *DNWriter) Cleanup(ctx context.Context, maxAge time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	for id, anchor := range w.anchors {
		if anchor.Created.Before(cutoff) {
			delete(w.anchors, id)
		}
	}

	return nil
}