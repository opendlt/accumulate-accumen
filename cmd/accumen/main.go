package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/engine/state/contracts"
	"github.com/opendlt/accumulate-accumen/internal/config"
	"github.com/opendlt/accumulate-accumen/internal/crypto/signer"
	"github.com/opendlt/accumulate-accumen/internal/health"
	"github.com/opendlt/accumulate-accumen/internal/l0"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/internal/metrics"
	"github.com/opendlt/accumulate-accumen/internal/netprofiles"
	"github.com/opendlt/accumulate-accumen/internal/rpc"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/l1"

	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
)

var (
	role       = flag.String("role", "sequencer", "Node role: sequencer or follower")
	configPath = flag.String("config", "", "Path to configuration file")
	network    = flag.String("network", "", "Network profile: mainnet, testnet, devnet, or local (overrides config and env)")
	logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	rpcAddr    = flag.String("rpc", ":8666", "RPC server address (default :8666)")
	metricsAddr = flag.String("metrics", ":8667", "Metrics server address (default :8667)")
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
	logz.SetDefaultPrefix("accumen")

	if *configPath == "" {
		logz.Fatal("--config flag is required")
	}

	if *role != "sequencer" && *role != "follower" {
		logz.Fatal("--role must be either 'sequencer' or 'follower'")
	}

	// Load configuration with network profile support
	cfg, err := config.LoadWithNetwork(*configPath, *network)
	if err != nil {
		logz.Fatal("Failed to load config: %v", err)
	}

	// Initialize metrics
	metrics.SetNodeMode(*role)
	logz.Info("Metrics initialized for %s mode", *role)

	logz.Info("Starting Accumen node in %s mode with config: %s", *role, *configPath)
	logz.Info("Using network profile: %s", cfg.EffectiveNetwork())
	logz.Debug("Configuration: %s", cfg.String())

	// Start metrics server
	go startMetricsServer(*metricsAddr)

	// Apply network profile if L0 source is empty and network name is set
	if cfg.L0.Source == "" && cfg.Network.Name != "" {
		if profile, exists := netprofiles.GetProfile(cfg.Network.Name); exists {
			cfg.L0.Source = "static"
			cfg.L0.Static = profile.JSONRPC
			cfg.L0.WSPath = profile.WSPath
			logz.Info("Applied network profile '%s': endpoints=%v, wsPath=%s",
				cfg.Network.Name, profile.JSONRPC, profile.WSPath)
		} else {
			logz.Warn("Unknown network profile '%s', using default configuration", cfg.Network.Name)
		}
	}

	// Initialize L0 endpoint manager
	l0Config := &l0.Config{
		Source:              l0.Source(cfg.L0.Source),
		ProxyURL:            cfg.L0.Proxy,
		StaticURLs:          cfg.L0.Static,
		WSPath:              cfg.L0.WSPath,
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		MaxFailures:         3,
		BackoffDuration:     5 * time.Minute,
	}
	endpointManager := l0.NewRoundRobin(l0Config)
	if err := endpointManager.Start(); err != nil {
		logz.Fatal("Failed to start L0 endpoint manager: %v", err)
	}
	defer endpointManager.Stop()
	logz.Info("L0 endpoint manager started with source: %s", cfg.L0.Source)

	// Get initial endpoint for client setup
	initialEndpoint, err := endpointManager.Next()
	if err != nil {
		logz.Fatal("Failed to get initial L0 endpoint: %v", err)
	}
	logz.Info("Using initial L0 endpoint: %s", initialEndpoint)

	// Create signer from configuration
	signerConfig := &signer.SignerConfig{
		Type: cfg.Signer.Type,
		Key:  cfg.Signer.Key,
	}
	nodeSigner, err := signer.NewFromConfig(signerConfig, cfg.SequencerKey)
	if err != nil {
		logz.Fatal("Failed to create signer: %v", err)
	}
	logz.Info("Signer initialized: type=%s", cfg.Signer.Type)

	// Create L0 API client with signer using endpoint manager
	l0ClientConfig := l0api.DefaultClientConfig(initialEndpoint)
	l0Client, err := l0api.NewClientWithSigner(l0ClientConfig, nodeSigner)
	if err != nil {
		logz.Fatal("Failed to create L0 API client: %v", err)
	}
	logz.Info("Connected to Accumulate API v3 endpoint: %s", initialEndpoint)

	// Create API v3 client for compatibility using endpoint manager
	apiClient, err := v3.New(initialEndpoint)
	if err != nil {
		logz.Fatal("Failed to create API v3 client: %v", err)
	}

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

	// Run the node based on role
	switch *role {
	case "sequencer":
		if err := runSequencer(ctx, cfg, apiClient, l0Client, nodeSigner, endpointManager); err != nil {
			logz.Fatal("Sequencer failed: %v", err)
		}
	case "follower":
		if err := runFollower(ctx, cfg, apiClient, endpointManager); err != nil {
			logz.Fatal("Follower failed: %v", err)
		}
	default:
		logz.Fatal("Unknown role: %s", *role)
	}

	logz.Info("Accumen node shutdown complete")
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

	logz.Info("Starting metrics server on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logz.Info("Metrics server error: %v", err)
	}
}

// recordSequencerMetrics periodically records sequencer metrics
func recordSequencerMetrics(ctx context.Context, seq *sequencer.Sequencer, logger *logz.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if seq.IsRunning() {
				stats := seq.GetStats()
				lastAnchorHeight := seq.GetLastAnchorHeight()

				// Record sequencer metrics
				metrics.RecordSequencerStats(stats.BlockHeight, stats.TxsProcessed, lastAnchorHeight)

				// Update individual metrics
				metrics.SetCurrentHeight(stats.BlockHeight)
				if lastAnchorHeight > 0 {
					estimatedTime := time.Now().Add(-time.Duration(stats.BlockHeight-lastAnchorHeight) * 5 * time.Second)
					metrics.SetLastAnchorTime(estimatedTime)
				}
			}
		}
	}
}

// runSequencer runs the node in sequencer mode
func runSequencer(ctx context.Context, cfg *config.Config, apiClient *v3.Client, l0Client *l0api.Client, nodeSigner signer.Signer, endpointManager *l0.RoundRobin) error {
	logger := logz.New(logz.INFO, "sequencer")
	logger.Info("Initializing Accumen sequencer...")

	// TODO: Create sequencer with new configuration
	// For now, create a placeholder sequencer config
	seqConfig := &sequencer.Config{
		ListenAddr:      "127.0.0.1:8080",
		BlockTime:       cfg.GetBlockTimeDuration(),
		MaxTransactions: 100, // Default value
		// Bridge configuration with confirmation support
		Bridge: sequencer.BridgeConfig{
			EnableBridge: false, // Disabled for now
			// When enabled, DN writer would be configured with:
			// WaitForExecution: cfg.Confirm.WaitForExecution
			// ExecutionTimeout: cfg.GetConfirmTimeoutDuration()
		},
	}

	// Create KV store based on configuration first (needed for sequencer)
	var kvStore state.KVStore
	var err_kv error

	if cfg.Storage.Backend == "badger" {
		// Ensure storage directory exists
		if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
			return fmt.Errorf("failed to create storage directory %s: %w", cfg.Storage.Path, err)
		}

		kvStore, err_kv = state.NewBadgerStore(cfg.Storage.Path)
		if err_kv != nil {
			return fmt.Errorf("failed to create Badger store: %w", err_kv)
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

	// Create and start sequencer (pass KV store)
	seq, err := sequencer.NewSequencer(seqConfig)
	if err != nil {
		return fmt.Errorf("failed to create sequencer: %w", err)
	}

	if err := seq.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %w", err)
	}

	logger.Info("Sequencer started successfully")
	logger.Info("Block time: %v, Anchor every: %v (%d blocks)",
		cfg.GetBlockTimeDuration(), cfg.GetAnchorIntervalDuration(), cfg.AnchorEveryN)
	logger.Info("Gas schedule ID: %s", cfg.GasScheduleID)
	logger.Info("DN paths - Anchors: %s, TxMeta: %s", cfg.DNPaths.Anchors, cfg.DNPaths.TxMeta)
	logger.Info("Storage: backend=%s, path=%s", cfg.Storage.Backend, cfg.Storage.Path)
	logger.Info("Confirmation settings: waitForExecution=%v, timeout=%s", cfg.Confirm.WaitForExecution, cfg.Confirm.Timeout)
	logger.Info("Signer: type=%s", cfg.Signer.Type)

	// Start periodic metrics recording
	go recordSequencerMetrics(ctx, seq, logger)

	// TODO: When bridge is enabled, DN writer and submitter would be configured with confirmation support:
	// if seqConfig.Bridge.EnableBridge {
	//     dnWriterConfig := anchors.DefaultDNWriterConfig()
	//     dnWriterConfig.SequencerKey = cfg.SequencerKey
	//     dnWriterConfig.WaitForExecution = cfg.Confirm.WaitForExecution
	//     dnWriterConfig.ExecutionTimeout = cfg.GetConfirmTimeoutDuration()
	//     dnWriter, err := anchors.NewDNWriter(dnWriterConfig)
	//     if err != nil {
	//         return fmt.Errorf("failed to create DN writer: %w", err)
	//     }
	//     defer dnWriter.Close()
	// }

	// Create contract store
	contractStore := contracts.NewStore(kvStore, cfg.Storage.Path)

	// Create persistent mempool using storage path + mempool subdirectory
	mempoolPath := filepath.Join(cfg.Storage.Path, "mempool")
	mempool, err := sequencer.NewPersistentMempool(mempoolPath)
	if err != nil {
		return fmt.Errorf("failed to create persistent mempool: %w", err)
	}
	defer mempool.Close()
	logger.Info("Persistent mempool initialized at: %s", mempoolPath)

	// Replay any leftover transactions from previous session
	err = mempool.Replay(func(tx l1.Tx) error {
		logger.Debug("Replaying transaction: %s", tx.String())
		// TODO: Forward to sequencer for processing
		// For now, just log the transaction
		return nil
	})
	if err != nil {
		logger.Info("Warning: Failed to replay mempool transactions: %v", err)
	}

	// Create pricing schedule provider
	scheduleProvider := pricing.Cached(
		pricing.CreateDNProvider(apiClient, cfg.Pricing.GasScheduleID),
		cfg.GetPricingRefreshDuration(),
	)

	// Start background schedule refresh
	scheduleProvider.StartAutoRefresh(ctx)
	logger.Info("Gas schedule provider started: ID=%s, refresh=%v", cfg.Pricing.GasScheduleID, cfg.GetPricingRefreshDuration())

	// Start RPC server if address is provided
	var rpcServer *rpc.Server
	if *rpcAddr != "" {
		rpcServer = rpc.NewServer(&rpc.Dependencies{
			Sequencer:     seq,
			KVStore:       kvStore,
			ContractStore: contractStore,
			Mempool:       mempool,
		})

		if err := rpcServer.Start(*rpcAddr); err != nil {
			logger.Info("Warning: Failed to start RPC server: %v", err)
		} else {
			logger.Info("RPC server started on %s", *rpcAddr)
		}
	}

	// Wait for shutdown signal
	<-ctx.Done()

	logger.Info("Shutting down sequencer...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop schedule provider
	if scheduleProvider != nil {
		scheduleProvider.Stop()
		logger.Info("Gas schedule provider stopped")
	}

	// Stop RPC server first
	if rpcServer != nil {
		if err := rpcServer.Stop(shutdownCtx); err != nil {
			logger.Info("Warning: Failed to stop RPC server: %v", err)
		} else {
			logger.Info("RPC server stopped")
		}
	}

	// Stop sequencer
	if err := seq.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("failed to stop sequencer: %w", err)
	}

	// Print final stats
	stats := seq.GetStats()
	logger.Info("Final stats: blocks=%d, txs=%d, uptime=%v",
		stats.BlocksProduced, stats.TxsProcessed, stats.Uptime)

	return nil
}

// runFollower runs the node in follower mode
func runFollower(ctx context.Context, cfg *config.Config, apiClient *v3.Client, endpointManager *l0.RoundRobin) error {
	logger := logz.New(logz.INFO, "follower")
	logger.Info("Initializing Accumen follower...")

	// TODO: Implement follower mode
	// For now, just run a placeholder that syncs with the sequencer
	logger.Info("Follower started successfully, will sync with API endpoints: %v", cfg.APIV3Endpoints)
	logger.Info("Block time: %v, Anchor every: %v (%d blocks)",
		cfg.GetBlockTimeDuration(), cfg.GetAnchorIntervalDuration(), cfg.AnchorEveryN)

	// Start RPC server for followers too (read-only mode)
	var rpcServer *rpc.Server
	if *rpcAddr != "" {
		// Create a simple KV store for queries (in follower mode, this would sync from sequencer)
		kvStore := state.NewMemoryKVStore()

		// For followers, we don't have a sequencer, so we'll need to modify the RPC server
		// For now, we'll skip RPC server for followers
		logger.Info("RPC server disabled for follower mode (not yet implemented)")
	}

	// Placeholder follower loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Follower shutdown requested")

			// Stop RPC server if running
			if rpcServer != nil {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer shutdownCancel()
				rpcServer.Stop(shutdownCtx)
			}

			return nil
		case <-ticker.C:
			// TODO: Implement follower sync logic
			// - Query latest block height from API client
			// - Sync missing blocks
			// - Validate transactions
			logger.Debug("Follower syncing... (placeholder)")

			// Example API call to show client is working
			// In real implementation, this would query for blocks/transactions
			_ = apiClient // Use the client to prevent unused variable warning
		}
	}
}