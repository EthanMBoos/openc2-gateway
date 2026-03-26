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
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-test-01

# Terminal 3: Connect with testclient
go run ./cmd/testclient

# Expected output:
# ✓ Connected to gateway
# ✓ Sent hello
# ✓ Received welcome (type=welcome)
# ✓ Reading telemetry frames...

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender; pkill -f testclient
```

**Test: Multiple Clients**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-test-01

# Terminal 3 & 4: Connect two clients (stay connected for 30s)
go run ./cmd/testclient -duration 30s
go run ./cmd/testclient -duration 30s

# Terminal 5 (or any): Check health endpoint shows 2 clients
curl http://localhost:9000/healthz
# {"clients":2,"status":"ok"}

# Both clients receive telemetry until duration expires

# Cleanup (any terminal):
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
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Run testclient with bad version
go run ./cmd/testclient -bad-version

# Expected output:
# ✓ Connected to gateway
# Testing: bad protocol version...
#   Sent hello with v=99
#   Response: {"protocolVersion": 1,"type":"error","vehicleId": "_gateway",...}
# ✓ Received expected PROTOCOL_VERSION_UNSUPPORTED error

# Cleanup (any terminal):
pkill -f gateway
```

**Test: Missing Hello**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Run testclient skipping hello
go run ./cmd/testclient -skip-hello

# Expected output:
# ✓ Connected to gateway
# Testing: command without hello...
#   Sent command without hello
#   Response: {"protocolVersion": 1,"type":"error","vehicleId": "_gateway",...}
# ✓ Received expected INVALID_MESSAGE error

# Cleanup (any terminal):
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
  "protocolVersion": 1,
  "type": "telemetry",
  "vehicleId": "ugv-husky-01",
  "timestampMs": 1710700800000,
  "gatewayTimestampMs": 1710700800000,
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

> **Note:** The gateway must be running first (`go run ./cmd/gateway` in a separate terminal) for testsender telemetry to be received and broadcast to clients.

**Command Line Options:**
```bash
go run ./cmd/testsender --help
# -vid string      Vehicle ID (default "ugv-test-01")
# -env string      Environment: ground, air, marine
# -group string    Multicast group (default "239.255.0.1")
# -port int        Multicast port (default 14550)
# -rate int        Telemetry rate in Hz (default 10)
```

**Multiple Vehicles:**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminals 2, 3, 4: Start multiple testsenders
go run ./cmd/testsender -vid ugv-alpha -env ground
go run ./cmd/testsender -vid uav-bravo -env air
go run ./cmd/testsender -vid usv-charlie -env marine

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender
```

**Stress Test (high rate):**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start high-rate testsender
go run ./cmd/testsender -vid stress-test -rate 100

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender
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

**Test: Basic Command Flow**

> **Prerequisite:** Install `websocat` for interactive WebSocket testing.

```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender (vehicle must exist in registry to receive commands)
go run ./cmd/testsender -vid ugv-husky-01

# Terminal 3: Connect with websocat and send commands interactively
websocat ws://localhost:9000

# In websocat, paste this hello message (press Enter after):
{"protocolVersion":1,"type":"hello","vehicleId":"_client","timestampMs":0,"data":{"protocolVersion":1,"clientId":"cmd-test"}}

# ⚠️  After welcome, you'll be spammed with telemetry frames (10/sec per vehicle).
# This is expected! Just paste your command anyway - it will be processed.

# Paste this command (it will appear between telemetry lines):
{"protocolVersion":1,"type":"command","vehicleId":"ugv-husky-01","timestampMs":0,"command":"stop","data":{"commandId":"cmd-001"}}

# Look for the command_ack response in the output:
# {"protocolVersion":1,"type":"command_ack","vehicleId":"ugv-husky-01","data":{"commandId":"cmd-001","status":"accepted"}}

# Press Ctrl+C to exit websocat

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender
```

> **Tip:** To reduce telemetry noise, use a lower rate testsender: `go run ./cmd/testsender -vid ugv-husky-01 -rate 1`

**Test: Command to Unknown Vehicle**
```bash
# Terminal 1: Start gateway (no testsender = no telemetry spam)
go run ./cmd/gateway

# Terminal 2: Connect with websocat
websocat ws://localhost:9000

# Send hello:
{"protocolVersion":1,"type":"hello","vehicleId":"_client","timestampMs":0,"data":{"protocolVersion":1,"clientId":"cmd-test"}}

# Send command to non-existent vehicle:
{"protocolVersion":1,"type":"command","vehicleId":"nonexistent","timestampMs":0,"command":"stop","data":{"commandId":"cmd-002"}}

# Expected error response:
# {"protocolVersion":1,"type":"error","vehicleId":"_gateway","data":{"code":"VEHICLE_NOT_FOUND","message":"vehicle nonexistent not found in registry","commandId":"cmd-002"}}

# Cleanup (any terminal):
pkill -f gateway
```

#### 3.2 Command Types Testing

**Supported Core Commands:**

> These are the built-in commands supported by all vehicles. Extensions may add additional commands via their namespace (see [EXTENSIBILITY.md](EXTENSIBILITY.md)).

| Command | Payload Fields | Description |
|---------|---------------|-------------|
| `goto` | `destination: {lat, lng}`, `speed` (optional) | Navigate to location |
| `stop` | (none) | Emergency stop |
| `return_home` | (none) | Return to launch |
| `set_mode` | `mode: "manual"/"autonomous"/"guided"` | Change mode |
| `set_speed` | `speed: float (m/s)` | Set target speed |

**Test: All Core Commands**

```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender with low rate (less spam)
go run ./cmd/testsender -vid ugv-husky-01 -rate 1
```

**Test commands using testclient:**
```bash
# Terminal 3: Send a stop command
go run ./cmd/testclient -cmd stop -vid ugv-husky-01

# Send a goto command
go run ./cmd/testclient -cmd goto -vid ugv-husky-01 -lat 37.7850 -lng -122.4000 -speed 3.5

# Send return_home command  
go run ./cmd/testclient -cmd return_home -vid ugv-husky-01

# Send set_mode command
go run ./cmd/testclient -cmd set_mode -vid ugv-husky-01 -mode autonomous

# Send set_speed command
go run ./cmd/testclient -cmd set_speed -vid ugv-husky-01 -speed 5.0

# Each command should return:
# ✓ Command accepted: stop-xxx

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender
```

**Test: Extension Commands (Husky)**

> Extension commands use `"command": "extension"` with a `namespace` and `payload` containing the command.
> **Note:** Only the Husky extension is currently implemented in testclient. Other extensions would follow the same pattern.

```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender with Husky vehicle ID
go run ./cmd/testsender -vid ugv-husky-01 -rate 1
```

**Test Husky extension commands using testclient:**
```bash
# Terminal 3: setDriveMode - change drive mode
go run ./cmd/testclient -cmd ext -vid ugv-husky-01 -action setDriveMode -mode autonomous

# triggerEStop - software emergency stop
go run ./cmd/testclient -cmd ext -vid ugv-husky-01 -action triggerEStop

# setBumperSensitivity - adjust collision detection (0-100)
go run ./cmd/testclient -cmd ext -vid ugv-husky-01 -action setBumperSensitivity -sensitivity 75

# ⚠️  Expected result: COMMAND_NOT_SUPPORTED error
# This is correct! testsender only simulates telemetry - it doesn't implement
# command handling. The error proves routing works (gateway found the vehicle
# and attempted to deliver the extension command).

# Cleanup (any terminal):
pkill -f gateway; pkill -f testsender
```

> **Note:** To test extension commands end-to-end with ACKs, you'd need a vehicle/simulator that implements the Husky codec's command handlers. See [EXTENSIBILITY.md](EXTENSIBILITY.md) for the codec interface.

#### 3.3 Command Validation Testing

**What's Implemented:**
- Vehicle must exist in registry (received at least one telemetry)
- Required fields: `commandId`, `command`, valid `vehicleId`
- Action-specific validation (e.g., valid mode values)

**Test: Unknown Vehicle**
```bash
# Terminal 1: Start gateway (no testsender)
go run ./cmd/gateway

# Terminal 2: Send command to non-existent vehicle
go run ./cmd/testclient -cmd stop -vid nonexistent-vehicle

# Expected output:
# ✗ Error [VEHICLE_NOT_FOUND]: vehicle nonexistent-vehicle not found in registry

# Cleanup:
pkill -f gateway
```

**Test: Invalid Mode Value**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-husky-01 -rate 1

# Terminal 3: Send set_mode with invalid mode
go run ./cmd/testclient -cmd set_mode -vid ugv-husky-01 -mode turbo

# Expected output:
# ✗ Error [INVALID_MESSAGE]: invalid set_mode command: invalid mode: turbo

# Cleanup:
pkill -f gateway; pkill -f testsender
```

#### 3.4 Rate Limiting Testing

**What's Implemented:**
- `internal/command/tracker.go` - 10 commands/second per vehicle (configurable)
- Sliding window rate limiting
- Rejection returns error frame with `RATE_LIMITED` code

**Test: Rate Limit Exceeded**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-husky-01 -rate 1

# Terminal 3: Send 15 commands rapidly (exceeds 10/sec limit)
go run ./cmd/testclient -cmd stop -vid ugv-husky-01 -burst 15

# Expected output:
# → Sending 15 stop commands rapidly to ugv-husky-01...
# ✓ Results: 10 accepted, 5 rate-limited
# ✓ Rate limiting is working!

# Cleanup:
pkill -f gateway; pkill -f testsender
```

**Test: Rate Limit Per-Vehicle**
```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2 & 3: Start two testsenders
go run ./cmd/testsender -vid vehicle-a -rate 1
go run ./cmd/testsender -vid vehicle-b -rate 1

# Terminal 4: Send 10 to each vehicle (both should succeed - limit is per-vehicle)
go run ./cmd/testclient -cmd stop -vid vehicle-a -burst 10
go run ./cmd/testclient -cmd stop -vid vehicle-b -burst 10

# Both should show: ✓ Results: 10 accepted, 0 rate-limited

# Cleanup:
pkill -f gateway; pkill -f testsender
```

#### 3.5 Command Timeout Testing

**What's Implemented:**
- Commands tracked pending vehicle ACK
- Timeout after 5s (configurable via `OPENC2_CMD_TIMEOUT`)
- Synthetic timeout ACK broadcast to clients

**Test: Command Timeout**
```bash
# Terminal 1: Start gateway with short timeout (3s for faster testing)
OPENC2_CMD_TIMEOUT=3s go run ./cmd/gateway

# Terminal 2: Start testsender (provides a vehicle in registry)
go run ./cmd/testsender -vid ugv-husky-01 -rate 1

# Terminal 3: Send command and wait for timeout ack
# The -wait flag keeps connection open after initial "accepted" to observe the timeout
go run ./cmd/testclient -cmd stop -vid ugv-husky-01 -wait 5s

# Expected output:
# ✓ Connected to gateway
# → Sent stop to ugv-husky-01 (id=stop-xxx)
# ✓ Command accepted: stop-xxx
# → Waiting 5s for timeout ack...
# ✓ Received timeout ack: No response from vehicle after 3 seconds

# Cleanup:
pkill -f gateway; pkill -f testsender
```

> **Note:** The timeout occurs because `testsender` only simulates telemetry - it doesn't process commands or send ACKs. A real vehicle would respond with `accepted`/`completed`/`failed` before the timeout.

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
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-test-01

# Terminal 3: Check health endpoint
curl http://localhost:9000/healthz
# {"clients":0,"status":"ok"}

# Terminal 4: Connect a client
go run ./cmd/testclient

# Terminal 3: Check health again (shows 1 client)
curl http://localhost:9000/healthz
# {"clients":1,"status":"ok"}

# Cleanup (any terminal):
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
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-test-01

# Terminal 3: Check metrics endpoint
curl http://localhost:9000/metrics

# Cleanup (any terminal):
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
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Start testsender
go run ./cmd/testsender -vid ugv-test-01

# Terminal 3: Connect client (keep running)
go run ./cmd/testclient

# Terminal 1: Send SIGINT (Ctrl+C) to gateway
# Expected logs:
# time=... level=INFO msg="received shutdown signal" signal=interrupt
# time=... level=INFO msg="shutting down..."
# time=... level=INFO msg="gateway stopped"

# Client should receive close frame and disconnect cleanly
# Cleanup (any terminal):
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
| `OPENC2_MCAST_SOURCES` | 239.255.0.1:14550 | Telemetry multicast sources |
| `OPENC2_CMD_MCAST_GROUP` | 239.255.0.2 | Command multicast |
| `OPENC2_CMD_MCAST_PORT` | 14551 | Command port |
| `OPENC2_STANDBY_TIMEOUT` | 3s | Time before standby |
| `OPENC2_OFFLINE_TIMEOUT` | 10s | Time before offline |

### Multi-Source Telemetry

To receive telemetry from vehicles broadcasting on different multicast groups:

```bash
# Multiple sources (comma-separated)
OPENC2_MCAST_SOURCES="239.255.0.1:14550,239.255.1.1:14551" go run ./cmd/gateway

# With labels for logging
OPENC2_MCAST_SOURCES="239.255.0.1:14550:ugv-fleet,239.255.1.1:14551:usv-fleet" go run ./cmd/gateway
```

Format: `group:port` or `group:port:label`, comma-separated.

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
