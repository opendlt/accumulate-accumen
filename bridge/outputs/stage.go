package outputs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

// StagedOutput represents an output that has been staged for submission
type StagedOutput struct {
	ID           string              `json:"id"`
	Envelope     *messaging.Envelope `json:"envelope"`
	Status       StageStatus         `json:"status"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	SubmittedAt  *time.Time          `json:"submitted_at,omitempty"`
	Metadata     *OutputMetadata     `json:"metadata"`
	Dependencies []string            `json:"dependencies,omitempty"`
	Priority     int                 `json:"priority"`
}

// StageStatus represents the status of a staged output
type StageStatus int

const (
	StageStatusPending StageStatus = iota
	StageStatusReady
	StageStatusSubmitting
	StageStatusSubmitted
	StageStatusFailed
	StageStatusExpired
)

// String returns the string representation of a stage status
func (s StageStatus) String() string {
	switch s {
	case StageStatusPending:
		return "pending"
	case StageStatusReady:
		return "ready"
	case StageStatusSubmitting:
		return "submitting"
	case StageStatusSubmitted:
		return "submitted"
	case StageStatusFailed:
		return "failed"
	case StageStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// OutputMetadata contains metadata for a staged output
type OutputMetadata struct {
	// Source information
	SequencerID string `json:"sequencer_id"`
	BlockHeight uint64 `json:"block_height"`
	OutputIndex uint64 `json:"output_index"`

	// Timing information
	ExpiresAt  time.Time `json:"expires_at"`
	RetryCount int       `json:"retry_count"`
	MaxRetries int       `json:"max_retries"`

	// Cost information
	EstimatedCost uint64 `json:"estimated_cost"`
	ActualCost    uint64 `json:"actual_cost,omitempty"`

	// Error information
	LastError string    `json:"last_error,omitempty"`
	ErrorAt   time.Time `json:"error_at,omitempty"`
}

// OutputStager manages the staging of outputs for submission to Accumulate L0
type OutputStager struct {
	mu       sync.RWMutex
	staged   map[string]*StagedOutput
	config   *StagerConfig
	sequence uint64
}

// StagerConfig defines configuration for the output stager
type StagerConfig struct {
	// Maximum number of staged outputs
	MaxStaged int
	// Default expiration time for staged outputs
	DefaultTTL time.Duration
	// Maximum retry attempts
	MaxRetries int
	// Cleanup interval for expired outputs
	CleanupInterval time.Duration
}

// DefaultStagerConfig returns a default stager configuration
func DefaultStagerConfig() *StagerConfig {
	return &StagerConfig{
		MaxStaged:       10000,
		DefaultTTL:      24 * time.Hour,
		MaxRetries:      3,
		CleanupInterval: time.Hour,
	}
}

// NewOutputStager creates a new output stager
func NewOutputStager(config *StagerConfig) *OutputStager {
	if config == nil {
		config = DefaultStagerConfig()
	}

	stager := &OutputStager{
		staged:   make(map[string]*StagedOutput),
		config:   config,
		sequence: 0,
	}

	// Start cleanup routine
	go stager.cleanupRoutine()

	return stager
}

// StageOutput stages an envelope for submission
func (os *OutputStager) StageOutput(ctx context.Context, envelope *messaging.Envelope, metadata *OutputMetadata) (*StagedOutput, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	os.mu.Lock()
	defer os.mu.Unlock()

	// Check if we're at capacity
	if len(os.staged) >= os.config.MaxStaged {
		return nil, fmt.Errorf("staging capacity exceeded: %d/%d", len(os.staged), os.config.MaxStaged)
	}

	// Generate unique ID
	os.sequence++
	id := fmt.Sprintf("output-%d-%d", time.Now().Unix(), os.sequence)

	// Set default metadata if not provided
	if metadata == nil {
		metadata = &OutputMetadata{}
	}

	if metadata.MaxRetries == 0 {
		metadata.MaxRetries = os.config.MaxRetries
	}

	if metadata.ExpiresAt.IsZero() {
		metadata.ExpiresAt = time.Now().Add(os.config.DefaultTTL)
	}

	// Create staged output
	staged := &StagedOutput{
		ID:        id,
		Envelope:  envelope,
		Status:    StageStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  metadata,
		Priority:  0, // Default priority
	}

	// Store in staging area
	os.staged[id] = staged

	return staged, nil
}

// GetStagedOutput retrieves a staged output by ID
func (os *OutputStager) GetStagedOutput(id string) (*StagedOutput, error) {
	os.mu.RLock()
	defer os.mu.RUnlock()

	staged, exists := os.staged[id]
	if !exists {
		return nil, fmt.Errorf("staged output not found: %s", id)
	}

	return staged, nil
}

// UpdateOutputStatus updates the status of a staged output
func (os *OutputStager) UpdateOutputStatus(id string, status StageStatus) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	staged, exists := os.staged[id]
	if !exists {
		return fmt.Errorf("staged output not found: %s", id)
	}

	staged.Status = status
	staged.UpdatedAt = time.Now()

	if status == StageStatusSubmitted {
		now := time.Now()
		staged.SubmittedAt = &now
	}

	return nil
}

// GetReadyOutputs returns outputs that are ready for submission
func (os *OutputStager) GetReadyOutputs() []*StagedOutput {
	os.mu.RLock()
	defer os.mu.RUnlock()

	var ready []*StagedOutput

	for _, staged := range os.staged {
		if staged.Status == StageStatusReady || staged.Status == StageStatusPending {
			// Check if dependencies are satisfied
			if os.areDependenciesSatisfied(staged) {
				ready = append(ready, staged)
			}
		}
	}

	return ready
}

// areDependenciesSatisfied checks if all dependencies for an output are satisfied
func (os *OutputStager) areDependenciesSatisfied(staged *StagedOutput) bool {
	for _, depID := range staged.Dependencies {
		dep, exists := os.staged[depID]
		if !exists || dep.Status != StageStatusSubmitted {
			return false
		}
	}
	return true
}

// MarkOutputFailed marks an output as failed with error information
func (os *OutputStager) MarkOutputFailed(id string, err error) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	staged, exists := os.staged[id]
	if !exists {
		return fmt.Errorf("staged output not found: %s", id)
	}

	staged.Status = StageStatusFailed
	staged.UpdatedAt = time.Now()
	staged.Metadata.RetryCount++

	if err != nil {
		staged.Metadata.LastError = err.Error()
		staged.Metadata.ErrorAt = time.Now()
	}

	// Check if we should retry
	if staged.Metadata.RetryCount < staged.Metadata.MaxRetries {
		staged.Status = StageStatusPending // Reset for retry
	}

	return nil
}

// RemoveStagedOutput removes a staged output from the stager
func (os *OutputStager) RemoveStagedOutput(id string) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	if _, exists := os.staged[id]; !exists {
		return fmt.Errorf("staged output not found: %s", id)
	}

	delete(os.staged, id)
	return nil
}

// GetStagingStats returns statistics about staged outputs
func (os *OutputStager) GetStagingStats() *StagingStats {
	os.mu.RLock()
	defer os.mu.RUnlock()

	stats := &StagingStats{
		Total: len(os.staged),
	}

	for _, staged := range os.staged {
		switch staged.Status {
		case StageStatusPending:
			stats.Pending++
		case StageStatusReady:
			stats.Ready++
		case StageStatusSubmitting:
			stats.Submitting++
		case StageStatusSubmitted:
			stats.Submitted++
		case StageStatusFailed:
			stats.Failed++
		case StageStatusExpired:
			stats.Expired++
		}
	}

	return stats
}

// StagingStats contains statistics about staged outputs
type StagingStats struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	Ready      int `json:"ready"`
	Submitting int `json:"submitting"`
	Submitted  int `json:"submitted"`
	Failed     int `json:"failed"`
	Expired    int `json:"expired"`
}

// cleanupRoutine periodically cleans up expired outputs
func (os *OutputStager) cleanupRoutine() {
	ticker := time.NewTicker(os.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		os.cleanup()
	}
}

// cleanup removes expired and old submitted outputs
func (os *OutputStager) cleanup() {
	os.mu.Lock()
	defer os.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for id, staged := range os.staged {
		// Remove expired outputs
		if now.After(staged.Metadata.ExpiresAt) {
			staged.Status = StageStatusExpired
			toRemove = append(toRemove, id)
			continue
		}

		// Remove old submitted outputs (after 1 hour)
		if staged.Status == StageStatusSubmitted &&
			staged.SubmittedAt != nil &&
			now.Sub(*staged.SubmittedAt) > time.Hour {
			toRemove = append(toRemove, id)
			continue
		}

		// Remove failed outputs that have exhausted retries
		if staged.Status == StageStatusFailed &&
			staged.Metadata.RetryCount >= staged.Metadata.MaxRetries {
			toRemove = append(toRemove, id)
		}
	}

	// Remove identified outputs
	for _, id := range toRemove {
		delete(os.staged, id)
	}
}

// Close shuts down the output stager
func (os *OutputStager) Close() error {
	os.mu.Lock()
	defer os.mu.Unlock()

	// Clear all staged outputs
	os.staged = make(map[string]*StagedOutput)
	return nil
}

// SetDependency adds a dependency relationship between outputs
func (os *OutputStager) SetDependency(outputID, dependsOnID string) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	output, exists := os.staged[outputID]
	if !exists {
		return fmt.Errorf("output not found: %s", outputID)
	}

	if _, exists := os.staged[dependsOnID]; !exists {
		return fmt.Errorf("dependency not found: %s", dependsOnID)
	}

	// Add dependency if not already present
	for _, dep := range output.Dependencies {
		if dep == dependsOnID {
			return nil // Already exists
		}
	}

	output.Dependencies = append(output.Dependencies, dependsOnID)
	return nil
}

// SetPriority sets the priority of a staged output
func (os *OutputStager) SetPriority(id string, priority int) error {
	os.mu.Lock()
	defer os.mu.Unlock()

	staged, exists := os.staged[id]
	if !exists {
		return fmt.Errorf("staged output not found: %s", id)
	}

	staged.Priority = priority
	staged.UpdatedAt = time.Now()
	return nil
}
