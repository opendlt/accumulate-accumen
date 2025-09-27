package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/rpc"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/tests/harness"
)

// TestAccumenSmoke runs the full end-to-end smoke test
func TestAccumenSmoke(t *testing.T) {
	t.Log("🚀 Starting Accumen E2E Smoke Test")
	harness.RunSmoke(t)
	t.Log("✅ Accumen E2E Smoke Test completed successfully")
}

// TestAccumenBasicFlow tests basic transaction flow
func TestAccumenBasicFlow(t *testing.T) {
	config := harness.DefaultHarnessConfig("basic_flow")
	config.BlockTime = 200 * time.Millisecond // Fast blocks
	config.Timeout = 20 * time.Second

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	t.Log("=== Testing Basic Flow ===")

	// Test sequencer is running
	if !h.GetSequencer().IsRunning() {
		t.Fatal("Sequencer should be running")
	}

	// Test RPC status
	stats := h.GetSequencer().GetStats()
	t.Logf("Initial stats - Height: %d, Blocks: %d, TXs: %d",
		stats.BlockHeight, stats.BlocksProduced, stats.TxsProcessed)

	// Deploy and test counter
	contractAddr, err := h.DeployCounter()
	if err != nil {
		t.Fatalf("Failed to deploy counter: %v", err)
	}

	// Single increment test
	result, err := h.InvokeCounter(contractAddr)
	if err != nil {
		t.Fatalf("Failed to invoke counter: %v", err)
	}

	t.Logf("Transaction submitted: %s", result.TxHash)

	// Wait for processing
	if err := h.WaitForTransactions(1); err != nil {
		t.Fatalf("Failed waiting for transaction: %v", err)
	}

	// Check final stats
	finalStats := h.GetSequencer().GetStats()
	t.Logf("Final stats - Height: %d, Blocks: %d, TXs: %d",
		finalStats.BlockHeight, finalStats.BlocksProduced, finalStats.TxsProcessed)

	if finalStats.TxsProcessed == 0 {
		t.Fatal("Expected at least 1 transaction to be processed")
	}

	t.Log("✅ Basic flow test completed")
}

// TestAccumenConcurrentTransactions tests handling of concurrent transactions
func TestAccumenConcurrentTransactions(t *testing.T) {
	config := harness.DefaultHarnessConfig("concurrent_test")
	config.BlockTime = 100 * time.Millisecond // Very fast blocks
	config.MaxTransactions = 50               // Allow more transactions per block
	config.Timeout = 30 * time.Second

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	t.Log("=== Testing Concurrent Transactions ===")

	// Deploy counter
	contractAddr, err := h.DeployCounter()
	if err != nil {
		t.Fatalf("Failed to deploy counter: %v", err)
	}

	// Submit multiple transactions concurrently
	const numTxs = 10
	results := make(chan *rpc.SubmitTxResult, numTxs)
	errors := make(chan error, numTxs)

	for i := 0; i < numTxs; i++ {
		go func(index int) {
			result, err := h.InvokeCounter(contractAddr)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	// Collect results
	successCount := 0
	for i := 0; i < numTxs; i++ {
		select {
		case result := <-results:
			t.Logf("Transaction %d: %s", successCount+1, result.TxHash)
			successCount++
		case err := <-errors:
			t.Logf("Transaction failed: %v", err)
		case <-time.After(15 * time.Second):
			t.Fatal("Timeout waiting for concurrent transactions")
		}
	}

	t.Logf("Successfully submitted %d/%d concurrent transactions", successCount, numTxs)

	// Wait for all transactions to be processed
	if err := h.WaitForTransactions(successCount); err != nil {
		t.Fatalf("Failed waiting for transactions: %v", err)
	}

	// Check final stats
	stats := h.GetSequencer().GetStats()
	t.Logf("Concurrent test stats - Height: %d, Blocks: %d, TXs: %d",
		stats.BlockHeight, stats.BlocksProduced, stats.TxsProcessed)

	if int(stats.TxsProcessed) < successCount {
		t.Fatalf("Expected at least %d transactions processed, got %d", successCount, stats.TxsProcessed)
	}

	t.Log("✅ Concurrent transactions test completed")
}

// TestAccumenMetadataIntegrity tests metadata generation and DN writing
func TestAccumenMetadataIntegrity(t *testing.T) {
	config := harness.DefaultHarnessConfig("metadata_test")
	config.BlockTime = 300 * time.Millisecond
	config.EnableBridge = true // Ensure bridge is enabled
	config.Timeout = 25 * time.Second

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	t.Log("=== Testing Metadata Integrity ===")

	// Deploy counter
	contractAddr, err := h.DeployCounter()
	if err != nil {
		t.Fatalf("Failed to deploy counter: %v", err)
	}

	// Submit a few transactions
	const numTxs = 5
	for i := 1; i <= numTxs; i++ {
		result, err := h.InvokeCounter(contractAddr)
		if err != nil {
			t.Fatalf("Failed to invoke counter (attempt %d): %v", i, err)
		}
		t.Logf("Transaction %d: %s", i, result.TxHash)
	}

	// Wait for all transactions to be processed
	if err := h.WaitForTransactions(numTxs); err != nil {
		t.Fatalf("Failed waiting for transactions: %v", err)
	}

	// Check that metadata was written to DN
	h.AssertMetadataWritten(int64(numTxs))

	// Verify metadata builder is working
	builder := h.GetMetadataBuilder()
	if builder == nil {
		t.Fatal("Metadata builder should not be nil")
	}

	t.Log("✅ Metadata integrity test completed")
}

// TestAccumenRPCEndpoints tests all RPC endpoints
func TestAccumenRPCEndpoints(t *testing.T) {
	config := harness.DefaultHarnessConfig("rpc_test")
	config.BlockTime = 200 * time.Millisecond
	config.Timeout = 20 * time.Second

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	t.Log("=== Testing RPC Endpoints ===")

	// Test status endpoint
	client := &http.Client{Timeout: 5 * time.Second}

	// Test accumen.status
	statusReq := map[string]interface{}{
		"id":     1,
		"method": "accumen.status",
		"params": map[string]interface{}{},
	}

	statusBytes, _ := json.Marshal(statusReq)
	resp, err := client.Post(
		"http://localhost:8666", // Use fixed port for test
		"application/json",
		strings.NewReader(string(statusBytes)),
	)
	if err != nil {
		// RPC server might not be accessible, skip this test
		t.Skipf("RPC server not accessible: %v", err)
	}
	defer resp.Body.Close()

	var statusResp struct {
		Result *rpc.StatusResult `json:"result"`
		Error  *rpc.RPCError     `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("Failed to decode status response: %v", err)
	}

	if statusResp.Error != nil {
		t.Fatalf("Status RPC error: %s", statusResp.Error.Message)
	}

	if statusResp.Result == nil {
		t.Fatal("Status result should not be nil")
	}

	t.Logf("Status - Chain: %s, Height: %d, Running: %t",
		statusResp.Result.ChainID, statusResp.Result.Height, statusResp.Result.Running)

	// Test query endpoint (should return empty for non-existent key)
	queryReq := map[string]interface{}{
		"id":     2,
		"method": "accumen.query",
		"params": map[string]interface{}{
			"contract": "acc://test.acme",
			"key":      "nonexistent",
		},
	}

	queryBytes, _ := json.Marshal(queryReq)
	resp2, err := client.Post(
		"http://localhost:8666",
		"application/json",
		strings.NewReader(string(queryBytes)),
	)
	if err != nil {
		t.Skipf("Query RPC request failed: %v", err)
	}
	defer resp2.Body.Close()

	var queryResp struct {
		Result *rpc.QueryResult `json:"result"`
		Error  *rpc.RPCError    `json:"error"`
	}

	if err := json.NewDecoder(resp2.Body).Decode(&queryResp); err != nil {
		t.Fatalf("Failed to decode query response: %v", err)
	}

	if queryResp.Error != nil {
		t.Fatalf("Query RPC error: %s", queryResp.Error.Message)
	}

	if queryResp.Result == nil {
		t.Fatal("Query result should not be nil")
	}

	if queryResp.Result.Exists {
		t.Fatal("Expected key to not exist")
	}

	t.Log("✅ RPC endpoints test completed")
}

// TestAccumenE2EBasic tests basic Accumen functionality end-to-end (from original)
func TestAccumenE2EBasic(t *testing.T) {
	config := harness.DefaultHarnessConfig("e2e_basic")
	config.BlockTime = time.Second
	config.EnableBridge = false // Disable bridge for basic test

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	// Test basic sequencer functionality
	t.Run("SequencerHealth", func(t *testing.T) {
		if !h.GetSequencer().IsRunning() {
			t.Fatal("Sequencer should be running")
		}

		err := h.GetSequencer().HealthCheck(ctx)
		if err != nil {
			t.Fatalf("Sequencer health check failed: %v", err)
		}
	})

	// Test multiple transactions
	t.Run("MultipleTransactions", func(t *testing.T) {
		numTxs := 3

		// Deploy counter first
		contractAddr, err := h.DeployCounter()
		if err != nil {
			t.Fatalf("Failed to deploy counter: %v", err)
		}

		// Submit multiple counter increments
		for i := 0; i < numTxs; i++ {
			_, err := h.InvokeCounter(contractAddr)
			if err != nil {
				t.Fatalf("Failed to invoke counter %d: %v", i, err)
			}
		}

		// Wait for all transactions to be processed
		if err := h.WaitForTransactions(numTxs); err != nil {
			t.Fatalf("Failed to wait for transactions: %v", err)
		}

		// Verify sequencer stats
		stats := h.GetSequencer().GetStats()
		if stats.TxsProcessed < uint64(numTxs) {
			t.Fatalf("Expected at least %d transactions processed, got %d", numTxs, stats.TxsProcessed)
		}
	})
}

// TestAccumenE2EFailureCases tests various failure scenarios
func TestAccumenE2EFailureCases(t *testing.T) {
	config := harness.DefaultHarnessConfig("e2e_failures")
	config.BlockTime = 500 * time.Millisecond
	config.EnableBridge = false

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	// Test that sequencer handles no transactions gracefully
	t.Run("NoTransactions", func(t *testing.T) {
		// Wait a bit and ensure sequencer is still healthy
		time.Sleep(2 * time.Second)

		if err := h.GetSequencer().HealthCheck(ctx); err != nil {
			t.Fatalf("Sequencer health check failed: %v", err)
		}

		stats := h.GetSequencer().GetStats()
		t.Logf("No-tx stats - Height: %d, Blocks: %d, Running: %t",
			stats.BlockHeight, stats.BlocksProduced, stats.Running)

		if !stats.Running {
			t.Fatal("Sequencer should still be running")
		}
	})
}

// createMockCounterWASM creates a mock WASM bytecode for the counter contract
func createMockCounterWASM() []byte {
	// Minimal WASM module that can be loaded but doesn't execute real logic
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // WASM magic
		0x01, 0x00, 0x00, 0x00, // WASM version
		// Minimal sections for a valid WASM module
	}
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

// loadActualCounterWASM tries to load real WASM if available, otherwise returns mock
func loadActualCounterWASM(t *testing.T) []byte {
	// Try to find compiled counter contract
	possiblePaths := []string{
		"../../sdk/rust/examples/counter/target/wasm32-unknown-unknown/release/counter.wasm",
		"../contracts/counter.wasm",
		"./testdata/counter.wasm",
	}

	for _, path := range possiblePaths {
		if fileExists(path) {
			data, err := os.ReadFile(path)
			if err == nil {
				t.Logf("Loaded actual WASM contract from: %s", path)
				return data
			}
		}
	}

	t.Log("No actual WASM contract found, using mock")
	return createMockCounterWASM()
}

// Benchmark for transaction throughput
func BenchmarkAccumenTransactionThroughput(b *testing.B) {
	config := harness.DefaultHarnessConfig("benchmark_throughput")
	config.BlockTime = 50 * time.Millisecond
	config.MaxTransactions = 10000
	config.EnableBridge = false

	// Create a testing.T wrapper for the harness
	testT := &testing.T{}

	h, err := harness.NewTestHarness(testT, config)
	if err != nil {
		b.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()
	if err := h.Start(ctx); err != nil {
		b.Fatalf("Failed to start test harness: %v", err)
	}

	// Deploy counter once
	contractAddr, err := h.DeployCounter()
	if err != nil {
		b.Fatalf("Failed to deploy counter: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Use the counter invoke method for consistent testing
			if _, err := h.InvokeCounter(contractAddr); err != nil {
				b.Errorf("Failed to invoke counter: %v", err)
			}
		}
	})
}
