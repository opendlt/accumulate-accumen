//go:build withvdk

package vdkshim

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/sequencer"

	"gitlab.com/accumulatenetwork/accumulate/vdk/logger"
	"gitlab.com/accumulatenetwork/accumulate/vdk/node"
)

// VDKDaemon wraps an Accumen sequencer in a VDK daemon
type VDKDaemon struct {
	sequencer  *sequencer.Sequencer
	httpServer *http.Server
	logger     logger.Logger
}

// NewVDKDaemon creates a new VDK daemon wrapper
func NewVDKDaemon(seq *sequencer.Sequencer, httpSrv *http.Server) *VDKDaemon {
	return &VDKDaemon{
		sequencer:  seq,
		httpServer: httpSrv,
		logger:     NewLogzAdapter(),
	}
}

// Start implements the VDK daemon interface
func (d *VDKDaemon) Start(ctx context.Context) error {
	d.logger.Info("Starting Accumen sequencer via VDK daemon")

	// Start the sequencer
	if err := d.sequencer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sequencer: %w", err)
	}

	// Start HTTP server if provided
	if d.httpServer != nil {
		go func() {
			d.logger.Info("Starting HTTP server on %s", d.httpServer.Addr)
			if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				d.logger.Error("HTTP server error: %v", err)
			}
		}()
	}

	d.logger.Info("VDK daemon started successfully")
	return nil
}

// Stop implements the VDK daemon interface
func (d *VDKDaemon) Stop(ctx context.Context) error {
	d.logger.Info("Stopping Accumen sequencer via VDK daemon")

	// Stop HTTP server first
	if d.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := d.httpServer.Shutdown(shutdownCtx); err != nil {
			d.logger.Error("Failed to shutdown HTTP server: %v", err)
		} else {
			d.logger.Info("HTTP server stopped")
		}
	}

	// Stop the sequencer
	if err := d.sequencer.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop sequencer: %w", err)
	}

	d.logger.Info("VDK daemon stopped successfully")
	return nil
}

// LogzAdapter adapts internal/logz to vdk/logger
type LogzAdapter struct {
	logger *logz.Logger
}

// NewLogzAdapter creates a new adapter
func NewLogzAdapter() *LogzAdapter {
	return &LogzAdapter{
		logger: logz.New(logz.INFO, "vdk"),
	}
}

// Debug implements vdk/logger.Logger
func (a *LogzAdapter) Debug(msg string, args ...interface{}) {
	a.logger.Debug(msg, args...)
}

// Info implements vdk/logger.Logger
func (a *LogzAdapter) Info(msg string, args ...interface{}) {
	a.logger.Info(msg, args...)
}

// Warn implements vdk/logger.Logger
func (a *LogzAdapter) Warn(msg string, args ...interface{}) {
	a.logger.Info("WARN: "+msg, args...)
}

// Error implements vdk/logger.Logger
func (a *LogzAdapter) Error(msg string, args ...interface{}) {
	a.logger.Info("ERROR: "+msg, args...)
}

// Fatal implements vdk/logger.Logger
func (a *LogzAdapter) Fatal(msg string, args ...interface{}) {
	logz.Fatal(msg, args...)
}

// With implements vdk/logger.Logger
func (a *LogzAdapter) With(fields ...interface{}) logger.Logger {
	// For simplicity, return the same adapter
	// In a full implementation, this would create a new logger with additional fields
	return a
}

// RunWithVDK runs the sequencer using VDK node lifecycle management
func RunWithVDK(ctx context.Context, seq *sequencer.Sequencer, httpSrv *http.Server) error {
	if seq == nil {
		return fmt.Errorf("sequencer cannot be nil")
	}

	// Create VDK daemon wrapper
	daemon := NewVDKDaemon(seq, httpSrv)

	// Create VDK node configuration
	nodeConfig := &node.Config{
		Name:        "accumen-sequencer",
		Description: "Accumen L1 Sequencer Node",
		Version:     "1.0.0",
		Logger:      daemon.logger,
	}

	// Create and start VDK node
	vdkNode := node.New(nodeConfig)
	vdkNode.AddDaemon("sequencer", daemon)

	daemon.logger.Info("Starting Accumen with VDK node lifecycle management")

	// Run the VDK node (blocks until shutdown)
	if err := vdkNode.Run(ctx); err != nil {
		return fmt.Errorf("VDK node failed: %w", err)
	}

	daemon.logger.Info("VDK node shutdown complete")
	return nil
}
