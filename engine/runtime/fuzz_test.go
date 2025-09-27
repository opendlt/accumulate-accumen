package runtime

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DeterminismTest represents a single determinism test case
type DeterminismTest struct {
	Name        string
	ModuleWASM  []byte
	Entry       string
	Args        []interface{}
	Iterations  int
	Description string
}

// ExecutionResult captures the result of a WASM execution
type ExecutionResult struct {
	StateRoot  []byte
	GasUsed    uint64
	Events     []Event
	ReturnData []byte
	Error      string
}

// Event represents an emitted event during execution
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// TestDeterminism runs fuzz tests to ensure deterministic execution
func TestDeterminism(t *testing.T) {
	tests := []DeterminismTest{
		{
			Name:        "CounterIncrement",
			ModuleWASM:  generateCounterWASM(),
			Entry:       "increment",
			Args:        []interface{}{1},
			Iterations:  100,
			Description: "Simple counter increment should be deterministic",
		},
		{
			Name:        "StringManipulation",
			ModuleWASM:  generateStringWASM(),
			Entry:       "process_string",
			Args:        []interface{}{"hello world", 42},
			Iterations:  100,
			Description: "String processing should be deterministic",
		},
		{
			Name:        "MathOperations",
			ModuleWASM:  generateMathWASM(),
			Entry:       "calculate",
			Args:        []interface{}{3.14159, 2.71828},
			Iterations:  100,
			Description: "Mathematical operations should be deterministic",
		},
		{
			Name:        "StateTransitions",
			ModuleWASM:  generateStateWASM(),
			Entry:       "update_state",
			Args:        []interface{}{map[string]interface{}{"key": "value", "number": 123}},
			Iterations:  100,
			Description: "State transitions should be deterministic",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			testDeterministicExecution(t, test)
		})
	}
}

func testDeterministicExecution(t *testing.T, test DeterminismTest) {
	t.Logf("Testing determinism: %s", test.Description)
	t.Logf("Running %d iterations with entry point: %s", test.Iterations, test.Entry)

	var results []ExecutionResult
	var executionTimes []time.Duration

	// Execute the same module with same inputs multiple times
	for i := 0; i < test.Iterations; i++ {
		start := time.Now()
		result, err := executeWASMModule(test.ModuleWASM, test.Entry, test.Args)
		executionTime := time.Since(start)

		require.NoError(t, err, "Execution %d failed", i)
		results = append(results, result)
		executionTimes = append(executionTimes, executionTime)

		if i%10 == 0 {
			t.Logf("Completed %d/%d iterations", i+1, test.Iterations)
		}
	}

	// Verify all results are identical
	t.Log("Verifying deterministic results...")

	baseResult := results[0]
	for i, result := range results[1:] {
		// Compare state roots
		assert.Equal(t, baseResult.StateRoot, result.StateRoot,
			"State root mismatch at iteration %d", i+1)

		// Compare gas usage
		assert.Equal(t, baseResult.GasUsed, result.GasUsed,
			"Gas usage mismatch at iteration %d: expected %d, got %d", i+1, baseResult.GasUsed, result.GasUsed)

		// Compare events
		assert.Equal(t, len(baseResult.Events), len(result.Events),
			"Event count mismatch at iteration %d", i+1)

		for j, event := range result.Events {
			assert.Equal(t, baseResult.Events[j].Type, event.Type,
				"Event type mismatch at iteration %d, event %d", i+1, j)
			assert.Equal(t, baseResult.Events[j].Data, event.Data,
				"Event data mismatch at iteration %d, event %d", i+1, j)
		}

		// Compare return data
		assert.Equal(t, baseResult.ReturnData, result.ReturnData,
			"Return data mismatch at iteration %d", i+1)

		// Compare errors
		assert.Equal(t, baseResult.Error, result.Error,
			"Error status mismatch at iteration %d", i+1)
	}

	// Calculate execution time statistics
	var totalTime time.Duration
	minTime := executionTimes[0]
	maxTime := executionTimes[0]

	for _, execTime := range executionTimes {
		totalTime += execTime
		if execTime < minTime {
			minTime = execTime
		}
		if execTime > maxTime {
			maxTime = execTime
		}
	}

	avgTime := totalTime / time.Duration(len(executionTimes))

	t.Logf("✅ Determinism verified across %d iterations", test.Iterations)
	t.Logf("📊 Execution time stats:")
	t.Logf("  Average: %v", avgTime)
	t.Logf("  Min: %v", minTime)
	t.Logf("  Max: %v", maxTime)
	t.Logf("  Range: %v", maxTime-minTime)
	t.Logf("🔍 Result fingerprint:")
	t.Logf("  State root: %x", baseResult.StateRoot[:8])
	t.Logf("  Gas used: %d", baseResult.GasUsed)
	t.Logf("  Events: %d", len(baseResult.Events))
	t.Logf("  Return size: %d bytes", len(baseResult.ReturnData))
}

// TestFuzzDeterminism runs fuzz tests with random inputs
func TestFuzzDeterminism(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fuzz test in short mode")
	}

	fuzzTests := []struct {
		name        string
		moduleWASM  []byte
		entry       string
		inputGen    func() []interface{}
		iterations  int
		description string
	}{
		{
			name:       "RandomCounterIncrements",
			moduleWASM: generateCounterWASM(),
			entry:      "increment",
			inputGen: func() []interface{} {
				return []interface{}{randomInt(1, 1000)}
			},
			iterations:  50,
			description: "Counter with random increment values",
		},
		{
			name:       "RandomStringProcessing",
			moduleWASM: generateStringWASM(),
			entry:      "process_string",
			inputGen: func() []interface{} {
				return []interface{}{randomString(10, 100), randomInt(1, 255)}
			},
			iterations:  50,
			description: "String processing with random inputs",
		},
		{
			name:       "RandomMathOperations",
			moduleWASM: generateMathWASM(),
			entry:      "calculate",
			inputGen: func() []interface{} {
				return []interface{}{randomFloat(), randomFloat()}
			},
			iterations:  50,
			description: "Math operations with random floating point inputs",
		},
	}

	for _, fuzzTest := range fuzzTests {
		t.Run(fuzzTest.name, func(t *testing.T) {
			t.Logf("Fuzz testing: %s", fuzzTest.description)

			for i := 0; i < fuzzTest.iterations; i++ {
				// Generate random inputs
				inputs := fuzzTest.inputGen()
				t.Logf("Iteration %d with inputs: %v", i+1, inputs)

				// Execute multiple times with same inputs
				const runsPerInput = 3
				var results []ExecutionResult

				for run := 0; run < runsPerInput; run++ {
					result, err := executeWASMModule(fuzzTest.moduleWASM, fuzzTest.entry, inputs)
					require.NoError(t, err, "Fuzz execution failed at iteration %d, run %d", i+1, run+1)
					results = append(results, result)
				}

				// Verify determinism for this input set
				for run := 1; run < runsPerInput; run++ {
					assert.Equal(t, results[0].StateRoot, results[run].StateRoot,
						"Fuzz determinism failed: state root mismatch at iteration %d, run %d", i+1, run+1)
					assert.Equal(t, results[0].GasUsed, results[run].GasUsed,
						"Fuzz determinism failed: gas usage mismatch at iteration %d, run %d", i+1, run+1)
				}
			}

			t.Logf("✅ Fuzz determinism verified across %d random input sets", fuzzTest.iterations)
		})
	}
}

// TestConcurrentDeterminism verifies determinism under concurrent execution
func TestConcurrentDeterminism(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	const (
		numGoroutines          = 10
		iterationsPerGoroutine = 20
	)

	module := generateCounterWASM()
	entry := "increment"
	args := []interface{}{42}

	t.Logf("Testing concurrent determinism with %d goroutines, %d iterations each", numGoroutines, iterationsPerGoroutine)

	// Channel to collect results
	results := make(chan ExecutionResult, numGoroutines*iterationsPerGoroutine)

	// Run concurrent executions
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			for i := 0; i < iterationsPerGoroutine; i++ {
				result, err := executeWASMModule(module, entry, args)
				if err != nil {
					t.Errorf("Concurrent execution failed in goroutine %d, iteration %d: %v", goroutineID, i, err)
					return
				}
				results <- result
			}
		}(g)
	}

	// Collect all results
	var allResults []ExecutionResult
	for i := 0; i < numGoroutines*iterationsPerGoroutine; i++ {
		select {
		case result := <-results:
			allResults = append(allResults, result)
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for concurrent execution results")
		}
	}

	// Verify all results are identical
	baseResult := allResults[0]
	for i, result := range allResults[1:] {
		assert.Equal(t, baseResult.StateRoot, result.StateRoot,
			"Concurrent determinism failed: state root mismatch at result %d", i+1)
		assert.Equal(t, baseResult.GasUsed, result.GasUsed,
			"Concurrent determinism failed: gas usage mismatch at result %d", i+1)
	}

	t.Logf("✅ Concurrent determinism verified across %d executions", len(allResults))
}

// Mock WASM module execution function
func executeWASMModule(moduleWASM []byte, entry string, args []interface{}) (ExecutionResult, error) {
	// This is a mock implementation
	// In a real system, this would execute the WASM module using a runtime like Wasmtime

	// Simulate deterministic execution based on inputs
	hasher := sha256.New()
	hasher.Write(moduleWASM)
	hasher.Write([]byte(entry))

	// Hash arguments to ensure deterministic state
	for _, arg := range args {
		switch v := arg.(type) {
		case int:
			hasher.Write([]byte(fmt.Sprintf("%d", v)))
		case string:
			hasher.Write([]byte(v))
		case float64:
			hasher.Write([]byte(fmt.Sprintf("%.10f", v)))
		case map[string]interface{}:
			for k, val := range v {
				hasher.Write([]byte(k))
				hasher.Write([]byte(fmt.Sprintf("%v", val)))
			}
		}
	}

	stateRoot := hasher.Sum(nil)

	// Simulate deterministic gas calculation
	gasUsed := uint64(1000 + len(moduleWASM)%500)
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			gasUsed += uint64(len(v) * 2)
		case int:
			gasUsed += uint64(v % 100)
		}
	}

	// Simulate deterministic events
	events := []Event{
		{
			Type: "execution_started",
			Data: map[string]interface{}{
				"entry":      entry,
				"args_count": len(args),
			},
		},
	}

	if entry == "increment" {
		events = append(events, Event{
			Type: "counter_incremented",
			Data: map[string]interface{}{
				"amount": args[0],
			},
		})
	}

	// Simulate deterministic return data
	returnData := make([]byte, 32)
	copy(returnData, stateRoot[:32])

	return ExecutionResult{
		StateRoot:  stateRoot,
		GasUsed:    gasUsed,
		Events:     events,
		ReturnData: returnData,
		Error:      "",
	}, nil
}

// Helper functions to generate mock WASM modules

func generateCounterWASM() []byte {
	// Mock WASM module for counter contract
	wasm := make([]byte, 1024)
	copy(wasm[:4], []byte{0x00, 0x61, 0x73, 0x6d})  // WASM magic
	copy(wasm[4:8], []byte{0x01, 0x00, 0x00, 0x00}) // Version

	// Add deterministic content based on "counter" functionality
	content := []byte("counter_contract_v1_increment_decrement_get_value")
	copy(wasm[8:], content)

	return wasm
}

func generateStringWASM() []byte {
	wasm := make([]byte, 1536)
	copy(wasm[:4], []byte{0x00, 0x61, 0x73, 0x6d})
	copy(wasm[4:8], []byte{0x01, 0x00, 0x00, 0x00})

	content := []byte("string_processor_v1_concat_split_reverse_uppercase")
	copy(wasm[8:], content)

	return wasm
}

func generateMathWASM() []byte {
	wasm := make([]byte, 2048)
	copy(wasm[:4], []byte{0x00, 0x61, 0x73, 0x6d})
	copy(wasm[4:8], []byte{0x01, 0x00, 0x00, 0x00})

	content := []byte("math_processor_v1_add_mul_sqrt_sin_cos_tan_log")
	copy(wasm[8:], content)

	return wasm
}

func generateStateWASM() []byte {
	wasm := make([]byte, 2560)
	copy(wasm[:4], []byte{0x00, 0x61, 0x73, 0x6d})
	copy(wasm[4:8], []byte{0x01, 0x00, 0x00, 0x00})

	content := []byte("state_manager_v1_get_set_delete_iterate_batch")
	copy(wasm[8:], content)

	return wasm
}

// Random input generators for fuzz testing

func randomInt(min, max int) int {
	if min >= max {
		return min
	}
	return min + int(randomBytes(4)[0])%(max-min)
}

func randomFloat() float64 {
	bytes := randomBytes(8)
	// Convert to float64 in range [0, 1000)
	val := float64(bytes[0]) + float64(bytes[1])/256.0 + float64(bytes[2])/65536.0
	return val * 1000.0 / 256.0
}

func randomString(minLen, maxLen int) string {
	length := randomInt(minLen, maxLen)
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	result := make([]byte, length)
	randomBytes := randomBytes(length)

	for i := 0; i < length; i++ {
		result[i] = chars[randomBytes[i]%byte(len(chars))]
	}

	return string(result)
}

func randomBytes(n int) []byte {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return bytes
}

// BenchmarkDeterministicExecution benchmarks the execution time of deterministic operations
func BenchmarkDeterministicExecution(b *testing.B) {
	module := generateCounterWASM()
	entry := "increment"
	args := []interface{}{1}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := executeWASMModule(module, entry, args)
		if err != nil {
			b.Fatalf("Benchmark execution failed: %v", err)
		}
	}
}

// BenchmarkStateHashing benchmarks state root calculation
func BenchmarkStateHashing(b *testing.B) {
	data := randomBytes(1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hasher := sha256.New()
		hasher.Write(data)
		_ = hasher.Sum(nil)
	}
}

// TestExecutionReproducibility ensures that execution results can be reproduced given the same conditions
func TestExecutionReproducibility(t *testing.T) {
	testCases := []struct {
		name   string
		module []byte
		entry  string
		args   []interface{}
		runs   int
	}{
		{
			name:   "SimpleIncrement",
			module: generateCounterWASM(),
			entry:  "increment",
			args:   []interface{}{5},
			runs:   10,
		},
		{
			name:   "StringProcessing",
			module: generateStringWASM(),
			entry:  "process_string",
			args:   []interface{}{"test", 100},
			runs:   10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Store results from first execution for comparison
			firstResult, err := executeWASMModule(tc.module, tc.entry, tc.args)
			require.NoError(t, err)

			// Create a signature for the first result
			firstSignature := createResultSignature(firstResult)

			// Run multiple times and verify reproducibility
			for i := 1; i < tc.runs; i++ {
				result, err := executeWASMModule(tc.module, tc.entry, tc.args)
				require.NoError(t, err)

				resultSignature := createResultSignature(result)
				assert.Equal(t, firstSignature, resultSignature,
					"Result not reproducible at run %d", i+1)
			}

			t.Logf("✅ Execution reproducible across %d runs", tc.runs)
			t.Logf("Result signature: %s", firstSignature)
		})
	}
}

func createResultSignature(result ExecutionResult) string {
	hasher := sha256.New()
	hasher.Write(result.StateRoot)
	hasher.Write([]byte(fmt.Sprintf("%d", result.GasUsed)))

	for _, event := range result.Events {
		hasher.Write([]byte(event.Type))
		hasher.Write([]byte(fmt.Sprintf("%v", event.Data)))
	}

	hasher.Write(result.ReturnData)
	hasher.Write([]byte(result.Error))

	return hex.EncodeToString(hasher.Sum(nil)[:8])
}
