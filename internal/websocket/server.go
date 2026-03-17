// Package websocket provides WebSocket server and client management for the gateway.
// It handles connection lifecycle, message routing, and broadcasting telemetry.
package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/command"
	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
	"github.com/EthanMBoos/openc2-gateway/internal/registry"
	"github.com/gorilla/websocket"
)

// ServerConfig holds WebSocket server configuration.
type ServerConfig struct {
	Port           int
	GatewayVersion string
}

// Server manages WebSocket connections and message broadcasting.
type Server struct {
	config     ServerConfig
	registry   *registry.Registry
	cmdTracker *command.Tracker
	cmdRouter  *command.Router
	upgrader   websocket.Upgrader

	// Client management
	mu      sync.RWMutex
	clients map[*Client]struct{}

	// Broadcast channel
	broadcast chan *protocol.Frame

	// HTTP server for graceful shutdown
	httpServer *http.Server

	// Optional metrics handler
	metricsHandler http.Handler
}

// NewServer creates a new WebSocket server.
func NewServer(cfg ServerConfig, reg *registry.Registry, cmdTracker *command.Tracker) *Server {
	s := &Server{
		config:     cfg,
		registry:   reg,
		cmdTracker: cmdTracker,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// Allow all origins for local development
			// In production, this should be restricted
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients:   make(map[*Client]struct{}),
		broadcast: make(chan *protocol.Frame, 256),
	}
	return s
}

// ListenAndServe starts the WebSocket server and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)
	mux.HandleFunc("/healthz", s.handleHealth)

	// Add metrics endpoint if configured
	if s.metricsHandler != nil {
		mux.Handle("/metrics", s.metricsHandler)
	}

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.Port),
		Handler: mux,
	}

	// Start broadcast loop
	go s.broadcastLoop(ctx)

	slog.Info("websocket server listening", "port", s.config.Port)

	// Start HTTP server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	// Close all client connections
	s.mu.Lock()
	for client := range s.clients {
		client.Close()
	}
	s.mu.Unlock()

	// Shutdown HTTP server
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Broadcast sends a frame to all connected, handshaked clients.
func (s *Server) Broadcast(frame *protocol.Frame) {
	select {
	case s.broadcast <- frame:
	default:
		// Channel full - drop frame (telemetry is droppable per PROTOCOL.md)
		slog.Debug("broadcast channel full, dropping frame", "type", frame.Type)
	}
}

// broadcastLoop sends frames to all clients.
func (s *Server) broadcastLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-s.broadcast:
			s.mu.RLock()
			for client := range s.clients {
				if client.Handshaked() {
					client.Send(frame)
				}
			}
			s.mu.RUnlock()
		}
	}
}

// handleWebSocket upgrades HTTP connections to WebSocket.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	client := NewClient(conn, s)
	s.registerClient(client)

	slog.Info("client connected", "remote", conn.RemoteAddr())

	// Start client message pumps
	go client.WritePump()
	go client.ReadPump()
}

// handleHealth provides a health check endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"clients": clientCount,
	})
}

// registerClient adds a client to the server.
func (s *Server) registerClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
}

// unregisterClient removes a client from the server.
func (s *Server) unregisterClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

// handleHello processes a hello message and sends welcome response.
func (s *Server) handleHello(c *Client, frame *protocol.Frame) error {
	// Parse hello payload
	payloadBytes, err := json.Marshal(frame.Data)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	var hello protocol.HelloPayload
	if err := json.Unmarshal(payloadBytes, &hello); err != nil {
		c.SendError(protocol.ErrInvalidMessage, "invalid hello payload")
		return fmt.Errorf("unmarshal hello: %w", err)
	}

	// Validate protocol version
	if hello.ProtocolVersion != protocol.ProtocolVersion {
		c.SendError(protocol.ErrProtocolVersionUnsupported,
			fmt.Sprintf("unsupported protocol version %d, supported: [%d]",
				hello.ProtocolVersion, protocol.ProtocolVersion))
		return fmt.Errorf("unsupported protocol version: %d", hello.ProtocolVersion)
	}

	// Build fleet snapshot
	fleet := s.registry.GetFleetSummary()

	// Send welcome response
	welcome := protocol.NewWelcomeFrame(
		s.config.GatewayVersion,
		fleet,
		10,   // telemetryRateHz
		1000, // heartbeatIntervalMs
	)

	c.send <- welcome
	c.setHandshaked(true)

	slog.Info("client handshaked",
		"client_id", hello.ClientID,
		"protocol_version", hello.ProtocolVersion,
		"fleet_size", len(fleet),
	)

	return nil
}

// handleCommand processes a command from a client.
func (s *Server) handleCommand(c *Client, frame *protocol.Frame) error {
	if s.cmdRouter == nil {
		c.SendError(protocol.ErrCommandSendFailed, "command routing not available")
		return nil
	}

	result := s.cmdRouter.Route(frame)

	// Send response back to client
	if result.Frame != nil {
		c.Send(result.Frame)
	}

	if !result.Success {
		slog.Debug("command rejected",
			"vid", frame.Vid,
			"reason", result.Frame.Data,
		)
	}

	return nil
}

// SetCommandRouter sets the command router for the server.
// Must be called before starting the server if command routing is needed.
func (s *Server) SetCommandRouter(router *command.Router) {
	s.cmdRouter = router
}

// SetMetricsHandler sets the handler for /metrics endpoint.
// If set, metrics will be exposed in Prometheus format at /metrics.
func (s *Server) SetMetricsHandler(handler http.Handler) {
	s.metricsHandler = handler
}

// GetWelcomeFrame generates a welcome frame with current fleet state.
func (s *Server) GetWelcomeFrame() *protocol.Frame {
	fleet := s.registry.GetFleetSummary()
	return protocol.NewWelcomeFrame(
		s.config.GatewayVersion,
		fleet,
		10,
		1000,
	)
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// HandshakedClientCount returns the number of handshaked clients.
func (s *Server) HandshakedClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for client := range s.clients {
		if client.Handshaked() {
			count++
		}
	}
	return count
}

// SendToClient sends a frame to a specific client by ID.
// Returns false if client not found.
func (s *Server) SendToClient(clientID string, frame *protocol.Frame) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		if client.ID() == clientID {
			client.Send(frame)
			return true
		}
	}
	return false
}

// timeNowMs returns current time in milliseconds.
func timeNowMs() int64 {
	return time.Now().UnixMilli()
}
