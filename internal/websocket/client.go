// Package websocket provides WebSocket client handling for the gateway.
package websocket

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 8192

	// Send buffer size
	sendBufferSize = 256
)

// Client represents a WebSocket client connection.
type Client struct {
	server *Server
	conn   *websocket.Conn
	send   chan *protocol.Frame

	// Client state
	mu         sync.RWMutex
	id         string // Set after hello handshake
	handshaked bool
	closed     bool
}

// NewClient creates a new client.
func NewClient(conn *websocket.Conn, server *Server) *Client {
	return &Client{
		server: server,
		conn:   conn,
		send:   make(chan *protocol.Frame, sendBufferSize),
	}
}

// ID returns the client ID (set after hello handshake).
func (c *Client) ID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

// Handshaked returns whether the client has completed the hello/welcome handshake.
func (c *Client) Handshaked() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.handshaked
}

// setHandshaked marks the client as handshaked.
func (c *Client) setHandshaked(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handshaked = value
}

// setID sets the client ID from hello message.
func (c *Client) setID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id = id
}

// Send queues a frame for sending to the client.
// Non-blocking: drops message if buffer is full.
func (c *Client) Send(frame *protocol.Frame) {
	select {
	case c.send <- frame:
	default:
		// Buffer full - for telemetry this is acceptable (droppable per PROTOCOL.md)
		slog.Debug("client send buffer full, dropping frame",
			"client_id", c.ID(),
			"frame_type", frame.Type,
		)
	}
}

// SendError sends an error frame to the client.
func (c *Client) SendError(code string, message string) {
	frame := protocol.NewErrorFrame(code, message)
	c.Send(frame)
}

// Close closes the client connection.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	close(c.send)
	c.conn.Close()
}

// ReadPump reads messages from the WebSocket connection.
// Runs in its own goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.server.unregisterClient(c)
		c.Close()
		slog.Info("client disconnected", "remote", c.conn.RemoteAddr())
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure,
			) {
				slog.Warn("websocket read error", "error", err)
			}
			return
		}

		if err := c.handleMessage(message); err != nil {
			slog.Warn("message handling error", "error", err)
		}
	}
}

// WritePump writes messages to the WebSocket connection.
// Runs in its own goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case frame, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel closed - send close message
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Marshal frame to JSON
			data, err := json.Marshal(frame)
			if err != nil {
				slog.Error("failed to marshal frame", "error", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				slog.Debug("websocket write error", "error", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes an incoming message.
func (c *Client) handleMessage(data []byte) error {
	// Parse as generic frame
	var frame protocol.Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		c.SendError(protocol.ErrInvalidMessage, "invalid JSON")
		return err
	}

	// Validate protocol version
	if frame.ProtocolVersion != protocol.ProtocolVersion {
		c.SendError(protocol.ErrProtocolVersionUnsupported,
			"unsupported protocol version")
		return nil
	}

	// Route by message type
	switch frame.Type {
	case protocol.TypeHello:
		// Must be first message
		if c.Handshaked() {
			c.SendError(protocol.ErrInvalidMessage, "already handshaked")
			return nil
		}
		return c.server.handleHello(c, &frame)

	case protocol.TypeCommand:
		// Must be handshaked first
		if !c.Handshaked() {
			c.SendError(protocol.ErrInvalidMessage, "hello required before commands")
			return nil
		}
		return c.server.handleCommand(c, &frame)

	default:
		c.SendError(protocol.ErrInvalidMessage, "unknown message type: "+frame.Type)
		return nil
	}
}
