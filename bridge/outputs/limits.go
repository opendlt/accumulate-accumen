package outputs

import (
	"fmt"
	"time"
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
		MaxGasPerOutput:     1000000,            // 1M gas
		MaxTotalGas:         10000000,           // 10M gas
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
		MaxGasPerOutput:     500000,            // 500K gas
		MaxTotalGas:         5000000,           // 5M gas
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