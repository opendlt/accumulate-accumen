package health

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/opendlt/accumulate-accumen/follower/indexer"
	"github.com/opendlt/accumulate-accumen/internal/metrics"
	"github.com/opendlt/accumulate-accumen/sequencer"
)

// HealthStatus represents the health status response
type HealthStatus struct {
	OK               bool      `json:"ok"`
	Height           uint64    `json:"height"`
	LastAnchorTime   *string   `json:"last_anchor_time,omitempty"`
	Mode             string    `json:"mode"`
	Timestamp        time.Time `json:"timestamp"`
	Uptime           string    `json:"uptime"`
	IndexerEntries   uint64    `json:"indexer_entries,omitempty"`
	L0SubmissionRate *float64  `json:"l0_submission_rate,omitempty"`
}

// HealthChecker provides health check functionality
type HealthChecker struct {
	sequencer *sequencer.Sequencer
	indexer   *indexer.Indexer
	mode      string
	startTime time.Time
}

// NewHealthChecker creates a new health checker for sequencer mode
func NewHealthChecker(seq *sequencer.Sequencer) *HealthChecker {
	return &HealthChecker{
		sequencer: seq,
		mode:      "sequencer",
		startTime: time.Now(),
	}
}

// NewFollowerHealthChecker creates a new health checker for follower mode
func NewFollowerHealthChecker(idx *indexer.Indexer) *HealthChecker {
	return &HealthChecker{
		indexer:   idx,
		mode:      "follower",
		startTime: time.Now(),
	}
}

// Handler returns an HTTP handler for /healthz endpoint
func (hc *HealthChecker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := hc.GetStatus()

		w.Header().Set("Content-Type", "application/json")

		if status.OK {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(status)
	}
}

// GetStatus returns the current health status
func (hc *HealthChecker) GetStatus() *HealthStatus {
	status := &HealthStatus{
		Mode:      hc.mode,
		Timestamp: time.Now().UTC(),
		Uptime:    time.Since(hc.startTime).String(),
	}

	if hc.mode == "sequencer" && hc.sequencer != nil {
		hc.getSequencerStatus(status)
	} else if hc.mode == "follower" && hc.indexer != nil {
		hc.getFollowerStatus(status)
	} else {
		// Fallback to metrics-based status
		hc.getMetricsStatus(status)
	}

	return status
}

// getSequencerStatus populates status for sequencer mode
func (hc *HealthChecker) getSequencerStatus(status *HealthStatus) {
	if !hc.sequencer.IsRunning() {
		status.OK = false
		status.Height = 0
		return
	}

	status.OK = true

	// Get sequencer stats
	stats := hc.sequencer.GetStats()
	status.Height = stats.BlockHeight

	// Get last anchor time
	lastAnchorHeight := hc.sequencer.GetLastAnchorHeight()
	if lastAnchorHeight > 0 {
		// Estimate last anchor time
		estimatedTime := time.Now().Add(-time.Duration(stats.BlockHeight-lastAnchorHeight) * 5 * time.Second)
		timeStr := estimatedTime.UTC().Format(time.RFC3339)
		status.LastAnchorTime = &timeStr
	}

	// Calculate L0 submission rate
	l0Success := metrics.L0Submitted.Value()
	l0Failed := metrics.L0Failed.Value()
	l0Total := l0Success + l0Failed
	if l0Total > 0 {
		rate := float64(l0Success) / float64(l0Total)
		status.L0SubmissionRate = &rate
	}
}

// getFollowerStatus populates status for follower mode
func (hc *HealthChecker) getFollowerStatus(status *HealthStatus) {
	indexerStats := hc.indexer.GetStats()

	status.OK = indexerStats.Running
	status.Height = indexerStats.CurrentHeight
	status.IndexerEntries = indexerStats.EntriesProcessed

	// Check if indexer is healthy (recent scan activity)
	if time.Since(indexerStats.LastScan) > 10*time.Minute {
		status.OK = false
	}

	// Check error rate
	if indexerStats.Errors > 0 && indexerStats.EntriesProcessed > 0 {
		errorRate := float64(indexerStats.Errors) / float64(indexerStats.EntriesProcessed)
		if errorRate > 0.1 { // More than 10% error rate
			status.OK = false
		}
	}
}

// getMetricsStatus populates status from metrics (fallback)
func (hc *HealthChecker) getMetricsStatus(status *HealthStatus) {
	if hc.mode == "sequencer" {
		status.Height = metrics.GetCurrentHeight()
		status.OK = status.Height > 0

		// Get last anchor time from metrics
		if anchorTime := metrics.GetLastAnchorTime(); anchorTime > 0 {
			timeStr := time.Unix(anchorTime, 0).UTC().Format(time.RFC3339)
			status.LastAnchorTime = &timeStr
		}

		// L0 submission rate
		l0Success := metrics.L0Submitted.Value()
		l0Failed := metrics.L0Failed.Value()
		l0Total := l0Success + l0Failed
		if l0Total > 0 {
			rate := float64(l0Success) / float64(l0Total)
			status.L0SubmissionRate = &rate
		}
	} else {
		status.Height = metrics.GetFollowerHeight()
		status.IndexerEntries = metrics.GetIndexerEntries()
		status.OK = status.IndexerEntries > 0
	}
}

// SimpleHandler returns a minimal health handler that doesn't require dependencies
func SimpleHandler() http.HandlerFunc {
	startTime := time.Now()

	return func(w http.ResponseWriter, r *http.Request) {
		// Use metrics to determine health
		nodeMode := metrics.NodeMode.Value()

		var height uint64
		var ok bool
		var lastAnchorTime *string
		var indexerEntries uint64

		if nodeMode == "sequencer" {
			height = metrics.GetCurrentHeight()
			ok = height > 0

			if anchorTime := metrics.GetLastAnchorTime(); anchorTime > 0 {
				timeStr := time.Unix(anchorTime, 0).UTC().Format(time.RFC3339)
				lastAnchorTime = &timeStr
			}
		} else {
			height = metrics.GetFollowerHeight()
			indexerEntries = metrics.GetIndexerEntries()
			ok = indexerEntries > 0
		}

		status := &HealthStatus{
			OK:             ok,
			Height:         height,
			LastAnchorTime: lastAnchorTime,
			Mode:           nodeMode,
			Timestamp:      time.Now().UTC(),
			Uptime:         time.Since(startTime).String(),
			IndexerEntries: indexerEntries,
		}

		w.Header().Set("Content-Type", "application/json")

		if status.OK {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(status)
	}
}

// ReadinessHandler returns a readiness check handler
func ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeMode := metrics.NodeMode.Value()

		ready := false
		if nodeMode == "sequencer" {
			// Sequencer is ready if it's producing blocks
			ready = metrics.GetCurrentHeight() > 0
		} else {
			// Follower is ready if it's processing entries
			ready = metrics.GetIndexerEntries() > 0
		}

		response := map[string]interface{}{
			"ready":     ready,
			"mode":      nodeMode,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")

		if ready {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// LivenessHandler returns a liveness check handler
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Simple liveness check - if we can respond, we're alive
		response := map[string]interface{}{
			"alive":     true,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// DetailedStatusHandler returns a detailed status handler
func DetailedStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := metrics.GetSnapshot()

		response := map[string]interface{}{
			"status":       "healthy",
			"node_mode":    snapshot.NodeMode,
			"timestamp":    snapshot.SnapshotTime.UTC().Format(time.RFC3339),
			"uptime":       snapshot.SnapshotTime.Sub(parseStartTime(snapshot.StartTime)).String(),
			"metrics":      snapshot,
		}

		// Determine overall health
		healthy := true
		if snapshot.NodeMode == "sequencer" {
			healthy = snapshot.CurrentHeight > 0
		} else {
			healthy = snapshot.IndexerEntries > 0
		}

		if !healthy {
			response["status"] = "unhealthy"
		}

		w.Header().Set("Content-Type", "application/json")

		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// parseStartTime parses the start time string
func parseStartTime(startTimeStr string) time.Time {
	if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
		return t
	}
	return time.Now() // Fallback
}