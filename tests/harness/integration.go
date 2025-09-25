package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/json"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
)

// TestHarness provides a complete test environment for Accumen integration tests
type TestHarness struct {
	mu              sync.RWMutex
	t               *testing.T
	config          *HarnessConfig
	sequencer       *sequencer.Sequencer
	runtime         *runtime.Runtime
	kvStore         state.KVStore
	l0Client        *l0api.Client
	metadataBuilder *json.MetadataBuilder
	tempDir         string
	cleanupFuncs    []func() error
}

// HarnessConfig defines configuration for the test harness
type HarnessConfig struct {
	// Test environment settings
	TestName       string
	TempDir        string
	CleanupOnExit  bool
	LogLevel       string
	Timeout        time.Duration

	// Sequencer configuration
	BlockTime        time.Duration
	MaxTransactions  int
	EnableBridge     bool
	GasLimit         uint64
	MaxMemoryPages   uint32

	// L0 configuration
	L0MockMode       bool
	L0Endpoint       string
	L0Timeout        time.Duration

	// WASM runtime configuration
	WASMDebug        bool
	WASMMaxMemory    uint32

	// Test data paths
	ContractPath     string
	TestDataPath     string
}

// DefaultHarnessConfig returns default test harness configuration
func DefaultHarnessConfig(testName string) *HarnessConfig {
	return &HarnessConfig{
		TestName:       testName,
		CleanupOnExit:  true,
		LogLevel:       "info",
		Timeout:        30 * time.Second,
		BlockTime:      time.Second,
		MaxTransactions: 100,
		EnableBridge:   false, // Disabled by default for unit tests
		GasLimit:       1000000,
		MaxMemoryPages: 16,
		L0MockMode:     true,
		L0Endpoint:     "http://localhost:8080",
		L0Timeout:      10 * time.Second,
		WASMDebug:      true,
		WASMMaxMemory:  16,
	}
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T, config *HarnessConfig) (*TestHarness, error) {
	if config == nil {
		config = DefaultHarnessConfig(t.Name())
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("accumen_test_%s_*", config.TestName))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	config.TempDir = tempDir

	harness := &TestHarness{
		t:            t,
		config:       config,
		tempDir:      tempDir,
		cleanupFuncs: make([]func() error, 0),
	}

	// Initialize components
	if err := harness.setupKVStore(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup KV store: %w", err)
	}

	if err := harness.setupRuntime(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup runtime: %w", err)
	}

	if err := harness.setupL0Client(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup L0 client: %w", err)
	}

	if err := harness.setupSequencer(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup sequencer: %w", err)
	}

	if err := harness.setupMetadataBuilder(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup metadata builder: %w", err)
	}

	return harness, nil
}

// setupKVStore initializes the key-value store
func (h *TestHarness) setupKVStore() error {
	store := state.NewInMemoryKVStore()
	h.kvStore = store
	return nil
}

// setupRuntime initializes the WASM runtime
func (h *TestHarness) setupRuntime() error {
	runtimeConfig := &runtime.Config{
		MaxMemoryPages: h.config.WASMMaxMemory,
		GasLimit:       h.config.GasLimit,
		Debug:          h.config.WASMDebug,
	}

	rt, err := runtime.NewRuntime(runtimeConfig)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	h.runtime = rt
	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return h.runtime.Close(context.Background())
	})

	return nil
}

// setupL0Client initializes the L0 API client
func (h *TestHarness) setupL0Client() error {
	if h.config.L0MockMode {
		// Use mock client for testing
		h.l0Client = &l0api.Client{} // Mock implementation
		return nil
	}

	client, err := l0api.NewClient(&l0api.ClientConfig{
		Endpoint: h.config.L0Endpoint,
		Timeout:  h.config.L0Timeout,
	})
	if err != nil {
		return fmt.Errorf("failed to create L0 client: %w", err)
	}

	h.l0Client = client
	return nil
}

// setupSequencer initializes the sequencer
func (h *TestHarness) setupSequencer() error {
	seqConfig := &sequencer.Config{
		ListenAddr:      "127.0.0.1:0", // Use random port
		BlockTime:       h.config.BlockTime,
		MaxTransactions: h.config.MaxTransactions,
		Mempool: sequencer.MempoolConfig{
			MaxSize:         1000,
			MaxAge:          time.Hour,
			PriorityEnabled: true,
			AccountLimit:    100,
		},
		Execution: sequencer.ExecutionConfig{
			Workers:      4,
			QueueSize:    100,
			Timeout:      10 * time.Second,
			GasLimit:     h.config.GasLimit,
			MemoryLimit:  h.config.MaxMemoryPages,
		},
		Bridge: sequencer.BridgeConfig{
			EnableBridge: h.config.EnableBridge,
			Client: l0api.ClientConfig{
				Endpoint: h.config.L0Endpoint,
				Timeout:  h.config.L0Timeout,
			},
		},
	}

	seq, err := sequencer.NewSequencer(seqConfig)
	if err != nil {
		return fmt.Errorf("failed to create sequencer: %w", err)
	}

	h.sequencer = seq
	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return h.sequencer.Stop(context.Background())
	})

	return nil
}

// setupMetadataBuilder initializes the metadata builder
func (h *TestHarness) setupMetadataBuilder() error {
	builderConfig := json.DefaultBuilderConfig()
	builderConfig.CompactMode = true // Use compact mode for tests

	h.metadataBuilder = json.NewMetadataBuilder(builderConfig)
	return nil
}

// Start starts all harness components
func (h *TestHarness) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Start sequencer
	if err := h.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %w", err)
	}

	h.t.Logf("Test harness started for test: %s", h.config.TestName)
	return nil
}

// Stop stops all harness components
func (h *TestHarness) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if h.sequencer != nil {
		if err := h.sequencer.Stop(ctx); err != nil {
			h.t.Errorf("Failed to stop sequencer: %v", err)
		}
	}

	h.t.Logf("Test harness stopped for test: %s", h.config.TestName)
	return nil
}

// Cleanup releases all resources and performs cleanup
func (h *TestHarness) Cleanup() {
	h.Stop()

	// Run cleanup functions in reverse order
	for i := len(h.cleanupFuncs) - 1; i >= 0; i-- {
		if err := h.cleanupFuncs[i](); err != nil {
			h.t.Errorf("Cleanup function failed: %v", err)
		}
	}

	// Remove temporary directory
	if h.config.CleanupOnExit && h.tempDir != "" {
		if err := os.RemoveAll(h.tempDir); err != nil {
			h.t.Errorf("Failed to remove temp directory: %v", err)
		}
	}
}

// LoadContract loads a WASM contract from file
func (h *TestHarness) LoadContract(contractPath string) error {
	wasmBytes, err := os.ReadFile(contractPath)
	if err != nil {
		return fmt.Errorf("failed to read contract file: %w", err)
	}

	if err := h.runtime.LoadModule(context.Background(), wasmBytes); err != nil {
		return fmt.Errorf("failed to load contract module: %w", err)
	}

	h.t.Logf("Loaded contract from: %s (%d bytes)", contractPath, len(wasmBytes))
	return nil
}

// ExecuteTransaction executes a transaction through the sequencer
func (h *TestHarness) ExecuteTransaction(tx *sequencer.Transaction) (*sequencer.ExecResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.config.Timeout)
	defer cancel()

	// Submit transaction
	if err := h.sequencer.SubmitTransaction(tx); err != nil {
		return nil, fmt.Errorf("failed to submit transaction: %w", err)
	}

	// Wait for transaction to be processed
	// In a real implementation, this would poll for the result
	time.Sleep(h.config.BlockTime + 100*time.Millisecond)

	// Simulate transaction result (in practice, this would come from the sequencer)
	result, err := h.sequencer.SimulateTransaction(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to simulate transaction: %w", err)
	}

	h.t.Logf("Executed transaction %s: success=%t, gas=%d", tx.ID, result.Success, result.GasUsed)
	return result, nil
}

// ExecuteWASMFunction executes a WASM function directly
func (h *TestHarness) ExecuteWASMFunction(functionName string, params []uint64) (*runtime.ExecutionResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.config.Timeout)
	defer cancel()

	result, err := h.runtime.Execute(ctx, functionName, params, h.kvStore)
	if err != nil {
		return nil, fmt.Errorf("failed to execute WASM function: %w", err)
	}

	h.t.Logf("Executed WASM function %s: success=%t, gas=%d", functionName, result.Success, result.GasUsed)
	return result, nil
}

// CreateTestTransaction creates a test transaction
func (h *TestHarness) CreateTestTransaction(from, to string, data []byte) *sequencer.Transaction {
	return &sequencer.Transaction{
		ID:       fmt.Sprintf("test_tx_%d", time.Now().UnixNano()),
		From:     from,
		To:       to,
		Data:     data,
		GasLimit: h.config.GasLimit,
		GasPrice: 1,
		Nonce:    uint64(time.Now().UnixNano()),
		Priority: 100,
	}
}

// GetSequencer returns the sequencer instance
func (h *TestHarness) GetSequencer() *sequencer.Sequencer {
	return h.sequencer
}

// GetRuntime returns the runtime instance
func (h *TestHarness) GetRuntime() *runtime.Runtime {
	return h.runtime
}

// GetKVStore returns the KV store instance
func (h *TestHarness) GetKVStore() state.KVStore {
	return h.kvStore
}

// GetL0Client returns the L0 client instance
func (h *TestHarness) GetL0Client() *l0api.Client {
	return h.l0Client
}

// GetMetadataBuilder returns the metadata builder instance
func (h *TestHarness) GetMetadataBuilder() *json.MetadataBuilder {
	return h.metadataBuilder
}

// WaitForBlock waits for a specific number of blocks to be produced
func (h *TestHarness) WaitForBlock(targetHeight uint64) error {
	timeout := time.NewTimer(h.config.Timeout)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout waiting for block %d", targetHeight)
		case <-ticker.C:
			currentHeight := h.sequencer.GetBlockHeight()
			if currentHeight >= targetHeight {
				h.t.Logf("Reached target block height: %d", currentHeight)
				return nil
			}
		}
	}
}

// AssertTransactionSuccess asserts that a transaction executed successfully
func (h *TestHarness) AssertTransactionSuccess(result *sequencer.ExecResult, msgAndArgs ...interface{}) {
	if result == nil {
		h.t.Fatalf("Transaction result is nil: %v", msgAndArgs)
	}
	if !result.Success {
		h.t.Fatalf("Transaction failed: %s, %v", result.Error, msgAndArgs)
	}
}

// AssertTransactionFailure asserts that a transaction failed
func (h *TestHarness) AssertTransactionFailure(result *sequencer.ExecResult, msgAndArgs ...interface{}) {
	if result == nil {
		h.t.Fatalf("Transaction result is nil: %v", msgAndArgs)
	}
	if result.Success {
		h.t.Fatalf("Expected transaction to fail but it succeeded: %v", msgAndArgs)
	}
}

// AssertGasUsed asserts that gas usage is within expected range
func (h *TestHarness) AssertGasUsed(result *sequencer.ExecResult, expectedGas uint64, tolerance uint64) {
	if result == nil {
		h.t.Fatal("Transaction result is nil")
	}

	gasUsed := result.GasUsed
	if gasUsed < expectedGas-tolerance || gasUsed > expectedGas+tolerance {
		h.t.Fatalf("Gas usage %d not within expected range %d±%d", gasUsed, expectedGas, tolerance)
	}
	h.t.Logf("Gas usage %d within expected range %d±%d", gasUsed, expectedGas, tolerance)
}

// AssertKVValue asserts that a key-value pair exists in storage
func (h *TestHarness) AssertKVValue(key []byte, expectedValue []byte) {
	value, exists := h.kvStore.Get(key)
	if !exists {
		h.t.Fatalf("Key %s not found in storage", string(key))
	}

	if len(value) != len(expectedValue) {
		h.t.Fatalf("Value length mismatch for key %s: expected %d, got %d", string(key), len(expectedValue), len(value))
	}

	for i, b := range expectedValue {
		if value[i] != b {
			h.t.Fatalf("Value mismatch for key %s at index %d: expected %d, got %d", string(key), i, b, value[i])
		}
	}

	h.t.Logf("Key-value assertion passed for key: %s", string(key))
}

// CreateContractPath returns a path to test contract
func (h *TestHarness) CreateContractPath(contractName string) string {
	return filepath.Join(h.tempDir, contractName+".wasm")
}

// WriteTestContract writes test contract data to a file
func (h *TestHarness) WriteTestContract(contractName string, wasmData []byte) (string, error) {
	contractPath := h.CreateContractPath(contractName)

	if err := os.WriteFile(contractPath, wasmData, 0644); err != nil {
		return "", fmt.Errorf("failed to write test contract: %w", err)
	}

	h.t.Logf("Wrote test contract %s to %s (%d bytes)", contractName, contractPath, len(wasmData))
	return contractPath, nil
}

// MockEnvelope creates a mock Accumulate envelope for testing
func (h *TestHarness) MockEnvelope(txHash string) *messaging.Envelope {
	return &messaging.Envelope{
		// Mock envelope structure
		// In practice, this would be properly constructed
	}
}

// GetTempDir returns the temporary directory path
func (h *TestHarness) GetTempDir() string {
	return h.tempDir
}

// GetConfig returns the harness configuration
func (h *TestHarness) GetConfig() *HarnessConfig {
	return h.config
}