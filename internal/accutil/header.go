package accutil

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
)

// CrossLinkMetadata represents metadata embedded in L0 transactions that links back to L1
type CrossLinkMetadata struct {
	Version     string `json:"version"`           // Metadata format version
	L1TxHash    string `json:"l1TxHash"`          // Full L1 transaction hash (hex)
	L1Height    uint64 `json:"l1Height"`          // L1 block height
	L1Index     uint64 `json:"l1Index"`           // Transaction index within L1 block
	ChainID     string `json:"chainId"`           // L1 chain identifier
	ContractURL string `json:"contractUrl"`       // Contract that triggered this L0 operation
	Timestamp   int64  `json:"timestamp"`         // Unix timestamp of L1 transaction
}

// WithMemo sets the memo field on a transaction builder
func WithMemo(builder build.TransactionBuilder, memo string) build.TransactionBuilder {
	if memo == "" {
		return builder
	}

	// Use the builder's memo functionality
	return builder.Memo(memo)
}

// WithMetadataJSON sets the metadata field on a transaction builder with JSON-encoded data
func WithMetadataJSON(builder build.TransactionBuilder, v any) (build.TransactionBuilder, error) {
	if v == nil {
		return builder, nil // No metadata to add
	}

	// Marshal the value to JSON
	metadataBytes, err := json.Marshal(v)
	if err != nil {
		return builder, fmt.Errorf("failed to marshal metadata to JSON: %w", err)
	}

	// Set the metadata on the builder
	return builder.Metadata(metadataBytes), nil
}

// WithCrossLink embeds L1→L0 cross-linking information in the transaction
// This puts a short reference in the Memo field and full metadata in the Metadata field
func WithCrossLink(builder build.TransactionBuilder, l1TxHash [32]byte) (build.TransactionBuilder, error) {
	// Create short hash reference for memo (first 8 bytes as hex)
	shortHash := hex.EncodeToString(l1TxHash[:8])
	memo := fmt.Sprintf("L1:%s", shortHash)

	// Set the memo with short reference
	builder = builder.Memo(memo)

	// Create full cross-link metadata
	fullHash := hex.EncodeToString(l1TxHash[:])
	crossLink := CrossLinkMetadata{
		Version:  "1.0",
		L1TxHash: fullHash,
	}

	// Add the full metadata
	builder, err := WithMetadataJSON(builder, crossLink)
	if err != nil {
		return builder, fmt.Errorf("failed to add cross-link metadata: %w", err)
	}

	return builder, nil
}

// WithCrossLinkDetailed embeds detailed L1→L0 cross-linking information
func WithCrossLinkDetailed(builder build.TransactionBuilder, l1TxHash [32]byte, l1Height, l1Index uint64, chainID, contractURL string, timestamp int64) (build.TransactionBuilder, error) {
	// Create short hash reference for memo (first 8 bytes as hex)
	shortHash := hex.EncodeToString(l1TxHash[:8])
	memo := fmt.Sprintf("L1:%s@%d", shortHash, l1Height)

	// Set the memo with short reference including height
	builder = builder.Memo(memo)

	// Create detailed cross-link metadata
	fullHash := hex.EncodeToString(l1TxHash[:])
	crossLink := CrossLinkMetadata{
		Version:     "1.0",
		L1TxHash:    fullHash,
		L1Height:    l1Height,
		L1Index:     l1Index,
		ChainID:     chainID,
		ContractURL: contractURL,
		Timestamp:   timestamp,
	}

	// Add the full metadata
	builder, err := WithMetadataJSON(builder, crossLink)
	if err != nil {
		return builder, fmt.Errorf("failed to add detailed cross-link metadata: %w", err)
	}

	return builder, nil
}

// ExtractCrossLink extracts cross-link metadata from transaction metadata
func ExtractCrossLink(metadataBytes []byte) (*CrossLinkMetadata, error) {
	if len(metadataBytes) == 0 {
		return nil, fmt.Errorf("no metadata provided")
	}

	var crossLink CrossLinkMetadata
	if err := json.Unmarshal(metadataBytes, &crossLink); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cross-link metadata: %w", err)
	}

	// Validate the cross-link format
	if crossLink.Version == "" {
		return nil, fmt.Errorf("cross-link metadata missing version")
	}

	if crossLink.L1TxHash == "" {
		return nil, fmt.Errorf("cross-link metadata missing L1 transaction hash")
	}

	// Validate hash format (should be 64 hex characters for SHA-256)
	if len(crossLink.L1TxHash) != 64 {
		return nil, fmt.Errorf("invalid L1 transaction hash length: expected 64, got %d", len(crossLink.L1TxHash))
	}

	if _, err := hex.DecodeString(crossLink.L1TxHash); err != nil {
		return nil, fmt.Errorf("invalid L1 transaction hash format: %w", err)
	}

	return &crossLink, nil
}

// IsCrossLinked checks if a transaction has cross-link metadata
func IsCrossLinked(metadataBytes []byte) bool {
	_, err := ExtractCrossLink(metadataBytes)
	return err == nil
}

// ParseMemoReference extracts the L1 transaction reference from a memo field
// Expected format: "L1:<shortHash>" or "L1:<shortHash>@<height>"
func ParseMemoReference(memo string) (shortHash string, height uint64, err error) {
	if memo == "" {
		return "", 0, fmt.Errorf("memo is empty")
	}

	// Check if it starts with "L1:"
	if len(memo) < 3 || memo[:3] != "L1:" {
		return "", 0, fmt.Errorf("memo does not contain L1 reference")
	}

	// Remove "L1:" prefix
	reference := memo[3:]

	// Check if it contains height information
	if atIndex := findAt(reference); atIndex != -1 {
		shortHash = reference[:atIndex]
		heightStr := reference[atIndex+1:]

		// Parse height
		var parsedHeight uint64
		if _, err := fmt.Sscanf(heightStr, "%d", &parsedHeight); err != nil {
			return "", 0, fmt.Errorf("invalid height in memo: %w", err)
		}
		height = parsedHeight
	} else {
		shortHash = reference
		height = 0
	}

	// Validate short hash format (should be 16 hex characters)
	if len(shortHash) != 16 {
		return "", 0, fmt.Errorf("invalid short hash length: expected 16, got %d", len(shortHash))
	}

	if _, err := hex.DecodeString(shortHash); err != nil {
		return "", 0, fmt.Errorf("invalid short hash format: %w", err)
	}

	return shortHash, height, nil
}

// findAt finds the index of '@' character in a string, returns -1 if not found
func findAt(s string) int {
	for i, r := range s {
		if r == '@' {
			return i
		}
	}
	return -1
}

// BuildL1Reference creates a memo reference string for an L1 transaction
func BuildL1Reference(l1TxHash [32]byte, l1Height uint64) string {
	shortHash := hex.EncodeToString(l1TxHash[:8])
	if l1Height > 0 {
		return fmt.Sprintf("L1:%s@%d", shortHash, l1Height)
	}
	return fmt.Sprintf("L1:%s", shortHash)
}

// WithL1Origin sets both memo and metadata for L1-originated transactions
// This is a convenience function that combines WithMemo and WithCrossLinkDetailed
func WithL1Origin(builder build.TransactionBuilder, l1TxHash [32]byte, l1Height, l1Index uint64, chainID, contractURL string, timestamp int64) (build.TransactionBuilder, error) {
	// Add detailed cross-link information
	return WithCrossLinkDetailed(builder, l1TxHash, l1Height, l1Index, chainID, contractURL, timestamp)
}

// AccumenMetadata represents Accumen-specific metadata for L0 transactions
type AccumenMetadata struct {
	Version      string            `json:"version"`
	Source       string            `json:"source"`       // "accumen-sequencer"
	SequencerID  string            `json:"sequencerId"`  // Sequencer identifier
	L1CrossLink  *CrossLinkMetadata `json:"l1CrossLink,omitempty"`
	Custom       map[string]any    `json:"custom,omitempty"` // Custom metadata fields
}

// WithAccumenMetadata adds Accumen-specific metadata to a transaction
func WithAccumenMetadata(builder build.TransactionBuilder, sequencerID string, l1CrossLink *CrossLinkMetadata) (build.TransactionBuilder, error) {
	metadata := AccumenMetadata{
		Version:     "1.0",
		Source:      "accumen-sequencer",
		SequencerID: sequencerID,
		L1CrossLink: l1CrossLink,
	}

	return WithMetadataJSON(builder, metadata)
}

// WithCustomMetadata adds custom key-value metadata to a transaction
func WithCustomMetadata(builder build.TransactionBuilder, key string, value any) (build.TransactionBuilder, error) {
	if key == "" {
		return builder, fmt.Errorf("metadata key cannot be empty")
	}

	// Create a simple key-value metadata structure
	metadata := map[string]any{
		key: value,
	}

	return WithMetadataJSON(builder, metadata)
}

// ChainMetadata adds metadata to identify the transaction as part of a chain
func ChainMetadata(chainID string, height uint64, timestamp int64) map[string]any {
	return map[string]any{
		"chainId":   chainID,
		"height":    height,
		"timestamp": timestamp,
		"source":    "accumen",
	}
}