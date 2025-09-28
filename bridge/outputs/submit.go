package outputs

import (
	"context"
	"fmt"
	"sync"
	"time"

	v3 "gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
)

// OutputSubmitter handles submission of staged outputs to Accumulate L0
type OutputSubmitter struct {
	client        *l0api.Client
	stager        *OutputStager
	creditManager *pricing.CreditManager
	config        *SubmitterConfig

	// Submission control
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
	workers  int
}

// SubmitterConfig defines configuration for the output submitter
type SubmitterConfig struct {
	// Number of concurrent submission workers
	WorkerCount int
	// Interval between submission attempts
	SubmissionInterval time.Duration
	// Maximum batch size for submissions
	MaxBatchSize int
	// Timeout for individual submissions
	SubmissionTimeout time.Duration
	// Enable validation before submission
	ValidateBeforeSubmit bool
	// Retry configuration
	RetryDelay time.Duration
}

// DefaultSubmitterConfig returns a default submitter configuration
func DefaultSubmitterConfig() *SubmitterConfig {
	return &SubmitterConfig{
		WorkerCount:          4,
		SubmissionInterval:   time.Second,
		MaxBatchSize:         100,
		SubmissionTimeout:    30 * time.Second,
		ValidateBeforeSubmit: true,
		RetryDelay:           5 * time.Second,
	}
}

// SubmissionResult represents the result of a submission attempt
type SubmissionResult struct {
	OutputID    string           `json:"output_id"`
	Success     bool             `json:"success"`
	TxHash      []byte           `json:"tx_hash,omitempty"`
	Response    []*v3.Submission `json:"response,omitempty"`
	Error       error            `json:"error,omitempty"`
	Cost        uint64           `json:"cost"`
	SubmittedAt time.Time        `json:"submitted_at"`
	Duration    time.Duration    `json:"duration"`
}

// NewOutputSubmitter creates a new output submitter
func NewOutputSubmitter(client *l0api.Client, stager *OutputStager, creditManager *pricing.CreditManager, config *SubmitterConfig) *OutputSubmitter {
	if config == nil {
		config = DefaultSubmitterConfig()
	}

	return &OutputSubmitter{
		client:        client,
		stager:        stager,
		creditManager: creditManager,
		config:        config,
		stopChan:      make(chan struct{}),
		workers:       0,
	}
}

// Start begins the output submission process
func (os *OutputSubmitter) Start(ctx context.Context) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	if os.running {
		return fmt.Errorf("output submitter is already running")
	}

	os.running = true

	// Start worker goroutines
	for i := 0; i < os.config.WorkerCount; i++ {
		os.workers++
		go os.submissionWorker(ctx, i)
	}

	return nil
}

// Stop stops the output submission process
func (os *OutputSubmitter) Stop() error {
	os.mu.Lock()
	defer os.mu.Unlock()

	if !os.running {
		return nil
	}

	os.running = false
	close(os.stopChan)

	// Wait for workers to finish (with timeout)
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	for os.workers > 0 {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout waiting for workers to stop")
		case <-time.After(100 * time.Millisecond):
			// Check again
		}
	}

	return nil
}

// SubmitOutput manually submits a specific staged output
func (os *OutputSubmitter) SubmitOutput(ctx context.Context, outputID string) (*SubmissionResult, error) {
	staged, err := os.stager.GetStagedOutput(outputID)
	if err != nil {
		return nil, fmt.Errorf("failed to get staged output: %w", err)
	}

	return os.submitStagedOutput(ctx, staged)
}

// SubmitBatch submits a batch of staged outputs
func (os *OutputSubmitter) SubmitBatch(ctx context.Context, outputIDs []string) ([]*SubmissionResult, error) {
	results := make([]*SubmissionResult, 0, len(outputIDs))

	for _, outputID := range outputIDs {
		result, err := os.SubmitOutput(ctx, outputID)
		if err != nil {
			// Create error result
			result = &SubmissionResult{
				OutputID:    outputID,
				Success:     false,
				Error:       err,
				SubmittedAt: time.Now(),
			}
		}
		results = append(results, result)
	}

	return results, nil
}

// submissionWorker runs the main submission loop for a worker
func (os *OutputSubmitter) submissionWorker(ctx context.Context, workerID int) {
	defer func() {
		os.mu.Lock()
		os.workers--
		os.mu.Unlock()
	}()

	ticker := time.NewTicker(os.config.SubmissionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-os.stopChan:
			return
		case <-ticker.C:
			os.processReadyOutputs(ctx, workerID)
		}
	}
}

// processReadyOutputs processes outputs that are ready for submission
func (os *OutputSubmitter) processReadyOutputs(ctx context.Context, workerID int) {
	readyOutputs := os.stager.GetReadyOutputs()
	if len(readyOutputs) == 0 {
		return
	}

	// Limit batch size
	if len(readyOutputs) > os.config.MaxBatchSize {
		readyOutputs = readyOutputs[:os.config.MaxBatchSize]
	}

	// Process each ready output
	for _, staged := range readyOutputs {
		// Check if another worker is already processing this output
		if err := os.stager.UpdateOutputStatus(staged.ID, StageStatusSubmitting); err != nil {
			continue // Skip if already being processed
		}

		// Submit the output
		result, err := os.submitStagedOutput(ctx, staged)
		if err != nil {
			os.stager.MarkOutputFailed(staged.ID, err)
			continue
		}

		// Update status based on result
		if result.Success {
			os.stager.UpdateOutputStatus(staged.ID, StageStatusSubmitted)

			// Update cost information
			if staged.Metadata != nil {
				staged.Metadata.ActualCost = result.Cost
			}
		} else {
			os.stager.MarkOutputFailed(staged.ID, result.Error)
		}
	}
}

// submitStagedOutput submits a single staged output
func (os *OutputSubmitter) submitStagedOutput(ctx context.Context, staged *StagedOutput) (*SubmissionResult, error) {
	start := time.Now()

	result := &SubmissionResult{
		OutputID:    staged.ID,
		SubmittedAt: start,
	}

	// Create submission context with timeout
	submitCtx, cancel := context.WithTimeout(ctx, os.config.SubmissionTimeout)
	defer cancel()

	// Validate before submission if enabled
	if os.config.ValidateBeforeSubmit {
		if err := os.validateEnvelope(submitCtx, staged.Envelope); err != nil {
			result.Error = fmt.Errorf("validation failed: %w", err)
			result.Duration = time.Since(start)
			return result, result.Error
		}
	}

	// Calculate expected cost
	if os.creditManager != nil && staged.Envelope != nil {
		cost, err := os.calculateEnvelopeCost(staged.Envelope)
		if err == nil {
			result.Cost = cost
			if staged.Metadata != nil {
				staged.Metadata.EstimatedCost = cost
			}
		}
	}

	// Submit to Accumulate L0
	response, err := os.client.Submit(submitCtx, staged.Envelope, v3.SubmitOptions{})
	if err != nil {
		result.Error = fmt.Errorf("submission failed: %w", err)
		result.Duration = time.Since(start)
		return result, result.Error
	}

	// Process successful response
	result.Success = true
	result.Response = response
	result.Duration = time.Since(start)

	// Extract transaction hash if available (simplified)
	if response != nil && len(response) > 0 {
		// TODO: extract transaction hash from response properly
		result.TxHash = []byte{} // Placeholder
	}

	return result, nil
}

// validateEnvelope validates an envelope before submission
func (os *OutputSubmitter) validateEnvelope(ctx context.Context, envelope *messaging.Envelope) error {
	if envelope == nil {
		return fmt.Errorf("envelope is nil")
	}

	// Use the L0 API client to validate
	_, err := os.client.Validate(ctx, envelope, v3.ValidateOptions{})
	if err != nil {
		return fmt.Errorf("envelope validation failed: %w", err)
	}

	return nil
}

// calculateEnvelopeCost calculates the cost of submitting an envelope
func (os *OutputSubmitter) calculateEnvelopeCost(envelope *messaging.Envelope) (uint64, error) {
	if os.creditManager == nil {
		return 0, fmt.Errorf("credit manager not available")
	}

	// Use the messaging envelope directly for cost calculation
	protocolEnvelope := envelope

	// Calculate cost using credit manager
	return os.creditManager.CalculateEnvelopeCost(protocolEnvelope)
}

// GetSubmissionStats returns statistics about submissions
func (os *OutputSubmitter) GetSubmissionStats() *SubmissionStats {
	os.mu.RLock()
	defer os.mu.RUnlock()

	return &SubmissionStats{
		Running:      os.running,
		WorkerCount:  os.workers,
		StagingStats: os.stager.GetStagingStats(),
	}
}

// SubmissionStats contains statistics about the submission process
type SubmissionStats struct {
	Running      bool          `json:"running"`
	WorkerCount  int           `json:"worker_count"`
	StagingStats *StagingStats `json:"staging_stats"`
}

// IsRunning returns whether the submitter is currently running
func (os *OutputSubmitter) IsRunning() bool {
	os.mu.RLock()
	defer os.mu.RUnlock()
	return os.running
}

// SetWorkerCount updates the number of submission workers
func (os *OutputSubmitter) SetWorkerCount(count int) error {
	if count <= 0 {
		return fmt.Errorf("worker count must be positive")
	}

	os.mu.Lock()
	defer os.mu.Unlock()

	os.config.WorkerCount = count
	return nil
}

// SetSubmissionInterval updates the submission interval
func (os *OutputSubmitter) SetSubmissionInterval(interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("submission interval must be positive")
	}

	os.mu.Lock()
	defer os.mu.Unlock()

	os.config.SubmissionInterval = interval
	return nil
}

// HealthCheck performs a health check on the submitter
func (os *OutputSubmitter) HealthCheck(ctx context.Context) error {
	// Check if client is healthy
	if err := os.client.IsHealthy(ctx); err != nil {
		return fmt.Errorf("L0 client unhealthy: %w", err)
	}

	// Check if submitter is running
	if !os.IsRunning() {
		return fmt.Errorf("submitter is not running")
	}

	// Check worker count
	os.mu.RLock()
	workerCount := os.workers
	os.mu.RUnlock()

	if workerCount == 0 {
		return fmt.Errorf("no workers running")
	}

	return nil
}

// Close gracefully shuts down the output submitter
func (os *OutputSubmitter) Close(ctx context.Context) error {
	if err := os.Stop(); err != nil {
		return fmt.Errorf("failed to stop submitter: %w", err)
	}

	return nil
}
