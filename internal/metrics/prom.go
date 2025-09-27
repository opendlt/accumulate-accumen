//go:build prom

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

var (
	// Counter metrics
	promBlocksProduced = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_blocks_produced_total",
		Help: "Total number of blocks produced by the sequencer",
	})

	promTxsExecuted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_transactions_executed_total",
		Help: "Total number of transactions executed",
	})

	promL0Submitted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_l0_submitted_total",
		Help: "Total number of successful L0 submissions to Accumulate network",
	})

	promL0Failed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_l0_failed_total",
		Help: "Total number of failed L0 submissions",
	})

	promAnchorsWritten = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_anchors_written_total",
		Help: "Total number of anchors written to DN",
	})

	promWasmGasUsed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_wasm_gas_used_total",
		Help: "Total WASM gas consumed by contract execution",
	})

	promIndexerEntriesProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "accumen_indexer_entries_processed_total",
		Help: "Total number of entries processed by follower indexer",
	})

	// Gauge metrics
	promCurrentHeight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "accumen_current_height",
		Help: "Current block height",
	})

	promFollowerHeight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "accumen_follower_height",
		Help: "Follower sync height",
	})

	promMempoolSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "accumen_mempool_size",
		Help: "Current number of transactions in mempool",
	})

	promMempoolAccounts = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "accumen_mempool_accounts",
		Help: "Number of unique accounts with transactions in mempool",
	})
)

func init() {
	// Register all metrics with the default registry
	prometheus.MustRegister(
		promBlocksProduced,
		promTxsExecuted,
		promL0Submitted,
		promL0Failed,
		promAnchorsWritten,
		promWasmGasUsed,
		promIndexerEntriesProcessed,
		promCurrentHeight,
		promFollowerHeight,
		promMempoolSize,
		promMempoolAccounts,
	)
}

// updatePrometheusMetrics synchronizes expvar metrics with Prometheus metrics
func updatePrometheusMetrics() {
	// Update counters
	promBlocksProduced.Add(float64(blocksProduced.Value()) - getPromCounterValue(promBlocksProduced))
	promTxsExecuted.Add(float64(txsExecuted.Value()) - getPromCounterValue(promTxsExecuted))
	promL0Submitted.Add(float64(l0Submitted.Value()) - getPromCounterValue(promL0Submitted))
	promL0Failed.Add(float64(l0Failed.Value()) - getPromCounterValue(promL0Failed))
	promAnchorsWritten.Add(float64(anchorsWritten.Value()) - getPromCounterValue(promAnchorsWritten))
	promWasmGasUsed.Add(float64(wasmGasUsedTotal.Value()) - getPromCounterValue(promWasmGasUsed))
	promIndexerEntriesProcessed.Add(float64(indexerEntriesProcessed.Value()) - getPromCounterValue(promIndexerEntriesProcessed))

	// Update gauges
	promCurrentHeight.Set(float64(currentHeight.Value()))
	promFollowerHeight.Set(float64(followerHeight.Value()))
	promMempoolSize.Set(float64(mempoolSize.Value()))
	promMempoolAccounts.Set(float64(mempoolAccounts.Value()))
}

// getPromCounterValue gets the current value of a Prometheus counter
func getPromCounterValue(counter prometheus.Counter) float64 {
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		return 0
	}
	return metric.GetCounter().GetValue()
}

// PrometheusHandler returns an HTTP handler for Prometheus metrics
func PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Update Prometheus metrics from expvar before serving
		updatePrometheusMetrics()

		// Serve Prometheus metrics
		promhttp.Handler().ServeHTTP(w, r)
	})
}
