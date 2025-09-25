package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opendlt/accumulate-accumen/sequencer"
)

var (
	role   = flag.String("role", "sequencer", "Node role: sequencer or follower")
	config = flag.String("config", "", "Path to configuration file")
)

func main() {
	flag.Parse()

	if *config == "" {
		log.Fatal("--config flag is required")
	}

	if *role != "sequencer" && *role != "follower" {
		log.Fatal("--role must be either 'sequencer' or 'follower'")
	}

	fmt.Printf("Starting Accumen node in %s mode with config: %s\n", *role, *config)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Handle shutdown signals
	go func() {
		sig := <-sigChan
		fmt.Printf("Received signal: %v, shutting down...\n", sig)
		cancel()
	}()

	// Run the node based on role
	switch *role {
	case "sequencer":
		if err := runSequencer(ctx, *config); err != nil {
			log.Fatalf("Sequencer failed: %v", err)
		}
	case "follower":
		if err := runFollower(ctx, *config); err != nil {
			log.Fatalf("Follower failed: %v", err)
		}
	default:
		log.Fatalf("Unknown role: %s", *role)
	}

	fmt.Println("Accumen node shutdown complete")
}

// runSequencer runs the node in sequencer mode
func runSequencer(ctx context.Context, configPath string) error {
	fmt.Println("Initializing Accumen sequencer...")

	// Load configuration
	config, err := sequencer.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create and start sequencer
	seq, err := sequencer.NewSequencer(config)
	if err != nil {
		return fmt.Errorf("failed to create sequencer: %w", err)
	}

	if err := seq.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %w", err)
	}

	fmt.Printf("Sequencer started successfully on %s\n", config.ListenAddr)
	fmt.Printf("Block time: %v, Max transactions: %d\n", config.BlockTime, config.MaxTransactions)
	fmt.Printf("Bridge enabled: %v\n", config.Bridge.EnableBridge)

	// Wait for shutdown signal
	<-ctx.Done()

	fmt.Println("Shutting down sequencer...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := seq.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("failed to stop sequencer: %w", err)
	}

	// Print final stats
	stats := seq.GetStats()
	fmt.Printf("Final stats: blocks=%d, txs=%d, uptime=%v\n",
		stats.BlocksProduced, stats.TxsProcessed, stats.Uptime)

	return nil
}

// runFollower runs the node in follower mode
func runFollower(ctx context.Context, configPath string) error {
	fmt.Println("Initializing Accumen follower...")

	// Load configuration (followers can use same config format)
	config, err := sequencer.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// TODO: Implement follower mode
	// For now, just run a placeholder that syncs with the sequencer
	fmt.Printf("Follower started successfully, syncing with: %s\n", config.Bridge.Client.Endpoint)

	// Placeholder follower loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Follower shutdown requested")
			return nil
		case <-ticker.C:
			// TODO: Implement follower sync logic
			// - Query latest block height from sequencer
			// - Sync missing blocks
			// - Validate transactions
			fmt.Println("Follower syncing... (placeholder)")
		}
	}
}