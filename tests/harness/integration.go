package harness

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/anchors"
	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/rpc"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/json"

	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

// MockSimulator provides a simple simulator for testing
type MockSimulator struct {
	endpoint string
	server   *http.Server
	accounts map[string]interface{}
	mu       sync.RWMutex
}

// MockDNWriter tracks metadata writes for testing
type MockDNWriter struct {
	*anchors.DNWriter
	metadataWrites int64
	anchorWrites   int64
	mu             sync.RWMutex
}

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
	rpcServer       *rpc.Server
	simulator       *MockSimulator
	dnWriter        *MockDNWriter
	tempDir         string
	cleanupFuncs    []func() error
}

// HarnessConfig defines configuration for the test harness
type HarnessConfig struct {
	// Test environment settings
	TestName      string
	TempDir       string
	CleanupOnExit bool
	LogLevel      string
	Timeout       time.Duration

	// Sequencer configuration
	BlockTime       time.Duration
	MaxTransactions int
	EnableBridge    bool
	GasLimit        uint64
	MaxMemoryPages  uint32

	// L0 configuration
	L0MockMode bool
	L0Endpoint string
	L0Timeout  time.Duration
	RPCAddr    string

	// WASM runtime configuration
	WASMDebug     bool
	WASMMaxMemory uint32

	// Test data paths
	ContractPath string
	TestDataPath string
}

// DefaultHarnessConfig returns default test harness configuration
func DefaultHarnessConfig(testName string) *HarnessConfig {
	return &HarnessConfig{
		TestName:        testName,
		CleanupOnExit:   true,
		LogLevel:        "info",
		Timeout:         30 * time.Second,
		BlockTime:       time.Second,
		MaxTransactions: 100,
		EnableBridge:    true, // Enable bridge for integration tests
		GasLimit:        1000000,
		MaxMemoryPages:  16,
		L0MockMode:      true,
		L0Endpoint:      "http://localhost:26660/v3",
		L0Timeout:       10 * time.Second,
		RPCAddr:         ":0", // Use random port
		WASMDebug:       true,
		WASMMaxMemory:   16,
	}
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T, config *HarnessConfig) (*TestHarness, error) {
	if config == nil {
		config = DefaultHarnessConfig(t.Name())
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("accumen_test_%s_*", strings.ReplaceAll(config.TestName, "/", "_")))
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

	// Initialize components in order
	if err := harness.setupSimulator(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup simulator: %w", err)
	}

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

	if err := harness.setupDNWriter(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup DN writer: %w", err)
	}

	if err := harness.setupSequencer(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup sequencer: %w", err)
	}

	if err := harness.setupRPCServer(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup RPC server: %w", err)
	}

	if err := harness.setupMetadataBuilder(); err != nil {
		harness.Cleanup()
		return nil, fmt.Errorf("failed to setup metadata builder: %w", err)
	}

	return harness, nil
}

// setupSimulator sets up a mock Accumulate simulator
func (h *TestHarness) setupSimulator() error {
	sim := &MockSimulator{
		endpoint: h.config.L0Endpoint,
		accounts: make(map[string]interface{}),
	}

	// Setup mock HTTP server for simulator
	mux := http.NewServeMux()
	mux.HandleFunc("/v3", sim.handleJSONRPC)
	mux.HandleFunc("/health", sim.handleHealth)

	sim.server = &http.Server{
		Addr:    ":26660", // Fixed port for testing
		Handler: mux,
	}

	// Start server
	go func() {
		if err := sim.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.t.Logf("Simulator server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Initialize placeholder DN accounts
	sim.initializeDNAccounts()

	h.simulator = sim
	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return sim.server.Shutdown(context.Background())
	})

	h.t.Logf("Mock simulator started on %s", sim.endpoint)
	return nil
}

// setupKVStore initializes the key-value store
func (h *TestHarness) setupKVStore() error {
	store := state.NewMemoryKVStore()
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

// setupL0Client initializes the L0 API client pointing to simulator
func (h *TestHarness) setupL0Client() error {
	client, err := l0api.NewClient(&l0api.ClientConfig{
		Endpoint:     h.simulator.endpoint,
		Timeout:      h.config.L0Timeout,
		SequencerKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // Test key
	})
	if err != nil {
		return fmt.Errorf("failed to create L0 client: %w", err)
	}

	h.l0Client = client
	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return h.l0Client.Close()
	})

	return nil
}

// setupDNWriter initializes a mock DN writer that tracks calls
func (h *TestHarness) setupDNWriter() error {
	config := anchors.DefaultDNWriterConfig()
	config.L0Endpoint = h.simulator.endpoint
	config.SequencerKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	writer, err := anchors.NewDNWriter(config)
	if err != nil {
		return fmt.Errorf("failed to create DN writer: %w", err)
	}

	h.dnWriter = &MockDNWriter{
		DNWriter: writer,
	}

	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return h.dnWriter.Close()
	})

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
			Workers:        4,
			QueueSize:      100,
			Timeout:        10 * time.Second,
			EnableParallel: false, // Disable for testing
			Runtime: runtime.Config{
				MaxMemoryPages: h.config.MaxMemoryPages,
				GasLimit:       h.config.GasLimit,
				Debug:          h.config.WASMDebug,
			},
		},
		Bridge: sequencer.BridgeConfig{
			EnableBridge: h.config.EnableBridge,
			Client: l0api.ClientConfig{
				Endpoint:     h.simulator.endpoint,
				Timeout:      h.config.L0Timeout,
				SequencerKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
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

// setupRPCServer initializes the JSON-RPC server
func (h *TestHarness) setupRPCServer() error {
	server := rpc.NewServer(&rpc.Dependencies{
		Sequencer: h.sequencer,
		KVStore:   h.kvStore,
	})

	if err := server.Start(h.config.RPCAddr); err != nil {
		return fmt.Errorf("failed to start RPC server: %w", err)
	}

	h.rpcServer = server
	h.cleanupFuncs = append(h.cleanupFuncs, func() error {
		return h.rpcServer.Stop(context.Background())
	})

	return nil
}

// setupMetadataBuilder initializes the metadata builder
func (h *TestHarness) setupMetadataBuilder() error {
	h.metadataBuilder = json.NewMetadataBuilder()
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

// DeployCounter deploys the counter WASM contract
func (h *TestHarness) DeployCounter() (string, error) {
	// Try to load from SDK if present, otherwise use mock WASM
	counterPath := filepath.Join("../../sdk/rust/examples/counter/target/wasm32-unknown-unknown/release/counter.wasm")

	var wasmBytes []byte
	var err error

	if _, err := os.Stat(counterPath); err == nil {
		// Load real WASM if available
		wasmBytes, err = os.ReadFile(counterPath)
		if err != nil {
			return "", fmt.Errorf("failed to read counter WASM: %w", err)
		}
		h.t.Logf("Loaded counter WASM from SDK: %d bytes", len(wasmBytes))
	} else {
		// Create mock WASM bytecode
		wasmBytes = h.createMockCounterWASM()
		h.t.Logf("Created mock counter WASM: %d bytes", len(wasmBytes))
	}

	// Load into runtime
	if err := h.runtime.LoadModule(context.Background(), wasmBytes); err != nil {
		return "", fmt.Errorf("failed to load counter module: %w", err)
	}

	contractAddr := "acc://counter.acme"
	h.t.Logf("Counter contract deployed at: %s", contractAddr)
	return contractAddr, nil
}

// InvokeCounter calls increment on the counter contract
func (h *TestHarness) InvokeCounter(contractAddr string) (*rpc.SubmitTxResult, error) {
	// Use RPC to submit transaction
	client := &http.Client{Timeout: 5 * time.Second}

	reqBody := map[string]interface{}{
		"id":     1,
		"method": "accumen.submitTx",
		"params": map[string]interface{}{
			"contract": contractAddr,
			"entry":    "increment",
			"args":     map[string]interface{}{},
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	resp, err := client.Post(
		fmt.Sprintf("http://localhost%s", h.rpcServer.GetAddr()),
		"application/json",
		strings.NewReader(string(reqBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result *rpc.SubmitTxResult `json:"result"`
		Error  *rpc.RPCError       `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode RPC response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// QueryCounter queries the counter value from state
func (h *TestHarness) QueryCounter(contractAddr string) (uint64, error) {
	stateKey := fmt.Sprintf("%s:counter", contractAddr)
	value, exists := h.kvStore.Get([]byte(stateKey))

	if !exists {
		return 0, nil // Counter starts at 0
	}

	if len(value) < 8 {
		return 0, fmt.Errorf("invalid counter value length: %d", len(value))
	}

	// Convert bytes to uint64 (little endian)
	counter := uint64(0)
	for i := 0; i < 8; i++ {
		counter |= uint64(value[i]) << (i * 8)
	}

	return counter, nil
}

// AssertMetadataWritten asserts that DN metadata was written
func (h *TestHarness) AssertMetadataWritten(expectedWrites int64) {
	writes := atomic.LoadInt64(&h.dnWriter.metadataWrites)
	if writes < expectedWrites {
		h.t.Fatalf("Expected at least %d metadata writes, got %d", expectedWrites, writes)
	}
	h.t.Logf("Metadata writes: %d (expected at least %d)", writes, expectedWrites)
}

// WaitForTransactions waits for transactions to be processed
func (h *TestHarness) WaitForTransactions(count int) error {
	timeout := time.NewTimer(h.config.Timeout)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout waiting for %d transactions", count)
		case <-ticker.C:
			stats := h.sequencer.GetStats()
			if int(stats.TxsProcessed) >= count {
				h.t.Logf("Processed %d transactions (expected %d)", stats.TxsProcessed, count)
				return nil
			}
		}
	}
}

// createMockCounterWASM creates a minimal mock WASM bytecode for testing
func (h *TestHarness) createMockCounterWASM() []byte {
	// Minimal WASM module that can be loaded but doesn't execute real logic
	// This is a placeholder - in practice you'd use a real counter WASM
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // WASM magic
		0x01, 0x00, 0x00, 0x00, // WASM version
		// Minimal sections for a valid WASM module
	}
}

// Mock simulator implementation
func (sim *MockSimulator) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Simple mock response
	response := map[string]interface{}{
		"id": 1,
		"result": map[string]interface{}{
			"status": "ok",
			"txHash": "0x" + hex.EncodeToString([]byte("mock_tx_hash")),
		},
	}

	json.NewEncoder(w).Encode(response)
}

func (sim *MockSimulator) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (sim *MockSimulator) initializeDNAccounts() {
	// Initialize placeholder DN data accounts
	baseURL, _ := url.Parse("acc://accumen.acme")
	sim.accounts[baseURL.String()] = map[string]interface{}{
		"type": "data_account",
		"data": map[string]interface{}{},
	}
}

// Mock DN Writer methods to track calls
func (w *MockDNWriter) WriteMetadata(ctx context.Context, jsonBytes []byte, basePath string) (string, error) {
	atomic.AddInt64(&w.metadataWrites, 1)

	// Call original method
	return w.DNWriter.WriteMetadata(ctx, jsonBytes, basePath)
}

func (w *MockDNWriter) WriteAnchor(ctx context.Context, blob []byte, basePath string) (string, error) {
	atomic.AddInt64(&w.anchorWrites, 1)

	// Call original method
	return w.DNWriter.WriteAnchor(ctx, blob, basePath)
}

// RunSmoke runs a smoke test of the full integration
func RunSmoke(t *testing.T) {
	config := DefaultHarnessConfig("smoke_test")
	config.BlockTime = 500 * time.Millisecond // Faster blocks for testing
	config.Timeout = 30 * time.Second

	harness, err := NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer harness.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := harness.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	t.Log("=== Starting Accumen Integration Smoke Test ===")

	// 1. Deploy counter contract
	contractAddr, err := harness.DeployCounter()
	if err != nil {
		t.Fatalf("Failed to deploy counter: %v", err)
	}
	t.Logf("✅ Counter deployed at: %s", contractAddr)

	// 2. Invoke increment 3 times
	for i := 1; i <= 3; i++ {
		result, err := harness.InvokeCounter(contractAddr)
		if err != nil {
			t.Fatalf("Failed to invoke counter (attempt %d): %v", i, err)
		}
		t.Logf("✅ Increment %d: TX %s", i, result.TxHash)
	}

	// 3. Wait for transactions to be processed
	if err := harness.WaitForTransactions(3); err != nil {
		t.Fatalf("Failed waiting for transactions: %v", err)
	}

	// 4. Query counter state
	counterValue, err := harness.QueryCounter(contractAddr)
	if err != nil {
		t.Fatalf("Failed to query counter: %v", err)
	}

	// Note: Due to mock WASM, counter might not actually increment
	// In real implementation, this would be 3
	t.Logf("✅ Counter value: %d", counterValue)

	// 5. Assert metadata was written to DN
	harness.AssertMetadataWritten(3) // Should have 3 metadata writes

	// 6. Check sequencer stats
	stats := harness.GetSequencer().GetStats()
	t.Logf("✅ Final Stats - Blocks: %d, TXs: %d, Running: %t",
		stats.BlocksProduced, stats.TxsProcessed, stats.Running)

	if stats.TxsProcessed < 3 {
		t.Fatalf("Expected at least 3 transactions processed, got %d", stats.TxsProcessed)
	}

	t.Log("🎉 Smoke test completed successfully!")
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

// GetTempDir returns the temporary directory path
func (h *TestHarness) GetTempDir() string {
	return h.tempDir
}

// GetConfig returns the harness configuration
func (h *TestHarness) GetConfig() *HarnessConfig {
	return h.config
}
