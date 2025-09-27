package l0

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/logz"
)

// Source represents the method for discovering L0 endpoints
type Source string

const (
	SourceProxy  Source = "proxy"
	SourceStatic Source = "static"
)

// EndpointHealth tracks the health status of an endpoint
type EndpointHealth struct {
	URL          string
	Healthy      bool
	LastCheck    time.Time
	FailureCount int
	NextRetry    time.Time
	ResponseTime time.Duration
}

// RoundRobin manages round-robin selection of healthy endpoints with health checking
type RoundRobin struct {
	mu        sync.RWMutex
	endpoints []*EndpointHealth
	current   int
	logger    *logz.Logger

	// Configuration
	healthCheckInterval time.Duration
	healthCheckTimeout  time.Duration
	maxFailures         int
	backoffDuration     time.Duration

	// Proxy discovery
	source     Source
	proxyURL   string
	staticURLs []string
	wsPath     string

	// Background tasks
	ctx    context.Context
	cancel context.CancelFunc
}

// Config holds configuration for the RoundRobin endpoint manager
type Config struct {
	Source              Source
	ProxyURL            string
	StaticURLs          []string
	WSPath              string
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
	MaxFailures         int
	BackoffDuration     time.Duration
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Source:              SourceStatic,
		WSPath:              "/ws",
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		MaxFailures:         3,
		BackoffDuration:     5 * time.Minute,
	}
}

// NewRoundRobin creates a new round-robin endpoint manager
func NewRoundRobin(cfg *Config) *RoundRobin {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	rr := &RoundRobin{
		logger:              logz.New(logz.INFO, "l0-endpoints"),
		healthCheckInterval: cfg.HealthCheckInterval,
		healthCheckTimeout:  cfg.HealthCheckTimeout,
		maxFailures:         cfg.MaxFailures,
		backoffDuration:     cfg.BackoffDuration,
		source:              cfg.Source,
		proxyURL:            cfg.ProxyURL,
		staticURLs:          cfg.StaticURLs,
		wsPath:              cfg.WSPath,
		ctx:                 ctx,
		cancel:              cancel,
	}

	return rr
}

// Start initializes the endpoint manager and starts background tasks
func (rr *RoundRobin) Start() error {
	rr.logger.Info("Starting L0 endpoint manager with source: %s", rr.source)

	// Initial endpoint discovery
	if err := rr.discover(); err != nil {
		return fmt.Errorf("initial endpoint discovery failed: %w", err)
	}

	// Start background health checks
	go rr.healthCheckLoop()

	// Start periodic re-discovery for proxy mode
	if rr.source == SourceProxy {
		go rr.discoveryLoop()
	}

	return nil
}

// Stop shuts down the endpoint manager
func (rr *RoundRobin) Stop() {
	rr.logger.Info("Stopping L0 endpoint manager")
	rr.cancel()
}

// Next returns the next healthy endpoint in round-robin fashion
func (rr *RoundRobin) Next() (string, error) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	if len(rr.endpoints) == 0 {
		return "", fmt.Errorf("no endpoints available")
	}

	// Find the next healthy endpoint
	attempts := 0
	for attempts < len(rr.endpoints) {
		endpoint := rr.endpoints[rr.current]
		rr.current = (rr.current + 1) % len(rr.endpoints)

		if rr.isEndpointHealthy(endpoint) {
			return endpoint.URL, nil
		}

		attempts++
	}

	// No healthy endpoints found, return the "least unhealthy" one
	var bestEndpoint *EndpointHealth
	for _, endpoint := range rr.endpoints {
		if bestEndpoint == nil || endpoint.FailureCount < bestEndpoint.FailureCount {
			bestEndpoint = endpoint
		}
	}

	if bestEndpoint != nil {
		rr.logger.Info("Warning: No healthy endpoints, using best available: %s", bestEndpoint.URL)
		return bestEndpoint.URL, nil
	}

	return "", fmt.Errorf("no usable endpoints available")
}

// NextWebSocket returns the next healthy WebSocket endpoint
func (rr *RoundRobin) NextWebSocket() (string, error) {
	httpURL, err := rr.Next()
	if err != nil {
		return "", err
	}

	// Convert HTTP(S) URL to WebSocket URL
	wsURL, err := rr.httpToWebSocket(httpURL)
	if err != nil {
		return "", fmt.Errorf("failed to convert to WebSocket URL: %w", err)
	}

	return wsURL, nil
}

// ReportFailure marks an endpoint as failed
func (rr *RoundRobin) ReportFailure(endpointURL string) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	for _, endpoint := range rr.endpoints {
		if endpoint.URL == endpointURL {
			endpoint.FailureCount++
			endpoint.Healthy = false
			endpoint.NextRetry = time.Now().Add(rr.backoffDuration)
			rr.logger.Info("Endpoint %s marked as failed (failures: %d)", endpointURL, endpoint.FailureCount)
			break
		}
	}
}

// GetEndpoints returns all current endpoints and their health status
func (rr *RoundRobin) GetEndpoints() []*EndpointHealth {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make([]*EndpointHealth, len(rr.endpoints))
	for i, ep := range rr.endpoints {
		result[i] = &EndpointHealth{
			URL:          ep.URL,
			Healthy:      ep.Healthy,
			LastCheck:    ep.LastCheck,
			FailureCount: ep.FailureCount,
			NextRetry:    ep.NextRetry,
			ResponseTime: ep.ResponseTime,
		}
	}

	return result
}

// discover finds endpoints based on the configured source
func (rr *RoundRobin) discover() error {
	var urls []string
	var err error

	switch rr.source {
	case SourceProxy:
		urls, err = FromProxy(rr.proxyURL)
		if err != nil {
			return fmt.Errorf("proxy discovery failed: %w", err)
		}
		rr.logger.Info("Discovered %d endpoints from proxy %s", len(urls), rr.proxyURL)
	case SourceStatic:
		urls = FromStatic(rr.staticURLs)
		rr.logger.Info("Using %d static endpoints", len(urls))
	default:
		return fmt.Errorf("unsupported source: %s", rr.source)
	}

	// Update endpoints
	rr.mu.Lock()
	defer rr.mu.Unlock()

	// Create new endpoint health trackers
	newEndpoints := make([]*EndpointHealth, len(urls))
	for i, url := range urls {
		// Check if we already have health info for this endpoint
		var existing *EndpointHealth
		for _, ep := range rr.endpoints {
			if ep.URL == url {
				existing = ep
				break
			}
		}

		if existing != nil {
			newEndpoints[i] = existing
		} else {
			newEndpoints[i] = &EndpointHealth{
				URL:       url,
				Healthy:   true, // Assume healthy initially
				LastCheck: time.Time{},
			}
		}
	}

	rr.endpoints = newEndpoints
	return nil
}

// healthCheckLoop periodically checks the health of all endpoints
func (rr *RoundRobin) healthCheckLoop() {
	ticker := time.NewTicker(rr.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rr.ctx.Done():
			return
		case <-ticker.C:
			rr.checkAllEndpoints()
		}
	}
}

// discoveryLoop periodically re-discovers endpoints when using proxy mode
func (rr *RoundRobin) discoveryLoop() {
	// Re-discover every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rr.ctx.Done():
			return
		case <-ticker.C:
			if err := rr.discover(); err != nil {
				rr.logger.Info("Endpoint re-discovery failed: %v", err)
			}
		}
	}
}

// checkAllEndpoints performs health checks on all endpoints
func (rr *RoundRobin) checkAllEndpoints() {
	rr.mu.Lock()
	endpoints := make([]*EndpointHealth, len(rr.endpoints))
	copy(endpoints, rr.endpoints)
	rr.mu.Unlock()

	for _, endpoint := range endpoints {
		// Skip if we're in backoff period
		if time.Now().Before(endpoint.NextRetry) {
			continue
		}

		healthy, responseTime := rr.checkEndpointHealth(endpoint.URL)

		rr.mu.Lock()
		endpoint.LastCheck = time.Now()
		endpoint.ResponseTime = responseTime

		if healthy {
			endpoint.Healthy = true
			endpoint.FailureCount = 0
			endpoint.NextRetry = time.Time{}
		} else {
			endpoint.Healthy = false
			endpoint.FailureCount++
			if endpoint.FailureCount >= rr.maxFailures {
				endpoint.NextRetry = time.Now().Add(rr.backoffDuration)
			}
		}
		rr.mu.Unlock()
	}
}

// checkEndpointHealth performs a health check on a single endpoint
func (rr *RoundRobin) checkEndpointHealth(endpointURL string) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(rr.ctx, rr.healthCheckTimeout)
	defer cancel()

	start := time.Now()

	// Simple health check - try to connect to the endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", endpointURL+"/health", nil)
	if err != nil {
		return false, time.Since(start)
	}

	client := &http.Client{Timeout: rr.healthCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false, time.Since(start)
	}
	defer resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	return healthy, time.Since(start)
}

// isEndpointHealthy checks if an endpoint is currently considered healthy
func (rr *RoundRobin) isEndpointHealthy(endpoint *EndpointHealth) bool {
	if !endpoint.Healthy {
		// Check if it's time to retry
		return time.Now().After(endpoint.NextRetry)
	}
	return true
}

// httpToWebSocket converts an HTTP(S) URL to a WebSocket URL
func (rr *RoundRobin) httpToWebSocket(httpURL string) (string, error) {
	parsedURL, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}

	// Convert scheme
	switch parsedURL.Scheme {
	case "http":
		parsedURL.Scheme = "ws"
	case "https":
		parsedURL.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported scheme: %s", parsedURL.Scheme)
	}

	// Add WebSocket path
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, "/") + rr.wsPath

	return parsedURL.String(), nil
}

// FromProxy discovers API v3 endpoints from a proxy/registry service
func FromProxy(proxyURL string) ([]string, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy URL cannot be empty")
	}

	// TODO: Implement actual proxy discovery using pkg/proxy
	// This is a placeholder implementation

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(proxyURL + "/api/v3/endpoints")
	if err != nil {
		return nil, fmt.Errorf("failed to query proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	// TODO: Parse actual response format from pkg/proxy
	// For now, return a mock response
	endpoints := []string{
		"https://mainnet.accumulatenetwork.io/v3",
		"https://testnet.accumulatenetwork.io/v3",
	}

	return endpoints, nil
}

// FromStatic returns the static list of endpoints
func FromStatic(endpoints []string) []string {
	// Validate and clean up endpoints
	var result []string
	for _, endpoint := range endpoints {
		if endpoint != "" {
			// Remove trailing slash for consistency
			endpoint = strings.TrimSuffix(endpoint, "/")
			result = append(result, endpoint)
		}
	}
	return result
}
