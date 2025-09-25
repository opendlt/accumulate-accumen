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
	"github.com/opendlt/accumulate-accumen/internal/rpc"
	"github.com/opendlt/accumulate-accumen/internal/vdkshim"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/l1"

	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
)

var (
	configPath  = flag.String("config", "", "Path to configuration file")
	logLevel    = flag.String("log-level", "info", "Log level: debug, info, warn, error")
	rpcAddr     = flag.String("rpc", ":8666", "RPC server address (default :8666)")
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
	logz.SetDefaultPrefix("accumen-vdk")

	if *configPath == "" {
		logz.Fatal("--config flag is required")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logz.Fatal("Failed to load config: %v", err)
	}

	// Initialize metrics
	metrics.SetNodeMode("sequencer")
	logz.Info("Metrics initialized for sequencer mode with VDK")

	logz.Info("Starting Accumen VDK runner with config: %s", *configPath)
	logz.Debug("Configuration: %s", cfg.String())

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
	logz.Info("VDK L0 endpoint manager started with source: %s", cfg.L0.Source)

	// Get initial endpoint for client setup
	initialEndpoint, err := endpointManager.Next()
	if err != nil {
		logz.Fatal("Failed to get initial L0 endpoint: %v", err)
	}
	logz.Info("VDK using initial L0 endpoint: %s", initialEndpoint)

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
		logz.Info("Received signal: %v, shutting down VDK runner...", sig)
		cancel()
	}()

	// Create and configure sequencer
	seq, httpSrv, err := createSequencer(cfg, apiClient, l0Client, nodeSigner)
	if err != nil {
		logz.Fatal("Failed to create sequencer: %v", err)
	}

	// Start metrics server
	go startMetricsServer(*metricsAddr)

	// Run with VDK lifecycle management
	if err := vdkshim.RunWithVDK(ctx, seq, httpSrv); err != nil {
		logz.Fatal("VDK runner failed: %v", err)
	}

	logz.Info("Accumen VDK runner shutdown complete")
}

// createSequencer creates and configures the sequencer with all its dependencies
func createSequencer(cfg *config.Config, apiClient *v3.Client, l0Client *l0api.Client, nodeSigner signer.Signer) (*sequencer.Sequencer, *http.Server, error) {
	logger := logz.New(logz.INFO, "sequencer")
	logger.Info("Initializing Accumen sequencer for VDK...")

	// Create sequencer configuration
	seqConfig := &sequencer.Config{
		ListenAddr:      "127.0.0.1:8080",
		BlockTime:       cfg.GetBlockTimeDuration(),
		MaxTransactions: 100,
		Bridge: sequencer.BridgeConfig{
			EnableBridge: false, // Disabled for now
		},
	}

	// Create KV store based on configuration
	var kvStore state.KVStore
	if cfg.Storage.Backend == "badger" {
		// Ensure storage directory exists
		if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create storage directory %s: %w", cfg.Storage.Path, err)
		}

		var err error
		kvStore, err = state.NewBadgerStore(cfg.Storage.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create Badger store: %w", err)
		}
		logger.Info("Using Badger storage at: %s", cfg.Storage.Path)
	} else {
		kvStore = state.NewMemoryKVStore()
		logger.Info("Using in-memory storage")
	}

	// Create and start sequencer
	seq, err := sequencer.NewSequencer(seqConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sequencer: %w", err)
	}

	// Create contract store
	contractStore := contracts.NewStore(kvStore, cfg.Storage.Path)

	// Create persistent mempool using storage path + mempool subdirectory
	mempoolPath := filepath.Join(cfg.Storage.Path, "mempool")
	mempool, err := sequencer.NewPersistentMempool(mempoolPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create persistent mempool: %w", err)
	}
	// Note: mempool will be closed by VDK lifecycle management
	logger.Info("VDK persistent mempool initialized at: %s", mempoolPath)

	// Replay any leftover transactions from previous session
	err = mempool.Replay(func(tx l1.Tx) error {
		logger.Debug("VDK replaying transaction: %s", tx.String())
		// TODO: Forward to sequencer for processing
		// For now, just log the transaction
		return nil
	})
	if err != nil {
		logger.Info("VDK Warning: Failed to replay mempool transactions: %v", err)
	}

	// Create pricing schedule provider
	scheduleProvider := pricing.Cached(
		pricing.CreateDNProvider(apiClient, cfg.Pricing.GasScheduleID),
		cfg.GetPricingRefreshDuration(),
	)

	// Create RPC server
	var httpSrv *http.Server
	if *rpcAddr != "" {
		rpcServer := rpc.NewServer(&rpc.Dependencies{
			Sequencer:     seq,
			KVStore:       kvStore,
			ContractStore: contractStore,
			Mempool:       mempool,
		})

		mux := http.NewServeMux()
		mux.Handle("/", rpcServer)

		httpSrv = &http.Server{
			Addr:    *rpcAddr,
			Handler: mux,
		}

		logger.Info("RPC server configured on %s", *rpcAddr)
	}

	logger.Info("Sequencer initialized successfully for VDK")
	logger.Info("Block time: %v, Anchor every: %v (%d blocks)",
		cfg.GetBlockTimeDuration(), cfg.GetAnchorIntervalDuration(), cfg.AnchorEveryN)
	logger.Info("Storage: backend=%s, path=%s", cfg.Storage.Backend, cfg.Storage.Path)
	logger.Info("Signer: type=%s", cfg.Signer.Type)

	return seq, httpSrv, nil
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

	logz.Info("Starting VDK metrics server on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logz.Info("VDK metrics server error: %v", err)
	}
}