// Tower Server - Bridge between vehicles and UI
//
// The server receives protobuf telemetry from vehicles via UDP multicast
// and relays it to UI clients over WebSocket in JSON format.
//
// Usage:
//
//	go run ./cmd/tower-server
//	TOWER_WS_PORT=8080 go run ./cmd/tower-server
//
// For testing, use testsender to simulate vehicle telemetry:
//
//	go run ./cmd/testsender -vid ugv-test-01
//
// See docs/SERVER_IMPLEMENTATION.md for full configuration options.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/EthanMBoos/tower-server/internal/command"
	"github.com/EthanMBoos/tower-server/internal/config"
	"github.com/EthanMBoos/tower-server/internal/extensions"
	"github.com/EthanMBoos/tower-server/internal/observability"
	"github.com/EthanMBoos/tower-server/internal/protocol"
	"github.com/EthanMBoos/tower-server/internal/registry"
	"github.com/EthanMBoos/tower-server/internal/telemetry"
	"github.com/EthanMBoos/tower-server/internal/websocket"

	// Extension codecs register themselves via init()
	_ "github.com/EthanMBoos/tower-server/internal/extensions/blueboat"
	_ "github.com/EthanMBoos/tower-server/internal/extensions/husky"
	_ "github.com/EthanMBoos/tower-server/internal/extensions/skydio"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	// Initialize structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Override version if built with ldflags
	if Version != "dev" {
		cfg.ServerVersion = Version
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Load extension manifests from YAML files
	manifestCount, err := extensions.LoadManifestsFromDir("internal/extensions")
	if err != nil {
		slog.Warn("failed to load some extension manifests", "error", err)
	} else {
		slog.Info("loaded extension manifests", "count", manifestCount)
	}

	// Log startup configuration
	slog.Info("starting tower-server",
		"version", cfg.ServerVersion,
		"ws_port", cfg.WSPort,
		"telemetry_sources", len(cfg.MulticastSources),
	)
	for _, src := range cfg.MulticastSources {
		slog.Info("telemetry source configured",
			"label", src.Label,
			"group", src.Group,
			"port", src.Port,
		)
	}

	// Create shared components
	seqTracker := protocol.NewSequenceTracker()
	reg := registry.New(seqTracker, registry.Config{
		StandbyTimeout: cfg.StandbyTimeout,
		OfflineTimeout: cfg.OfflineTimeout,
	})

	// Create command tracker for rate limiting and timeout handling
	cmdTracker := command.NewTracker(command.TrackerConfig{
		Timeout:    cfg.CmdTimeout,
		RateLimit:  cfg.CmdRateLimit,
		RateWindow: 1 * time.Second, // Sliding window for rate limiting
	}, nil) // onTimeout callback will be wired later

	// Create WebSocket server
	wsServer := websocket.NewServer(websocket.ServerConfig{
		Port:           cfg.WSPort,
		ServerVersion: cfg.ServerVersion,
	}, reg, cmdTracker)

	// Create metrics for observability
	metrics := observability.NewMetrics()
	wsServer.SetMetricsHandler(metrics.PrometheusHandler())

	// Create command router for sending commands to vehicles
	cmdRouter := command.NewRouter(command.RouterConfig{
		MulticastGroup: cfg.CmdMulticastGroup,
		MulticastPort:  cfg.CmdMulticastPort,
	}, reg, cmdTracker)

	// Wire command router to WebSocket server
	wsServer.SetCommandRouter(cmdRouter)

	// Wire command timeout callback to broadcast via WebSocket
	cmdTracker.SetTimeoutCallback(func(frame *protocol.Frame) {
		wsServer.Broadcast(frame)
	})

	// Wire registry status transitions to broadcast via WebSocket
	reg.SetTransitionCallback(func(t registry.StatusTransition) {
		frame := protocol.NewStatusFrame(t.VehicleID, string(t.To), nil, "server")
		wsServer.Broadcast(frame)
	})

	// Create context that cancels on shutdown signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	// Start telemetry sources (UDP multicast from vehicles)
	// Each source gets its own channel; we fan-in to the shared telemetryFrames channel.
	telemetryFrames := make(chan *protocol.Frame, 100)

	// WaitGroup to track when all sources have stopped
	var sourceWg sync.WaitGroup

	for _, srcCfg := range cfg.MulticastSources {
		sourceWg.Add(1)

		// Each source writes to its own channel (source owns and closes it)
		sourceCh := make(chan *protocol.Frame, 50)
		src := telemetry.NewMulticastSource(telemetry.MulticastConfig{
			Group: srcCfg.Group,
			Port:  srcCfg.Port,
		})

		// Start the multicast listener
		go func(label string, s *telemetry.MulticastSource, ch chan *protocol.Frame) {
			defer sourceWg.Done()
			if err := s.Start(ctx, ch); err != nil && ctx.Err() == nil {
				slog.Error("telemetry source error", "label", label, "error", err)
			}
		}(srcCfg.Label, src, sourceCh)

		// Fan-in: forward frames from source channel to shared channel
		go func(label string, ch <-chan *protocol.Frame) {
			for frame := range ch {
				select {
				case telemetryFrames <- frame:
				case <-ctx.Done():
					return
				}
			}
		}(srcCfg.Label, sourceCh)
	}

	// Close telemetryFrames when all sources are done
	go func() {
		sourceWg.Wait()
		close(telemetryFrames)
	}()

	// Telemetry → Registry → WebSocket pipeline
	go func() {
		for frame := range telemetryFrames {
			// Update registry based on frame type
			switch frame.Type {
			case protocol.TypeTelemetry:
				if payload, ok := frame.Data.(protocol.TelemetryPayload); ok {
					reg.RecordTelemetry(frame.VehicleID, payload.Environment)
				}
			case protocol.TypeHeartbeat:
				// Update capabilities from heartbeat
				if payload, ok := frame.Data.(protocol.HeartbeatPayload); ok {
					if payload.Capabilities != nil {
						reg.UpdateCapabilities(frame.VehicleID, payload.Capabilities)
					}
				}
			}

			// Broadcast to connected clients
			wsServer.Broadcast(frame)
		}
	}()

	// Start periodic status check (detect standby/offline transitions)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reg.CheckTimeouts()

				// Update vehicle metrics
				counts := reg.CountByStatus()
				metrics.SetVehicleCounts(
					counts[registry.StatusOnline],
					counts[registry.StatusStandby],
					counts[registry.StatusOffline],
				)
			}
		}
	}()

	// Start command router
	if err := cmdRouter.Start(); err != nil {
		return fmt.Errorf("command router: %w", err)
	}
	defer cmdRouter.Stop()

	// Start WebSocket server (blocking until context cancelled)
	if err := wsServer.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return fmt.Errorf("websocket server: %w", err)
	}

	// Graceful shutdown
	slog.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := wsServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("websocket shutdown error", "error", err)
	}

	slog.Info("server stopped")
	return nil
}
