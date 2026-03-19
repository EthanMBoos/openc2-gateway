// Package telemetry provides multicast UDP listener for vehicle telemetry.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
	"golang.org/x/net/ipv4"
)

// MulticastConfig configures the multicast listener.
type MulticastConfig struct {
	Group string // Multicast group address (e.g., "239.255.0.1")
	Port  int    // UDP port (e.g., 14550)
}

// DefaultMulticastConfig returns defaults per PROTOCOL.md.
func DefaultMulticastConfig() MulticastConfig {
	return MulticastConfig{
		Group: "239.255.0.1",
		Port:  14550,
	}
}

// MulticastSource listens for vehicle telemetry on UDP multicast.
type MulticastSource struct {
	config MulticastConfig

	// Buffer pool for zero-alloc receive
	bufPool sync.Pool
}

// NewMulticastSource creates a new multicast telemetry source.
func NewMulticastSource(cfg MulticastConfig) *MulticastSource {
	return &MulticastSource{
		config: cfg,
		bufPool: sync.Pool{
			New: func() interface{} {
				// Allocate buffer large enough for max UDP payload
				// 1400 bytes per PROTOCOL.md to avoid fragmentation
				buf := make([]byte, protocol.MaxUDPFrameSize)
				return &buf
			},
		},
	}
}

// Start begins receiving multicast telemetry and translating to frames.
// Blocks until ctx is cancelled.
func (m *MulticastSource) Start(ctx context.Context, frames chan<- *protocol.Frame) error {
	defer close(frames)

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", m.config.Group, m.config.Port))
	if err != nil {
		return fmt.Errorf("resolve multicast addr: %w", err)
	}

	// Create UDP connection
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: m.config.Port})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	defer conn.Close()

	// Join multicast group using ipv4 package for proper IGMP handling
	p := ipv4.NewPacketConn(conn)

	// Find all interfaces and join multicast on each
	// This handles both local testing and multi-NIC deployments
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}

	joinedAny := false
	for _, iface := range ifaces {
		// Skip interfaces that can't do multicast
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}

		if err := p.JoinGroup(&iface, addr); err != nil {
			slog.Debug("failed to join multicast on interface",
				"iface", iface.Name,
				"error", err,
			)
			continue
		}
		slog.Info("joined multicast group",
			"group", m.config.Group,
			"port", m.config.Port,
			"iface", iface.Name,
		)
		joinedAny = true
	}

	if !joinedAny {
		return fmt.Errorf("failed to join multicast group on any interface")
	}

	// Enable loopback for local testing (receive our own packets)
	if err := p.SetMulticastLoopback(true); err != nil {
		slog.Warn("failed to enable multicast loopback", "error", err)
	}

	slog.Info("multicast telemetry listener started",
		"group", m.config.Group,
		"port", m.config.Port,
	)

	// Receive loop
	for {
		select {
		case <-ctx.Done():
			slog.Info("multicast telemetry stopped")
			return nil
		default:
		}

		// Get buffer from pool
		bufPtr := m.bufPool.Get().(*[]byte)
		buf := *bufPtr

		// Read datagram (blocking)
		// TODO: Use SetReadDeadline for graceful shutdown
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Return buffer to pool before handling error
			m.bufPool.Put(bufPtr)

			// Check if context was cancelled
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			slog.Warn("multicast read error", "error", err)
			continue
		}

		// Process datagram in this goroutine (decode is fast)
		frame, err := protocol.DecodeVehicleMessage(buf[:n])
		if err != nil {
			m.bufPool.Put(bufPtr)
			slog.Debug("failed to decode vehicle message",
				"error", err,
				"size", n,
			)
			continue
		}

		// Return buffer to pool
		m.bufPool.Put(bufPtr)

		// Send frame (non-blocking)
		select {
		case frames <- frame:
		default:
			// Channel full, drop frame (telemetry is droppable)
			slog.Debug("telemetry channel full, dropping frame",
				"vehicle_id", frame.VehicleID,
			)
		}
	}
}
