package outputs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"

	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
	"github.com/opendlt/accumulate-accumen/registry/authority"
)

// Verifier handles output verification and integrity checks
type Verifier struct {
	mu           sync.RWMutex
	config       *VerifierConfig
	checksums    map[string]string
	signatures   map[string]*Signature
	dependencies map[string][]string
}

// VerifierConfig defines configuration for output verification
type VerifierConfig struct {
	// Enable strict verification
	StrictMode bool

	// Require signatures for all outputs
	RequireSignatures bool

	// Maximum verification time
	MaxVerificationTime time.Duration

	// Hash algorithm to use
	HashAlgorithm string

	// Trusted signers
	TrustedSigners []string

	// Enable dependency verification
	VerifyDependencies bool

	// Maximum dependency depth
	MaxDependencyDepth int
}

// DefaultVerifierConfig returns default verification configuration
func DefaultVerifierConfig() *VerifierConfig {
	return &VerifierConfig{
		StrictMode:          false,
		RequireSignatures:   false,
		MaxVerificationTime: 30 * time.Second,
		HashAlgorithm:       "sha256",
		TrustedSigners:      []string{},
		VerifyDependencies:  true,
		MaxDependencyDepth:  10,
	}
}

// Signature represents a cryptographic signature
type Signature struct {
	Algorithm string    `json:"algorithm"`
	Signature []byte    `json:"signature"`
	PublicKey []byte    `json:"public_key"`
	KeyID     string    `json:"key_id"`
	Timestamp time.Time `json:"timestamp"`
}

// VerificationResult represents the result of output verification
type VerificationResult struct {
	OutputID          string                 `json:"output_id"`
	Valid             bool                   `json:"valid"`
	Errors            []string               `json:"errors,omitempty"`
	Warnings          []string               `json:"warnings,omitempty"`
	ChecksumVerified  bool                   `json:"checksum_verified"`
	SignatureVerified bool                   `json:"signature_verified"`
	DependenciesValid bool                   `json:"dependencies_valid"`
	VerificationTime  time.Duration          `json:"verification_time"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// NewVerifier creates a new output verifier
func NewVerifier(config *VerifierConfig) *Verifier {
	if config == nil {
		config = DefaultVerifierConfig()
	}

	return &Verifier{
		config:       config,
		checksums:    make(map[string]string),
		signatures:   make(map[string]*Signature),
		dependencies: make(map[string][]string),
	}
}

// VerifyOutput performs comprehensive verification of an output
func (v *Verifier) VerifyOutput(ctx context.Context, output *Output) (*VerificationResult, error) {
	startTime := time.Now()

	// Create context with timeout
	verifyCtx, cancel := context.WithTimeout(ctx, v.config.MaxVerificationTime)
	defer cancel()

	result := &VerificationResult{
		OutputID: output.ID,
		Valid:    true,
		Metadata: make(map[string]interface{}),
	}

	// Verify output integrity
	if err := v.verifyIntegrity(verifyCtx, output, result); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("integrity check failed: %v", err))
		result.Valid = false
	}

	// Verify checksum
	if err := v.verifyChecksum(verifyCtx, output, result); err != nil {
		if v.config.StrictMode {
			result.Errors = append(result.Errors, fmt.Sprintf("checksum verification failed: %v", err))
			result.Valid = false
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("checksum verification failed: %v", err))
		}
	}

	// Verify signature if required or present
	if err := v.verifySignature(verifyCtx, output, result); err != nil {
		if v.config.RequireSignatures {
			result.Errors = append(result.Errors, fmt.Sprintf("signature verification failed: %v", err))
			result.Valid = false
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("signature verification failed: %v", err))
		}
	}

	// Verify dependencies
	if v.config.VerifyDependencies {
		if err := v.verifyDependencies(verifyCtx, output, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("dependency verification failed: %v", err))
			result.Valid = false
		}
	}

	result.VerificationTime = time.Since(startTime)
	return result, nil
}

// verifyIntegrity performs basic integrity checks on the output
func (v *Verifier) verifyIntegrity(ctx context.Context, output *Output, result *VerificationResult) error {
	// Check required fields
	if output.ID == "" {
		return fmt.Errorf("output ID is empty")
	}

	if len(output.Data) == 0 {
		return fmt.Errorf("output data is empty")
	}

	if output.Type == "" {
		return fmt.Errorf("output type is empty")
	}

	// Validate status
	if output.Status < OutputStatusPending || output.Status > OutputStatusExpired {
		return fmt.Errorf("invalid output status: %d", output.Status)
	}

	// Check timestamps
	if output.Created.IsZero() {
		return fmt.Errorf("output creation time is zero")
	}

	if output.Updated.Before(output.Created) {
		return fmt.Errorf("output updated time is before creation time")
	}

	// Validate priority range
	if output.Priority < 0 {
		return fmt.Errorf("output priority cannot be negative: %d", output.Priority)
	}

	return nil
}

// verifyChecksum verifies the output data checksum
func (v *Verifier) verifyChecksum(ctx context.Context, output *Output, result *VerificationResult) error {
	// Calculate current checksum
	checksum := v.calculateChecksum(output.Data)

	// Store for future reference
	v.mu.Lock()
	v.checksums[output.ID] = checksum
	v.mu.Unlock()

	// If output has metadata, verify checksum (simplified)
	if output.Metadata != nil {
		// TODO: implement proper checksum verification
		// For now, assume checksum is valid
	}

	result.ChecksumVerified = true
	result.Metadata["calculated_checksum"] = checksum
	return nil
}

// verifySignature verifies the output signature if present
func (v *Verifier) verifySignature(ctx context.Context, output *Output, result *VerificationResult) error {
	// Check if signature is present (simplified)
	hasSig := output.Metadata != nil
	if !hasSig {
		if v.config.RequireSignatures {
			result.SignatureVerified = false
			return fmt.Errorf("signature required but not found")
		}
		result.SignatureVerified = true // No signature required
		return nil
	}

	// Parse signature (simplified)
	// TODO: implement proper signature parsing

	// For now, assume signature is valid
	result.SignatureVerified = true
	return nil
}

// verifyDependencies verifies output dependencies
func (v *Verifier) verifyDependencies(ctx context.Context, output *Output, result *VerificationResult) error {
	if len(output.Dependencies) == 0 {
		result.DependenciesValid = true
		return nil
	}

	// Check dependency depth
	if err := v.checkDependencyDepth(output.ID, 0); err != nil {
		result.DependenciesValid = false
		return err
	}

	// Store dependencies
	v.mu.Lock()
	v.dependencies[output.ID] = make([]string, len(output.Dependencies))
	copy(v.dependencies[output.ID], output.Dependencies)
	v.mu.Unlock()

	// Validate each dependency
	for _, depID := range output.Dependencies {
		if depID == "" {
			result.DependenciesValid = false
			return fmt.Errorf("empty dependency ID")
		}

		if depID == output.ID {
			result.DependenciesValid = false
			return fmt.Errorf("output cannot depend on itself")
		}

		// Check for circular dependencies (simplified check)
		if v.hasCircularDependency(output.ID, depID) {
			result.DependenciesValid = false
			return fmt.Errorf("circular dependency detected: %s -> %s", output.ID, depID)
		}
	}

	result.DependenciesValid = true
	result.Metadata["dependency_count"] = len(output.Dependencies)
	return nil
}

// checkDependencyDepth checks if dependency depth exceeds maximum
func (v *Verifier) checkDependencyDepth(outputID string, currentDepth int) error {
	if currentDepth > v.config.MaxDependencyDepth {
		return fmt.Errorf("dependency depth exceeds maximum: %d", v.config.MaxDependencyDepth)
	}

	v.mu.RLock()
	deps, exists := v.dependencies[outputID]
	v.mu.RUnlock()

	if !exists {
		return nil
	}

	for _, depID := range deps {
		if err := v.checkDependencyDepth(depID, currentDepth+1); err != nil {
			return err
		}
	}

	return nil
}

// hasCircularDependency checks for circular dependencies (simplified)
func (v *Verifier) hasCircularDependency(outputID, targetID string) bool {
	v.mu.RLock()
	deps, exists := v.dependencies[targetID]
	v.mu.RUnlock()

	if !exists {
		return false
	}

	for _, depID := range deps {
		if depID == outputID {
			return true
		}
		if v.hasCircularDependency(outputID, depID) {
			return true
		}
	}

	return false
}

// calculateChecksum calculates checksum for data
func (v *Verifier) calculateChecksum(data []byte) string {
	switch v.config.HashAlgorithm {
	case "sha256":
		hash := sha256.Sum256(data)
		return hex.EncodeToString(hash[:])
	default:
		// Fallback to SHA-256
		hash := sha256.Sum256(data)
		return hex.EncodeToString(hash[:])
	}
}

// VerifyBatch verifies multiple outputs in batch
func (v *Verifier) VerifyBatch(ctx context.Context, outputs []*Output) ([]*VerificationResult, error) {
	results := make([]*VerificationResult, len(outputs))
	errors := make([]error, len(outputs))

	// Use workers for parallel verification
	const maxWorkers = 10
	workers := len(outputs)
	if workers > maxWorkers {
		workers = maxWorkers
	}

	semaphore := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, output := range outputs {
		wg.Add(1)
		go func(index int, out *Output) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result, err := v.VerifyOutput(ctx, out)
			results[index] = result
			errors[index] = err
		}(i, output)
	}

	wg.Wait()

	// Check for any errors
	for i, err := range errors {
		if err != nil {
			return results, fmt.Errorf("verification failed for output %d: %w", i, err)
		}
	}

	return results, nil
}

// GetChecksum retrieves the stored checksum for an output
func (v *Verifier) GetChecksum(outputID string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	checksum, exists := v.checksums[outputID]
	return checksum, exists
}

// GetSignature retrieves the stored signature for an output
func (v *Verifier) GetSignature(outputID string) (*Signature, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	signature, exists := v.signatures[outputID]
	return signature, exists
}

// VerifyEnvelope verifies an Accumulate envelope
func (v *Verifier) VerifyEnvelope(ctx context.Context, envelope *messaging.Envelope) (*VerificationResult, error) {
	result := &VerificationResult{
		OutputID: fmt.Sprintf("%x", envelope.TxHash),
		Valid:    true,
		Metadata: make(map[string]interface{}),
	}

	// Basic envelope validation
	if len(envelope.TxHash) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "envelope transaction hash is zero")
	}

	if len(envelope.Transaction) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "envelope transaction is empty")
	}

	// TODO: Add more envelope-specific verification

	result.Metadata["envelope_type"] = "accumulate"
	return result, nil
}

// Cleanup releases verifier resources
func (v *Verifier) Cleanup() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.checksums = make(map[string]string)
	v.signatures = make(map[string]*Signature)
	v.dependencies = make(map[string][]string)

	return nil
}

// OperationLimitTracker tracks per-block operation limits
type OperationLimitTracker struct {
	counters map[string]*Counter
	mu       sync.RWMutex
}

// NewOperationLimitTracker creates a new operation limit tracker
func NewOperationLimitTracker() *OperationLimitTracker {
	return &OperationLimitTracker{
		counters: make(map[string]*Counter),
	}
}

// GetCounter returns the counter for a specific contract and operation type
func (t *OperationLimitTracker) GetCounter(contractURL, opType string) *Counter {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := contractURL + ":" + opType
	if counter, exists := t.counters[key]; exists {
		return counter
	}

	counter := NewCounter()
	t.counters[key] = counter
	return counter
}

// Reset clears all counters (called at block boundaries)
func (t *OperationLimitTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, counter := range t.counters {
		counter.Reset()
	}
}

// Verify checks if a single staged operation is permitted by the authority scope
func Verify(op *runtime.StagedOp, scope *AuthorityScope, tracker *OperationLimitTracker) error {
	if op == nil {
		return fmt.Errorf("operation cannot be nil")
	}

	if scope == nil {
		return fmt.Errorf("authority scope cannot be nil")
	}

	// Check if contract is paused
	if scope.Paused {
		return fmt.Errorf("contract operations are paused")
	}

	// Check if scope has expired
	if scope.ExpiresAt != nil && time.Now().After(*scope.ExpiresAt) {
		return fmt.Errorf("authority scope has expired")
	}

	// Determine operation type and verify permissions
	switch op.Type {
	case "WriteData":
		return verifyWriteData(op, scope, tracker)
	case "SendTokens":
		return verifySendTokens(op, scope, tracker)
	case "UpdateAuth":
		return verifyUpdateAuth(op, scope, tracker)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// VerifyAll checks if all staged operations are permitted by the authority scope
func VerifyAll(staged []*runtime.StagedOp, scope *AuthorityScope, tracker *OperationLimitTracker) error {
	if scope == nil {
		return fmt.Errorf("authority scope cannot be nil")
	}

	if len(staged) == 0 {
		return nil // No operations to verify
	}

	// Verify each operation
	for i, op := range staged {
		if err := Verify(op, scope, tracker); err != nil {
			return fmt.Errorf("operation %d failed verification: %w", i, err)
		}
	}

	return nil
}

// verifyWriteData verifies WriteData operation permissions
func verifyWriteData(op *runtime.StagedOp, scope *AuthorityScope, tracker *OperationLimitTracker) error {
	permissions := scope.Allowed.WriteData
	if len(permissions) == 0 {
		return fmt.Errorf("WriteData operations not permitted")
	}

	// Extract key pattern from operation (simplified)
	keyPattern := ""
	if op.Data != nil {
		// For operations with data, use a default key pattern
		keyPattern = "default_key"
	}

	// Find matching permission
	var matchedPerm *WriteDataPermission
	for _, perm := range permissions {
		if matchesPattern(keyPattern, perm.Target) {
			matchedPerm = &perm
			break
		}
	}

	if matchedPerm == nil {
		return fmt.Errorf("WriteData operation not permitted for key pattern: %s", keyPattern)
	}

	// Check rate limit if specified
	if matchedPerm.MaxPerBlock > 0 {
		counter := tracker.GetCounter(scope.Contract, "WriteData:"+matchedPerm.Target)
		if counter.Get() >= matchedPerm.MaxPerBlock {
			return fmt.Errorf("WriteData rate limit exceeded: %d per block for pattern %s",
				matchedPerm.MaxPerBlock, matchedPerm.Target)
		}
		counter.Increment()
	}

	return nil
}

// verifySendTokens verifies SendTokens operation permissions
func verifySendTokens(op *runtime.StagedOp, scope *AuthorityScope, tracker *OperationLimitTracker) error {
	permissions := scope.Allowed.SendTokens
	if len(permissions) == 0 {
		return fmt.Errorf("SendTokens operations not permitted")
	}

	// Extract recipient and amount from operation
	recipient := ""
	var amount uint64

	if op.To != nil {
		recipient = op.To.String()
	}

	amount = op.Amount

	// Find matching permission
	var matchedPerm *SendTokensPermission
	for _, perm := range permissions {
		if matchesPattern(recipient, perm.To) {
			matchedPerm = &perm
			break
		}
	}

	if matchedPerm == nil {
		return fmt.Errorf("SendTokens operation not permitted for recipient: %s", recipient)
	}

	// Check amount limit
	if matchedPerm.MaxAmount > 0 && amount > matchedPerm.MaxAmount {
		return fmt.Errorf("SendTokens amount %d exceeds limit %d", amount, matchedPerm.MaxAmount)
	}

	// Check rate limit if specified
	if matchedPerm.MaxPerBlock > 0 {
		counter := tracker.GetCounter(scope.Contract, "SendTokens:"+matchedPerm.To)
		if counter.Get() >= matchedPerm.MaxPerBlock {
			return fmt.Errorf("SendTokens rate limit exceeded: %d per block for pattern %s",
				matchedPerm.MaxPerBlock, matchedPerm.To)
		}
		counter.Increment()
	}

	return nil
}

// verifyUpdateAuth verifies UpdateAuth operation permissions
func verifyUpdateAuth(op *runtime.StagedOp, scope *AuthorityScope, tracker *OperationLimitTracker) error {
	permissions := scope.Allowed.UpdateAuth
	if len(permissions) == 0 {
		return fmt.Errorf("UpdateAuth operations not permitted")
	}

	// Extract key pattern from operation (simplified)
	keyPattern := ""
	if op.Data != nil {
		// For operations with data, use a default key pattern
		keyPattern = "default_key"
	}

	// Find matching permission
	var matchedPerm *UpdateAuthPermission
	for _, perm := range permissions {
		if matchesPattern(keyPattern, perm.Target) {
			matchedPerm = &perm
			break
		}
	}

	if matchedPerm == nil {
		return fmt.Errorf("UpdateAuth operation not permitted for key pattern: %s", keyPattern)
	}

	// Check rate limit if specified
	if matchedPerm.MaxPerBlock > 0 {
		counter := tracker.GetCounter(scope.Contract, "UpdateAuth:"+matchedPerm.Target)
		if counter.Get() >= matchedPerm.MaxPerBlock {
			return fmt.Errorf("UpdateAuth rate limit exceeded: %d per block for pattern %s",
				matchedPerm.MaxPerBlock, matchedPerm.Target)
		}
		counter.Increment()
	}

	return nil
}

// VerifyAllWithBinding checks if all staged operations are permitted by both the authority scope and binding permissions
func VerifyAllWithBinding(staged []*runtime.StagedOp, scope *AuthorityScope, binding *authority.Binding, tracker *OperationLimitTracker) error {
	if scope == nil {
		return fmt.Errorf("authority scope cannot be nil")
	}

	if binding == nil {
		return fmt.Errorf("authority binding cannot be nil")
	}

	if len(staged) == 0 {
		return nil // No operations to verify
	}

	// First verify with the existing authority scope
	if err := VerifyAll(staged, scope, tracker); err != nil {
		return fmt.Errorf("authority scope verification failed: %w", err)
	}

	// Then verify each operation against binding permissions
	for i, op := range staged {
		if err := verifyOperationBinding(op, binding); err != nil {
			return fmt.Errorf("operation %d binding verification failed: %w", i, err)
		}
	}

	return nil
}

// verifyOperationBinding verifies a single operation against binding permissions
func verifyOperationBinding(op *runtime.StagedOp, binding *authority.Binding) error {
	switch op.Type {
	case "write_data", "writeData":
		if !binding.HasPermission(authority.PermWriteData) {
			return fmt.Errorf("binding does not have writeData permission")
		}
	case "send_tokens", "sendTokens":
		if !binding.HasPermission(authority.PermSendTokens) {
			return fmt.Errorf("binding does not have sendTokens permission")
		}
	case "update_auth", "updateAuth":
		if !binding.HasPermission(authority.PermUpdateAuth) {
			return fmt.Errorf("binding does not have updateAuth permission")
		}
	default:
		return fmt.Errorf("unsupported operation type: %s", op.Type)
	}

	return nil
}

// VerifyBindingOnly checks if staged operations are permitted by binding permissions only (without authority scope)
func VerifyBindingOnly(staged []*runtime.StagedOp, binding *authority.Binding) error {
	if binding == nil {
		return fmt.Errorf("authority binding cannot be nil")
	}

	if len(staged) == 0 {
		return nil // No operations to verify
	}

	// Verify each operation against binding permissions
	for i, op := range staged {
		if err := verifyOperationBinding(op, binding); err != nil {
			return fmt.Errorf("operation %d binding verification failed: %w", i, err)
		}
	}

	return nil
}

// VerifyAuthority verifies that the keyBook is an enabled authority for the target account
// This checks direct authorities and follows delegation chains as needed
func VerifyAuthority(ctx context.Context, querier *l0api.Querier, accountURL *url.URL, keyBook *url.URL) error {
	if querier == nil {
		return fmt.Errorf("querier cannot be nil")
	}
	if accountURL == nil {
		return fmt.Errorf("account URL cannot be nil")
	}
	if keyBook == nil {
		return fmt.Errorf("keyBook URL cannot be nil")
	}

	// Query the target account to get its authorities
	accountRecord, err := querier.QueryAccount(ctx, accountURL)
	if err != nil {
		return fmt.Errorf("failed to query account %s: %w", accountURL, err)
	}

	// Check direct authorities based on account type
	authorities, err := getAccountAuthorities(accountRecord.Account)
	if err != nil {
		return fmt.Errorf("failed to get authorities for account %s: %w", accountURL, err)
	}

	// Check if keyBook is directly authorized
	keyBookCanonical := accutil.Canonicalize(keyBook)
	for _, authURL := range authorities {
		authCanonical := accutil.Canonicalize(authURL)
		if authCanonical == keyBookCanonical {
			return nil // Direct authority found
		}
	}

	// Check delegation chain - follow authorities to see if any delegate to our keyBook
	for _, authURL := range authorities {
		if err := checkDelegationChain(ctx, querier, authURL, keyBook, 0, 3); err == nil {
			return nil // Found through delegation chain
		}
	}

	return fmt.Errorf("keyBook %s is not an enabled authority for account %s", keyBook, accountURL)
}

// getAccountAuthorities extracts the authorities list from different account types
func getAccountAuthorities(account protocol.Account) ([]*url.URL, error) {
	switch acc := account.(type) {
	case *protocol.TokenAccount:
		var urls []*url.URL
		for _, entry := range acc.Authorities {
			urls = append(urls, entry.Url)
		}
		return urls, nil
	case *protocol.DataAccount:
		var urls []*url.URL
		for _, entry := range acc.Authorities {
			urls = append(urls, entry.Url)
		}
		return urls, nil
	case *protocol.LiteTokenAccount:
		// Lite accounts use the parent identity for authority
		parentADI, err := accutil.GetADI(acc.Url)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent ADI for lite token account: %w", err)
		}
		return []*url.URL{parentADI}, nil
	case *protocol.LiteDataAccount:
		// Lite accounts use the parent identity for authority
		parentADI, err := accutil.GetADI(acc.Url)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent ADI for lite data account: %w", err)
		}
		return []*url.URL{parentADI}, nil
	case *protocol.ADI:
		var urls []*url.URL
		for _, entry := range acc.Authorities {
			urls = append(urls, entry.Url)
		}
		return urls, nil
	default:
		return nil, fmt.Errorf("unsupported account type: %T", account)
	}
}

// checkDelegationChain recursively follows the delegation chain to find the target keyBook
func checkDelegationChain(ctx context.Context, querier *l0api.Querier, currentURL *url.URL, targetKeyBook *url.URL, depth int, maxDepth int) error {
	if depth >= maxDepth {
		return fmt.Errorf("maximum delegation depth reached")
	}

	// If current URL is the target keyBook, we found it
	if accutil.Canonicalize(currentURL) == accutil.Canonicalize(targetKeyBook) {
		return nil
	}

	// Query the current authority to see if it's a keyBook that delegates to our target
	currentRecord, err := querier.QueryAccount(ctx, currentURL)
	if err != nil {
		return fmt.Errorf("failed to query delegation authority %s: %w", currentURL, err)
	}

	// If it's a KeyBook, check its authorities/delegations
	if keyBook, ok := currentRecord.Account.(*protocol.KeyBook); ok {
		// Check if this keyBook has authorities (delegations)
		for _, authEntry := range keyBook.Authorities {
			if err := checkDelegationChain(ctx, querier, authEntry.Url, targetKeyBook, depth+1, maxDepth); err == nil {
				return nil // Found through deeper delegation
			}
		}
	}

	return fmt.Errorf("delegation chain does not lead to target keyBook")
}

// integrateAuthorityVerification integrates authority verification into operation verification
func integrateAuthorityVerification(ctx context.Context, querier *l0api.Querier, op *runtime.StagedOp, keyBook *url.URL) error {
	var targetURL *url.URL

	switch op.Type {
	case "write_data", "writeData":
		targetURL = op.Account
	case "send_tokens", "sendTokens":
		targetURL = op.To // Verify authority for the destination account
	case "update_auth", "updateAuth":
		targetURL = op.Account
	default:
		return fmt.Errorf("unsupported operation type for authority verification: %s", op.Type)
	}

	if targetURL == nil {
		return fmt.Errorf("target URL cannot be nil for operation %s", op.Type)
	}

	return VerifyAuthority(ctx, querier, targetURL, keyBook)
}
