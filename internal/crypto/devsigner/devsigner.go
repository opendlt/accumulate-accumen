package devsigner

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"

	"github.com/opendlt/accumulate-accumen/internal/crypto/signer"
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/address"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/encoding"
)

// WARNING: This is a development-only signer implementation.
// DEPRECATED: Use internal/crypto/signer package instead.
// This package is maintained for backward compatibility only.
// In production, this should be replaced with proper HSM or secure key management.
// DO NOT USE IN PRODUCTION - private keys are stored in memory.

// Signer provides ed25519 signing capabilities for development purposes
// DEPRECATED: Use signer.DevKeySigner instead
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyHash    []byte
	// Embed new signer for forward compatibility
	newSigner signer.Signer
}

// NewFromHex creates a new development signer from a hex-encoded private key
// DEPRECATED: Use signer.NewDevKeySignerFromKey instead
func NewFromHex(hexKey string) (*Signer, error) {
	if hexKey == "" {
		return nil, fmt.Errorf("private key cannot be empty")
	}

	// Create new signer using the updated interface
	newSigner, err := signer.NewDevKeySignerFromKey(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create new signer: %w", err)
	}

	// Remove 0x prefix if present
	if len(hexKey) > 2 && hexKey[:2] == "0x" {
		hexKey = hexKey[2:]
	}

	// Decode hex private key
	privateKeyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex private key: %w", err)
	}

	// Ed25519 private key should be 32 bytes (seed) or 64 bytes (full key)
	var privateKey ed25519.PrivateKey
	switch len(privateKeyBytes) {
	case ed25519.SeedSize:
		// Generate full private key from seed
		privateKey = ed25519.NewKeyFromSeed(privateKeyBytes)
	case ed25519.PrivateKeySize:
		// Use full private key directly
		privateKey = privateKeyBytes
	default:
		return nil, fmt.Errorf("invalid private key length: expected %d or %d bytes, got %d",
			ed25519.SeedSize, ed25519.PrivateKeySize, len(privateKeyBytes))
	}

	// Extract public key
	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Calculate key hash for Accumulate addressing
	keyHash := address.FromED25519PublicKey(publicKey)

	legacySigner := &Signer{
		privateKey: privateKey,
		publicKey:  publicKey,
		keyHash:    keyHash,
		newSigner:  newSigner,
	}

	return legacySigner, nil
}

// AsNewSigner returns the new signer interface for forward compatibility
func (s *Signer) AsNewSigner() signer.Signer {
	return s.newSigner
}

// Sign signs an envelope using the ed25519 private key
func (s *Signer) Sign(env *build.EnvelopeBuilder) error {
	if env == nil {
		return fmt.Errorf("envelope cannot be nil")
	}

	// Get the transaction data to sign
	txData, err := env.Transaction().MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal transaction for signing: %w", err)
	}

	// Sign the transaction data
	signature := ed25519.Sign(s.privateKey, txData)

	// Create Accumulate signature
	accSignature := &encoding.ED25519Signature{
		PublicKey: s.publicKey,
		Signature: signature,
	}

	// Add signature to envelope
	env.AddSignature(accSignature)

	return nil
}

// GetPublicKey returns the public key
func (s *Signer) GetPublicKey() ed25519.PublicKey {
	return s.publicKey
}

// GetKeyHash returns the key hash used for Accumulate addressing
func (s *Signer) GetKeyHash() []byte {
	return s.keyHash
}

// GetAddress returns the Accumulate address derived from this key
func (s *Signer) GetAddress() string {
	return fmt.Sprintf("acc://%x.acme", s.keyHash[:4]) // Simplified address format
}

// Verify verifies a signature against the message using this signer's public key
func (s *Signer) Verify(message, signature []byte) bool {
	return ed25519.Verify(s.publicKey, message, signature)
}

// String returns a string representation of the signer (without private key)
func (s *Signer) String() string {
	return fmt.Sprintf("DevSigner{publicKey: %x, keyHash: %x}",
		s.publicKey[:8], s.keyHash[:8])
}

// NewKeyPair generates a new ed25519 key pair for development
func NewKeyPair() (*Signer, string, error) {
	// Generate new key pair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Calculate key hash
	keyHash := address.FromED25519PublicKey(publicKey)

	signer := &Signer{
		privateKey: privateKey,
		publicKey:  publicKey,
		keyHash:    keyHash,
	}

	// Return signer and hex-encoded private key
	hexKey := hex.EncodeToString(privateKey.Seed())
	return signer, hexKey, nil
}

// ValidateHexKey validates that a hex string is a valid ed25519 private key
func ValidateHexKey(hexKey string) error {
	if hexKey == "" {
		return fmt.Errorf("private key cannot be empty")
	}

	// Remove 0x prefix if present
	if len(hexKey) > 2 && hexKey[:2] == "0x" {
		hexKey = hexKey[2:]
	}

	// Decode hex
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return fmt.Errorf("invalid hex encoding: %w", err)
	}

	// Check length
	if len(keyBytes) != ed25519.SeedSize && len(keyBytes) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid key length: expected %d or %d bytes, got %d",
			ed25519.SeedSize, ed25519.PrivateKeySize, len(keyBytes))
	}

	return nil
}