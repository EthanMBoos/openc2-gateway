// Package observability provides metrics and health check endpoints.
package observability

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics holds runtime metrics for the server.
// All fields are safe for concurrent access.
type Metrics struct {
	// Connection metrics
	wsConnections          atomic.Int64
	wsConnectionsTotal     atomic.Int64
	wsHandshakesTotal      atomic.Int64
	wsHandshakeFailedTotal atomic.Int64

	// Telemetry metrics
	telemetryReceivedTotal atomic.Int64
	telemetryBroadcastTotal atomic.Int64
	telemetryDroppedTotal  atomic.Int64

	// Command metrics
	commandsReceivedTotal  atomic.Int64
	commandsSentTotal      atomic.Int64
	commandsRejectedTotal  atomic.Int64
	commandsTimedOutTotal  atomic.Int64

	// Vehicle metrics
	vehiclesOnline  atomic.Int64
	vehiclesStandby atomic.Int64
	vehiclesOffline atomic.Int64

	// Start time for uptime calculation
	startTime time.Time
}

// NewMetrics creates a new metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// WSConnection increments/decrements active WebSocket connections.
func (m *Metrics) WSConnect() {
	m.wsConnections.Add(1)
	m.wsConnectionsTotal.Add(1)
}

func (m *Metrics) WSDisconnect() {
	m.wsConnections.Add(-1)
}

func (m *Metrics) WSHandshakeSuccess() {
	m.wsHandshakesTotal.Add(1)
}

func (m *Metrics) WSHandshakeFailed() {
	m.wsHandshakeFailedTotal.Add(1)
}

// Telemetry metrics
func (m *Metrics) TelemetryReceived() {
	m.telemetryReceivedTotal.Add(1)
}

func (m *Metrics) TelemetryBroadcast() {
	m.telemetryBroadcastTotal.Add(1)
}

func (m *Metrics) TelemetryDropped() {
	m.telemetryDroppedTotal.Add(1)
}

// Command metrics
func (m *Metrics) CommandReceived() {
	m.commandsReceivedTotal.Add(1)
}

func (m *Metrics) CommandSent() {
	m.commandsSentTotal.Add(1)
}

func (m *Metrics) CommandRejected() {
	m.commandsRejectedTotal.Add(1)
}

func (m *Metrics) CommandTimedOut() {
	m.commandsTimedOutTotal.Add(1)
}

// Vehicle status
func (m *Metrics) SetVehicleCounts(online, standby, offline int) {
	m.vehiclesOnline.Store(int64(online))
	m.vehiclesStandby.Store(int64(standby))
	m.vehiclesOffline.Store(int64(offline))
}

// Snapshot returns current metric values.
type MetricsSnapshot struct {
	UptimeSeconds float64 `json:"uptimeSeconds"`

	// Connections
	WSConnections          int64 `json:"wsConnections"`
	WSConnectionsTotal     int64 `json:"wsConnectionsTotal"`
	WSHandshakesTotal      int64 `json:"wsHandshakesTotal"`
	WSHandshakeFailedTotal int64 `json:"wsHandshakeFailedTotal"`

	// Telemetry
	TelemetryReceivedTotal  int64 `json:"telemetryReceivedTotal"`
	TelemetryBroadcastTotal int64 `json:"telemetryBroadcastTotal"`
	TelemetryDroppedTotal   int64 `json:"telemetryDroppedTotal"`

	// Commands
	CommandsReceivedTotal int64 `json:"commandsReceivedTotal"`
	CommandsSentTotal     int64 `json:"commandsSentTotal"`
	CommandsRejectedTotal int64 `json:"commandsRejectedTotal"`
	CommandsTimedOutTotal int64 `json:"commandsTimedOutTotal"`

	// Vehicles
	VehiclesOnline  int64 `json:"vehiclesOnline"`
	VehiclesStandby int64 `json:"vehiclesStandby"`
	VehiclesOffline int64 `json:"vehiclesOffline"`
}

// Snapshot returns current metric values.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		UptimeSeconds:           time.Since(m.startTime).Seconds(),
		WSConnections:           m.wsConnections.Load(),
		WSConnectionsTotal:      m.wsConnectionsTotal.Load(),
		WSHandshakesTotal:       m.wsHandshakesTotal.Load(),
		WSHandshakeFailedTotal:  m.wsHandshakeFailedTotal.Load(),
		TelemetryReceivedTotal:  m.telemetryReceivedTotal.Load(),
		TelemetryBroadcastTotal: m.telemetryBroadcastTotal.Load(),
		TelemetryDroppedTotal:   m.telemetryDroppedTotal.Load(),
		CommandsReceivedTotal:   m.commandsReceivedTotal.Load(),
		CommandsSentTotal:       m.commandsSentTotal.Load(),
		CommandsRejectedTotal:   m.commandsRejectedTotal.Load(),
		CommandsTimedOutTotal:   m.commandsTimedOutTotal.Load(),
		VehiclesOnline:          m.vehiclesOnline.Load(),
		VehiclesStandby:         m.vehiclesStandby.Load(),
		VehiclesOffline:         m.vehiclesOffline.Load(),
	}
}

// PrometheusHandler returns a handler that exports metrics in Prometheus format.
func (m *Metrics) PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := m.Snapshot()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		// Uptime
		fmt.Fprintf(w, "# HELP tower_uptime_seconds Server uptime in seconds\n")
		fmt.Fprintf(w, "# TYPE tower_uptime_seconds gauge\n")
		fmt.Fprintf(w, "tower_uptime_seconds %.3f\n", s.UptimeSeconds)

		// WebSocket connections
		fmt.Fprintf(w, "\n# HELP tower_ws_connections Current WebSocket connections\n")
		fmt.Fprintf(w, "# TYPE tower_ws_connections gauge\n")
		fmt.Fprintf(w, "tower_ws_connections %d\n", s.WSConnections)

		fmt.Fprintf(w, "\n# HELP tower_ws_connections_total Total WebSocket connections since startup\n")
		fmt.Fprintf(w, "# TYPE tower_ws_connections_total counter\n")
		fmt.Fprintf(w, "tower_ws_connections_total %d\n", s.WSConnectionsTotal)

		fmt.Fprintf(w, "\n# HELP tower_ws_handshakes_total Total successful handshakes\n")
		fmt.Fprintf(w, "# TYPE tower_ws_handshakes_total counter\n")
		fmt.Fprintf(w, "tower_ws_handshakes_total %d\n", s.WSHandshakesTotal)

		// Telemetry
		fmt.Fprintf(w, "\n# HELP tower_telemetry_received_total Total telemetry frames received\n")
		fmt.Fprintf(w, "# TYPE tower_telemetry_received_total counter\n")
		fmt.Fprintf(w, "tower_telemetry_received_total %d\n", s.TelemetryReceivedTotal)

		fmt.Fprintf(w, "\n# HELP tower_telemetry_broadcast_total Total telemetry frames broadcast\n")
		fmt.Fprintf(w, "# TYPE tower_telemetry_broadcast_total counter\n")
		fmt.Fprintf(w, "tower_telemetry_broadcast_total %d\n", s.TelemetryBroadcastTotal)

		fmt.Fprintf(w, "\n# HELP tower_telemetry_dropped_total Total telemetry frames dropped\n")
		fmt.Fprintf(w, "# TYPE tower_telemetry_dropped_total counter\n")
		fmt.Fprintf(w, "tower_telemetry_dropped_total %d\n", s.TelemetryDroppedTotal)

		// Commands
		fmt.Fprintf(w, "\n# HELP tower_commands_received_total Total commands received from UI\n")
		fmt.Fprintf(w, "# TYPE tower_commands_received_total counter\n")
		fmt.Fprintf(w, "tower_commands_received_total %d\n", s.CommandsReceivedTotal)

		fmt.Fprintf(w, "\n# HELP tower_commands_sent_total Total commands sent to vehicles\n")
		fmt.Fprintf(w, "# TYPE tower_commands_sent_total counter\n")
		fmt.Fprintf(w, "tower_commands_sent_total %d\n", s.CommandsSentTotal)

		fmt.Fprintf(w, "\n# HELP tower_commands_rejected_total Total commands rejected\n")
		fmt.Fprintf(w, "# TYPE tower_commands_rejected_total counter\n")
		fmt.Fprintf(w, "tower_commands_rejected_total %d\n", s.CommandsRejectedTotal)

		fmt.Fprintf(w, "\n# HELP tower_commands_timedout_total Total commands timed out\n")
		fmt.Fprintf(w, "# TYPE tower_commands_timedout_total counter\n")
		fmt.Fprintf(w, "tower_commands_timedout_total %d\n", s.CommandsTimedOutTotal)

		// Vehicles
		fmt.Fprintf(w, "\n# HELP tower_vehicles Vehicles by status\n")
		fmt.Fprintf(w, "# TYPE tower_vehicles gauge\n")
		fmt.Fprintf(w, "tower_vehicles{status=\"online\"} %d\n", s.VehiclesOnline)
		fmt.Fprintf(w, "tower_vehicles{status=\"standby\"} %d\n", s.VehiclesStandby)
		fmt.Fprintf(w, "tower_vehicles{status=\"offline\"} %d\n", s.VehiclesOffline)
	}
}
