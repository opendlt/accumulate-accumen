package l1

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/fxamacker/cbor/v2"
)

// Tx represents an L1 transaction with deterministic serialization
type Tx struct {
	Contract  string         `json:"contract" cbor:"contract"`   // Contract address (e.g., "acc://counter.acme")
	Entry     string         `json:"entry" cbor:"entry"`         // Entry point function name
	Args      map[string]any `json:"args" cbor:"args"`           // Function arguments
	Nonce     []byte         `json:"nonce" cbor:"nonce"`         // Unique nonce to prevent replay
	Timestamp int64          `json:"timestamp" cbor:"timestamp"` // Unix timestamp in nanoseconds
}

// Hash computes the SHA256 hash of the transaction using canonical CBOR encoding
func (tx *Tx) Hash() [32]byte {
	// Use CBOR for canonical serialization to ensure deterministic hashing
	data, err := tx.MarshalCBOR()
	if err != nil {
		// This should not happen with valid transaction data
		panic(fmt.Sprintf("failed to marshal transaction for hashing: %v", err))
	}
	return sha256.Sum256(data)
}

// MarshalCBOR serializes the transaction to CBOR format with deterministic ordering
func (tx *Tx) MarshalCBOR() ([]byte, error) {
	// Create a deterministic representation
	canonical := struct {
		Contract  string         `cbor:"contract"`
		Entry     string         `cbor:"entry"`
		Args      map[string]any `cbor:"args"`
		Nonce     []byte         `cbor:"nonce"`
		Timestamp int64          `cbor:"timestamp"`
	}{
		Contract:  tx.Contract,
		Entry:     tx.Entry,
		Args:      tx.normalizeArgs(),
		Nonce:     tx.Nonce,
		Timestamp: tx.Timestamp,
	}

	// Use deterministic CBOR encoding mode
	em, err := cbor.CoreDetEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("failed to create CBOR encoder: %w", err)
	}

	data, err := em.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction to CBOR: %w", err)
	}

	return data, nil
}

// UnmarshalCBOR deserializes the transaction from CBOR format
func (tx *Tx) UnmarshalCBOR(data []byte) error {
	// Use default DecOptions for deterministic decoding
	dm, err := cbor.DecOptions{}.DecMode()
	if err != nil {
		return fmt.Errorf("failed to create CBOR decoder: %w", err)
	}

	err = dm.Unmarshal(data, tx)
	if err != nil {
		return fmt.Errorf("failed to unmarshal transaction from CBOR: %w", err)
	}

	return nil
}

// MarshalJSON serializes the transaction to JSON format with deterministic ordering
func (tx *Tx) MarshalJSON() ([]byte, error) {
	// Create a deterministic representation
	canonical := struct {
		Contract  string         `json:"contract"`
		Entry     string         `json:"entry"`
		Args      map[string]any `json:"args"`
		Nonce     []byte         `json:"nonce"`
		Timestamp int64          `json:"timestamp"`
	}{
		Contract:  tx.Contract,
		Entry:     tx.Entry,
		Args:      tx.normalizeArgs(),
		Nonce:     tx.Nonce,
		Timestamp: tx.Timestamp,
	}

	return json.Marshal(canonical)
}

// UnmarshalJSON deserializes the transaction from JSON format
func (tx *Tx) UnmarshalJSON(data []byte) error {
	type txAlias Tx
	err := json.Unmarshal(data, (*txAlias)(tx))
	if err != nil {
		return fmt.Errorf("failed to unmarshal transaction from JSON: %w", err)
	}

	return nil
}

// Marshal serializes the transaction using CBOR (preferred) or falls back to JSON
func (tx *Tx) Marshal() ([]byte, error) {
	// Prefer CBOR for efficiency and deterministic encoding
	data, err := tx.MarshalCBOR()
	if err != nil {
		// Fallback to JSON if CBOR fails
		return tx.MarshalJSON()
	}
	return data, nil
}

// Unmarshal deserializes the transaction, detecting format automatically
func (tx *Tx) Unmarshal(data []byte) error {
	// Try CBOR first (more efficient)
	if err := tx.UnmarshalCBOR(data); err == nil {
		return nil
	}

	// Fallback to JSON
	return tx.UnmarshalJSON(data)
}

// normalizeArgs ensures deterministic serialization of the Args map
func (tx *Tx) normalizeArgs() map[string]any {
	if tx.Args == nil {
		return nil
	}

	// Create a new map to avoid modifying the original
	normalized := make(map[string]any, len(tx.Args))

	// Get sorted keys for deterministic ordering
	keys := make([]string, 0, len(tx.Args))
	for k := range tx.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Copy values in sorted order
	for _, k := range keys {
		normalized[k] = tx.Args[k]
	}

	return normalized
}

// Validate performs basic validation of transaction fields
func (tx *Tx) Validate() error {
	if tx.Contract == "" {
		return fmt.Errorf("contract address cannot be empty")
	}
	if tx.Entry == "" {
		return fmt.Errorf("entry point cannot be empty")
	}
	if len(tx.Nonce) == 0 {
		return fmt.Errorf("nonce cannot be empty")
	}
	if tx.Timestamp <= 0 {
		return fmt.Errorf("timestamp must be positive")
	}
	return nil
}

// String returns a human-readable representation of the transaction
func (tx *Tx) String() string {
	hash := tx.Hash()
	return fmt.Sprintf("Tx{Hash: %x, Contract: %s, Entry: %s, Timestamp: %d}",
		hash[:8], tx.Contract, tx.Entry, tx.Timestamp)
}
