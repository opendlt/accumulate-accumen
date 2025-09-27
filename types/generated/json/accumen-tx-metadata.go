// Code generated from JSON schema. DO NOT EDIT.
// Source: accumen-tx-metadata.schema.json

package jsontypes

import (
	"encoding/json"
	"time"
)

// AccumenTransactionMetadata represents the Accumen Transaction Metadata
type AccumenTransactionMetadata struct {
	// Unique transaction identifier
	TxId string `json:"txId"`
	// Block height where transaction was included
	BlockHeight uint64 `json:"blockHeight"`
	// Transaction index within the block
	BlockIndex *uint64 `json:"blockIndex,omitempty"`
	// Timestamp when transaction was executed (RFC3339)
	Timestamp time.Time `json:"timestamp"`
	// Identifier of the sequencer that processed the transaction
	SequencerId *string `json:"sequencerId,omitempty"`
	// WASM execution context information
	ExecutionContext *ExecutionContext `json:"executionContext,omitempty"`
	// L0 bridge submission information
	BridgeInfo *BridgeInfo `json:"bridgeInfo,omitempty"`
	// State changes made by this transaction
	StateChanges []StateChange `json:"stateChanges,omitempty"`
	// Log entries emitted during execution
	Logs []LogEntry `json:"logs,omitempty"`
	// Transaction signature information
	Signature *SignatureInfo `json:"signature,omitempty"`
	// Transaction priority score used for ordering
	Priority *uint64 `json:"priority,omitempty"`
	// Transaction nonce for replay protection
	Nonce *uint64 `json:"nonce,omitempty"`
	// Transaction size in bytes
	Size *uint64 `json:"size,omitempty"`
	// Metadata schema version
	Version *string `json:"version,omitempty"`
	// Extension fields for future use
	Extensions map[string]interface{} `json:"extensions,omitempty"`
	// Amount of gas consumed during execution (top-level for compatibility)
	GasUsed uint64 `json:"gasUsed"`
	// Maximum gas allowed for execution (top-level for compatibility)
	GasLimit uint64 `json:"gasLimit"`
}

// ExecutionContext represents WASM execution context information
type ExecutionContext struct {
	// Amount of gas consumed during execution
	GasUsed uint64 `json:"gasUsed"`
	// Maximum gas allowed for execution
	GasLimit uint64 `json:"gasLimit"`
	// Gas price in credits per unit
	GasPrice *uint64 `json:"gasPrice,omitempty"`
	// Execution time in milliseconds
	ExecutionTime float64 `json:"executionTime"`
	// WASM module identifier or hash
	WasmModule *string `json:"wasmModule,omitempty"`
	// WASM function that was executed
	FunctionName *string `json:"functionName,omitempty"`
	// Peak memory usage in bytes
	MemoryUsed *uint64 `json:"memoryUsed,omitempty"`
}

// BridgeInfo represents L0 bridge submission information
type BridgeInfo struct {
	// Accumulate L0 transaction hash
	L0TxHash *string `json:"l0TxHash,omitempty"`
	// L0 block height where transaction was anchored
	L0BlockHeight *uint64 `json:"l0BlockHeight,omitempty"`
	// When transaction was submitted to L0
	SubmissionTime *time.Time `json:"submissionTime,omitempty"`
	// When transaction was confirmed on L0
	ConfirmationTime *time.Time `json:"confirmationTime,omitempty"`
	// Cost in credits to bridge to L0
	BridgeCost *uint64 `json:"bridgeCost,omitempty"`
	// Number of submission retry attempts
	RetryCount *uint64 `json:"retryCount,omitempty"`
}

// StateChange represents a state change made by a transaction
type StateChange struct {
	// Type of state operation
	Operation string `json:"operation"`
	// State key (base64 encoded)
	Key string `json:"key"`
	// Previous value (base64 encoded, if any)
	OldValue *string `json:"oldValue,omitempty"`
	// New value (base64 encoded, for set operations)
	NewValue *string `json:"newValue,omitempty"`
}

// LogEntry represents a log entry emitted during execution
type LogEntry struct {
	// Log level
	Level string `json:"level"`
	// Log message
	Message string `json:"message"`
	// When log entry was created
	Timestamp time.Time `json:"timestamp"`
	// Source of the log entry (e.g., function name)
	Source *string `json:"source,omitempty"`
}

// SignatureInfo represents transaction signature information
type SignatureInfo struct {
	// Signature algorithm used
	Algorithm string `json:"algorithm"`
	// Signer's public key (base64 encoded)
	PublicKey string `json:"publicKey"`
	// Transaction signature (base64 encoded)
	Signature string `json:"signature"`
	// Hash of the public key for verification
	KeyHash *string `json:"keyHash,omitempty"`
}

// Validate validates the AccumenTransactionMetadata
func (m *AccumenTransactionMetadata) Validate() error {
	// TODO: Add validation logic based on schema constraints
	return nil
}

// ToJSON converts the AccumenTransactionMetadata to JSON
func (m *AccumenTransactionMetadata) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// FromJSON creates a AccumenTransactionMetadata from JSON
func (m *AccumenTransactionMetadata) FromJSON(data []byte) error {
	return json.Unmarshal(data, m)
}