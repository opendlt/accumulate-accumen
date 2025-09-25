package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/config"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/sequencer"

	"gitlab.com/accumulatenetwork/accumulate/pkg/client/v3"
)

var (
	role       = flag.String("role", "sequencer", "Node role: sequencer or follower")
	configPath = flag.String("config", "", "Path to configuration file")
	logLevel   = flag.String("log-level", "info", "Log level: debug, info, warn, error")
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

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logz.Fatal("Failed to load config: %v", err)
	}

	logz.Info("Starting Accumen node in %s mode with config: %s", *role, *configPath)
	logz.Debug("Configuration: %s", cfg.String())

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

	// Run the node based on role
	switch *role {
	case "sequencer":
		if err := runSequencer(ctx, cfg, apiClient); err != nil {
			logz.Fatal("Sequencer failed: %v", err)
		}
	case "follower":
		if err := runFollower(ctx, cfg, apiClient); err != nil {
			logz.Fatal("Follower failed: %v", err)
		}
	default:
		logz.Fatal("Unknown role: %s", *role)
	}

	logz.Info("Accumen node shutdown complete")
}

// runSequencer runs the node in sequencer mode
func runSequencer(ctx context.Context, cfg *config.Config, apiClient *v3.Client) error {
	logger := logz.New(logz.INFO, "sequencer")
	logger.Info("Initializing Accumen sequencer...")

	// TODO: Create sequencer with new configuration
	// For now, create a placeholder sequencer config
	seqConfig := &sequencer.Config{
		ListenAddr:      "127.0.0.1:8080",
		BlockTime:       cfg.GetBlockTimeDuration(),
		MaxTransactions: 100, // Default value
		// Bridge configuration will be updated later
		Bridge: sequencer.BridgeConfig{
			EnableBridge: false, // Disabled for now
		},
	}

	// Create and start sequencer
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

	// Wait for shutdown signal
	<-ctx.Done()

	logger.Info("Shutting down sequencer...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

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
func runFollower(ctx context.Context, cfg *config.Config, apiClient *v3.Client) error {
	logger := logz.New(logz.INFO, "follower")
	logger.Info("Initializing Accumen follower...")

	// TODO: Implement follower mode
	// For now, just run a placeholder that syncs with the sequencer
	logger.Info("Follower started successfully, will sync with API endpoints: %v", cfg.APIV3Endpoints)
	logger.Info("Block time: %v, Anchor every: %v (%d blocks)",
		cfg.GetBlockTimeDuration(), cfg.GetAnchorIntervalDuration(), cfg.AnchorEveryN)

	// Placeholder follower loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Follower shutdown requested")
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