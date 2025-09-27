package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/internal/rpc"
	"github.com/opendlt/accumulate-accumen/types/l1"
)

var (
	rpcAddr      = flag.String("rpc", "http://localhost:8666", "RPC endpoint URL")
	contracts    = flag.Int("contracts", 10, "Number of contracts to deploy and stress test")
	tps          = flag.Int("tps", 100, "Target transactions per second")
	duration     = flag.Duration("duration", 30*time.Second, "Test duration")
	payloadBytes = flag.Int("payload-bytes", 256, "Size of random payload per transaction")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	workers      = flag.Int("workers", 10, "Number of worker goroutines")
)

type StressTestResult struct {
	TotalTxs       int64
	SuccessfulTxs  int64
	FailedTxs      int64
	ErrorRate      float64
	LatencyP50     time.Duration
	LatencyP95     time.Duration
	LatencyP99     time.Duration
	MeanLatency    time.Duration
	ActualTPS      float64
	TestDuration   time.Duration
	ContractsUsed  int
	PayloadSize    int
	ErrorsByType   map[string]int64
}

type TxResult struct {
	Latency time.Duration
	Error   error
	TxHash  string
}

type StressTester struct {
	rpcClient     *rpc.Client
	contracts     []string
	logger        *logz.Logger
	results       chan TxResult
	contractIndex int64
	txCounter     int64
}

func main() {
	flag.Parse()

	// Setup logging
	logLevel := logz.INFO
	if *verbose {
		logLevel = logz.DEBUG
	}
	logz.SetDefaultLevel(logLevel)
	logger := logz.New(logLevel, "accustress")

	logger.Info("🚀 Starting Accumen Stress Test")
	logger.Info("Configuration:")
	logger.Info("  RPC Endpoint: %s", *rpcAddr)
	logger.Info("  Contracts: %d", *contracts)
	logger.Info("  Target TPS: %d", *tps)
	logger.Info("  Duration: %v", *duration)
	logger.Info("  Payload Size: %d bytes", *payloadBytes)
	logger.Info("  Workers: %d", *workers)

	// Create RPC client
	client, err := rpc.NewClient(*rpcAddr)
	if err != nil {
		log.Fatalf("Failed to create RPC client: %v", err)
	}

	// Create stress tester
	tester := &StressTester{
		rpcClient: client,
		logger:    logger,
		results:   make(chan TxResult, *workers*10), // Buffer for results
	}

	// Deploy contracts for testing
	logger.Info("📦 Deploying %d test contracts...", *contracts)
	if err := tester.deployTestContracts(*contracts); err != nil {
		log.Fatalf("Failed to deploy test contracts: %v", err)
	}
	logger.Info("✅ Deployed %d contracts successfully", len(tester.contracts))

	// Run stress test
	logger.Info("⚡ Starting stress test for %v...", *duration)
	result, err := tester.runStressTest(*tps, *duration, *workers)
	if err != nil {
		log.Fatalf("Stress test failed: %v", err)
	}

	// Print results
	printResults(result, logger)
}

func (st *StressTester) deployTestContracts(count int) error {
	st.contracts = make([]string, 0, count)

	// Simple counter contract WASM (base64 encoded)
	// This would be the actual compiled WASM bytes
	contractWASM := generateMockWASM(*payloadBytes)

	for i := 0; i < count; i++ {
		contractAddr := fmt.Sprintf("acc://stress-test-%d.acme", i)

		// Deploy contract
		req := &rpc.DeployRequest{
			Address: contractAddr,
			WASM:    contractWASM,
		}

		resp, err := st.rpcClient.Deploy(context.Background(), req)
		if err != nil {
			return fmt.Errorf("failed to deploy contract %d: %w", i, err)
		}

		if !resp.Success {
			return fmt.Errorf("contract deployment %d failed: %s", i, resp.Error)
		}

		st.contracts = append(st.contracts, contractAddr)
		st.logger.Debug("Deployed contract %s", contractAddr)
	}

	return nil
}

func (st *StressTester) runStressTest(targetTPS int, testDuration time.Duration, numWorkers int) (*StressTestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testDuration+10*time.Second)
	defer cancel()

	// Calculate target interval between transactions
	targetInterval := time.Second / time.Duration(targetTPS)

	// Start result collector
	results := make([]TxResult, 0, targetTPS*int(testDuration.Seconds())+1000)
	var resultsMutex sync.Mutex

	// Result collector goroutine
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for result := range st.results {
			resultsMutex.Lock()
			results = append(results, result)
			resultsMutex.Unlock()
		}
	}()

	// Start workers
	var wg sync.WaitGroup
	workQueue := make(chan struct{}, targetTPS*2) // Buffer to smooth rate

	// Rate limiter goroutine
	go func() {
		ticker := time.NewTicker(targetInterval)
		defer ticker.Stop()

		testStart := time.Now()
		for {
			select {
			case <-ctx.Done():
				close(workQueue)
				return
			case <-ticker.C:
				if time.Since(testStart) >= testDuration {
					close(workQueue)
					return
				}
				select {
				case workQueue <- struct{}{}:
				default:
					// Queue full, skip this tick
					st.logger.Debug("Work queue full, skipping tick")
				}
			}
		}
	}()

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			st.worker(ctx, workerID, workQueue)
		}(i)
	}

	// Wait for test completion
	testStart := time.Now()
	wg.Wait()
	testEnd := time.Now()
	actualDuration := testEnd.Sub(testStart)

	// Close results channel and wait for collector
	close(st.results)
	<-collectorDone

	// Analyze results
	return st.analyzeResults(results, actualDuration), nil
}

func (st *StressTester) worker(ctx context.Context, workerID int, workQueue <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-workQueue:
			if !ok {
				return
			}
			st.executeTransaction()
		}
	}
}

func (st *StressTester) executeTransaction() {
	start := time.Now()

	// Select contract in round-robin fashion
	contractIndex := atomic.AddInt64(&st.contractIndex, 1) % int64(len(st.contracts))
	contractAddr := st.contracts[contractIndex]

	// Generate random transaction data
	txData := st.generateRandomTxData()

	// Execute transaction
	req := &rpc.SubmitRequest{
		Contract: contractAddr,
		Entry:    "stress_test",
		Args:     txData,
	}

	resp, err := st.rpcClient.Submit(context.Background(), req)
	latency := time.Since(start)

	// Record result
	result := TxResult{
		Latency: latency,
		Error:   err,
	}

	if err == nil && resp.Success {
		result.TxHash = resp.TxHash
		atomic.AddInt64(&st.txCounter, 1)
	} else {
		if err == nil {
			result.Error = fmt.Errorf("transaction failed: %s", resp.Error)
		}
	}

	// Send result to collector
	select {
	case st.results <- result:
	default:
		st.logger.Debug("Results channel full, dropping result")
	}
}

func (st *StressTester) generateRandomTxData() map[string]interface{} {
	// Generate random payload
	payload := make([]byte, *payloadBytes)
	rand.Read(payload)

	// Create transaction arguments
	return map[string]interface{}{
		"operation": "increment",
		"amount":    1,
		"payload":   payload,
		"timestamp": time.Now().UnixNano(),
		"nonce":     atomic.AddInt64(&st.txCounter, 1),
	}
}

func (st *StressTester) analyzeResults(results []TxResult, duration time.Duration) *StressTestResult {
	if len(results) == 0 {
		return &StressTestResult{
			TestDuration:  duration,
			ContractsUsed: len(st.contracts),
			PayloadSize:   *payloadBytes,
			ErrorsByType:  make(map[string]int64),
		}
	}

	var successful, failed int64
	var latencies []time.Duration
	var totalLatency time.Duration
	errorsByType := make(map[string]int64)

	// Process results
	for _, result := range results {
		if result.Error != nil {
			failed++
			errorType := "unknown"
			if result.Error != nil {
				errorType = fmt.Sprintf("%T", result.Error)
			}
			errorsByType[errorType]++
		} else {
			successful++
			latencies = append(latencies, result.Latency)
			totalLatency += result.Latency
		}
	}

	// Calculate latency percentiles
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var p50, p95, p99, mean time.Duration
	if len(latencies) > 0 {
		p50 = latencies[int(float64(len(latencies))*0.5)]
		p95 = latencies[int(float64(len(latencies))*0.95)]
		p99 = latencies[int(float64(len(latencies))*0.99)]
		mean = totalLatency / time.Duration(len(latencies))
	}

	total := successful + failed
	errorRate := 0.0
	if total > 0 {
		errorRate = float64(failed) / float64(total) * 100
	}

	actualTPS := float64(total) / duration.Seconds()

	return &StressTestResult{
		TotalTxs:      total,
		SuccessfulTxs: successful,
		FailedTxs:     failed,
		ErrorRate:     errorRate,
		LatencyP50:    p50,
		LatencyP95:    p95,
		LatencyP99:    p99,
		MeanLatency:   mean,
		ActualTPS:     actualTPS,
		TestDuration:  duration,
		ContractsUsed: len(st.contracts),
		PayloadSize:   *payloadBytes,
		ErrorsByType:  errorsByType,
	}
}

func printResults(result *StressTestResult, logger *logz.Logger) {
	logger.Info("📊 Stress Test Results")
	logger.Info("=" * 50)
	logger.Info("Test Configuration:")
	logger.Info("  Duration: %v", result.TestDuration)
	logger.Info("  Contracts: %d", result.ContractsUsed)
	logger.Info("  Payload Size: %d bytes", result.PayloadSize)
	logger.Info("")
	logger.Info("Throughput:")
	logger.Info("  Total Transactions: %d", result.TotalTxs)
	logger.Info("  Successful: %d", result.SuccessfulTxs)
	logger.Info("  Failed: %d", result.FailedTxs)
	logger.Info("  Actual TPS: %.2f", result.ActualTPS)
	logger.Info("  Error Rate: %.2f%%", result.ErrorRate)
	logger.Info("")
	logger.Info("Latency (successful transactions):")
	logger.Info("  Mean: %v", result.MeanLatency)
	logger.Info("  P50: %v", result.LatencyP50)
	logger.Info("  P95: %v", result.LatencyP95)
	logger.Info("  P99: %v", result.LatencyP99)

	if len(result.ErrorsByType) > 0 {
		logger.Info("")
		logger.Info("Errors by Type:")
		for errorType, count := range result.ErrorsByType {
			logger.Info("  %s: %d", errorType, count)
		}
	}

	// Performance assessment
	logger.Info("")
	logger.Info("Performance Assessment:")
	if result.ErrorRate < 1.0 {
		logger.Info("  ✅ Error rate is acceptable (<1%%)")
	} else if result.ErrorRate < 5.0 {
		logger.Info("  ⚠️  Error rate is elevated (%.2f%%)", result.ErrorRate)
	} else {
		logger.Info("  ❌ Error rate is high (%.2f%%)", result.ErrorRate)
	}

	if result.LatencyP95 < 100*time.Millisecond {
		logger.Info("  ✅ P95 latency is good (<%v)", 100*time.Millisecond)
	} else if result.LatencyP95 < 500*time.Millisecond {
		logger.Info("  ⚠️  P95 latency is moderate (%v)", result.LatencyP95)
	} else {
		logger.Info("  ❌ P95 latency is high (%v)", result.LatencyP95)
	}

	if result.ActualTPS >= float64(*tps)*0.9 {
		logger.Info("  ✅ Throughput achieved target (%.2f TPS >= %.2f TPS)", result.ActualTPS, float64(*tps)*0.9)
	} else {
		logger.Info("  ⚠️  Throughput below target (%.2f TPS < %.2f TPS)", result.ActualTPS, float64(*tps)*0.9)
	}

	// Output JSON for automated analysis
	if *verbose {
		jsonOutput, _ := json.MarshalIndent(result, "", "  ")
		logger.Info("")
		logger.Info("JSON Output:")
		fmt.Println(string(jsonOutput))
	}
}

func generateMockWASM(size int) []byte {
	// Generate a mock WASM module for testing
	// In a real implementation, this would be actual compiled WASM
	mockWASM := make([]byte, size)
	copy(mockWASM[:4], []byte{0x00, 0x61, 0x73, 0x6d}) // WASM magic bytes
	copy(mockWASM[4:8], []byte{0x01, 0x00, 0x00, 0x00}) // WASM version

	// Fill rest with random data
	if len(mockWASM) > 8 {
		rand.Read(mockWASM[8:])
	}

	return mockWASM
}

// Helper function for string repetition (Go doesn't have built-in string * operator)
func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}