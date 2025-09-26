package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/opendlt/accumulate-accumen/types/proto/accumen"
	"google.golang.org/protobuf/proto"
)

// Receipt represents a transaction execution receipt
type Receipt struct {
	TxHash   [32]byte  `json:"tx_hash"`
	Events   []Event   `json:"events"`
	GasUsed  uint64    `json:"gas_used"`
	Error    string    `json:"error,omitempty"`
	L0TxIDs  []string  `json:"l0_tx_ids"`
}

// Event represents an event emitted during transaction execution
type Event struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

// LegacyReceipt represents the original execution receipt for WASM contract execution
type LegacyReceipt struct {
	// Execution metadata
	Success      bool      `json:"success"`
	Timestamp    time.Time `json:"timestamp"`
	BlockHeight  uint64    `json:"block_height,omitempty"`
	TxHash       []byte    `json:"tx_hash,omitempty"`

	// Function execution details
	FunctionName string    `json:"function_name"`
	Parameters   [][]byte  `json:"parameters,omitempty"`
	ReturnValue  []byte    `json:"return_value,omitempty"`

	// Gas and resource usage
	GasUsed      uint64    `json:"gas_used"`
	GasLimit     uint64    `json:"gas_limit"`
	GasPrice     uint64    `json:"gas_price,omitempty"`

	// Memory usage (in pages)
	MemoryUsed   uint32    `json:"memory_used"`
	MemoryLimit  uint32    `json:"memory_limit"`

	// Execution time (for performance metrics)
	ExecutionTime time.Duration `json:"execution_time"`

	// Error information
	Error        string    `json:"error,omitempty"`
	ErrorCode    uint32    `json:"error_code,omitempty"`

	// State changes
	StateChanges []StateChange `json:"state_changes,omitempty"`

	// Logs emitted during execution
	Logs         []LogEntry `json:"logs,omitempty"`
}

// StateChange represents a change to the key-value store
type StateChange struct {
	Operation Operation `json:"operation"`
	Key       []byte    `json:"key"`
	OldValue  []byte    `json:"old_value,omitempty"`
	NewValue  []byte    `json:"new_value,omitempty"`
}

// Operation represents the type of state operation
type Operation uint8

const (
	OperationSet Operation = iota
	OperationDelete
)

// String returns the string representation of an operation
func (op Operation) String() string {
	switch op {
	case OperationSet:
		return "set"
	case OperationDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// LogEntry represents a log message emitted during execution
type LogEntry struct {
	Level     LogLevel `json:"level"`
	Message   string   `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// LogLevel represents the severity level of a log entry
type LogLevel uint8

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// String returns the string representation of a log level
func (level LogLevel) String() string {
	switch level {
	case LogLevelDebug:
		return "debug"
	case LogLevelInfo:
		return "info"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "error"
	default:
		return "unknown"
	}
}

// NewReceipt creates a new transaction receipt
func NewReceipt(txHash [32]byte) *Receipt {
	return &Receipt{
		TxHash:  txHash,
		Events:  make([]Event, 0),
		L0TxIDs: make([]string, 0),
	}
}

// NewLegacyReceipt creates a new execution receipt
func NewLegacyReceipt() *LegacyReceipt {
	return &LegacyReceipt{
		Timestamp:     time.Now(),
		StateChanges:  make([]StateChange, 0),
		Logs:          make([]LogEntry, 0),
		Parameters:    make([][]byte, 0),
	}
}

// AddEvent adds an event to the receipt
func (r *Receipt) AddEvent(eventType string, data []byte) {
	event := Event{
		Type: eventType,
		Data: append([]byte(nil), data...), // Copy data
	}
	r.Events = append(r.Events, event)
}

// AddL0TxID adds an L0 transaction ID to the receipt
func (r *Receipt) AddL0TxID(txID string) {
	r.L0TxIDs = append(r.L0TxIDs, txID)
}

// AddStateChange adds a state change to the legacy receipt
func (r *LegacyReceipt) AddStateChange(op Operation, key, oldValue, newValue []byte) {
	change := StateChange{
		Operation: op,
		Key:       make([]byte, len(key)),
		OldValue:  make([]byte, len(oldValue)),
		NewValue:  make([]byte, len(newValue)),
	}

	copy(change.Key, key)
	copy(change.OldValue, oldValue)
	copy(change.NewValue, newValue)

	r.StateChanges = append(r.StateChanges, change)
}

// AddLog adds a log entry to the legacy receipt
func (r *LegacyReceipt) AddLog(level LogLevel, message string) {
	log := LogEntry{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
	}

	r.Logs = append(r.Logs, log)
}

// SetError sets error information in the legacy receipt
func (r *LegacyReceipt) SetError(err error, code uint32) {
	r.Success = false
	if err != nil {
		r.Error = err.Error()
	}
	r.ErrorCode = code
}

// GetGasEfficiency returns the gas efficiency ratio (0-1)
func (r *LegacyReceipt) GetGasEfficiency() float64 {
	if r.GasLimit == 0 {
		return 0
	}
	return float64(r.GasUsed) / float64(r.GasLimit)
}

// GetMemoryEfficiency returns the memory efficiency ratio (0-1)
func (r *LegacyReceipt) GetMemoryEfficiency() float64 {
	if r.MemoryLimit == 0 {
		return 0
	}
	return float64(r.MemoryUsed) / float64(r.MemoryLimit)
}

// ToJSON converts the receipt to JSON
func (r *Receipt) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ToJSON converts the legacy receipt to JSON
func (r *LegacyReceipt) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// FromJSON creates a receipt from JSON data
func FromJSON(data []byte) (*Receipt, error) {
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

// FromLegacyJSON creates a legacy receipt from JSON data
func FromLegacyJSON(data []byte) (*LegacyReceipt, error) {
	var receipt LegacyReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

// Clone creates a deep copy of the receipt
func (r *Receipt) Clone() *Receipt {
	clone := &Receipt{
		TxHash:  r.TxHash,
		GasUsed: r.GasUsed,
		Error:   r.Error,
	}

	// Deep copy events
	clone.Events = make([]Event, len(r.Events))
	for i, event := range r.Events {
		clone.Events[i] = Event{
			Type: event.Type,
			Data: append([]byte(nil), event.Data...),
		}
	}

	// Deep copy L0 transaction IDs
	clone.L0TxIDs = make([]string, len(r.L0TxIDs))
	copy(clone.L0TxIDs, r.L0TxIDs)

	return clone
}

// Clone creates a deep copy of the legacy receipt
func (r *LegacyReceipt) Clone() *LegacyReceipt {
	clone := &LegacyReceipt{
		Success:       r.Success,
		Timestamp:     r.Timestamp,
		BlockHeight:   r.BlockHeight,
		FunctionName:  r.FunctionName,
		GasUsed:       r.GasUsed,
		GasLimit:      r.GasLimit,
		GasPrice:      r.GasPrice,
		MemoryUsed:    r.MemoryUsed,
		MemoryLimit:   r.MemoryLimit,
		ExecutionTime: r.ExecutionTime,
		Error:         r.Error,
		ErrorCode:     r.ErrorCode,
	}

	// Deep copy slices
	if r.TxHash != nil {
		clone.TxHash = make([]byte, len(r.TxHash))
		copy(clone.TxHash, r.TxHash)
	}

	if r.ReturnValue != nil {
		clone.ReturnValue = make([]byte, len(r.ReturnValue))
		copy(clone.ReturnValue, r.ReturnValue)
	}

	// Deep copy parameters
	clone.Parameters = make([][]byte, len(r.Parameters))
	for i, param := range r.Parameters {
		clone.Parameters[i] = make([]byte, len(param))
		copy(clone.Parameters[i], param)
	}

	// Deep copy state changes
	clone.StateChanges = make([]StateChange, len(r.StateChanges))
	copy(clone.StateChanges, r.StateChanges)

	// Deep copy logs
	clone.Logs = make([]LogEntry, len(r.Logs))
	copy(clone.Logs, r.Logs)

	return clone
}

// HashTxResult computes the SHA256 hash of a transaction result
func HashTxResult(result *accumen.TxResult) []byte {
	// Serialize the TxResult using protobuf
	data, err := proto.Marshal(result)
	if err != nil {
		// Should never happen with valid protobuf messages
		panic("failed to marshal TxResult: " + err.Error())
	}

	// Return SHA256 hash
	hash := sha256.Sum256(data)
	return hash[:]
}

// MerkleRoot computes the Merkle root of a list of hashes
// Uses a binary tree structure with SHA256 for internal nodes
func MerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		// Empty tree has zero hash
		return make([]byte, 32)
	}

	if len(hashes) == 1 {
		return hashes[0]
	}

	// Create a copy to avoid mutating the input
	nodes := make([][]byte, len(hashes))
	copy(nodes, hashes)

	// Build tree bottom-up
	for len(nodes) > 1 {
		var nextLevel [][]byte

		// Process pairs of nodes
		for i := 0; i < len(nodes); i += 2 {
			if i+1 < len(nodes) {
				// Hash pair of nodes
				left := nodes[i]
				right := nodes[i+1]
				combined := append(left, right...)
				hash := sha256.Sum256(combined)
				nextLevel = append(nextLevel, hash[:])
			} else {
				// Odd number of nodes - promote the last one
				nextLevel = append(nextLevel, nodes[i])
			}
		}

		nodes = nextLevel
	}

	return nodes[0]
}

// ComputeTxsRoot computes the Merkle root of transaction hashes
func ComputeTxsRoot(results []*accumen.TxResult) []byte {
	if len(results) == 0 {
		return make([]byte, 32)
	}

	var hashes [][]byte
	for _, result := range results {
		hashes = append(hashes, result.TxHash)
	}

	return MerkleRoot(hashes)
}

// ComputeResultsRoot computes the Merkle root of transaction result hashes
func ComputeResultsRoot(results []*accumen.TxResult) []byte {
	if len(results) == 0 {
		return make([]byte, 32)
	}

	var hashes [][]byte
	for _, result := range results {
		hashes = append(hashes, HashTxResult(result))
	}

	return MerkleRoot(hashes)
}

// VerifyTxInclusion verifies that a transaction is included in a block
// Returns the Merkle proof path and verification result
func VerifyTxInclusion(txHash []byte, txsRoot []byte, allTxHashes [][]byte) ([][]byte, bool) {
	// Find the index of the transaction
	txIndex := -1
	for i, hash := range allTxHashes {
		if len(hash) == len(txHash) {
			match := true
			for j := range hash {
				if hash[j] != txHash[j] {
					match = false
					break
				}
			}
			if match {
				txIndex = i
				break
			}
		}
	}

	if txIndex == -1 {
		return nil, false
	}

	// Generate Merkle proof
	proof := generateMerkleProof(allTxHashes, txIndex)

	// Verify the proof
	computedRoot := verifyMerkleProof(txHash, proof, txIndex)

	// Check if computed root matches expected root
	if len(computedRoot) != len(txsRoot) {
		return proof, false
	}

	for i := range computedRoot {
		if computedRoot[i] != txsRoot[i] {
			return proof, false
		}
	}

	return proof, true
}

// generateMerkleProof generates a Merkle proof for a transaction at given index
func generateMerkleProof(hashes [][]byte, index int) [][]byte {
	if len(hashes) <= 1 {
		return nil
	}

	var proof [][]byte
	nodes := make([][]byte, len(hashes))
	copy(nodes, hashes)
	currentIndex := index

	// Build proof by collecting sibling nodes at each level
	for len(nodes) > 1 {
		var nextLevel [][]byte
		nextIndex := currentIndex / 2

		for i := 0; i < len(nodes); i += 2 {
			if i+1 < len(nodes) {
				left := nodes[i]
				right := nodes[i+1]

				// Add sibling to proof if current node is part of the path
				if i == currentIndex || i+1 == currentIndex {
					if i == currentIndex {
						proof = append(proof, right)
					} else {
						proof = append(proof, left)
					}
				}

				// Compute parent hash
				combined := append(left, right...)
				hash := sha256.Sum256(combined)
				nextLevel = append(nextLevel, hash[:])
			} else {
				// Odd number of nodes
				nextLevel = append(nextLevel, nodes[i])
			}
		}

		nodes = nextLevel
		currentIndex = nextIndex
	}

	return proof
}

// verifyMerkleProof verifies a Merkle proof and returns the computed root
func verifyMerkleProof(leafHash []byte, proof [][]byte, index int) []byte {
	currentHash := leafHash
	currentIndex := index

	for _, sibling := range proof {
		if currentIndex%2 == 0 {
			// Current node is left child
			combined := append(currentHash, sibling...)
			hash := sha256.Sum256(combined)
			currentHash = hash[:]
		} else {
			// Current node is right child
			combined := append(sibling, currentHash...)
			hash := sha256.Sum256(combined)
			currentHash = hash[:]
		}
		currentIndex = currentIndex / 2
	}

	return currentHash
}

// SortTxResults sorts transaction results by transaction hash for deterministic ordering
func SortTxResults(results []*accumen.TxResult) {
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i].TxHash, results[j].TxHash
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
}

// KVStore interface for key-value storage operations
type KVStore interface {
	Get(key []byte) ([]byte, error)
	Set(key []byte, value []byte) error
	Delete(key []byte) error
	Iterate(fn func(key, value []byte) error) error
}

// StoreReceipt stores a receipt in the KV store
func StoreReceipt(kv KVStore, height uint64, index int, receipt *Receipt) error {
	// Serialize receipt to JSON
	receiptData, err := receipt.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize receipt: %w", err)
	}

	// Create keys for storage
	// Primary key: /receipts/height/index -> receipt data
	heightIndexKey := fmt.Sprintf("/receipts/%d/%d", height, index)
	if err := kv.Set([]byte(heightIndexKey), receiptData); err != nil {
		return fmt.Errorf("failed to store receipt by height/index: %w", err)
	}

	// Secondary index: /receipts/hash/<hash> -> height,index
	hashHex := hex.EncodeToString(receipt.TxHash[:])
	hashKey := fmt.Sprintf("/receipts/hash/%s", hashHex)
	locationData := fmt.Sprintf("%d,%d", height, index)
	if err := kv.Set([]byte(hashKey), []byte(locationData)); err != nil {
		return fmt.Errorf("failed to store receipt hash index: %w", err)
	}

	return nil
}

// LoadReceiptByHash loads a receipt by transaction hash
func LoadReceiptByHash(kv KVStore, hash [32]byte) (*Receipt, uint64, int, error) {
	// Look up location by hash
	hashHex := hex.EncodeToString(hash[:])
	hashKey := fmt.Sprintf("/receipts/hash/%s", hashHex)

	locationData, err := kv.Get([]byte(hashKey))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("receipt not found for hash %s: %w", hashHex, err)
	}

	// Parse location (height,index)
	var height uint64
	var index int
	if _, err := fmt.Sscanf(string(locationData), "%d,%d", &height, &index); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to parse receipt location: %w", err)
	}

	// Load receipt data
	heightIndexKey := fmt.Sprintf("/receipts/%d/%d", height, index)
	receiptData, err := kv.Get([]byte(heightIndexKey))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to load receipt data: %w", err)
	}

	// Deserialize receipt
	receipt, err := FromJSON(receiptData)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to deserialize receipt: %w", err)
	}

	return receipt, height, index, nil
}

// LoadReceiptByHeightIndex loads a receipt by block height and transaction index
func LoadReceiptByHeightIndex(kv KVStore, height uint64, index int) (*Receipt, error) {
	heightIndexKey := fmt.Sprintf("/receipts/%d/%d", height, index)
	receiptData, err := kv.Get([]byte(heightIndexKey))
	if err != nil {
		return nil, fmt.Errorf("receipt not found for height %d, index %d: %w", height, index, err)
	}

	receipt, err := FromJSON(receiptData)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize receipt: %w", err)
	}

	return receipt, nil
}

// DeleteReceipt removes a receipt from storage
func DeleteReceipt(kv KVStore, height uint64, index int, hash [32]byte) error {
	// Delete primary storage
	heightIndexKey := fmt.Sprintf("/receipts/%d/%d", height, index)
	if err := kv.Delete([]byte(heightIndexKey)); err != nil {
		return fmt.Errorf("failed to delete receipt data: %w", err)
	}

	// Delete hash index
	hashHex := hex.EncodeToString(hash[:])
	hashKey := fmt.Sprintf("/receipts/hash/%s", hashHex)
	if err := kv.Delete([]byte(hashKey)); err != nil {
		return fmt.Errorf("failed to delete receipt hash index: %w", err)
	}

	return nil
}

// ListReceiptsByHeight returns all receipts for a given block height
func ListReceiptsByHeight(kv KVStore, height uint64) ([]*Receipt, error) {
	var receipts []*Receipt
	prefix := fmt.Sprintf("/receipts/%d/", height)

	err := kv.Iterate(func(key, value []byte) error {
		keyStr := string(key)
		if len(keyStr) >= len(prefix) && keyStr[:len(prefix)] == prefix {
			receipt, err := FromJSON(value)
			if err != nil {
				return fmt.Errorf("failed to deserialize receipt at key %s: %w", keyStr, err)
			}
			receipts = append(receipts, receipt)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate receipts for height %d: %w", height, err)
	}

	return receipts, nil
}