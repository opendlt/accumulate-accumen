package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/tests/harness"
	"github.com/opendlt/accumulate-accumen/types/json"
)

// TestAccumenE2EBasic tests basic Accumen functionality end-to-end
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

	// Test transaction submission
	t.Run("TransactionSubmission", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/sender",
			"acc://test.acme/receiver",
			[]byte(`{"type":"transfer","amount":100}`),
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Basic transaction should succeed")
		h.AssertGasUsed(result, 1000, 500) // Expect around 1000 gas ±500
	})

	// Test multiple transactions
	t.Run("MultipleTransactions", func(t *testing.T) {
		numTxs := 10
		results := make([]*sequencer.ExecResult, numTxs)

		for i := 0; i < numTxs; i++ {
			tx := h.CreateTestTransaction(
				fmt.Sprintf("acc://test.acme/sender%d", i),
				"acc://test.acme/receiver",
				[]byte(fmt.Sprintf(`{"type":"batch_transfer","index":%d,"amount":%d}`, i, i*10)),
			)

			result, err := h.ExecuteTransaction(tx)
			if err != nil {
				t.Fatalf("Failed to execute transaction %d: %v", i, err)
			}

			results[i] = result
			h.AssertTransactionSuccess(result, fmt.Sprintf("Transaction %d should succeed", i))
		}

		// Wait for all transactions to be processed
		if err := h.WaitForBlock(2); err != nil {
			t.Fatalf("Failed to wait for blocks: %v", err)
		}

		// Verify sequencer stats
		stats := h.GetSequencer().GetStats()
		if stats.TxsProcessed < uint64(numTxs) {
			t.Fatalf("Expected at least %d transactions processed, got %d", numTxs, stats.TxsProcessed)
		}
	})
}

// TestAccumenE2ECounter tests the counter contract end-to-end
func TestAccumenE2ECounter(t *testing.T) {
	config := harness.DefaultHarnessConfig("e2e_counter")
	config.BlockTime = 500 * time.Millisecond
	config.EnableBridge = false

	h, err := harness.NewTestHarness(t, config)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()

	// Create mock counter WASM contract
	counterWASM := createMockCounterWASM()
	contractPath, err := h.WriteTestContract("counter", counterWASM)
	if err != nil {
		t.Fatalf("Failed to write counter contract: %v", err)
	}

	// Load the contract
	if err := h.LoadContract(contractPath); err != nil {
		t.Fatalf("Failed to load counter contract: %v", err)
	}

	// Start the harness
	if err := h.Start(ctx); err != nil {
		t.Fatalf("Failed to start test harness: %v", err)
	}

	// Test counter increment
	t.Run("CounterIncrement", func(t *testing.T) {
		command := map[string]interface{}{
			"type":   "Increment",
			"params": map[string]interface{}{"amount": 5},
		}

		commandData, err := json.Marshal(command)
		if err != nil {
			t.Fatalf("Failed to marshal command: %v", err)
		}

		tx := h.CreateTestTransaction(
			"acc://test.acme/user1",
			"acc://test.acme/counter",
			commandData,
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute increment transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Increment transaction should succeed")

		// Verify counter state in KV store
		h.AssertKVValue([]byte("counter_state"), []byte(`{"value":5,"increments":1,"decrements":0}`))
	})

	// Test counter decrement
	t.Run("CounterDecrement", func(t *testing.T) {
		command := map[string]interface{}{
			"type":   "Decrement",
			"params": map[string]interface{}{"amount": 2},
		}

		commandData, err := json.Marshal(command)
		if err != nil {
			t.Fatalf("Failed to marshal command: %v", err)
		}

		tx := h.CreateTestTransaction(
			"acc://test.acme/user2",
			"acc://test.acme/counter",
			commandData,
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute decrement transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Decrement transaction should succeed")

		// Verify updated counter state
		h.AssertKVValue([]byte("counter_state"), []byte(`{"value":3,"increments":1,"decrements":1}`))
	})

	// Test counter query
	t.Run("CounterQuery", func(t *testing.T) {
		command := map[string]interface{}{
			"type": "Get",
		}

		commandData, err := json.Marshal(command)
		if err != nil {
			t.Fatalf("Failed to marshal command: %v", err)
		}

		tx := h.CreateTestTransaction(
			"acc://test.acme/user3",
			"acc://test.acme/counter",
			commandData,
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute query transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Query transaction should succeed")
		h.AssertGasUsed(result, 500, 200) // Query should use less gas
	})

	// Test counter reset
	t.Run("CounterReset", func(t *testing.T) {
		command := map[string]interface{}{
			"type": "Reset",
		}

		commandData, err := json.Marshal(command)
		if err != nil {
			t.Fatalf("Failed to marshal command: %v", err)
		}

		tx := h.CreateTestTransaction(
			"acc://test.acme/admin",
			"acc://test.acme/counter",
			commandData,
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute reset transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Reset transaction should succeed")

		// Verify counter was reset
		h.AssertKVValue([]byte("counter_state"), []byte(`{"value":0,"increments":0,"decrements":0}`))
	})
}

// TestAccumenE2EWithBridge tests Accumen with L0 bridge enabled
func TestAccumenE2EWithBridge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping bridge test in short mode")
	}

	config := harness.DefaultHarnessConfig("e2e_bridge")
	config.BlockTime = time.Second
	config.EnableBridge = true
	config.L0MockMode = true // Use mock L0 for testing

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

	// Test transaction with bridge
	t.Run("TransactionWithBridge", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/sender",
			"acc://bridge.acme/receiver",
			[]byte(`{"type":"bridge_transfer","amount":1000,"destination":"acc://accumulate.acme/receiver"}`),
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute bridge transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Bridge transaction should succeed")

		// Wait for bridge processing
		time.Sleep(2 * time.Second)

		// Verify metadata was generated
		metadata, err := h.GetMetadataBuilder().BuildFromReceipt(result.Receipt)
		if err != nil {
			t.Fatalf("Failed to build metadata: %v", err)
		}

		if metadata.BridgeInfo == nil {
			t.Fatal("Expected bridge info in metadata")
		}
	})

	// Test metadata generation
	t.Run("MetadataGeneration", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/metadata_test",
			"acc://test.acme/target",
			[]byte(`{"type":"metadata_test","data":"test_data"}`),
		)

		result, err := h.ExecuteTransaction(tx)
		if err != nil {
			t.Fatalf("Failed to execute metadata test transaction: %v", err)
		}

		h.AssertTransactionSuccess(result, "Metadata test transaction should succeed")

		// Build metadata
		metadata, err := h.GetMetadataBuilder().BuildFromReceipt(result.Receipt)
		if err != nil {
			t.Fatalf("Failed to build metadata: %v", err)
		}

		// Validate metadata structure
		if metadata.TxID == "" {
			t.Fatal("Metadata should have transaction ID")
		}

		if metadata.BlockHeight == 0 {
			t.Fatal("Metadata should have block height")
		}

		if metadata.ExecutionContext == nil {
			t.Fatal("Metadata should have execution context")
		}

		if metadata.ExecutionContext.GasUsed == 0 {
			t.Fatal("Metadata should show gas used")
		}

		// Convert to JSON and validate
		metadataJSON, err := h.GetMetadataBuilder().ToJSON(metadata)
		if err != nil {
			t.Fatalf("Failed to convert metadata to JSON: %v", err)
		}

		t.Logf("Generated metadata: %s", string(metadataJSON))
	})
}

// TestAccumenE2EStress tests Accumen under stress conditions
func TestAccumenE2EStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	config := harness.DefaultHarnessConfig("e2e_stress")
	config.BlockTime = 100 * time.Millisecond // Fast blocks
	config.MaxTransactions = 1000
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

	// Test high transaction volume
	t.Run("HighVolumeTransactions", func(t *testing.T) {
		numTxs := 1000
		successCount := 0
		startTime := time.Now()

		for i := 0; i < numTxs; i++ {
			tx := h.CreateTestTransaction(
				fmt.Sprintf("acc://stress.acme/user%d", i%100),
				"acc://stress.acme/target",
				[]byte(fmt.Sprintf(`{"type":"stress_test","index":%d,"timestamp":%d}`, i, time.Now().UnixNano())),
			)

			// Submit without waiting for individual results
			if err := h.GetSequencer().SubmitTransaction(tx); err != nil {
				t.Logf("Failed to submit transaction %d: %v", i, err)
				continue
			}

			successCount++

			// Brief pause to avoid overwhelming the system
			if i%100 == 0 {
				time.Sleep(10 * time.Millisecond)
			}
		}

		// Wait for processing to complete
		if err := h.WaitForBlock(10); err != nil {
			t.Fatalf("Failed to wait for blocks: %v", err)
		}

		duration := time.Since(startTime)
		tps := float64(successCount) / duration.Seconds()

		t.Logf("Stress test completed: %d/%d transactions in %v (%.2f TPS)", successCount, numTxs, duration, tps)

		// Verify sequencer is still healthy
		stats := h.GetSequencer().GetStats()
		if stats.TxsProcessed < uint64(successCount/2) {
			t.Fatalf("Expected at least %d transactions processed, got %d", successCount/2, stats.TxsProcessed)
		}

		// Check that sequencer is still responsive
		if err := h.GetSequencer().HealthCheck(ctx); err != nil {
			t.Fatalf("Sequencer health check failed after stress test: %v", err)
		}
	})
}

// TestAccumenE2EFailureCases tests various failure scenarios
func TestAccumenE2EFailureCases(t *testing.T) {
	config := harness.DefaultHarnessConfig("e2e_failures")
	config.BlockTime = time.Second
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

	// Test invalid transaction data
	t.Run("InvalidTransactionData", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/sender",
			"acc://test.acme/receiver",
			[]byte("invalid json data {{{"),
		)

		result, err := h.ExecuteTransaction(tx)
		if err == nil {
			h.AssertTransactionFailure(result, "Transaction with invalid data should fail")
		}
	})

	// Test gas limit exceeded
	t.Run("GasLimitExceeded", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/sender",
			"acc://test.acme/receiver",
			[]byte(`{"type":"gas_intensive","loops":1000000}`),
		)
		tx.GasLimit = 100 // Very low gas limit

		result, err := h.ExecuteTransaction(tx)
		if err == nil {
			h.AssertTransactionFailure(result, "Transaction with insufficient gas should fail")
		}
	})

	// Test empty transaction
	t.Run("EmptyTransaction", func(t *testing.T) {
		tx := h.CreateTestTransaction(
			"acc://test.acme/sender",
			"acc://test.acme/receiver",
			[]byte{},
		)

		result, err := h.ExecuteTransaction(tx)
		if err == nil {
			h.AssertTransactionFailure(result, "Empty transaction should fail")
		}
	})
}

// createMockCounterWASM creates a mock WASM bytecode for the counter contract
// In a real test, this would be actual compiled WASM
func createMockCounterWASM() []byte {
	// This is a placeholder - in practice, you would either:
	// 1. Load actual compiled WASM from the Rust counter example
	// 2. Use a WASM compiler to generate bytecode
	// 3. Have pre-compiled test contracts
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // WASM magic number
		0x01, 0x00, 0x00, 0x00, // WASM version
		// Minimal WASM module structure would follow
		// For testing purposes, this is just a placeholder
	}
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

// Helper function to load actual WASM contract if available
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

	h, err := harness.NewTestHarness(&testing.T{}, config)
	if err != nil {
		b.Fatalf("Failed to create test harness: %v", err)
	}
	defer h.Cleanup()

	ctx := context.Background()
	if err := h.Start(ctx); err != nil {
		b.Fatalf("Failed to start test harness: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tx := h.CreateTestTransaction(
				"acc://bench.acme/sender",
				"acc://bench.acme/receiver",
				[]byte(`{"type":"benchmark","data":"test"}`),
			)

			if err := h.GetSequencer().SubmitTransaction(tx); err != nil {
				b.Errorf("Failed to submit transaction: %v", err)
			}
		}
	})
}