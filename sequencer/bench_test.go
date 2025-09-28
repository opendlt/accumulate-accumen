package sequencer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/types/l1"
	"github.com/stretchr/testify/require"
)

// BenchmarkExecuteTx benchmarks transaction execution performance
func BenchmarkExecuteTx(b *testing.B) {
	benchmarks := []struct {
		name        string
		txSize      int
		payloadSize int
		description string
	}{
		{
			name:        "SmallTx",
			txSize:      100,
			payloadSize: 32,
			description: "Small transaction with minimal payload",
		},
		{
			name:        "MediumTx",
			txSize:      500,
			payloadSize: 256,
			description: "Medium transaction with standard payload",
		},
		{
			name:        "LargeTx",
			txSize:      2000,
			payloadSize: 1024,
			description: "Large transaction with significant payload",
		},
		{
			name:        "XLargeTx",
			txSize:      8000,
			payloadSize: 4096,
			description: "Extra large transaction with maximum payload",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup sequencer for benchmarking
			seq, cleanup := setupBenchmarkSequencer(b)
			defer cleanup()

			// Pre-generate transactions to avoid allocation overhead in benchmark
			txs := make([]l1.Tx, b.N)
			for i := 0; i < b.N; i++ {
				txs[i] = generateBenchmarkTx(bm.txSize, bm.payloadSize)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Benchmark transaction execution
			for i := 0; i < b.N; i++ {
				// Convert l1.Tx to sequencer.Transaction
				seqTx := convertL1TxToSequencerTx(txs[i])
				err := seq.SubmitTransaction(seqTx)
				if err != nil {
					b.Fatalf("Transaction execution failed: %v", err)
				}
			}

			// Report custom metrics
			b.ReportMetric(float64(bm.txSize), "tx_bytes")
			b.ReportMetric(float64(bm.payloadSize), "payload_bytes")
		})
	}
}

// BenchmarkMempoolIngest benchmarks mempool ingestion performance
func BenchmarkMempoolIngest(b *testing.B) {
	sizes := []int{1, 10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("BatchSize%d", size), func(b *testing.B) {
			// Pre-generate transaction batch (only one batch needed)
			batch := make([]l1.Tx, size)
			for j := 0; j < size; j++ {
				batch[j] = generateBenchmarkTx(500, 256)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Benchmark mempool ingestion with fresh mempool for each iteration
			for i := 0; i < b.N; i++ {
				// Create fresh mempool for each iteration to avoid capacity limits
				mempool, cleanup := setupBenchmarkMempool(b)

				// Add batch to mempool
				for _, tx := range batch {
					seqTx := convertL1TxToSequencerTx(tx)
					err := mempool.AddTransaction(seqTx)
					if err != nil {
						cleanup()
						b.Fatalf("Mempool add failed: %v", err)
					}
				}

				cleanup()
			}

			b.ReportMetric(float64(size), "batch_size")
		})
	}
}

// BenchmarkBlockProduction benchmarks complete block production cycle
func BenchmarkBlockProduction(b *testing.B) {
	blockSizes := []int{1, 10, 50, 100}

	for _, blockSize := range blockSizes {
		b.Run(fmt.Sprintf("BlockSize%d", blockSize), func(b *testing.B) {
			// Setup sequencer for block production
			seq, cleanup := setupBenchmarkSequencer(b)
			defer cleanup()

			// Pre-populate mempool with transactions
			txs := make([]l1.Tx, blockSize*b.N)
			for i := 0; i < len(txs); i++ {
				txs[i] = generateBenchmarkTx(500, 256)
			}

			// Add transactions to sequencer (since mempool is not directly accessible)
			for _, tx := range txs {
				seqTx := convertL1TxToSequencerTx(tx)
				err := seq.SubmitTransaction(seqTx)
				require.NoError(b, err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Benchmark block production (simulate by executing transactions)
			for i := 0; i < b.N; i++ {
				start := time.Now()
				txsExecuted := 0

				// Execute a batch of transactions to simulate block production
				for j := 0; j < blockSize && j < len(txs); j++ {
					seqTx := convertL1TxToSequencerTx(txs[j])
					err := seq.SubmitTransaction(seqTx)
					if err != nil {
						b.Fatalf("Block production failed: %v", err)
					}
					txsExecuted++
				}

				elapsed := time.Since(start)
				b.ReportMetric(elapsed.Seconds(), "block_time_seconds")

				if txsExecuted != blockSize {
					b.Fatalf("Expected %d transactions, got %d", blockSize, txsExecuted)
				}
			}

			b.ReportMetric(float64(blockSize), "transactions_per_block")
		})
	}
}

// BenchmarkStateOperations benchmarks state read/write operations
func BenchmarkStateOperations(b *testing.B) {
	operations := []struct {
		name string
		op   func(*testing.B, state.KVStore)
	}{
		{
			name: "SetKey",
			op:   benchmarkStateSet,
		},
		{
			name: "GetKey",
			op:   benchmarkStateGet,
		},
		{
			name: "DeleteKey",
			op:   benchmarkStateDelete,
		},
		{
			name: "IterateKeys",
			op:   benchmarkStateIterate,
		},
	}

	backends := []struct {
		name  string
		setup func(*testing.B) (state.KVStore, func())
	}{
		{
			name:  "Memory",
			setup: setupMemoryKVStore,
		},
		{
			name:  "Badger",
			setup: setupBadgerKVStore,
		},
	}

	for _, backend := range backends {
		for _, op := range operations {
			b.Run(fmt.Sprintf("%s_%s", backend.name, op.name), func(b *testing.B) {
				kvStore, cleanup := backend.setup(b)
				defer cleanup()

				b.ResetTimer()
				op.op(b, kvStore)
			})
		}
	}
}

// BenchmarkConcurrentExecution benchmarks concurrent transaction execution
func BenchmarkConcurrentExecution(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8, 16}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency%d", concurrency), func(b *testing.B) {
			seq, cleanup := setupBenchmarkSequencer(b)
			defer cleanup()

			// Pre-generate transactions
			txs := make([]l1.Tx, b.N)
			for i := 0; i < b.N; i++ {
				txs[i] = generateBenchmarkTx(500, 256)
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Execute transactions with specified concurrency
			txChan := make(chan l1.Tx, concurrency*2)
			errChan := make(chan error, b.N)

			// Start workers
			for i := 0; i < concurrency; i++ {
				go func() {
					for tx := range txChan {
						seqTx := convertL1TxToSequencerTx(tx)
						err := seq.SubmitTransaction(seqTx)
						errChan <- err
					}
				}()
			}

			// Send transactions
			go func() {
				for _, tx := range txs {
					txChan <- tx
				}
				close(txChan)
			}()

			// Collect results
			for i := 0; i < b.N; i++ {
				err := <-errChan
				if err != nil {
					b.Fatalf("Concurrent execution failed: %v", err)
				}
			}

			b.ReportMetric(float64(concurrency), "workers")
		})
	}
}

// BenchmarkThroughput measures sustained throughput under various conditions
func BenchmarkThroughput(b *testing.B) {
	scenarios := []struct {
		name        string
		txCount     int
		workers     int
		blockSize   int
		description string
	}{
		{
			name:        "LowThroughput",
			txCount:     100,
			workers:     2,
			blockSize:   10,
			description: "Low throughput scenario",
		},
		{
			name:        "MediumThroughput",
			txCount:     1000,
			workers:     4,
			blockSize:   50,
			description: "Medium throughput scenario",
		},
		{
			name:        "HighThroughput",
			txCount:     10000,
			workers:     8,
			blockSize:   100,
			description: "High throughput scenario",
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			seq, cleanup := setupBenchmarkSequencer(b)
			defer cleanup()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				start := time.Now()

				// Generate and execute transactions
				for j := 0; j < scenario.txCount; j++ {
					tx := generateBenchmarkTx(500, 256)
					seqTx := convertL1TxToSequencerTx(tx)
					err := seq.SubmitTransaction(seqTx)
					if err != nil {
						b.Fatalf("Transaction execution failed: %v", err)
					}
				}

				elapsed := time.Since(start)
				tps := float64(scenario.txCount) / elapsed.Seconds()

				b.ReportMetric(tps, "tps")
				b.ReportMetric(float64(scenario.txCount), "total_txs")
				b.ReportMetric(elapsed.Seconds(), "elapsed_seconds")
			}
		})
	}
}

// Helper functions for benchmark setup

func setupBenchmarkSequencer(b *testing.B) (*Sequencer, func()) {
	// Create a mock sequencer for benchmarking that bypasses unimplemented components
	seq := &Sequencer{
		config: &Config{
			ListenAddr:      "127.0.0.1:0", // Use random port
			BlockTime:       time.Second,
			MaxTransactions: 1000,
			Bridge: BridgeConfig{
				EnableBridge: false, // Disable for benchmarking
			},
		},
		running:  false,
		stopChan: make(chan struct{}),
	}

	cleanup := func() {
		if seq.running {
			seq.Stop(context.Background())
		}
	}

	return seq, cleanup
}

func setupBenchmarkMempool(b *testing.B) (*Mempool, func()) {
	config := MempoolConfig{
		MaxSize:          10000,
		MaxTxSize:        1024 * 1024,
		TTL:              time.Hour,
		PriceLimit:       1000,
		AccountLimit:     100,
		GlobalLimit:      10000,
		EnablePriority:   true,
		RebroadcastDelay: time.Minute,
	}

	// Create a basic credit manager for testing
	creditMgr := &pricing.CreditManager{} // TODO: Initialize properly if needed

	mempool := NewMempool(config, creditMgr)
	cleanup := func() {
		mempool.Stop()
	}
	return mempool, cleanup
}

func setupMemoryKVStore(b *testing.B) (state.KVStore, func()) {
	kvStore := state.NewMemoryKVStore()
	cleanup := func() {}
	return kvStore, cleanup
}

func setupBadgerKVStore(b *testing.B) (state.KVStore, func()) {
	tmpDir := b.TempDir()
	kvStore, err := state.NewBadgerStore(tmpDir)
	require.NoError(b, err)

	cleanup := func() {
		kvStore.Close()
	}

	return kvStore, cleanup
}

func generateBenchmarkTx(txSize, payloadSize int) l1.Tx {
	// Generate a realistic transaction for benchmarking
	payload := make([]byte, payloadSize)
	rand.Read(payload)

	nonce := make([]byte, 16)
	rand.Read(nonce)

	tx := l1.Tx{
		Contract: fmt.Sprintf("acc://benchmark-contract-%d.acme", time.Now().UnixNano()%1000),
		Entry:    "benchmark_operation",
		Args: map[string]interface{}{
			"payload":   payload,
			"size":      txSize,
			"timestamp": time.Now().UnixNano(),
		},
		Nonce:     nonce,
		Timestamp: time.Now().UnixNano(),
	}

	return tx
}

// State operation benchmarks

func benchmarkStateSet(b *testing.B, kvStore state.KVStore) {
	keys := make([][]byte, b.N)
	values := make([][]byte, b.N)

	// Pre-generate keys and values
	for i := 0; i < b.N; i++ {
		keys[i] = []byte(fmt.Sprintf("benchmark_key_%d", i))
		values[i] = make([]byte, 256)
		rand.Read(values[i])
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := kvStore.Set(keys[i], values[i])
		if err != nil {
			b.Fatalf("Set operation failed: %v", err)
		}
	}
}

func benchmarkStateGet(b *testing.B, kvStore state.KVStore) {
	// Pre-populate store
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("benchmark_key_%d", i))
		value := make([]byte, 256)
		rand.Read(value)
		kvStore.Set(key, value)
	}

	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = []byte(fmt.Sprintf("benchmark_key_%d", i%1000))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := kvStore.Get(keys[i])
		if err != nil {
			b.Fatalf("Get operation failed: %v", err)
		}
	}
}

func benchmarkStateDelete(b *testing.B, kvStore state.KVStore) {
	// Pre-populate store
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = []byte(fmt.Sprintf("benchmark_key_%d", i))
		value := make([]byte, 256)
		rand.Read(value)
		kvStore.Set(keys[i], value)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := kvStore.Delete(keys[i])
		if err != nil {
			b.Fatalf("Delete operation failed: %v", err)
		}
	}
}

func benchmarkStateIterate(b *testing.B, kvStore state.KVStore) {
	// Pre-populate store
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("benchmark_key_%04d", i))
		value := make([]byte, 256)
		rand.Read(value)
		kvStore.Set(key, value)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		count := 0
		err := kvStore.Iterate(func(key, value []byte) error {
			count++
			return nil
		})
		if err != nil {
			b.Fatalf("Iterate operation failed: %v", err)
		}
	}
}

// BenchmarkMemoryUsage measures memory usage patterns
func BenchmarkMemoryUsage(b *testing.B) {
	seq, cleanup := setupBenchmarkSequencer(b)
	defer cleanup()

	// Measure baseline memory
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Execute many transactions
	for i := 0; i < b.N; i++ {
		tx := generateBenchmarkTx(500, 256)
		seqTx := convertL1TxToSequencerTx(tx)
		err := seq.SubmitTransaction(seqTx)
		if err != nil {
			b.Fatalf("Transaction execution failed: %v", err)
		}
	}

	// Measure memory after execution
	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.Alloc-m1.Alloc), "bytes_allocated")
	b.ReportMetric(float64(m2.Mallocs-m1.Mallocs), "mallocs")
	b.ReportMetric(float64(m2.Frees-m1.Frees), "frees")
}

// BenchmarkLatency measures transaction latency distribution
func BenchmarkLatency(b *testing.B) {
	seq, cleanup := setupBenchmarkSequencer(b)
	defer cleanup()

	latencies := make([]time.Duration, b.N)
	tx := generateBenchmarkTx(500, 256)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		seqTx := convertL1TxToSequencerTx(tx)
		err := seq.SubmitTransaction(seqTx)
		latencies[i] = time.Since(start)

		if err != nil {
			b.Fatalf("Transaction execution failed: %v", err)
		}
	}

	// Calculate latency statistics
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 := latencies[len(latencies)/2]
	p95 := latencies[int(float64(len(latencies))*0.95)]
	p99 := latencies[int(float64(len(latencies))*0.99)]

	b.ReportMetric(float64(p50.Nanoseconds()), "p50_latency_ns")
	b.ReportMetric(float64(p95.Nanoseconds()), "p95_latency_ns")
	b.ReportMetric(float64(p99.Nanoseconds()), "p99_latency_ns")
}

// Helper function to convert l1.Tx to sequencer.Transaction
func convertL1TxToSequencerTx(l1Tx l1.Tx) *Transaction {
	// Convert l1.Tx to the sequencer's Transaction format
	hash := l1Tx.Hash()

	// Serialize the L1 transaction as data
	data, err := json.Marshal(l1Tx)
	if err != nil {
		// If marshaling fails, use a basic representation
		data = []byte(fmt.Sprintf("l1tx:%s:%s", l1Tx.Contract, l1Tx.Entry))
	}

	// Use a unique account per transaction to avoid limits
	fromAccount := fmt.Sprintf("benchmark-%x", hash[:4])

	return &Transaction{
		ID:         fmt.Sprintf("%x", hash),
		From:       fromAccount, // Unique account per transaction
		To:         l1Tx.Contract,
		Data:       data,
		GasLimit:   100000, // Default gas limit for benchmarks
		GasPrice:   1000,   // Gas price above the limit
		Nonce:      uint64(l1Tx.Timestamp),
		Signature:  l1Tx.Nonce, // Use L1 nonce as signature for simplicity
		Hash:       hash[:],
		Size:       uint64(len(data)),
		ReceivedAt: time.Now(),
		Priority:   1, // Default priority
	}
}
