package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
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
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Main run loop
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("Node running as %s...\n", *role)
		case sig := <-sigChan:
			fmt.Printf("Received signal: %v, shutting down...\n", sig)
			return
		}
	}
}