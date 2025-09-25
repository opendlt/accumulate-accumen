package outputs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
)

// Scope represents a transaction execution scope for output management
type Scope struct {
	mu       sync.RWMutex
	txID     string
	chainID  string
	height   uint64
	outputs  map[string]*Output
	state    ScopeState
	created  time.Time
	updated  time.Time
	limits   *Limits
	metadata map[string]interface{}
}

// ScopeState represents the current state of a scope
type ScopeState int

const (
	ScopeStateUnknown ScopeState = iota
	ScopeStateActive
	ScopeStateStaging
	ScopeStateFinalizing
	ScopeStateFinalized
	ScopeStateFailed
	ScopeStateExpired
)

// String returns the string representation of the scope state
func (s ScopeState) String() string {
	switch s {
	case ScopeStateActive:
		return "active"
	case ScopeStateStaging:
		return "staging"
	case ScopeStateFinalizing:
		return "finalizing"
	case ScopeStateFinalized:
		return "finalized"
	case ScopeStateFailed:
		return "failed"
	case ScopeStateExpired:
		return "expired"
	default:
		return "unknown"
	}
}

// ScopeConfig defines configuration for scope creation
type ScopeConfig struct {
	TxID       string
	ChainID    string
	Height     uint64
	Limits     *Limits
	Metadata   map[string]interface{}
	TTL        time.Duration
	MaxOutputs int
}

// NewScope creates a new transaction scope
func NewScope(config *ScopeConfig) (*Scope, error) {
	if config.TxID == "" {
		return nil, fmt.Errorf("transaction ID is required")
	}

	if config.ChainID == "" {
		return nil, fmt.Errorf("chain ID is required")
	}

	limits := config.Limits
	if limits == nil {
		limits = DefaultLimits()
	}

	now := time.Now()
	scope := &Scope{
		txID:     config.TxID,
		chainID:  config.ChainID,
		height:   config.Height,
		outputs:  make(map[string]*Output),
		state:    ScopeStateActive,
		created:  now,
		updated:  now,
		limits:   limits,
		metadata: make(map[string]interface{}),
	}

	if config.Metadata != nil {
		for k, v := range config.Metadata {
			scope.metadata[k] = v
		}
	}

	if config.TTL > 0 {
		scope.metadata["ttl"] = config.TTL
		scope.metadata["expires_at"] = now.Add(config.TTL)
	}

	if config.MaxOutputs > 0 {
		scope.metadata["max_outputs"] = config.MaxOutputs
	}

	return scope, nil
}

// AddOutput adds an output to the scope
func (s *Scope) AddOutput(output *Output) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != ScopeStateActive {
		return fmt.Errorf("cannot add output to scope in state %s", s.state)
	}

	// Check limits
	if maxOutputs, ok := s.metadata["max_outputs"].(int); ok {
		if len(s.outputs) >= maxOutputs {
			return fmt.Errorf("scope output limit reached: %d", maxOutputs)
		}
	}

	// Validate output against scope limits
	if err := s.limits.ValidateOutput(output); err != nil {
		return fmt.Errorf("output validation failed: %w", err)
	}

	// Check for duplicate output IDs
	if _, exists := s.outputs[output.ID]; exists {
		return fmt.Errorf("output with ID %s already exists", output.ID)
	}

	s.outputs[output.ID] = output
	s.updated = time.Now()

	return nil
}

// GetOutput retrieves an output by ID
func (s *Scope) GetOutput(outputID string) (*Output, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	output, exists := s.outputs[outputID]
	return output, exists
}

// ListOutputs returns all outputs in the scope
func (s *Scope) ListOutputs() []*Output {
	s.mu.RLock()
	defer s.mu.RUnlock()

	outputs := make([]*Output, 0, len(s.outputs))
	for _, output := range s.outputs {
		outputs = append(outputs, output)
	}

	return outputs
}

// RemoveOutput removes an output from the scope
func (s *Scope) RemoveOutput(outputID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == ScopeStateFinalized {
		return fmt.Errorf("cannot remove output from finalized scope")
	}

	if _, exists := s.outputs[outputID]; !exists {
		return fmt.Errorf("output %s not found", outputID)
	}

	delete(s.outputs, outputID)
	s.updated = time.Now()

	return nil
}

// GetState returns the current scope state
func (s *Scope) GetState() ScopeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetState updates the scope state
func (s *Scope) SetState(state ScopeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate state transitions
	if !s.isValidStateTransition(s.state, state) {
		return fmt.Errorf("invalid state transition from %s to %s", s.state, state)
	}

	s.state = state
	s.updated = time.Now()

	return nil
}

// isValidStateTransition checks if a state transition is valid
func (s *Scope) isValidStateTransition(from, to ScopeState) bool {
	switch from {
	case ScopeStateActive:
		return to == ScopeStateStaging || to == ScopeStateFailed || to == ScopeStateExpired
	case ScopeStateStaging:
		return to == ScopeStateFinalizing || to == ScopeStateFailed || to == ScopeStateExpired
	case ScopeStateFinalizing:
		return to == ScopeStateFinalized || to == ScopeStateFailed
	case ScopeStateFinalized:
		return false // Final state
	case ScopeStateFailed:
		return to == ScopeStateActive // Allow retry
	case ScopeStateExpired:
		return false // Final state
	default:
		return false
	}
}

// GetInfo returns basic information about the scope
func (s *Scope) GetInfo() ScopeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ScopeInfo{
		TxID:        s.txID,
		ChainID:     s.chainID,
		Height:      s.height,
		State:       s.state,
		OutputCount: len(s.outputs),
		Created:     s.created,
		Updated:     s.updated,
		Metadata:    s.copyMetadata(),
	}
}

// ScopeInfo contains basic scope information
type ScopeInfo struct {
	TxID        string                 `json:"tx_id"`
	ChainID     string                 `json:"chain_id"`
	Height      uint64                 `json:"height"`
	State       ScopeState             `json:"state"`
	OutputCount int                    `json:"output_count"`
	Created     time.Time              `json:"created"`
	Updated     time.Time              `json:"updated"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// IsExpired checks if the scope has expired based on TTL
func (s *Scope) IsExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if expiresAt, ok := s.metadata["expires_at"].(time.Time); ok {
		return time.Now().After(expiresAt)
	}

	return false
}

// CanFinalize checks if the scope can be finalized
func (s *Scope) CanFinalize() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state != ScopeStateStaging {
		return false
	}

	// Check if all outputs are ready
	for _, output := range s.outputs {
		if output.Status != OutputStatusReady {
			return false
		}
	}

	return true
}

// GetLimits returns the scope limits
func (s *Scope) GetLimits() *Limits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.limits
}

// SetMetadata sets a metadata value
func (s *Scope) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metadata[key] = value
	s.updated = time.Now()
}

// GetMetadata gets a metadata value
func (s *Scope) GetMetadata(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, exists := s.metadata[key]
	return value, exists
}

// copyMetadata creates a copy of the metadata map
func (s *Scope) copyMetadata() map[string]interface{} {
	metadata := make(map[string]interface{})
	for k, v := range s.metadata {
		metadata[k] = v
	}
	return metadata
}

// ToEnvelope converts scope outputs to Accumulate envelopes
func (s *Scope) ToEnvelope(ctx context.Context) ([]*messaging.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state != ScopeStateFinalized {
		return nil, fmt.Errorf("scope must be finalized before creating envelope")
	}

	var envelopes []*messaging.Envelope
	for _, output := range s.outputs {
		envelope, err := output.ToEnvelope(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create envelope for output %s: %w", output.ID, err)
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

// Cleanup releases resources associated with the scope
func (s *Scope) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up all outputs
	for _, output := range s.outputs {
		if err := output.Cleanup(); err != nil {
			return fmt.Errorf("failed to cleanup output %s: %w", output.ID, err)
		}
	}

	// Clear outputs map
	s.outputs = make(map[string]*Output)
	s.state = ScopeStateExpired
	s.updated = time.Now()

	return nil
}