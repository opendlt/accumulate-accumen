package outputs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/engine/state"
)

// Limits defines resource limits for output processing
type Limits struct {
	// Maximum number of outputs per scope
	MaxOutputsPerScope int

	// Maximum total size of all outputs in bytes
	MaxTotalSize uint64

	// Maximum size per individual output in bytes
	MaxOutputSize uint64

	// Maximum number of dependencies per output
	MaxDependencies int

	// Maximum processing time per output
	MaxProcessingTime time.Duration

	// Maximum age before output expires
	MaxAge time.Duration

	// Maximum retry attempts
	MaxRetries int

	// Rate limiting
	MaxOutputsPerSecond int
	MaxOutputsPerMinute int
	MaxOutputsPerHour   int

	// Memory limits
	MaxMemoryUsage uint64

	// Gas limits
	MaxGasPerOutput uint64
	MaxTotalGas     uint64

	// Priority limits
	MinPriority int64
	MaxPriority int64

	// Chain-specific limits
	ChainLimits map[string]*ChainLimits
}

// ChainLimits defines chain-specific resource limits
type ChainLimits struct {
	MaxOutputsPerBlock int
	MaxSizePerBlock    uint64
	MaxGasPerBlock     uint64
	BlockTime          time.Duration
	ConfirmationTime   time.Duration
}

// OutputStatus represents the status of an output
type OutputStatus int

const (
	OutputStatusPending OutputStatus = iota
	OutputStatusReady
	OutputStatusSubmitted
	OutputStatusConfirmed
	OutputStatusFailed
	OutputStatusExpired
)

// Output represents a basic output structure for validation
type Output struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Data         []byte          `json:"data"`
	Dependencies []string        `json:"dependencies"`
	Priority     int64           `json:"priority"`
	GasLimit     uint64          `json:"gas_limit"`
	Created      time.Time       `json:"created"`
	Updated      time.Time       `json:"updated"`
	Status       OutputStatus    `json:"status"`
	Metadata     *OutputMetadata `json:"metadata,omitempty"`
}

// ToEnvelope converts the output to a messaging envelope
func (o *Output) ToEnvelope(ctx context.Context) (*messaging.Envelope, error) {
	// TODO: implement actual envelope creation logic
	return &messaging.Envelope{}, nil
}

// Cleanup performs any necessary cleanup for the output
func (o *Output) Cleanup() error {
	// TODO: implement actual cleanup logic
	return nil
}

// Counter represents a simple in-memory counter for limits tracking
type Counter struct {
	count uint64
	mu    sync.RWMutex
}

// NewCounter creates a new counter
func NewCounter() *Counter {
	return &Counter{}
}

// Increment increments the counter
func (c *Counter) Increment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}

// Get returns the current count
func (c *Counter) Get() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.count
}

// Reset resets the counter to zero
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count = 0
}

// DefaultLimits returns default resource limits
func DefaultLimits() *Limits {
	return &Limits{
		MaxOutputsPerScope:  1000,
		MaxTotalSize:        10 * 1024 * 1024, // 10MB
		MaxOutputSize:       1024 * 1024,      // 1MB
		MaxDependencies:     100,
		MaxProcessingTime:   30 * time.Second,
		MaxAge:              24 * time.Hour,
		MaxRetries:          3,
		MaxOutputsPerSecond: 10,
		MaxOutputsPerMinute: 100,
		MaxOutputsPerHour:   1000,
		MaxMemoryUsage:      100 * 1024 * 1024, // 100MB
		MaxGasPerOutput:     1000000,           // 1M gas
		MaxTotalGas:         10000000,          // 10M gas
		MinPriority:         0,
		MaxPriority:         1000,
		ChainLimits: map[string]*ChainLimits{
			"accumulate": {
				MaxOutputsPerBlock: 100,
				MaxSizePerBlock:    1024 * 1024, // 1MB
				MaxGasPerBlock:     5000000,     // 5M gas
				BlockTime:          15 * time.Second,
				ConfirmationTime:   30 * time.Second,
			},
		},
	}
}

// TestLimits returns limits suitable for testing
func TestLimits() *Limits {
	limits := DefaultLimits()
	limits.MaxAge = 5 * time.Minute
	limits.MaxProcessingTime = 5 * time.Second
	limits.MaxRetries = 1
	return limits
}

// StrictLimits returns more restrictive limits for production
func StrictLimits() *Limits {
	return &Limits{
		MaxOutputsPerScope:  100,
		MaxTotalSize:        1024 * 1024, // 1MB
		MaxOutputSize:       64 * 1024,   // 64KB
		MaxDependencies:     10,
		MaxProcessingTime:   10 * time.Second,
		MaxAge:              1 * time.Hour,
		MaxRetries:          2,
		MaxOutputsPerSecond: 5,
		MaxOutputsPerMinute: 50,
		MaxOutputsPerHour:   500,
		MaxMemoryUsage:      50 * 1024 * 1024, // 50MB
		MaxGasPerOutput:     500000,           // 500K gas
		MaxTotalGas:         5000000,          // 5M gas
		MinPriority:         10,
		MaxPriority:         500,
		ChainLimits: map[string]*ChainLimits{
			"accumulate": {
				MaxOutputsPerBlock: 50,
				MaxSizePerBlock:    512 * 1024, // 512KB
				MaxGasPerBlock:     2500000,    // 2.5M gas
				BlockTime:          15 * time.Second,
				ConfirmationTime:   45 * time.Second,
			},
		},
	}
}

// ValidateOutput validates an output against the limits
func (l *Limits) ValidateOutput(output *Output) error {
	if output == nil {
		return fmt.Errorf("output cannot be nil")
	}

	// Check output size
	if l.MaxOutputSize > 0 {
		if uint64(len(output.Data)) > l.MaxOutputSize {
			return fmt.Errorf("output size %d exceeds maximum %d", len(output.Data), l.MaxOutputSize)
		}
	}

	// Check dependencies
	if l.MaxDependencies > 0 {
		if len(output.Dependencies) > l.MaxDependencies {
			return fmt.Errorf("output has %d dependencies, maximum %d allowed",
				len(output.Dependencies), l.MaxDependencies)
		}
	}

	// Check priority
	if output.Priority < l.MinPriority || output.Priority > l.MaxPriority {
		return fmt.Errorf("output priority %d not in range [%d, %d]",
			output.Priority, l.MinPriority, l.MaxPriority)
	}

	// Check gas limit
	if l.MaxGasPerOutput > 0 {
		if output.GasLimit > l.MaxGasPerOutput {
			return fmt.Errorf("output gas limit %d exceeds maximum %d",
				output.GasLimit, l.MaxGasPerOutput)
		}
	}

	// Check age
	if l.MaxAge > 0 {
		age := time.Since(output.Created)
		if age > l.MaxAge {
			return fmt.Errorf("output age %v exceeds maximum %v", age, l.MaxAge)
		}
	}

	return nil
}

// ValidateScope validates a scope against the limits
func (l *Limits) ValidateScope(outputCount int, totalSize uint64, totalGas uint64) error {
	// Check output count
	if l.MaxOutputsPerScope > 0 {
		if outputCount > l.MaxOutputsPerScope {
			return fmt.Errorf("scope has %d outputs, maximum %d allowed",
				outputCount, l.MaxOutputsPerScope)
		}
	}

	// Check total size
	if l.MaxTotalSize > 0 {
		if totalSize > l.MaxTotalSize {
			return fmt.Errorf("scope total size %d exceeds maximum %d",
				totalSize, l.MaxTotalSize)
		}
	}

	// Check total gas
	if l.MaxTotalGas > 0 {
		if totalGas > l.MaxTotalGas {
			return fmt.Errorf("scope total gas %d exceeds maximum %d",
				totalGas, l.MaxTotalGas)
		}
	}

	return nil
}

// GetChainLimits returns limits for a specific chain
func (l *Limits) GetChainLimits(chainID string) *ChainLimits {
	if l.ChainLimits == nil {
		return nil
	}
	return l.ChainLimits[chainID]
}

// SetChainLimits sets limits for a specific chain
func (l *Limits) SetChainLimits(chainID string, limits *ChainLimits) {
	if l.ChainLimits == nil {
		l.ChainLimits = make(map[string]*ChainLimits)
	}
	l.ChainLimits[chainID] = limits
}

// Clone creates a deep copy of the limits
func (l *Limits) Clone() *Limits {
	clone := &Limits{
		MaxOutputsPerScope:  l.MaxOutputsPerScope,
		MaxTotalSize:        l.MaxTotalSize,
		MaxOutputSize:       l.MaxOutputSize,
		MaxDependencies:     l.MaxDependencies,
		MaxProcessingTime:   l.MaxProcessingTime,
		MaxAge:              l.MaxAge,
		MaxRetries:          l.MaxRetries,
		MaxOutputsPerSecond: l.MaxOutputsPerSecond,
		MaxOutputsPerMinute: l.MaxOutputsPerMinute,
		MaxOutputsPerHour:   l.MaxOutputsPerHour,
		MaxMemoryUsage:      l.MaxMemoryUsage,
		MaxGasPerOutput:     l.MaxGasPerOutput,
		MaxTotalGas:         l.MaxTotalGas,
		MinPriority:         l.MinPriority,
		MaxPriority:         l.MaxPriority,
		ChainLimits:         make(map[string]*ChainLimits),
	}

	// Clone chain limits
	if l.ChainLimits != nil {
		for chainID, chainLimits := range l.ChainLimits {
			clone.ChainLimits[chainID] = &ChainLimits{
				MaxOutputsPerBlock: chainLimits.MaxOutputsPerBlock,
				MaxSizePerBlock:    chainLimits.MaxSizePerBlock,
				MaxGasPerBlock:     chainLimits.MaxGasPerBlock,
				BlockTime:          chainLimits.BlockTime,
				ConfirmationTime:   chainLimits.ConfirmationTime,
			}
		}
	}

	return clone
}

// Update updates the limits with values from another limits object
func (l *Limits) Update(other *Limits) {
	if other == nil {
		return
	}

	if other.MaxOutputsPerScope > 0 {
		l.MaxOutputsPerScope = other.MaxOutputsPerScope
	}
	if other.MaxTotalSize > 0 {
		l.MaxTotalSize = other.MaxTotalSize
	}
	if other.MaxOutputSize > 0 {
		l.MaxOutputSize = other.MaxOutputSize
	}
	if other.MaxDependencies > 0 {
		l.MaxDependencies = other.MaxDependencies
	}
	if other.MaxProcessingTime > 0 {
		l.MaxProcessingTime = other.MaxProcessingTime
	}
	if other.MaxAge > 0 {
		l.MaxAge = other.MaxAge
	}
	if other.MaxRetries > 0 {
		l.MaxRetries = other.MaxRetries
	}
	if other.MaxOutputsPerSecond > 0 {
		l.MaxOutputsPerSecond = other.MaxOutputsPerSecond
	}
	if other.MaxOutputsPerMinute > 0 {
		l.MaxOutputsPerMinute = other.MaxOutputsPerMinute
	}
	if other.MaxOutputsPerHour > 0 {
		l.MaxOutputsPerHour = other.MaxOutputsPerHour
	}
	if other.MaxMemoryUsage > 0 {
		l.MaxMemoryUsage = other.MaxMemoryUsage
	}
	if other.MaxGasPerOutput > 0 {
		l.MaxGasPerOutput = other.MaxGasPerOutput
	}
	if other.MaxTotalGas > 0 {
		l.MaxTotalGas = other.MaxTotalGas
	}

	l.MinPriority = other.MinPriority
	l.MaxPriority = other.MaxPriority

	// Update chain limits
	if other.ChainLimits != nil {
		if l.ChainLimits == nil {
			l.ChainLimits = make(map[string]*ChainLimits)
		}
		for chainID, chainLimits := range other.ChainLimits {
			l.ChainLimits[chainID] = &ChainLimits{
				MaxOutputsPerBlock: chainLimits.MaxOutputsPerBlock,
				MaxSizePerBlock:    chainLimits.MaxSizePerBlock,
				MaxGasPerBlock:     chainLimits.MaxGasPerBlock,
				BlockTime:          chainLimits.BlockTime,
				ConfirmationTime:   chainLimits.ConfirmationTime,
			}
		}
	}
}

// RateLimitCheck represents a rate limit check result
type RateLimitCheck struct {
	Allowed   bool
	Remaining int
	ResetTime time.Time
}

// CheckRateLimit checks if the current rate is within limits
// This is a simplified implementation - in production you'd want proper rate limiting
func (l *Limits) CheckRateLimit(count int, duration time.Duration) RateLimitCheck {
	var limit int
	var allowed bool

	switch {
	case duration <= time.Second:
		limit = l.MaxOutputsPerSecond
		allowed = count <= limit
	case duration <= time.Minute:
		limit = l.MaxOutputsPerMinute
		allowed = count <= limit
	case duration <= time.Hour:
		limit = l.MaxOutputsPerHour
		allowed = count <= limit
	default:
		allowed = true
		limit = -1
	}

	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	return RateLimitCheck{
		Allowed:   allowed,
		Remaining: remaining,
		ResetTime: time.Now().Add(duration),
	}
}

// OperationCounter tracks per-operation counters with daily/hourly limits
type OperationCounter struct {
	mu    sync.RWMutex
	store state.KVStore
}

// OperationCounterKey represents the key for operation counting
type OperationCounterKey struct {
	ContractURL string `json:"contractUrl"`
	OpType      string `json:"opType"`
	Period      string `json:"period"`    // "hourly" or "daily"
	Timestamp   string `json:"timestamp"` // Hour: "2024-01-01T15" or Day: "2024-01-01"
}

// OperationCounterData represents the counter data stored in KV
type OperationCounterData struct {
	Count     uint64    `json:"count"`
	LastReset time.Time `json:"lastReset"`
}

// NewOperationCounter creates a new operation counter
func NewOperationCounter(store state.KVStore) *OperationCounter {
	return &OperationCounter{
		store: store,
	}
}

// IncrementOperation increments the counter for a specific contract and operation type
func (oc *OperationCounter) IncrementOperation(contractURL *url.URL, opType string) error {
	oc.mu.Lock()
	defer oc.mu.Unlock()

	now := time.Now().UTC()

	// Increment hourly counter
	if err := oc.incrementCounter(contractURL, opType, "hourly", now.Format("2006-01-02T15")); err != nil {
		return fmt.Errorf("failed to increment hourly counter: %w", err)
	}

	// Increment daily counter
	if err := oc.incrementCounter(contractURL, opType, "daily", now.Format("2006-01-02")); err != nil {
		return fmt.Errorf("failed to increment daily counter: %w", err)
	}

	return nil
}

// GetOperationCount returns the current count for a specific contract, operation, and period
func (oc *OperationCounter) GetOperationCount(contractURL *url.URL, opType string, period string) (uint64, error) {
	oc.mu.RLock()
	defer oc.mu.RUnlock()

	now := time.Now().UTC()
	var timestamp string

	switch period {
	case "hourly":
		timestamp = now.Format("2006-01-02T15")
	case "daily":
		timestamp = now.Format("2006-01-02")
	default:
		return 0, fmt.Errorf("invalid period: %s (must be 'hourly' or 'daily')", period)
	}

	key := OperationCounterKey{
		ContractURL: contractURL.String(),
		OpType:      opType,
		Period:      period,
		Timestamp:   timestamp,
	}

	data, err := oc.getCounterData(key)
	if err != nil {
		return 0, err
	}

	return data.Count, nil
}

// CheckOperationLimits checks if an operation is within daily/hourly limits
func (oc *OperationCounter) CheckOperationLimits(contractURL *url.URL, opType string, hourlyLimit, dailyLimit uint64) error {
	// Check hourly limit
	if hourlyLimit > 0 {
		hourlyCount, err := oc.GetOperationCount(contractURL, opType, "hourly")
		if err != nil {
			return fmt.Errorf("failed to get hourly count: %w", err)
		}
		if hourlyCount >= hourlyLimit {
			return fmt.Errorf("hourly limit exceeded for %s %s: %d/%d", contractURL, opType, hourlyCount, hourlyLimit)
		}
	}

	// Check daily limit
	if dailyLimit > 0 {
		dailyCount, err := oc.GetOperationCount(contractURL, opType, "daily")
		if err != nil {
			return fmt.Errorf("failed to get daily count: %w", err)
		}
		if dailyCount >= dailyLimit {
			return fmt.Errorf("daily limit exceeded for %s %s: %d/%d", contractURL, opType, dailyCount, dailyLimit)
		}
	}

	return nil
}

// incrementCounter increments a specific counter
func (oc *OperationCounter) incrementCounter(contractURL *url.URL, opType string, period string, timestamp string) error {
	key := OperationCounterKey{
		ContractURL: contractURL.String(),
		OpType:      opType,
		Period:      period,
		Timestamp:   timestamp,
	}

	data, err := oc.getCounterData(key)
	if err != nil {
		return err
	}

	data.Count++
	data.LastReset = time.Now().UTC()

	return oc.setCounterData(key, data)
}

// getCounterData retrieves counter data from KV store
func (oc *OperationCounter) getCounterData(key OperationCounterKey) (*OperationCounterData, error) {
	keyBytes, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal counter key: %w", err)
	}

	storeKey := fmt.Sprintf("/operation-counters/%x", keyBytes)
	dataBytes, err := oc.store.Get([]byte(storeKey))
	if err != nil {
		// Return zero data if key doesn't exist
		return &OperationCounterData{
			Count:     0,
			LastReset: time.Now().UTC(),
		}, nil
	}

	if dataBytes == nil {
		return &OperationCounterData{
			Count:     0,
			LastReset: time.Now().UTC(),
		}, nil
	}

	var data OperationCounterData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal counter data: %w", err)
	}

	return &data, nil
}

// setCounterData stores counter data in KV store
func (oc *OperationCounter) setCounterData(key OperationCounterKey, data *OperationCounterData) error {
	keyBytes, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("failed to marshal counter key: %w", err)
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal counter data: %w", err)
	}

	storeKey := fmt.Sprintf("/operation-counters/%x", keyBytes)
	return oc.store.Set([]byte(storeKey), dataBytes)
}

// CleanupExpiredCounters removes expired counter entries
func (oc *OperationCounter) CleanupExpiredCounters() error {
	oc.mu.Lock()
	defer oc.mu.Unlock()

	now := time.Now().UTC()
	cutoffDaily := now.AddDate(0, 0, -7).Format("2006-01-02")  // Keep 7 days
	cutoffHourly := now.AddDate(0, 0, -2).Format("2006-01-02") // Keep 2 days for hourly

	// Create an iterator for all operation counter keys
	iter, err := oc.store.Iterator([]byte("/operation-counters/"))
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var keysToDelete [][]byte
	for iter.Valid() {
		keyBytes := iter.Key()

		// Extract the JSON key from the storage key
		if len(keyBytes) < len("/operation-counters/") {
			iter.Next()
			continue
		}

		jsonKeyHex := string(keyBytes[len("/operation-counters/"):])
		jsonKeyBytes := make([]byte, len(jsonKeyHex)/2)
		if _, err := fmt.Sscanf(jsonKeyHex, "%x", &jsonKeyBytes); err != nil {
			iter.Next()
			continue
		}

		var key OperationCounterKey
		if err := json.Unmarshal(jsonKeyBytes, &key); err != nil {
			iter.Next()
			continue
		}

		// Check if this counter should be expired
		shouldDelete := false
		if key.Period == "daily" && key.Timestamp < cutoffDaily {
			shouldDelete = true
		} else if key.Period == "hourly" && key.Timestamp < cutoffHourly {
			shouldDelete = true
		}

		if shouldDelete {
			keysToDelete = append(keysToDelete, append([]byte(nil), keyBytes...))
		}

		iter.Next()
	}

	// Delete expired keys
	for _, key := range keysToDelete {
		if err := oc.store.Delete(key); err != nil {
			return fmt.Errorf("failed to delete expired counter key: %w", err)
		}
	}

	return nil
}

// GetStats returns statistics about operation counters
func (oc *OperationCounter) GetStats() (map[string]interface{}, error) {
	oc.mu.RLock()
	defer oc.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	// Count total entries
	iter, err := oc.store.Iterator([]byte("/operation-counters/"))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	totalCount := 0
	contractCounts := make(map[string]int)
	opTypeCounts := make(map[string]int)

	for iter.Valid() {
		totalCount++

		keyBytes := iter.Key()
		if len(keyBytes) >= len("/operation-counters/") {
			jsonKeyHex := string(keyBytes[len("/operation-counters/"):])
			jsonKeyBytes := make([]byte, len(jsonKeyHex)/2)
			if n, err := fmt.Sscanf(jsonKeyHex, "%x", &jsonKeyBytes); err == nil && n > 0 {
				var key OperationCounterKey
				if err := json.Unmarshal(jsonKeyBytes, &key); err == nil {
					contractCounts[key.ContractURL]++
					opTypeCounts[key.OpType]++
				}
			}
		}

		iter.Next()
	}

	stats["total_counters"] = totalCount
	stats["contracts"] = contractCounts
	stats["operation_types"] = opTypeCounts

	return stats, nil
}
