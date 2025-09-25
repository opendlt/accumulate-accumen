package json

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/opendlt/accumulate-accumen/engine/state"
)

// MetadataBuilder constructs transaction metadata from execution receipts
type MetadataBuilder struct {
	config *BuilderConfig
}

// BuilderConfig defines configuration for metadata building
type BuilderConfig struct {
	IncludeStateDiff bool
	IncludeLogs      bool
	IncludeGasInfo   bool
	CompactMode      bool
	SchemaVersion    string
}

// DefaultBuilderConfig returns default builder configuration
func DefaultBuilderConfig() *BuilderConfig {
	return &BuilderConfig{
		IncludeStateDiff: true,
		IncludeLogs:      true,
		IncludeGasInfo:   true,
		CompactMode:      false,
		SchemaVersion:    "v1.0.0",
	}
}

// TransactionMetadata represents the structured metadata for a transaction
type TransactionMetadata struct {
	TxID             string                 `json:"txId"`
	BlockHeight      uint64                 `json:"blockHeight"`
	BlockIndex       *uint64                `json:"blockIndex,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
	SequencerID      string                 `json:"sequencerId,omitempty"`
	ExecutionContext *ExecutionContext      `json:"executionContext"`
	BridgeInfo       *BridgeInfo            `json:"bridgeInfo,omitempty"`
	StateChanges     []StateChange          `json:"stateChanges,omitempty"`
	Logs             []LogEntry             `json:"logs,omitempty"`
	Signature        *SignatureInfo         `json:"signature,omitempty"`
	Priority         uint64                 `json:"priority,omitempty"`
	Nonce            uint64                 `json:"nonce,omitempty"`
	Size             uint64                 `json:"size,omitempty"`
	Version          string                 `json:"version"`
	Extensions       map[string]interface{} `json:"extensions,omitempty"`
}

// ExecutionContext contains WASM execution details
type ExecutionContext struct {
	GasUsed       uint64  `json:"gasUsed"`
	GasLimit      uint64  `json:"gasLimit"`
	GasPrice      *uint64 `json:"gasPrice,omitempty"`
	ExecutionTime float64 `json:"executionTime"`
	WasmModule    string  `json:"wasmModule,omitempty"`
	FunctionName  string  `json:"functionName,omitempty"`
	MemoryUsed    *uint64 `json:"memoryUsed,omitempty"`
}

// BridgeInfo contains L0 bridge submission details
type BridgeInfo struct {
	L0TxHash         string     `json:"l0TxHash,omitempty"`
	L0BlockHeight    *uint64    `json:"l0BlockHeight,omitempty"`
	SubmissionTime   *time.Time `json:"submissionTime,omitempty"`
	ConfirmationTime *time.Time `json:"confirmationTime,omitempty"`
	BridgeCost       *uint64    `json:"bridgeCost,omitempty"`
	RetryCount       uint32     `json:"retryCount,omitempty"`
}

// StateChange represents a modification to the state store
type StateChange struct {
	Operation string `json:"operation"`
	Key       string `json:"key"`
	OldValue  string `json:"oldValue,omitempty"`
	NewValue  string `json:"newValue,omitempty"`
}

// LogEntry represents a log message emitted during execution
type LogEntry struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source,omitempty"`
}

// SignatureInfo contains transaction signature details
type SignatureInfo struct {
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	KeyHash   string `json:"keyHash,omitempty"`
}

// NewMetadataBuilder creates a new metadata builder
func NewMetadataBuilder(config *BuilderConfig) *MetadataBuilder {
	if config == nil {
		config = DefaultBuilderConfig()
	}

	return &MetadataBuilder{
		config: config,
	}
}

// BuildFromReceipt constructs metadata from an execution receipt
func (b *MetadataBuilder) BuildFromReceipt(receipt *state.Receipt) (*TransactionMetadata, error) {
	if receipt == nil {
		return nil, fmt.Errorf("receipt cannot be nil")
	}

	metadata := &TransactionMetadata{
		TxID:        receipt.TxID,
		BlockHeight: receipt.BlockHeight,
		Timestamp:   receipt.Timestamp,
		Version:     b.config.SchemaVersion,
		Extensions:  make(map[string]interface{}),
	}

	// Set block index if available
	if receipt.BlockIndex > 0 {
		metadata.BlockIndex = &receipt.BlockIndex
	}

	// Set sequencer ID if available
	if receipt.SequencerID != "" {
		metadata.SequencerID = receipt.SequencerID
	}

	// Build execution context
	if err := b.buildExecutionContext(receipt, metadata); err != nil {
		return nil, fmt.Errorf("failed to build execution context: %w", err)
	}

	// Build bridge info if available
	if err := b.buildBridgeInfo(receipt, metadata); err != nil {
		return nil, fmt.Errorf("failed to build bridge info: %w", err)
	}

	// Build state changes
	if b.config.IncludeStateDiff {
		if err := b.buildStateChanges(receipt, metadata); err != nil {
			return nil, fmt.Errorf("failed to build state changes: %w", err)
		}
	}

	// Build logs
	if b.config.IncludeLogs {
		if err := b.buildLogs(receipt, metadata); err != nil {
			return nil, fmt.Errorf("failed to build logs: %w", err)
		}
	}

	// Build signature info if available
	if err := b.buildSignatureInfo(receipt, metadata); err != nil {
		return nil, fmt.Errorf("failed to build signature info: %w", err)
	}

	// Set additional fields
	if receipt.Priority > 0 {
		metadata.Priority = receipt.Priority
	}

	if receipt.Nonce > 0 {
		metadata.Nonce = receipt.Nonce
	}

	if receipt.Size > 0 {
		metadata.Size = receipt.Size
	}

	// Add computed fields
	if !b.config.CompactMode {
		metadata.Extensions["receipt_hash"] = b.computeReceiptHash(receipt)
		metadata.Extensions["metadata_generated_at"] = time.Now().UTC()
	}

	return metadata, nil
}

// buildExecutionContext constructs execution context from receipt
func (b *MetadataBuilder) buildExecutionContext(receipt *state.Receipt, metadata *TransactionMetadata) error {
	execContext := &ExecutionContext{
		GasUsed:       receipt.GasUsed,
		GasLimit:      receipt.GasLimit,
		ExecutionTime: float64(receipt.ExecutionTime.Nanoseconds()) / 1e6, // Convert to milliseconds
	}

	// Add optional fields
	if receipt.GasPrice > 0 {
		execContext.GasPrice = &receipt.GasPrice
	}

	if receipt.FunctionName != "" {
		execContext.FunctionName = receipt.FunctionName
	}

	if receipt.WasmModule != "" {
		execContext.WasmModule = receipt.WasmModule
	}

	if receipt.MemoryUsed > 0 {
		execContext.MemoryUsed = &receipt.MemoryUsed
	}

	metadata.ExecutionContext = execContext
	return nil
}

// buildBridgeInfo constructs bridge info from receipt
func (b *MetadataBuilder) buildBridgeInfo(receipt *state.Receipt, metadata *TransactionMetadata) error {
	// Check if bridge info is available
	if receipt.L0TxHash == "" {
		return nil
	}

	bridgeInfo := &BridgeInfo{
		L0TxHash:   receipt.L0TxHash,
		RetryCount: receipt.RetryCount,
	}

	// Add optional fields
	if receipt.L0BlockHeight > 0 {
		bridgeInfo.L0BlockHeight = &receipt.L0BlockHeight
	}

	if !receipt.SubmissionTime.IsZero() {
		bridgeInfo.SubmissionTime = &receipt.SubmissionTime
	}

	if !receipt.ConfirmationTime.IsZero() {
		bridgeInfo.ConfirmationTime = &receipt.ConfirmationTime
	}

	if receipt.BridgeCost > 0 {
		bridgeInfo.BridgeCost = &receipt.BridgeCost
	}

	metadata.BridgeInfo = bridgeInfo
	return nil
}

// buildStateChanges constructs state changes from receipt
func (b *MetadataBuilder) buildStateChanges(receipt *state.Receipt, metadata *TransactionMetadata) error {
	if len(receipt.StateChanges) == 0 {
		return nil
	}

	stateChanges := make([]StateChange, len(receipt.StateChanges))

	for i, change := range receipt.StateChanges {
		stateChange := StateChange{
			Key: hex.EncodeToString(change.Key),
		}

		// Set operation
		switch change.Operation {
		case state.StateOpSet:
			stateChange.Operation = "set"
			stateChange.NewValue = hex.EncodeToString(change.NewValue)
			if len(change.OldValue) > 0 {
				stateChange.OldValue = hex.EncodeToString(change.OldValue)
			}
		case state.StateOpDelete:
			stateChange.Operation = "delete"
			if len(change.OldValue) > 0 {
				stateChange.OldValue = hex.EncodeToString(change.OldValue)
			}
		default:
			return fmt.Errorf("unknown state operation: %d", change.Operation)
		}

		stateChanges[i] = stateChange
	}

	metadata.StateChanges = stateChanges
	return nil
}

// buildLogs constructs log entries from receipt
func (b *MetadataBuilder) buildLogs(receipt *state.Receipt, metadata *TransactionMetadata) error {
	if len(receipt.Logs) == 0 {
		return nil
	}

	logs := make([]LogEntry, len(receipt.Logs))

	for i, log := range receipt.Logs {
		logEntry := LogEntry{
			Message:   log.Message,
			Timestamp: log.Timestamp,
		}

		// Map log level
		switch log.Level {
		case state.LogLevelDebug:
			logEntry.Level = "debug"
		case state.LogLevelInfo:
			logEntry.Level = "info"
		case state.LogLevelWarn:
			logEntry.Level = "warn"
		case state.LogLevelError:
			logEntry.Level = "error"
		default:
			logEntry.Level = "info"
		}

		if log.Source != "" {
			logEntry.Source = log.Source
		}

		logs[i] = logEntry
	}

	metadata.Logs = logs
	return nil
}

// buildSignatureInfo constructs signature info from receipt
func (b *MetadataBuilder) buildSignatureInfo(receipt *state.Receipt, metadata *TransactionMetadata) error {
	// Check if signature info is available
	if len(receipt.Signature) == 0 {
		return nil
	}

	sigInfo := &SignatureInfo{
		Algorithm: receipt.SignatureAlgorithm,
		PublicKey: hex.EncodeToString(receipt.PublicKey),
		Signature: hex.EncodeToString(receipt.Signature),
	}

	if receipt.KeyHash != "" {
		sigInfo.KeyHash = receipt.KeyHash
	}

	metadata.Signature = sigInfo
	return nil
}

// computeReceiptHash computes a hash of the receipt for integrity checking
func (b *MetadataBuilder) computeReceiptHash(receipt *state.Receipt) string {
	// Create a simplified representation for hashing
	hashData := fmt.Sprintf("%s:%d:%d:%d:%t",
		receipt.TxID,
		receipt.BlockHeight,
		receipt.GasUsed,
		receipt.ExecutionTime.Nanoseconds(),
		receipt.Success,
	)

	hash := sha256.Sum256([]byte(hashData))
	return hex.EncodeToString(hash[:])
}

// BuildBatch constructs metadata for multiple receipts
func (b *MetadataBuilder) BuildBatch(receipts []*state.Receipt) ([]*TransactionMetadata, error) {
	if len(receipts) == 0 {
		return nil, fmt.Errorf("receipts slice is empty")
	}

	metadata := make([]*TransactionMetadata, len(receipts))

	for i, receipt := range receipts {
		meta, err := b.BuildFromReceipt(receipt)
		if err != nil {
			return nil, fmt.Errorf("failed to build metadata for receipt %d: %w", i, err)
		}
		metadata[i] = meta
	}

	return metadata, nil
}

// ToJSON converts metadata to JSON bytes
func (b *MetadataBuilder) ToJSON(metadata *TransactionMetadata) ([]byte, error) {
	if b.config.CompactMode {
		return json.Marshal(metadata)
	} else {
		return json.MarshalIndent(metadata, "", "  ")
	}
}

// FromJSON creates metadata from JSON bytes
func (b *MetadataBuilder) FromJSON(data []byte) (*TransactionMetadata, error) {
	var metadata TransactionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return &metadata, nil
}

// Validate validates the constructed metadata against the JSON schema
func (b *MetadataBuilder) Validate(metadata *TransactionMetadata) error {
	// Basic validation
	if metadata.TxID == "" {
		return fmt.Errorf("transaction ID is required")
	}

	if metadata.BlockHeight == 0 {
		return fmt.Errorf("block height must be greater than 0")
	}

	if metadata.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}

	if metadata.ExecutionContext == nil {
		return fmt.Errorf("execution context is required")
	}

	if metadata.ExecutionContext.GasUsed == 0 {
		return fmt.Errorf("gas used must be greater than 0")
	}

	if metadata.ExecutionContext.GasLimit == 0 {
		return fmt.Errorf("gas limit must be greater than 0")
	}

	if metadata.ExecutionContext.ExecutionTime < 0 {
		return fmt.Errorf("execution time cannot be negative")
	}

	// Validate state changes
	for i, change := range metadata.StateChanges {
		if change.Operation != "set" && change.Operation != "delete" {
			return fmt.Errorf("invalid state operation at index %d: %s", i, change.Operation)
		}
		if change.Key == "" {
			return fmt.Errorf("state change key is required at index %d", i)
		}
	}

	// Validate log entries
	for i, log := range metadata.Logs {
		if log.Level == "" {
			return fmt.Errorf("log level is required at index %d", i)
		}
		if log.Message == "" {
			return fmt.Errorf("log message is required at index %d", i)
		}
		if log.Timestamp.IsZero() {
			return fmt.Errorf("log timestamp is required at index %d", i)
		}
	}

	return nil
}

// GetStats returns statistics about the metadata
func (b *MetadataBuilder) GetStats(metadata *TransactionMetadata) map[string]interface{} {
	stats := map[string]interface{}{
		"tx_id":          metadata.TxID,
		"block_height":   metadata.BlockHeight,
		"gas_used":       metadata.ExecutionContext.GasUsed,
		"gas_limit":      metadata.ExecutionContext.GasLimit,
		"execution_time": metadata.ExecutionContext.ExecutionTime,
		"state_changes":  len(metadata.StateChanges),
		"log_entries":    len(metadata.Logs),
		"has_bridge":     metadata.BridgeInfo != nil,
		"has_signature":  metadata.Signature != nil,
		"schema_version": metadata.Version,
	}

	if metadata.ExecutionContext.GasLimit > 0 {
		gasEfficiency := float64(metadata.ExecutionContext.GasUsed) / float64(metadata.ExecutionContext.GasLimit)
		stats["gas_efficiency"] = gasEfficiency
	}

	return stats
}