package gas

import (
	"fmt"
)

// GasLimit represents the maximum gas that can be consumed
type GasLimit uint64

// GasUsed represents the amount of gas consumed
type GasUsed uint64

// Meter tracks gas consumption during WASM execution
type Meter struct {
	limit     GasLimit
	consumed  GasUsed
	gasConfig *Config
}

// Config defines gas costs for different operations
type Config struct {
	// Basic operation costs
	BaseGas    uint64
	MemoryGas  uint64
	ComputeGas uint64
	StorageGas uint64

	// Host function costs
	GetGas    uint64
	SetGas    uint64
	DeleteGas uint64
	LogGas    uint64
}

// DefaultGasConfig returns a default gas configuration
func DefaultGasConfig() *Config {
	return &Config{
		BaseGas:    1,
		MemoryGas:  1,
		ComputeGas: 1,
		StorageGas: 100,
		GetGas:     100,
		SetGas:     200,
		DeleteGas:  150,
		LogGas:     50,
	}
}

// NewMeter creates a new gas meter with the specified limit
func NewMeter(limit GasLimit) *Meter {
	return &Meter{
		limit:     limit,
		consumed:  0,
		gasConfig: DefaultGasConfig(),
	}
}

// NewMeterWithConfig creates a new gas meter with custom configuration
func NewMeterWithConfig(limit GasLimit, config *Config) *Meter {
	return &Meter{
		limit:     limit,
		consumed:  0,
		gasConfig: config,
	}
}

// ConsumeGas attempts to consume the specified amount of gas
func (m *Meter) ConsumeGas(amount uint64) error {
	if m.consumed+GasUsed(amount) > GasUsed(m.limit) {
		return fmt.Errorf("out of gas: trying to consume %d, but only %d remaining",
			amount, m.GasRemaining())
	}

	m.consumed += GasUsed(amount)
	return nil
}

// GasRemaining returns the amount of gas remaining
func (m *Meter) GasRemaining() uint64 {
	return uint64(m.limit - GasLimit(m.consumed))
}

// GasConsumed returns the total amount of gas consumed
func (m *Meter) GasConsumed() uint64 {
	return uint64(m.consumed)
}

// GasLimit returns the total gas limit
func (m *Meter) GasLimit() uint64 {
	return uint64(m.limit)
}

// Reset resets the meter to zero consumption
func (m *Meter) Reset() {
	m.consumed = 0
}

// SetLimit updates the gas limit (useful for testing)
func (m *Meter) SetLimit(limit GasLimit) {
	m.limit = limit
}

// IsExhausted returns true if all gas has been consumed
func (m *Meter) IsExhausted() bool {
	return m.consumed >= GasUsed(m.limit)
}

// ConsumeMemoryGas consumes gas for memory operations
func (m *Meter) ConsumeMemoryGas(bytes uint64) error {
	return m.ConsumeGas(bytes * m.gasConfig.MemoryGas)
}

// ConsumeComputeGas consumes gas for computation
func (m *Meter) ConsumeComputeGas(operations uint64) error {
	return m.ConsumeGas(operations * m.gasConfig.ComputeGas)
}

// ConsumeStorageGas consumes gas for storage operations
func (m *Meter) ConsumeStorageGas(bytes uint64) error {
	return m.ConsumeGas(bytes * m.gasConfig.StorageGas)
}
