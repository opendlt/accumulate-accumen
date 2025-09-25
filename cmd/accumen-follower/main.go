package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/follower/indexer"
	"github.com/opendlt/accumulate-accumen/internal/config"
	"github.com/opendlt/accumulate-accumen/internal/health"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/internal/metrics"
	"github.com/opendlt/accumulate-accumen/internal/rpc"

	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
)

var (
	configPath  = flag.String("config", "", "Path to configuration file")
	rpcAddr     = flag.String("rpc", ":8667", "RPC server address (default :8667)")
	logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	metricsAddr = flag.String("metrics", ":8668", "Metrics server address (default :8668)")
)

func main() {
	flag.Parse()

	// Setup logging
	level, err := logz.ParseLevel(*logLevel)
	if err != nil {
		fmt.Printf("Invalid log level %s: %v\n", *logLevel, err)
		os.Exit(1)
	}
	logz.SetDefaultLevel(level)
	logz.SetDefaultPrefix("accumen-follower")

	if *configPath == "" {
		logz.Fatal("--config flag is required")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logz.Fatal("Failed to load config: %v", err)
	}

	// Initialize metrics
	metrics.SetNodeMode("follower")
	logz.Info("Metrics initialized for follower mode")

	logz.Info("Starting Accumen follower with config: %s", *configPath)
	logz.Debug("Configuration: %s", cfg.String())

	// Start metrics server
	go startMetricsServer(*metricsAddr)

	// Create API v3 client
	apiClient, err := v3.New(cfg.APIV3Endpoints[0]) // Use first endpoint for now
	if err != nil {
		logz.Fatal("Failed to create API v3 client: %v", err)
	}
	logz.Info("Connected to Accumulate API v3 endpoint: %s", cfg.APIV3Endpoints[0])

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Handle shutdown signals
	go func() {
		sig := <-sigChan
		logz.Info("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// Run the follower
	if err := runFollower(ctx, cfg, apiClient); err != nil {
		logz.Fatal("Follower failed: %v", err)
	}

	logz.Info("Accumen follower shutdown complete")
}

// runFollower runs the follower node
func runFollower(ctx context.Context, cfg *config.Config, apiClient *v3.Client) error {
	logger := logz.New(logz.INFO, "follower")
	logger.Info("Initializing Accumen follower...")

	// Create KV store for state reconstruction
	var kvStore state.KVStore
	var err error

	if cfg.Storage.Backend == "badger" {
		// Ensure storage directory exists
		if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
			return fmt.Errorf("failed to create storage directory %s: %w", cfg.Storage.Path, err)
		}

		kvStore, err = state.NewBadgerStore(cfg.Storage.Path)
		if err != nil {
			return fmt.Errorf("failed to create Badger store: %w", err)
		}
		logger.Info("Using Badger storage at: %s", cfg.Storage.Path)

		// Ensure proper cleanup on shutdown
		defer func() {
			if badgerStore, ok := kvStore.(*state.BadgerStore); ok {
				if closeErr := badgerStore.Close(); closeErr != nil {
					logger.Info("Warning: Failed to close Badger store: %v", closeErr)
				}
			}
		}()
	} else {
		kvStore = state.NewMemoryKVStore()
		logger.Info("Using in-memory storage")
	}

	// Create indexer for DN metadata scanning
	indexerConfig := &indexer.Config{
		APIClient:     apiClient,
		KVStore:       kvStore,
		MetadataPath:  cfg.DNPaths.TxMeta,
		AnchorsPath:   cfg.DNPaths.Anchors,
		ScanInterval:  30 * time.Second,
		BatchSize:     100,
		StartFromKey:  "", // Start from beginning
		MaxRetries:    3,
		RetryDelay:    time.Second,
	}

	idx, err := indexer.NewIndexer(indexerConfig)
	if err != nil {
		return fmt.Errorf("failed to create indexer: %w", err)
	}

	// Start indexer
	if err := idx.Start(ctx); err != nil {
		return fmt.Errorf("failed to start indexer: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := idx.Stop(shutdownCtx); err != nil {
			logger.Info("Warning: Failed to stop indexer: %v", err)
		}
	}()

	logger.Info("Indexer started, scanning metadata path: %s", cfg.DNPaths.TxMeta)

	// Start periodic metrics recording
	go recordFollowerMetrics(ctx, idx, logger)

	// Start RPC server in read-only mode
	var rpcServer *rpc.Server
	if *rpcAddr != "" {
		// Create read-only RPC server (no sequencer, no submission endpoints)
		rpcServer = rpc.NewReadOnlyServer(&rpc.ReadOnlyDependencies{
			KVStore: kvStore,
			Indexer: idx,
		})

		if err := rpcServer.Start(*rpcAddr); err != nil {
			logger.Info("Warning: Failed to start RPC server: %v", err)
		} else {
			logger.Info("Read-only RPC server started on %s", *rpcAddr)
		}

		defer func() {
			if rpcServer != nil {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer shutdownCancel()
				if err := rpcServer.Stop(shutdownCtx); err != nil {
					logger.Info("Warning: Failed to stop RPC server: %v", err)
				} else {
					logger.Info("RPC server stopped")
				}
			}
		}()
	}

	// Log configuration and status
	logger.Info("Follower started successfully")
	logger.Info("API endpoints: %v", cfg.APIV3Endpoints)
	logger.Info("Storage: backend=%s, path=%s", cfg.Storage.Backend, cfg.Storage.Path)
	logger.Info("DN paths - Anchors: %s, TxMeta: %s", cfg.DNPaths.Anchors, cfg.DNPaths.TxMeta)
	logger.Info("RPC server: %s", *rpcAddr)

	// Main follower loop - monitor and report status
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Follower shutdown requested")
			return nil

		case <-ticker.C:
			// Log periodic status
			stats := idx.GetStats()
			logger.Info("Follower status: entries=%d, height=%d, errors=%d, last_scan=%v",
				stats.EntriesProcessed, stats.CurrentHeight, stats.Errors, stats.LastScan)

			// Log RPC server status if available
			if rpcServer != nil {
				rpcStats := rpcServer.GetStats()
				logger.Debug("RPC stats: requests=%d, errors=%d, uptime=%v",
					rpcStats.RequestCount, rpcStats.ErrorCount, rpcStats.Uptime)
			}
		}
	}
}

// FollowerStats contains statistics about the follower
type FollowerStats struct {
	Running       bool                `json:"running"`
	IndexerStats  *indexer.Stats      `json:"indexer_stats"`
	RPCStats      *rpc.ServerStats    `json:"rpc_stats,omitempty"`
	Uptime        time.Duration       `json:"uptime"`
}

// GetFollowerStats returns comprehensive follower statistics
func GetFollowerStats(idx *indexer.Indexer, rpcServer *rpc.Server, startTime time.Time) *FollowerStats {
	stats := &FollowerStats{
		Running:      true,
		IndexerStats: idx.GetStats(),
		Uptime:       time.Since(startTime),
	}

	if rpcServer != nil {
		stats.RPCStats = rpcServer.GetStats()
	}

	return stats
}

// HealthCheck performs a health check on the follower
func HealthCheck(idx *indexer.Indexer, kvStore state.KVStore) error {
	// Check indexer health
	stats := idx.GetStats()
	if stats.Errors > 0 && stats.Errors > stats.EntriesProcessed/10 {
		return fmt.Errorf("indexer has high error rate: %d errors out of %d entries", stats.Errors, stats.EntriesProcessed)
	}

	// Check if indexer is making progress
	if time.Since(stats.LastScan) > 5*time.Minute {
		return fmt.Errorf("indexer hasn't scanned in %v", time.Since(stats.LastScan))
	}

	// Check KV store accessibility
	testKey := []byte("health_check")
	if err := kvStore.Set(testKey, []byte("ok")); err != nil {
		return fmt.Errorf("KV store write failed: %w", err)
	}

	value, err := kvStore.Get(testKey)
	if err != nil {
		return fmt.Errorf("KV store read failed: %w", err)
	}

	if string(value) != "ok" {
		return fmt.Errorf("KV store data integrity check failed")
	}

	// Clean up test key
	kvStore.Delete(testKey)

	return nil
}

// startMetricsServer starts the metrics and health check server
func startMetricsServer(addr string) {
	mux := http.NewServeMux()

	// Mount observability endpoints
	mux.Handle("/debug/vars", metrics.Handler())
	mux.HandleFunc("/healthz", health.SimpleHandler())
	mux.HandleFunc("/health/ready", health.ReadinessHandler())
	mux.HandleFunc("/health/live", health.LivenessHandler())
	mux.HandleFunc("/health/detailed", health.DetailedStatusHandler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logz.Info("Starting follower metrics server on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logz.Info("Follower metrics server error: %v", err)
	}
}

// recordFollowerMetrics periodically records follower metrics
func recordFollowerMetrics(ctx context.Context, idx *indexer.Indexer, logger *logz.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := idx.GetStats()
			if stats.Running {
				// Record follower metrics
				metrics.RecordFollowerStats(stats.CurrentHeight, stats.EntriesProcessed)

				// Update individual metrics
				metrics.SetFollowerHeight(stats.CurrentHeight)
				metrics.SetIndexerEntries(stats.EntriesProcessed)
			}
		}
	}
}