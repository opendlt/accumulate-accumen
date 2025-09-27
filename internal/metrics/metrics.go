package metrics

import (
	"expvar"
	"net/http"
	"sync/atomic"
	"time"
)

// Global metrics exposed via expvar
var (
	// Counters
	BlocksProduced   = expvar.NewInt("blocks_produced")
	TxsExecuted      = expvar.NewInt("txs_executed")
	L0Submitted      = expvar.NewInt("l0_submitted")
	L0Failed         = expvar.NewInt("l0_failed")
	AnchorsWritten   = expvar.NewInt("anchors_written")
	WasmGasUsedTotal = expvar.NewInt("wasm_gas_used_total")

	// Gauges (using atomic values)
	currentHeight   int64
	lastAnchorTime  int64
	followerHeight  int64
	indexerEntries  int64
	rpcRequestCount int64
	rpcErrorCount   int64

	// Expvar gauges
	CurrentHeight   = expvar.NewInt("current_height")
	LastAnchorTime  = expvar.NewInt("last_anchor_time_unix")
	FollowerHeight  = expvar.NewInt("follower_height")
	IndexerEntries  = expvar.NewInt("indexer_entries_processed")
	RPCRequestCount = expvar.NewInt("rpc_request_count")
	RPCErrorCount   = expvar.NewInt("rpc_error_count")

	// String metrics
	NodeMode  = expvar.NewString("node_mode")
	StartTime = expvar.NewString("start_time")
)

// Initialize metrics on package load
func init() {
	// Set start time
	StartTime.Set(time.Now().UTC().Format(time.RFC3339))

	// Initialize node mode (will be set by application)
	NodeMode.Set("unknown")
}

// SetNodeMode sets the node operation mode
func SetNodeMode(mode string) {
	NodeMode.Set(mode)
}

// IncrementBlocksProduced increments the blocks produced counter
func IncrementBlocksProduced() {
	BlocksProduced.Add(1)
}

// IncrementTxsExecuted increments the transactions executed counter
func IncrementTxsExecuted(count int64) {
	TxsExecuted.Add(count)
}

// IncrementL0Submitted increments the L0 submissions counter
func IncrementL0Submitted() {
	L0Submitted.Add(1)
}

// IncrementL0Failed increments the L0 failures counter
func IncrementL0Failed() {
	L0Failed.Add(1)
}

// IncrementAnchorsWritten increments the anchors written counter
func IncrementAnchorsWritten() {
	AnchorsWritten.Add(1)
}

// AddWasmGasUsed adds to the total WASM gas used
func AddWasmGasUsed(gas uint64) {
	WasmGasUsedTotal.Add(int64(gas))
}

// SetCurrentHeight updates the current block height gauge
func SetCurrentHeight(height uint64) {
	atomic.StoreInt64(&currentHeight, int64(height))
	CurrentHeight.Set(int64(height))
}

// SetLastAnchorTime updates the last anchor time gauge
func SetLastAnchorTime(timestamp time.Time) {
	unixTime := timestamp.Unix()
	atomic.StoreInt64(&lastAnchorTime, unixTime)
	LastAnchorTime.Set(unixTime)
}

// SetFollowerHeight updates the follower height gauge
func SetFollowerHeight(height uint64) {
	atomic.StoreInt64(&followerHeight, int64(height))
	FollowerHeight.Set(int64(height))
}

// SetIndexerEntries updates the indexer entries processed gauge
func SetIndexerEntries(count uint64) {
	atomic.StoreInt64(&indexerEntries, int64(count))
	IndexerEntries.Set(int64(count))
}

// IncrementRPCRequests increments the RPC request counter
func IncrementRPCRequests() {
	atomic.AddInt64(&rpcRequestCount, 1)
	RPCRequestCount.Set(atomic.LoadInt64(&rpcRequestCount))
}

// IncrementRPCErrors increments the RPC error counter
func IncrementRPCErrors() {
	atomic.AddInt64(&rpcErrorCount, 1)
	RPCErrorCount.Set(atomic.LoadInt64(&rpcErrorCount))
}

// GetCurrentHeight returns the current height
func GetCurrentHeight() uint64 {
	return uint64(atomic.LoadInt64(&currentHeight))
}

// GetLastAnchorTime returns the last anchor time as Unix timestamp
func GetLastAnchorTime() int64 {
	return atomic.LoadInt64(&lastAnchorTime)
}

// GetFollowerHeight returns the follower height
func GetFollowerHeight() uint64 {
	return uint64(atomic.LoadInt64(&followerHeight))
}

// Handler returns the HTTP handler for /debug/vars
func Handler() http.Handler {
	return expvar.Handler()
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	BlocksProduced   int64     `json:"blocks_produced"`
	TxsExecuted      int64     `json:"txs_executed"`
	L0Submitted      int64     `json:"l0_submitted"`
	L0Failed         int64     `json:"l0_failed"`
	AnchorsWritten   int64     `json:"anchors_written"`
	WasmGasUsedTotal int64     `json:"wasm_gas_used_total"`
	CurrentHeight    uint64    `json:"current_height"`
	LastAnchorTime   int64     `json:"last_anchor_time_unix"`
	FollowerHeight   uint64    `json:"follower_height"`
	IndexerEntries   uint64    `json:"indexer_entries_processed"`
	RPCRequestCount  int64     `json:"rpc_request_count"`
	RPCErrorCount    int64     `json:"rpc_error_count"`
	NodeMode         string    `json:"node_mode"`
	StartTime        string    `json:"start_time"`
	SnapshotTime     time.Time `json:"snapshot_time"`
}

// GetSnapshot returns a snapshot of current metrics
func GetSnapshot() *MetricsSnapshot {
	return &MetricsSnapshot{
		BlocksProduced:   BlocksProduced.Value(),
		TxsExecuted:      TxsExecuted.Value(),
		L0Submitted:      L0Submitted.Value(),
		L0Failed:         L0Failed.Value(),
		AnchorsWritten:   AnchorsWritten.Value(),
		WasmGasUsedTotal: WasmGasUsedTotal.Value(),
		CurrentHeight:    GetCurrentHeight(),
		LastAnchorTime:   GetLastAnchorTime(),
		FollowerHeight:   GetFollowerHeight(),
		IndexerEntries:   GetIndexerEntries(),
		RPCRequestCount:  atomic.LoadInt64(&rpcRequestCount),
		RPCErrorCount:    atomic.LoadInt64(&rpcErrorCount),
		NodeMode:         NodeMode.Value(),
		StartTime:        StartTime.Value(),
		SnapshotTime:     time.Now().UTC(),
	}
}

// GetIndexerEntries returns the indexer entries count
func GetIndexerEntries() uint64 {
	return uint64(atomic.LoadInt64(&indexerEntries))
}

// RecordSequencerStats records sequencer statistics
func RecordSequencerStats(blockHeight, txsProcessed uint64, lastAnchorHeight uint64) {
	SetCurrentHeight(blockHeight)

	// Estimate last anchor time if we have anchor height
	if lastAnchorHeight > 0 {
		// Simple estimation - in practice would be stored properly
		estimatedTime := time.Now().Add(-time.Duration(blockHeight-lastAnchorHeight) * 5 * time.Second)
		SetLastAnchorTime(estimatedTime)
	}
}

// RecordFollowerStats records follower statistics
func RecordFollowerStats(height, entriesProcessed uint64) {
	SetFollowerHeight(height)
	SetIndexerEntries(entriesProcessed)
}

// RecordBlockProduction records a new block being produced
func RecordBlockProduction(txCount int, gasUsed uint64) {
	IncrementBlocksProduced()
	IncrementTxsExecuted(int64(txCount))
	AddWasmGasUsed(gasUsed)
}

// RecordL0Submission records an L0 submission attempt
func RecordL0Submission(success bool) {
	if success {
		IncrementL0Submitted()
	} else {
		IncrementL0Failed()
	}
}

// RecordAnchorWrite records an anchor being written
func RecordAnchorWrite() {
	IncrementAnchorsWritten()
}

// Reset resets all metrics (useful for testing)
func Reset() {
	// Reset counters
	BlocksProduced.Set(0)
	TxsExecuted.Set(0)
	L0Submitted.Set(0)
	L0Failed.Set(0)
	AnchorsWritten.Set(0)
	WasmGasUsedTotal.Set(0)

	// Reset gauges
	atomic.StoreInt64(&currentHeight, 0)
	atomic.StoreInt64(&lastAnchorTime, 0)
	atomic.StoreInt64(&followerHeight, 0)
	atomic.StoreInt64(&indexerEntries, 0)
	atomic.StoreInt64(&rpcRequestCount, 0)
	atomic.StoreInt64(&rpcErrorCount, 0)

	CurrentHeight.Set(0)
	LastAnchorTime.Set(0)
	FollowerHeight.Set(0)
	IndexerEntries.Set(0)
	RPCRequestCount.Set(0)
	RPCErrorCount.Set(0)

	// Reset start time
	StartTime.Set(time.Now().UTC().Format(time.RFC3339))
}

// Summary provides a human-readable summary of key metrics
func Summary() map[string]interface{} {
	return map[string]interface{}{
		"node_mode":       NodeMode.Value(),
		"blocks_produced": BlocksProduced.Value(),
		"txs_executed":    TxsExecuted.Value(),
		"current_height":  GetCurrentHeight(),
		"l0_submissions": map[string]interface{}{
			"success": L0Submitted.Value(),
			"failed":  L0Failed.Value(),
			"rate":    calculateSuccessRate(),
		},
		"indexer_entries": GetIndexerEntries(),
		"rpc": map[string]interface{}{
			"requests": atomic.LoadInt64(&rpcRequestCount),
			"errors":   atomic.LoadInt64(&rpcErrorCount),
		},
		"uptime_seconds": time.Since(parseStartTime()).Seconds(),
	}
}

// calculateSuccessRate calculates L0 submission success rate
func calculateSuccessRate() float64 {
	success := L0Submitted.Value()
	failed := L0Failed.Value()
	total := success + failed

	if total == 0 {
		return 0.0
	}

	return float64(success) / float64(total)
}

// parseStartTime parses the start time string back to time.Time
func parseStartTime() time.Time {
	startTimeStr := StartTime.Value()
	if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
		return t
	}
	return time.Now() // Fallback
}
