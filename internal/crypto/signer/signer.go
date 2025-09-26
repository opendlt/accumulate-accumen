package signer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/internal/crypto/keystore"
)

// Signer interface for signing envelopes
type Signer interface {
	// SignEnvelope signs an envelope builder
	SignEnvelope(*build.EnvelopeBuilder) error
	// GetPublicKey returns the public key used for signing
	GetPublicKey() (ed25519.PublicKey, error)
	// GetKeyAlias returns the key alias (if applicable)
	GetKeyAlias() string
}

// SignerSelector is a function that selects a signer based on contract URL
type SignerSelector func(contractURL *url.URL) (Signer, error)

// FileKeySigner reads ed25519 private key from file
type FileKeySigner struct {
	privateKey ed25519.PrivateKey
	keyAlias   string
}

// NewFileKeySigner creates a signer that reads key from file
func NewFileKeySigner(keyPath, keyAlias string) (*FileKeySigner, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %w", keyPath, err)
	}

	privateKey, err := parsePrivateKey(strings.TrimSpace(string(keyData)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key from %s: %w", keyPath, err)
	}

	return &FileKeySigner{
		privateKey: privateKey,
		keyAlias:   keyAlias,
	}, nil
}

// SignEnvelope signs the envelope with the file-based key
func (f *FileKeySigner) SignEnvelope(envelope *build.EnvelopeBuilder) error {
	if f.privateKey == nil {
		return fmt.Errorf("no private key available")
	}

	// Sign the envelope with the private key
	if err := envelope.SignWith(f.privateKey); err != nil {
		return fmt.Errorf("failed to sign envelope: %w", err)
	}

	return nil
}

// GetPublicKey returns the public key for the file signer
func (f *FileKeySigner) GetPublicKey() (ed25519.PublicKey, error) {
	if f.privateKey == nil {
		return nil, fmt.Errorf("no private key available")
	}
	return f.privateKey.Public().(ed25519.PublicKey), nil
}

// GetKeyAlias returns the key alias for the file signer
func (f *FileKeySigner) GetKeyAlias() string {
	return f.keyAlias
}

// EnvKeySigner reads ed25519 private key from environment variable
type EnvKeySigner struct {
	privateKey ed25519.PrivateKey
	keyAlias   string
}

// NewEnvKeySigner creates a signer that reads key from environment variable
func NewEnvKeySigner(envVar, keyAlias string) (*EnvKeySigner, error) {
	keyData := os.Getenv(envVar)
	if keyData == "" {
		return nil, fmt.Errorf("environment variable %s is not set or empty", envVar)
	}

	privateKey, err := parsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key from %s: %w", envVar, err)
	}

	return &EnvKeySigner{
		privateKey: privateKey,
		keyAlias:   keyAlias,
	}, nil
}

// SignEnvelope signs the envelope with the environment-based key
func (e *EnvKeySigner) SignEnvelope(envelope *build.EnvelopeBuilder) error {
	if e.privateKey == nil {
		return fmt.Errorf("no private key available")
	}

	// Sign the envelope with the private key
	if err := envelope.SignWith(e.privateKey); err != nil {
		return fmt.Errorf("failed to sign envelope: %w", err)
	}

	return nil
}

// GetPublicKey returns the public key for the env signer
func (e *EnvKeySigner) GetPublicKey() (ed25519.PublicKey, error) {
	if e.privateKey == nil {
		return nil, fmt.Errorf("no private key available")
	}
	return e.privateKey.Public().(ed25519.PublicKey), nil
}

// GetKeyAlias returns the key alias for the env signer
func (e *EnvKeySigner) GetKeyAlias() string {
	return e.keyAlias
}

// DevKeySigner uses a hardcoded development key (insecure, dev only)
type DevKeySigner struct {
	privateKey ed25519.PrivateKey
	keyAlias   string
}

// NewDevKeySigner creates a signer with a hardcoded development key
func NewDevKeySigner() *DevKeySigner {
	// Generate a deterministic key for development
	// In production, this should NEVER be used
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i % 256)
	}

	privateKey := ed25519.NewKeyFromSeed(seed)

	return &DevKeySigner{
		privateKey: privateKey,
		keyAlias:   "dev-default",
	}
}

// NewDevKeySignerFromKey creates a dev signer from raw key material
func NewDevKeySignerFromKey(keyData, keyAlias string) (*DevKeySigner, error) {
	privateKey, err := parsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dev key: %w", err)
	}

	return &DevKeySigner{
		privateKey: privateKey,
		keyAlias:   keyAlias,
	}, nil
}

// SignEnvelope signs the envelope with the development key
func (d *DevKeySigner) SignEnvelope(envelope *build.EnvelopeBuilder) error {
	if d.privateKey == nil {
		return fmt.Errorf("no private key available")
	}

	// Sign the envelope with the private key
	if err := envelope.SignWith(d.privateKey); err != nil {
		return fmt.Errorf("failed to sign envelope: %w", err)
	}

	return nil
}

// GetPublicKey returns the public key for the dev signer
func (d *DevKeySigner) GetPublicKey() (ed25519.PublicKey, error) {
	if d.privateKey == nil {
		return nil, fmt.Errorf("no private key available")
	}
	return d.privateKey.Public().(ed25519.PublicKey), nil
}

// GetKeyAlias returns the key alias for the dev signer
func (d *DevKeySigner) GetKeyAlias() string {
	return d.keyAlias
}

// KeystoreSigner implements signing using a keystore with key selection
type KeystoreSigner struct {
	keystore *keystore.Keystore
	keyAlias string
}

// NewKeystoreSigner creates a new keystore-based signer for a specific key
func NewKeystoreSigner(ks *keystore.Keystore, keyAlias string) *KeystoreSigner {
	return &KeystoreSigner{
		keystore: ks,
		keyAlias: keyAlias,
	}
}

// SignEnvelope signs an envelope using the keystore key
func (ks *KeystoreSigner) SignEnvelope(envelope *build.EnvelopeBuilder) error {
	if ks.keystore == nil {
		return fmt.Errorf("keystore is nil")
	}

	if ks.keyAlias == "" {
		return fmt.Errorf("key alias is empty")
	}

	// Get private key from keystore
	privateKey, err := ks.keystore.GetPrivateKey(ks.keyAlias)
	if err != nil {
		return fmt.Errorf("failed to get private key for alias '%s': %w", ks.keyAlias, err)
	}

	// Sign the envelope
	if err := envelope.SignWith(privateKey); err != nil {
		return fmt.Errorf("failed to sign envelope: %w", err)
	}

	return nil
}

// GetPublicKey returns the public key for the configured alias
func (ks *KeystoreSigner) GetPublicKey() (ed25519.PublicKey, error) {
	if ks.keystore == nil {
		return nil, fmt.Errorf("keystore is nil")
	}

	if ks.keyAlias == "" {
		return nil, fmt.Errorf("key alias is empty")
	}

	return ks.keystore.GetPublicKey(ks.keyAlias)
}

// GetKeyAlias returns the key alias
func (ks *KeystoreSigner) GetKeyAlias() string {
	return ks.keyAlias
}

// SignerConfig represents signer configuration
type SignerConfig struct {
	Type string `yaml:"type"` // "file" | "env" | "dev"
	Key  string `yaml:"key"`  // path or env name or raw key (dev only)
}

// NewFromConfig creates a signer from configuration
func NewFromConfig(cfg *SignerConfig, fallbackKey string) (Signer, error) {
	if cfg == nil || cfg.Type == "" {
		// Fallback to legacy sequencer key if provided
		if fallbackKey != "" {
			return NewDevKeySignerFromKey(fallbackKey, "legacy")
		}
		return nil, fmt.Errorf("no signer configuration provided")
	}

	switch cfg.Type {
	case "file":
		if cfg.Key == "" {
			return nil, fmt.Errorf("file signer requires key path")
		}
		return NewFileKeySigner(cfg.Key, "file")

	case "env":
		if cfg.Key == "" {
			return nil, fmt.Errorf("env signer requires environment variable name")
		}
		return NewEnvKeySigner(cfg.Key, "env")

	case "dev":
		if cfg.Key == "" {
			// Use default dev key
			return NewDevKeySigner(), nil
		}
		// Use provided dev key
		return NewDevKeySignerFromKey(cfg.Key, "dev")

	default:
		return nil, fmt.Errorf("unsupported signer type: %s", cfg.Type)
	}
}

// parsePrivateKey parses a private key from hex or base64 string
func parsePrivateKey(keyData string) (ed25519.PrivateKey, error) {
	keyData = strings.TrimSpace(keyData)

	// Try hex decoding first
	if decoded, err := hex.DecodeString(keyData); err == nil {
		if len(decoded) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decoded), nil
		}
		if len(decoded) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(decoded), nil
		}
	}

	// Try base64 decoding
	if decoded, err := base64.StdEncoding.DecodeString(keyData); err == nil {
		if len(decoded) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decoded), nil
		}
		if len(decoded) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(decoded), nil
		}
	}

	// Try base64 URL encoding
	if decoded, err := base64.URLEncoding.DecodeString(keyData); err == nil {
		if len(decoded) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decoded), nil
		}
		if len(decoded) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(decoded), nil
		}
	}

	return nil, fmt.Errorf("unable to parse private key: invalid format or length")
}

// GenerateDevKey generates a new development key and returns it as hex string
func GenerateDevKey() (string, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}

	return hex.EncodeToString(privateKey), nil
}

// GetPublicKey returns the public key corresponding to the private key
func GetPublicKey(signer Signer) (ed25519.PublicKey, error) {
	return signer.GetPublicKey()
}

// GenerateEd25519 generates a new Ed25519 key pair and returns public and private keys as hex strings
func GenerateEd25519() (pubHex, privHex string, err error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate Ed25519 key pair: %w", err)
	}

	pubHex = hex.EncodeToString(publicKey)
	privHex = hex.EncodeToString(privateKey)

	return pubHex, privHex, nil
}

// LoadFromFile loads a private key from a file and returns it as hex string
func LoadFromFile(path string) (privHex string, err error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read key file %s: %w", path, err)
	}

	// Parse the key to validate it
	privateKey, err := parsePrivateKey(strings.TrimSpace(string(keyData)))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key from %s: %w", path, err)
	}

	// Return as hex string
	return hex.EncodeToString(privateKey), nil
}

// SaveToFile saves a private key (in hex format) to a file with secure permissions
func SaveToFile(path, privHex string) error {
	// Validate the private key format
	privKey, err := hex.DecodeString(privHex)
	if err != nil {
		return fmt.Errorf("invalid private key hex format: %w", err)
	}

	if len(privKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key size: expected %d bytes, got %d bytes",
			ed25519.PrivateKeySize, len(privKey))
	}

	// Write the key to file with secure permissions
	// On Unix-like systems, this sets 0600 (read/write for owner only)
	// On Windows, this is best-effort due to different permission model
	err = os.WriteFile(path, []byte(privHex), 0600)
	if err != nil {
		return fmt.Errorf("failed to write key file %s: %w", path, err)
	}

	return nil
}