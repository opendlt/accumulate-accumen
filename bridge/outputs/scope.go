package outputs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
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

// AuthorityScope defines what L0 operations a contract is authorized to perform
type AuthorityScope struct {
	Version   int                 `json:"version"`
	Contract  string              `json:"contract"`  // Contract URL
	Paused    bool                `json:"paused"`    // If true, no operations allowed
	Allowed   AllowedOperations   `json:"allowed"`   // Allowed operation types and targets
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	ExpiresAt *time.Time          `json:"expires_at,omitempty"`
}

// AllowedOperations defines which L0 operations are permitted
type AllowedOperations struct {
	WriteData  []WriteDataPermission  `json:"write_data"`   // WriteData permissions
	SendTokens []SendTokensPermission `json:"send_tokens"`  // Token transfer permissions
	UpdateAuth []UpdateAuthPermission `json:"update_auth"`  // Authority update permissions
}

// WriteDataPermission defines permissions for WriteData operations
type WriteDataPermission struct {
	Target      string `json:"target"`       // Target account URL pattern (e.g., "acc://data.acme/*")
	MaxSize     uint64 `json:"max_size"`     // Maximum data size in bytes
	MaxPerBlock uint64 `json:"max_per_block"` // Maximum operations per block
	ContentType string `json:"content_type,omitempty"` // Allowed content type pattern
}

// SendTokensPermission defines permissions for token transfers
type SendTokensPermission struct {
	From        string `json:"from"`         // Source account URL pattern
	To          string `json:"to"`           // Destination account URL pattern
	TokenURL    string `json:"token_url"`    // Token type URL
	MaxAmount   uint64 `json:"max_amount"`   // Maximum amount per operation
	MaxPerBlock uint64 `json:"max_per_block"` // Maximum operations per block
}

// UpdateAuthPermission defines permissions for authority updates
type UpdateAuthPermission struct {
	Target      string `json:"target"`       // Target account URL pattern
	Operations  []string `json:"operations"`  // Allowed operations (add_authority, remove_authority, etc.)
	MaxPerBlock uint64 `json:"max_per_block"` // Maximum operations per block
}

// DefaultAuthorityScope returns a restrictive default scope
func DefaultAuthorityScope(contractURL string) *AuthorityScope {
	return &AuthorityScope{
		Version:   1,
		Contract:  contractURL,
		Paused:    false,
		Allowed:   AllowedOperations{}, // No operations allowed by default
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// FetchFromDN fetches an AuthorityScope document from the DN registry
func FetchFromDN(ctx context.Context, client *v3.Client, contractURL string) (*AuthorityScope, error) {
	if client == nil {
		return nil, fmt.Errorf("DN client is nil")
	}

	if contractURL == "" {
		return nil, fmt.Errorf("contract URL cannot be empty")
	}

	// Construct DN registry URL for authority scope
	// Format: acc://dn.acme/registry/authority-scopes/{contract-hash}
	contractHash := hashContractURL(contractURL)
	scopeURL := fmt.Sprintf("acc://dn.acme/registry/authority-scopes/%s", contractHash)

	// Query the DN for the authority scope
	resp, err := client.Query(ctx, scopeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query authority scope from DN: %w", err)
	}

	// Check if the scope exists
	if resp.Type == "unknown" {
		// Return default restrictive scope if not found
		return DefaultAuthorityScope(contractURL), nil
	}

	// Parse the scope data
	var scope AuthorityScope
	if err := json.Unmarshal(resp.Data, &scope); err != nil {
		return nil, fmt.Errorf("failed to parse authority scope JSON: %w", err)
	}

	// Validate the scope
	if err := validateAuthorityScope(&scope, contractURL); err != nil {
		return nil, fmt.Errorf("invalid authority scope: %w", err)
	}

	// Check expiration
	if scope.ExpiresAt != nil && time.Now().UTC().After(*scope.ExpiresAt) {
		return DefaultAuthorityScope(contractURL), nil
	}

	return &scope, nil
}

// validateAuthorityScope performs basic validation on an authority scope
func validateAuthorityScope(scope *AuthorityScope, expectedContract string) error {
	if scope.Contract != expectedContract {
		return fmt.Errorf("scope contract mismatch: expected %s, got %s", expectedContract, scope.Contract)
	}

	if scope.Version < 1 {
		return fmt.Errorf("invalid scope version: %d", scope.Version)
	}

	// Validate WriteData permissions
	for i, perm := range scope.Allowed.WriteData {
		if perm.Target == "" {
			return fmt.Errorf("WriteData permission %d: target cannot be empty", i)
		}
		if perm.MaxSize == 0 {
			return fmt.Errorf("WriteData permission %d: max_size must be positive", i)
		}
	}

	// Validate SendTokens permissions
	for i, perm := range scope.Allowed.SendTokens {
		if perm.From == "" || perm.To == "" {
			return fmt.Errorf("SendTokens permission %d: from and to cannot be empty", i)
		}
		if perm.TokenURL == "" {
			return fmt.Errorf("SendTokens permission %d: token_url cannot be empty", i)
		}
		if perm.MaxAmount == 0 {
			return fmt.Errorf("SendTokens permission %d: max_amount must be positive", i)
		}
	}

	// Validate UpdateAuth permissions
	for i, perm := range scope.Allowed.UpdateAuth {
		if perm.Target == "" {
			return fmt.Errorf("UpdateAuth permission %d: target cannot be empty", i)
		}
		if len(perm.Operations) == 0 {
			return fmt.Errorf("UpdateAuth permission %d: operations cannot be empty", i)
		}
	}

	return nil
}

// hashContractURL creates a deterministic hash for a contract URL
func hashContractURL(contractURL string) string {
	// Simple hash based on contract URL
	// In production, would use proper cryptographic hash
	hash := 0
	for _, c := range contractURL {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%08x", uint32(hash))
}

// IsAllowed checks if the scope is active (not paused and not expired)
func (scope *AuthorityScope) IsAllowed() bool {
	if scope.Paused {
		return false
	}

	if scope.ExpiresAt != nil && time.Now().UTC().After(*scope.ExpiresAt) {
		return false
	}

	return true
}

// HasWriteDataPermission checks if WriteData operation is allowed for a target
func (scope *AuthorityScope) HasWriteDataPermission(target string, size uint64) *WriteDataPermission {
	if !scope.IsAllowed() {
		return nil
	}

	for _, perm := range scope.Allowed.WriteData {
		if matchesPattern(target, perm.Target) && size <= perm.MaxSize {
			return &perm
		}
	}

	return nil
}

// HasSendTokensPermission checks if SendTokens operation is allowed
func (scope *AuthorityScope) HasSendTokensPermission(from, to, tokenURL string, amount uint64) *SendTokensPermission {
	if !scope.IsAllowed() {
		return nil
	}

	for _, perm := range scope.Allowed.SendTokens {
		if matchesPattern(from, perm.From) &&
		   matchesPattern(to, perm.To) &&
		   matchesPattern(tokenURL, perm.TokenURL) &&
		   amount <= perm.MaxAmount {
			return &perm
		}
	}

	return nil
}

// HasUpdateAuthPermission checks if UpdateAuth operation is allowed
func (scope *AuthorityScope) HasUpdateAuthPermission(target, operation string) *UpdateAuthPermission {
	if !scope.IsAllowed() {
		return nil
	}

	for _, perm := range scope.Allowed.UpdateAuth {
		if matchesPattern(target, perm.Target) {
			for _, allowedOp := range perm.Operations {
				if operation == allowedOp {
					return &perm
				}
			}
		}
	}

	return nil
}

// matchesPattern performs simple pattern matching with wildcard support
func matchesPattern(value, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if pattern == value {
		return true
	}

	// Simple prefix matching with *
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	return false
}

// SetPaused updates the paused state of a contract's authority scope on DN
func SetPaused(ctx context.Context, client *v3.Client, dnURL, contractURL *url.URL, paused bool) error {
	if client == nil {
		return fmt.Errorf("DN client is nil")
	}

	if dnURL == nil {
		return fmt.Errorf("DN URL cannot be nil")
	}

	if contractURL == nil {
		return fmt.Errorf("contract URL cannot be nil")
	}

	// Construct DN registry path for authority scope
	contractHash := hashContractURL(contractURL.String())
	scopePath := fmt.Sprintf("registry/authority-scopes/%s", contractHash)
	scopeURL, err := dnURL.Parse(scopePath)
	if err != nil {
		return fmt.Errorf("failed to construct scope URL: %w", err)
	}

	// Fetch current scope to get version for atomic update
	currentScope, err := FetchFromDN(ctx, client, contractURL.String())
	if err != nil {
		return fmt.Errorf("failed to fetch current authority scope: %w", err)
	}

	// Update the scope with new paused state and version bump
	updatedScope := *currentScope
	updatedScope.Paused = paused
	updatedScope.Version++
	updatedScope.UpdatedAt = time.Now().UTC()

	// Marshal the updated scope
	scopeData, err := json.Marshal(updatedScope)
	if err != nil {
		return fmt.Errorf("failed to marshal authority scope: %w", err)
	}

	// Create WriteData transaction to update the scope
	memo := fmt.Sprintf("Update contract pause status: %s -> %t", contractURL.String(), paused)
	envelope := l0api.BuildWriteData(scopeURL, scopeData, memo, map[string]interface{}{
		"action":     "set_paused",
		"contract":   contractURL.String(),
		"paused":     paused,
		"version":    updatedScope.Version,
		"timestamp":  updatedScope.UpdatedAt.Unix(),
	})

	// Submit the transaction
	result, err := client.Submit(ctx, envelope.Done())
	if err != nil {
		return fmt.Errorf("failed to submit authority scope update: %w", err)
	}

	if result.Code != 0 {
		return fmt.Errorf("authority scope update failed with code %d: %s", result.Code, result.Message)
	}

	return nil
}
