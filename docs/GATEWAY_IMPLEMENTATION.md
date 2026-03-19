# OpenC2-Gateway Implementation Plan

> Build-out plan for the Go gateway bridging vehicles to the OpenC2 UI.  
> For system topology and package map see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Implemented Components

The following are complete in `internal/`:

| Component | Location | Description |
|-----------|----------|-------------|
| Proto definitions | `api/proto/openc2.proto` | Protobuf schema for vehicle↔gateway |
| Frame types | `internal/protocol/frame.go` | JSON wire types for gateway↔UI |
| Sequence tracker | `internal/protocol/sequence.go` | Per-vehicle deduplication with wrap-around |
| Registry | `internal/registry/registry.go` | Vehicle tracking + status state machine |
| Proto→JSON translation | `internal/protocol/translate.go` | `DecodeVehicleMessage()` entry point |
| Frame builders | `internal/protocol/builders.go` | `NewStatusFrame()`, `NewWelcomeFrame()`, etc. |
| Validation | `internal/protocol/validate.go` | Frame size + field constraints |
| Command tracker | `internal/command/tracker.go` | Rate limiting + timeout ACKs |

---

## Phase 1: Entry Point + WebSocket Server

**Goal**: UI can connect and receive mock telemetry

### 1.1 Config + Entry Point

Create `cmd/gateway/main.go` and `internal/config/config.go`:

```go
type Config struct {
    MulticastGroup string        `env:"OPENC2_MCAST_GROUP" default:"239.255.0.1"`
    MulticastPort  int           `env:"OPENC2_MCAST_PORT" default:"14550"`
    WSPort         int           `env:"OPENC2_WS_PORT" default:"9000"`
    StandbyTimeout time.Duration `env:"OPENC2_STANDBY_TIMEOUT" default:"3s"`
    OfflineTimeout time.Duration `env:"OPENC2_OFFLINE_TIMEOUT" default:"10s"`
}
```

- [x] Create `cmd/gateway/main.go` with signal handling + graceful shutdown
- [x] Create `internal/config/config.go` with env loading
- [x] Wire existing `registry` and `protocol` packages

### 1.2 WebSocket Server

- [x] Create `internal/websocket/server.go` — gorilla/websocket, client mgmt
- [x] Create `internal/websocket/client.go` — read/write pumps, ping/pong
- [x] Broadcast channel with non-blocking send

### 1.3 Hello/Welcome Handshake

Per [PROTOCOL.md](PROTOCOL.md):

- [x] Validate `hello` message and protocol version
- [x] Send `welcome` with fleet snapshot from registry
- [x] Only broadcast telemetry after successful handshake

### 1.4 Test Sender

- [x] Create `cmd/testsender/main.go` to simulate vehicle telemetry
- [x] Realistic movement patterns at configurable Hz

### Testing Phase 1

**Cleanup (run before/after tests):**
```bash
# Kill gateway binaries (go run spawns child processes with these names)
pkill -f gateway 2>/dev/null || true
pkill -f testclient 2>/dev/null || true  
pkill -f testsender 2>/dev/null || true

# Or kill by port (frees :9000)
lsof -ti:9000 | xargs kill -9 2>/dev/null || true
```

#### 1.1 Configuration Testing

**What's Implemented:**
- `internal/config/config.go` loads all settings from environment variables
- Defaults match PROTOCOL.md specification
- Validation rejects invalid combinations (e.g., OfflineTimeout < StandbyTimeout)

**Test: Default Configuration**
```bash
# Start gateway with defaults
go run ./cmd/gateway

# Expected log output:
# time=... level=INFO msg="starting openc2-gateway" version=0.1.0 ws_port=9000 mcast_group=239.255.0.1 mcast_port=14550
```

**Test: Custom Configuration**
```bash
# Override defaults via environment
OPENC2_WS_PORT=8080 \
OPENC2_STANDBY_TIMEOUT=5s \
OPENC2_OFFLINE_TIMEOUT=15s \
go run ./cmd/gateway

# Verify in logs: ws_port=8080
```

**Test: Invalid Configuration**
```bash
# OfflineTimeout must be > StandbyTimeout
OPENC2_STANDBY_TIMEOUT=10s \
OPENC2_OFFLINE_TIMEOUT=5s \
go run ./cmd/gateway

# Expected: error="invalid config: OfflineTimeout (5s) must be greater than StandbyTimeout (10s)"
```

#### 1.2 WebSocket Server Testing

**What's Implemented:**
- `internal/websocket/server.go` - Connection upgrades, client tracking, broadcast
- `internal/websocket/client.go` - Per-client read/write pumps with ping/pong keepalive
- Non-blocking broadcast (drops frames if client buffer full)

**Test: Basic Connection**
```bash
# Terminal 1: Start gateway and testsender
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

# Terminal 2: Connect with testclient
go run ./cmd/testclient

# Expected output:
# ✓ Connected to gateway
# ✓ Sent hello
# ✓ Received welcome (type=welcome)
# ✓ Reading telemetry frames...

# Cleanup:
pkill -f gateway; pkill -f testsender; pkill -f testclient
```

**Test: Multiple Clients**
```bash
# Terminal 1: Start gateway and testsender
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

# Terminal 2 & 3: Connect two clients (stay connected for 30s)
go run ./cmd/testclient -duration 30s &
go run ./cmd/testclient -duration 30s &

# Check health endpoint shows 2 clients:
curl http://localhost:9000/healthz
# {"clients":2,"status":"ok"}

# Both clients receive telemetry until duration expires

# Cleanup:
pkill -f gateway; pkill -f testsender; pkill -f testclient
```

**Test: Client Disconnection**
```bash
# Connect and immediately disconnect (Ctrl+C)
# Verify server logs: "client disconnected"
# Verify /healthz shows client count decremented
```

#### 1.3 Hello/Welcome Handshake Testing

**What's Implemented:**
- Client MUST send `hello` as first message
- Gateway validates protocol version (currently v1)
- Gateway responds with `welcome` containing fleet snapshot
- Telemetry only broadcast to handshaked clients

> **Note:** Valid handshake is already tested in 1.2 via `testclient`. These tests cover error cases only.

**Test: Protocol Version Mismatch**
```bash
go run ./cmd/gateway &
go run ./cmd/testclient -bad-version

# Expected output:
# ✓ Connected to gateway
# Testing: bad protocol version...
#   Sent hello with v=99
#   Response: {"v":1,"type":"error","vid":"_gateway",...}
# ✓ Received expected PROTOCOL_VERSION_UNSUPPORTED error

# Cleanup:
pkill -f gateway
```

**Test: Missing Hello**
```bash
go run ./cmd/gateway &
go run ./cmd/testclient -skip-hello

# Expected output:
# ✓ Connected to gateway
# Testing: command without hello...
#   Sent command without hello
#   Response: {"v":1,"type":"error","vid":"_gateway",...}
# ✓ Received expected INVALID_MESSAGE error

# Cleanup:
pkill -f gateway
```

#### 1.4 Testsender & Telemetry Format

**What's Implemented:**
- `cmd/testsender/main.go` simulates vehicle telemetry via UDP multicast
- Configurable vehicle ID, environment, and update rate
- Movement patterns: random walk with realistic dynamics
- Sequence numbers increment monotonically

> **Note:** Telemetry flow is already exercised in 1.2's Basic Connection test. This section documents the frame format.

**Telemetry Frame Format:**
```json
{
  "v": 1,
  "type": "telemetry",
  "vid": "ugv-husky-01",
  "ts": 1710700800000,
  "gts": 1710700800000,
  "data": {
    "location": {"lat": 37.7749, "lng": -122.4194},
    "speed": 2.0,
    "heading": 45.5,
    "environment": "ground",
    "seq": 100,
    "batteryPct": 85,
    "signalStrength": 4
  }
}
```

---

## Phase 2: UDP Multicast Listener

**Goal**: Receive real protobuf telemetry from vehicles

### 2.1 Multicast Listener

- [x] Create `internal/telemetry/multicast.go`
- [x] Join multicast group via `golang.org/x/net/ipv4`
- [x] Use `sync.Pool` for buffer reuse
- [x] Decode via existing `protocol.DecodeVehicleMessage()`

### 2.2 Integration

- [x] Bridge multicast output → WebSocket broadcast
- [x] Update registry via `registry.RecordTelemetry()`
- [x] Status transitions emit status frames

### 2.3 Test Sender (Optional)

- [x] Create `cmd/testsender/main.go` for integration testing

### Testing Phase 2

> **Note:** The full multicast → WebSocket pipeline is already exercised by Phase 1 integration tests (`testsender` + `testclient`). This section covers unit tests and testsender usage only.

#### Unit Tests

Run unit tests for Phase 2 components:

```bash
# All protocol tests (sequence tracking, protobuf translation, validation)
go test ./internal/protocol/... -v

# Registry state machine (ONLINE → STANDBY → OFFLINE)
go test ./internal/registry/... -v

# Command tracker (rate limiting, timeouts)
go test ./internal/command/... -v

# Run all unit tests
go test ./... -v
```

#### Testsender Usage

**Command Line Options:**
```bash
go run ./cmd/testsender --help
# -vid string      Vehicle ID (default "ugv-test-01")
# -env string      Environment: ground, air, surface, subsurface (default "ground")
# -group string    Multicast group (default "239.255.0.1")
# -port int        Multicast port (default 14550)
# -rate int        Telemetry rate in Hz (default 10)
```

**Multiple Vehicles:**
```bash
go run ./cmd/testsender -vid ugv-alpha -env ground &
go run ./cmd/testsender -vid uav-bravo -env air &
go run ./cmd/testsender -vid usv-charlie -env surface &
```

**Stress Test (high rate):**
```bash
go run ./cmd/testsender -vid stress-test -rate 100
```

---

## Phase 3: Command Routing

**Goal**: UI can send commands to vehicles

- [x] Create `internal/command/router.go`
- [x] Parse command JSON from WebSocket
- [x] Validate against registry (vehicle must exist)
- [x] Use existing `command.Tracker` for rate limiting
- [x] Convert to protobuf, broadcast to `239.255.0.2:14551`
- [x] Handle vehicle ACKs and timeouts

Commands: `goto`, `stop`, `return_home`, `set_mode`, `set_speed`

### Testing Phase 3

**Cleanup (run before/after tests):**
```bash
pkill -f gateway 2>/dev/null || true
pkill -f testclient 2>/dev/null || true
pkill -f testsender 2>/dev/null || true
lsof -ti:9000 | xargs kill -9 2>/dev/null || true
```

#### 3.1 Command Router Testing

**What's Implemented:**
- `internal/command/router.go` - Routes commands from UI to vehicles
- Parses JSON command frames from WebSocket
- Validates vehicle exists in registry
- Converts JSON to protobuf Command message
- Broadcasts via UDP multicast to `239.255.0.2:14551`

**Test: Basic Command Flow (Manual)**
```bash
# This requires a custom WebSocket client or browser extension
# Send command after hello/welcome:

# 1. Connect to ws://localhost:9000
# 2. Send hello: {"v":1,"type":"hello","vid":"_client","ts":0,"data":{"protocolVersion":1,"clientId":"cmd-test"}}
# 3. Receive welcome
# 4. Send command:
{
  "v": 1,
  "type": "command",
  "vid": "ugv-husky-01",
  "ts": 1710700800000,
  "data": {
    "action": "stop",
    "commandId": "cmd-001"
  }
}

# Expected response:
{
  "v": 1,
  "type": "command_ack",
  "vid": "ugv-husky-01",
  "data": {
    "commandId": "cmd-001",
    "status": "accepted"
  }
}
```

#### 3.2 Command Types Testing

**Supported Commands:**

| Action | Payload Fields | Description |
|--------|---------------|-------------|
| `goto` | `destination: {lat, lng}`, `speed` (optional) | Navigate to location |
| `stop` | (none) | Emergency stop |
| `return_home` | (none) | Return to launch |
| `set_mode` | `mode: "manual"/"autonomous"/"guided"` | Change mode |
| `set_speed` | `speed: float (m/s)` | Set target speed |

**Test: Goto Command**
```json
{
  "v": 1,
  "type": "command",
  "vid": "ugv-husky-01",
  "data": {
    "action": "goto",
    "commandId": "goto-001",
    "destination": {"lat": 37.7850, "lng": -122.4000},
    "speed": 3.5
  }
}
```

**Test: Set Mode Command**
```json
{
  "v": 1,
  "type": "command",
  "vid": "uav-quad-02",
  "data": {
    "action": "set_mode",
    "commandId": "mode-001",
    "mode": "autonomous"
  }
}
```

#### 3.3 Command Validation Testing

**What's Implemented:**
- Vehicle must exist in registry (received at least one telemetry)
- Required fields: `commandId`, `action`, valid `vid`
- Action-specific validation (e.g., valid mode values)

**Test: Unknown Vehicle**
```bash
# Send command to non-existent vehicle
# Command vid: "nonexistent-vehicle"

# Expected error response:
{
  "v": 1,
  "type": "error",
  "vid": "_gateway",
  "data": {
    "code": "VEHICLE_NOT_FOUND",
    "message": "vehicle nonexistent-vehicle not found in registry",
    "commandId": "cmd-xxx"
  }
}
```

**Test: Missing CommandId**
```bash
# Send command without commandId field

# Expected error response:
{
  "v": 1,
  "type": "error",
  "vid": "_gateway",
  "data": {
    "code": "INVALID_MESSAGE",
    "message": "missing commandId"
  }
}
```

**Test: Invalid Mode Value**
```bash
# Send set_mode with invalid mode
# "mode": "turbo"

# Expected error response:
{
  "v": 1,
  "type": "error",
  "vid": "_gateway",
  "data": {
    "code": "INVALID_MESSAGE",
    "message": "invalid set_mode command: invalid mode: turbo"
  }
}
```

#### 3.4 Rate Limiting Testing

**What's Implemented:**
- `internal/command/tracker.go` - 10 commands/second per vehicle (configurable)
- Sliding window rate limiting
- Rejection returns error frame with `RATE_LIMITED` code

**Test: Rate Limit Exceeded**
```bash
# Send >10 commands per second to same vehicle
# (Requires programmatic client)

# After 10th command in 1 second:
{
  "v": 1,
  "type": "error",
  "vid": "_gateway",
  "data": {
    "code": "RATE_LIMITED",
    "message": "Command rate limit exceeded for ugv-husky-01 (10/sec)",
    "commandId": "cmd-011"
  }
}
```

**Test: Rate Limit Per-Vehicle**
```bash
# Send 10 commands to vehicle A, then 10 to vehicle B
# All 20 should succeed (limit is per-vehicle, not global)
```

#### 3.5 Command Timeout Testing

**What's Implemented:**
- Commands tracked pending vehicle ACK
- Timeout after 5s (configurable via `OPENC2_CMD_TIMEOUT`)
- Synthetic timeout ACK broadcast to clients

**Test: Command Timeout**
```bash
# 1. Start gateway
OPENC2_CMD_TIMEOUT=3s go run ./cmd/gateway

# 2. Start testsender (provides a vehicle in registry)
go run ./cmd/testsender -vid timeout-test

# 3. Send command via custom client
# 4. Wait 3 seconds (no vehicle ACK since testsender doesn't process commands)

# Expected: Client receives synthetic timeout ACK:
{
  "v": 1,
  "type": "command_ack",
  "vid": "timeout-test",
  "data": {
    "commandId": "cmd-xxx",
    "status": "timeout",
    "message": "No response from vehicle after 3 seconds"
  }
}

# Cleanup:
pkill -f testsender
pkill -f gateway
```

#### 3.6 Multicast Command Output Testing

**Test: Capture Command Multicast**
```bash
# Listen on command multicast group:
# (Requires netcat with multicast support or custom receiver)
# Address: 239.255.0.2:14551

# Send command via UI
# Verify protobuf Command message arrives on multicast
# Protobuf contains: command_id, vehicle_id, timestamp_ms, and payload oneof
```

---

## Phase 4: Observability

**Goal**: Production-ready logging and health checks

- [x] Replace `log.Printf` with `log/slog`
- [x] Add `/healthz` endpoint
- [x] Add `/metrics` for Prometheus (optional)
- [x] Graceful shutdown with drain timeout

### Testing Phase 4

**Cleanup (run before/after tests):**
```bash
pkill -f gateway 2>/dev/null || true
pkill -f testclient 2>/dev/null || true
lsof -ti:9000 | xargs kill -9 2>/dev/null || true
```

#### 4.1 Structured Logging Testing

**What's Implemented:**
- All logging uses `log/slog` with structured key-value pairs
- JSON handler available for production (currently using text handler)
- Log levels: DEBUG, INFO, WARN, ERROR
- Contextual fields for correlation (vid, command_id, etc.)

**Test: Log Output Format**
```bash
go run ./cmd/gateway

# Expected structured logs:
# time=2026-03-18T10:00:00.000-04:00 level=INFO msg="starting openc2-gateway" version=0.1.0 ws_port=9000 mcast_group=239.255.0.1 mcast_port=14550
# time=2026-03-18T10:00:00.001-04:00 level=INFO msg="joined multicast group" group=239.255.0.1 port=14550 iface=lo0
# time=2026-03-18T10:00:00.002-04:00 level=INFO msg="websocket server listening" port=9000
```

**Test: Client Connection Logging**
```bash
# Connect client
go run ./cmd/testclient

# Expected logs:
# time=... level=INFO msg="client connected" remote=127.0.0.1:xxxxx
# time=... level=INFO msg="client handshaked" client_id=test-client protocol_version=1 fleet_size=3
# time=... level=INFO msg="client disconnected" remote=127.0.0.1:xxxxx
```

#### 4.2 Health Endpoint Testing

**What's Implemented:**
- `/healthz` endpoint returns JSON health status
- Includes current client count
- Returns HTTP 200 when healthy

**Test: Health Check**
```bash
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

# Basic health check
curl http://localhost:9000/healthz
# {"clients":0,"status":"ok"}

# After client connects:
go run ./cmd/testclient &
sleep 1
curl http://localhost:9000/healthz
# {"clients":1,"status":"ok"}

# Cleanup:
pkill -f gateway; pkill -f testsender; pkill -f testclient
```

**Test: HTTP Health Probe**
```bash
# Verify endpoint returns HTTP 200:
curl -f http://localhost:9000/healthz
echo $?  # Should be 0 (success)
```

#### 4.3 Prometheus Metrics Testing

**What's Implemented:**
- `/metrics` endpoint exports Prometheus-format metrics
- `internal/observability/metrics.go` - Thread-safe atomic counters
- Metrics categories: connections, telemetry, commands, vehicles

**Test: Metrics Endpoint**
```bash
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

curl http://localhost:9000/metrics

# Cleanup when done:
# pkill -f gateway; pkill -f testsender

# Expected output (Prometheus format):
# HELP openc2_uptime_seconds Gateway uptime in seconds
# TYPE openc2_uptime_seconds gauge
openc2_uptime_seconds 10.500

# HELP openc2_ws_connections Current WebSocket connections
# TYPE openc2_ws_connections gauge
openc2_ws_connections 0

# HELP openc2_vehicles Vehicles by status
# TYPE openc2_vehicles gauge
openc2_vehicles{status="online"} 3
openc2_vehicles{status="standby"} 0
openc2_vehicles{status="offline"} 0
```

**Available Metrics:**

| Metric | Type | Description |
|--------|------|-------------|
| `openc2_uptime_seconds` | gauge | Gateway uptime |
| `openc2_ws_connections` | gauge | Current active connections |
| `openc2_ws_connections_total` | counter | Total connections since startup |
| `openc2_ws_handshakes_total` | counter | Successful handshakes |
| `openc2_telemetry_received_total` | counter | Telemetry frames received |
| `openc2_telemetry_broadcast_total` | counter | Telemetry frames broadcast |
| `openc2_telemetry_dropped_total` | counter | Frames dropped (buffer full) |
| `openc2_commands_received_total` | counter | Commands from UI |
| `openc2_commands_sent_total` | counter | Commands sent to vehicles |
| `openc2_commands_rejected_total` | counter | Commands rejected |
| `openc2_commands_timedout_total` | counter | Commands timed out |
| `openc2_vehicles{status="..."}` | gauge | Vehicles by status |

**Test: Metrics Under Load**
```bash
# Generate traffic
go run ./cmd/testclient &
sleep 5

# Check counters increased
curl -s http://localhost:9000/metrics | grep telemetry
# openc2_telemetry_received_total 500
# openc2_telemetry_broadcast_total 500

# Cleanup:
pkill -f testclient
pkill -f gateway
```

**Test: Prometheus Integration**
```yaml
# prometheus.yml scrape config:
scrape_configs:
  - job_name: 'openc2-gateway'
    static_configs:
      - targets: ['localhost:9000']
    metrics_path: '/metrics'
```

#### 4.4 Graceful Shutdown Testing

**What's Implemented:**
- Signal handling for SIGINT (Ctrl+C) and SIGTERM
- 5-second drain timeout for active connections
- Clean shutdown of WebSocket server and command router

**Test: Graceful Shutdown**
```bash
# Terminal 1: Start gateway and testsender
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

# Terminal 2: Connect client (keep running)
go run ./cmd/testclient

# Terminal 1: Send SIGINT (Ctrl+C) to gateway
# Expected logs:
# time=... level=INFO msg="received shutdown signal" signal=interrupt
# time=... level=INFO msg="shutting down..."
# time=... level=INFO msg="gateway stopped"

# Client should receive close frame and disconnect cleanly
# Cleanup:
pkill -f gateway; pkill -f testsender; pkill -f testclient
```

**Test: SIGTERM (Docker/Kubernetes)**
```bash
# Start gateway in background
go run ./cmd/gateway &
PID=$!

# Send SIGTERM
kill -TERM $PID

# Expected: Same clean shutdown as SIGINT
```

#### 4.5 Debug Logging Testing

**What's Implemented:**
- Debug level logs for detailed troubleshooting
- Includes: frame drops, decode errors, command routing details

**Test: Enable Debug Logging**
```go
// In cmd/gateway/main.go, temporarily change:
slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,  // Change from LevelInfo
}))
```

```bash
# Debug output includes:
# time=... level=DEBUG msg="broadcast channel full, dropping frame" type=telemetry
# time=... level=DEBUG msg="client send buffer full, dropping frame" client_id=xxx frame_type=telemetry
# time=... level=DEBUG msg="command sent" vid=ugv-husky-01 action=stop command_id=xxx
```

---

## Test Cleanup Quick Reference

Use these commands to clean up processes between tests:

```bash
# Kill all gateway-related processes
pkill -f gateway 2>/dev/null
pkill -f testclient 2>/dev/null
pkill -f testsender 2>/dev/null

# Alternative: Kill by port (if process names don't match)
lsof -ti:9000 | xargs kill -9 2>/dev/null

# Verify nothing is running on port 9000
lsof -i:9000
# (should return empty)

# Full cleanup one-liner
pkill -f gateway; pkill -f testsender; pkill -f testclient; echo "✓ Cleaned"
```

---

## Quick Reference

| Env Variable | Default | Description |
|--------------|---------|-------------|
| `OPENC2_WS_PORT` | 9000 | WebSocket port |
| `OPENC2_MCAST_GROUP` | 239.255.0.1 | Telemetry multicast |
| `OPENC2_MCAST_PORT` | 14550 | Telemetry port |
| `OPENC2_CMD_MCAST_GROUP` | 239.255.0.2 | Command multicast |
| `OPENC2_CMD_MCAST_PORT` | 14551 | Command port |
| `OPENC2_STANDBY_TIMEOUT` | 3s | Time before standby |
| `OPENC2_OFFLINE_TIMEOUT` | 10s | Time before offline |

## Dependencies

```bash
go get github.com/gorilla/websocket
go get github.com/kelseyhightower/envconfig
go get github.com/google/uuid
go get golang.org/x/net
```

---

## MVP Path

| Phase | Delivers |
|-------|----------|
| **Phase 1** | WebSocket server, testsender, hello/welcome |
| **Phase 2** | Real UDP/protobuf ingestion |
| **Phase 3** | Command routing |
| **Phase 4** | Observability |

**MVP cutoff:** End of Phase 2 (UI sees real vehicles).

---

## Phase 5: Extensibility

**Goal**: Extension codecs and middleware adapters (ROS2, Zenoh) without forking.

> See [EXTENSIBILITY.md](EXTENSIBILITY.md) for the codec interface, registry, manifest spec, and implementation checklist.
