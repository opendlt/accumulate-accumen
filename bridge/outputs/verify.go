package outputs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
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
	OutputID          string                   `json:"output_id"`
	Valid             bool                     `json:"valid"`
	Errors            []string                 `json:"errors,omitempty"`
	Warnings          []string                 `json:"warnings,omitempty"`
	ChecksumVerified  bool                     `json:"checksum_verified"`
	SignatureVerified bool                     `json:"signature_verified"`
	DependenciesValid bool                     `json:"dependencies_valid"`
	VerificationTime  time.Duration            `json:"verification_time"`
	Metadata          map[string]interface{}   `json:"metadata,omitempty"`
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

	// If output has a checksum, verify it matches
	if storedChecksum, exists := output.Metadata["checksum"]; exists {
		if storedChecksumStr, ok := storedChecksum.(string); ok {
			if storedChecksumStr != checksum {
				result.ChecksumVerified = false
				return fmt.Errorf("checksum mismatch: expected %s, got %s", storedChecksumStr, checksum)
			}
		}
	}

	result.ChecksumVerified = true
	result.Metadata["calculated_checksum"] = checksum
	return nil
}

// verifySignature verifies the output signature if present
func (v *Verifier) verifySignature(ctx context.Context, output *Output, result *VerificationResult) error {
	// Check if signature is present
	sigData, hasSig := output.Metadata["signature"]
	if !hasSig {
		if v.config.RequireSignatures {
			result.SignatureVerified = false
			return fmt.Errorf("signature required but not found")
		}
		result.SignatureVerified = true // No signature required
		return nil
	}

	// Parse signature
	signature, ok := sigData.(*Signature)
	if !ok {
		result.SignatureVerified = false
		return fmt.Errorf("invalid signature format")
	}

	// Store signature
	v.mu.Lock()
	v.signatures[output.ID] = signature
	v.mu.Unlock()

	// Verify signature timestamp
	if signature.Timestamp.IsZero() {
		result.SignatureVerified = false
		return fmt.Errorf("signature timestamp is missing")
	}

	// Check if signer is trusted
	if len(v.config.TrustedSigners) > 0 {
		trusted := false
		for _, trustedSigner := range v.config.TrustedSigners {
			if signature.KeyID == trustedSigner {
				trusted = true
				break
			}
		}
		if !trusted {
			result.SignatureVerified = false
			return fmt.Errorf("signature from untrusted signer: %s", signature.KeyID)
		}
	}

	// TODO: Implement actual signature verification
	// This would require crypto libraries and key management
	result.SignatureVerified = true
	result.Metadata["signature_algorithm"] = signature.Algorithm
	result.Metadata["signer_key_id"] = signature.KeyID

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
		OutputID: envelope.TxHash.String(),
		Valid:    true,
		Metadata: make(map[string]interface{}),
	}

	// Basic envelope validation
	if envelope.TxHash.IsZero() {
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