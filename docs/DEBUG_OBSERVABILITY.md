# Gateway Debug & Observability Strategy

> **Purpose**: Defines the tools, techniques, and phased implementation plan for debugging protocol translation issues, network problems, and message flow visibility in the OpenC2 gateway.

---

## Why This Matters

The gateway performs real-time protocol translation between disparate systems:

```
Radio Node (protobuf/UDP multicast) ←→ Gateway ←→ UI Client (JSON/WebSocket)
```

Debugging challenges include:
- **Protocol misalignment**: Field naming, type coercion, precision loss
- **Timing issues**: Message ordering, race conditions, timeout mismatches
- **Network problems**: Multicast routing, packet loss, connection drops
- **State divergence**: Vehicle registry vs. actual vehicle state

Without proper observability, these issues are nearly impossible to diagnose in production.

---

## Implementation Phases

### Phase 0: UDP Drop Visibility (MVP Critical)
**Effort**: 2 hours | **Priority**: Critical (do this first)

At 100Hz telemetry × 10 vehicles = 1000 packets/sec. If the gateway's UDP receive buffer fills, packets are silently dropped by the kernel. The UI has **no visibility** into this — vehicles appear to stutter or freeze with no error.

#### The Problem

```
Normal flow:
  Vehicle ──UDP──▶ Kernel Buffer ──▶ Gateway goroutine ──▶ Process & broadcast

Under load (buffer full):
  Vehicle ──UDP──▶ Kernel Buffer ──X  (kernel drops packet silently)
                        │
                        └── No syscall, no error, no log
                            Vehicle appears to "stutter" in UI
```

#### MVP Mitigation

1. **Atomic counter for drops** — increment when `ReadFromUDP` returns error or when processing can't keep up
2. **Rate-limited logging** — log drops at most once per second (don't spam logs under load)
3. **Expose in `/debug/stats`** — simple JSON counter, no Prometheus dependency yet

#### Implementation

```go
// internal/telemetry/udp.go
type UDPListener struct {
    conn        *net.UDPConn
    dropsTotal  atomic.Uint64  // Kernel or processing drops
    lastDropLog time.Time      // Rate-limit drop logging
    dropLogMu   sync.Mutex
}

func (l *UDPListener) receive() {
    buf := make([]byte, 65535)
    for {
        n, _, err := l.conn.ReadFromUDP(buf)
        if err != nil {
            l.recordDrop("udp_read_error", err)
            continue
        }
        
        select {
        case l.incoming <- buf[:n]:
            // OK
        default:
            // Channel full — processing can't keep up
            l.recordDrop("channel_full", nil)
        }
    }
}

func (l *UDPListener) recordDrop(reason string, err error) {
    l.dropsTotal.Add(1)
    
    // Rate-limited logging: max 1 log per second
    l.dropLogMu.Lock()
    defer l.dropLogMu.Unlock()
    if time.Since(l.lastDropLog) > time.Second {
        l.lastDropLog = time.Now()
        slog.Warn("udp.drop",
            "reason", reason,
            "total_drops", l.dropsTotal.Load(),
            "error", err,
        )
    }
}

func (l *UDPListener) DropsTotal() uint64 {
    return l.dropsTotal.Load()
}
```

#### Debug Endpoint

```go
// GET /debug/stats response includes:
{
    "udp": {
        "drops_total": 0,
        "packets_received": 847293,
        "bytes_received": 1058616250
    }
}
```

#### Kernel Buffer Tuning (Optional)

If drops occur under expected load, increase the kernel receive buffer:

```bash
# Check current buffer size
sysctl net.core.rmem_max

# Increase to 4MB (Linux)
sudo sysctl -w net.core.rmem_max=4194304

# Set in Go before binding
conn.SetReadBuffer(4 * 1024 * 1024)
```

---

### Phase 1: Structured Logging with Correlation IDs
**Effort**: 1 day | **Priority**: Critical

Every message flowing through the gateway gets a correlation ID for end-to-end tracing.

#### Requirements

1. **Correlation ID generation**: UUID or ULID assigned at ingress point
2. **Boundary logging**: Log at each translation boundary with consistent fields
3. **JSON output**: Machine-parseable for log aggregation

#### Log Fields (Required)

| Field | Description |
|-------|-------------|
| `correlation_id` | Unique ID for this message flow |
| `direction` | `vehicle→gateway`, `gateway→ui`, `ui→gateway`, `gateway→vehicle` |
| `type` | Frame type (`telemetry`, `command`, `status`, etc.) |
| `vid` | Vehicle ID or `_client`/`_gateway` |
| `raw_bytes` | Size of raw payload |
| `latency_us` | Processing time in microseconds |
| `error` | Error message if translation failed |

#### Example Log Output

```json
{"level":"info","ts":"2026-03-17T14:32:01.234Z","msg":"frame.received","correlation_id":"01HQXYZ...","direction":"vehicle→gateway","type":"telemetry","vid":"ugv-husky-07","raw_bytes":1247}
{"level":"info","ts":"2026-03-17T14:32:01.235Z","msg":"frame.translated","correlation_id":"01HQXYZ...","direction":"gateway→ui","type":"telemetry","vid":"ugv-husky-07","raw_bytes":892,"latency_us":1102}
```

#### Implementation

```go
// internal/observability/logger.go
type FrameLogger struct {
    logger *slog.Logger
}

func (l *FrameLogger) LogInbound(correlationID, direction, frameType, vid string, rawBytes int) {
    l.logger.Info("frame.received",
        "correlation_id", correlationID,
        "direction", direction,
        "type", frameType,
        "vid", vid,
        "raw_bytes", rawBytes,
    )
}

func (l *FrameLogger) LogTranslated(correlationID, direction, frameType, vid string, rawBytes int, latencyUs int64) {
    l.logger.Info("frame.translated",
        "correlation_id", correlationID,
        "direction", direction,
        "type", frameType,
        "vid", vid,
        "raw_bytes", rawBytes,
        "latency_us", latencyUs,
    )
}
```

---

### Phase 2: HTTP Debug Endpoints
**Effort**: 1 day | **Priority**: Critical

REST endpoints for inspecting gateway state and recent message history.

#### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/debug/frames` | GET | Recent frame history (ring buffer) |
| `/debug/frames/{correlation_id}` | GET | Specific frame by correlation ID |
| `/debug/errors` | GET | Recent validation/translation errors |
| `/debug/vehicles` | GET | Current vehicle registry state |
| `/debug/clients` | GET | Connected WebSocket clients |
| `/debug/stats` | GET | Runtime statistics |

#### Frame History Response

```json
{
  "frames": [
    {
      "correlation_id": "01HQXYZ...",
      "timestamp": "2026-03-17T14:32:01.234Z",
      "direction": "inbound",
      "protocol": "udp",
      "type": "telemetry",
      "vid": "ugv-husky-07",
      "raw_size": 1247,
      "decoded": {
        "v": 1,
        "type": "telemetry",
        "vid": "ugv-husky-07",
        "ts": 1710700800000,
        "data": { "...": "..." }
      },
      "translation_latency_us": 1102,
      "errors": []
    }
  ],
  "total": 847,
  "limit": 100,
  "offset": 0
}
```

#### Error History Response

```json
{
  "errors": [
    {
      "timestamp": "2026-03-17T14:30:45.123Z",
      "correlation_id": "01HQABC...",
      "error_code": "INVALID_FRAME_TYPE",
      "message": "Unknown frame type: 'telmetry' (typo?)",
      "raw_input": "eyJ2IjoxLCJ0eXBlIjoidGVsbWV0cnkiLC4uLn0=",
      "context": {
        "source_ip": "192.168.1.42",
        "vid": "ugv-husky-07"
      }
    }
  ]
}
```

#### Implementation

```go
// internal/debug/server.go
type DebugServer struct {
    frameBuffer *RingBuffer[FrameRecord]
    errorBuffer *RingBuffer[ErrorRecord]
    registry    *registry.Registry
    hub         *hub.Hub
}

func (s *DebugServer) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /debug/frames", s.handleFrames)
    mux.HandleFunc("GET /debug/frames/{id}", s.handleFrameByID)
    mux.HandleFunc("GET /debug/errors", s.handleErrors)
    mux.HandleFunc("GET /debug/vehicles", s.handleVehicles)
    mux.HandleFunc("GET /debug/clients", s.handleClients)
    mux.HandleFunc("GET /debug/stats", s.handleStats)
}
```

#### Ring Buffer Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `OPENC2_DEBUG_FRAME_BUFFER` | 1000 | Max frames to retain |
| `OPENC2_DEBUG_ERROR_BUFFER` | 500 | Max errors to retain |
| `OPENC2_DEBUG_ENABLED` | true | Enable debug endpoints |
| `OPENC2_DEBUG_PORT` | 9001 | Debug HTTP server port |

---

### Phase 3: Prometheus Metrics + Grafana Dashboard
**Effort**: 1-2 days | **Priority**: High

Expose quantitative metrics for monitoring and alerting.

#### Metrics to Expose

```prometheus
# Counters
openc2_frames_total{direction="inbound|outbound", type="telemetry|command|status|...", protocol="udp|ws"}
openc2_frame_bytes_total{direction="inbound|outbound", protocol="udp|ws"}
openc2_udp_drops_total{reason="read_error|channel_full"}  # CRITICAL: silent packet loss
openc2_validation_errors_total{error_code="INVALID_FRAME|UNKNOWN_TYPE|..."}
openc2_translation_errors_total{direction="proto_to_json|json_to_proto"}
openc2_commands_total{vid="...", command_type="goto|stop|..."}
openc2_command_acks_total{vid="...", status="ok|failed|timeout"}

# Gauges
openc2_vehicles_online
openc2_vehicles_standby
openc2_vehicles_offline
openc2_ws_clients_connected
openc2_frame_buffer_size

# Histograms
openc2_translation_latency_seconds{direction="proto_to_json|json_to_proto"}
openc2_ws_broadcast_latency_seconds
openc2_command_roundtrip_seconds{vid="..."}
```

#### Grafana Dashboard Panels

| Panel | Type | Description |
|-------|------|-------------|
| Frame Rate | Time series | Frames/sec by direction and type |
| Translation Latency | Heatmap | P50/P95/P99 latency distribution |
| Error Rate | Time series | Validation + translation errors |
| Vehicle Status | State timeline | Online/standby/offline per vehicle |
| Client Connections | Gauge | Current WebSocket clients |
| Bytes Throughput | Time series | Inbound vs outbound bandwidth |
| Command Success Rate | Stat | % of commands that received ACK |

#### Implementation

```go
// internal/observability/metrics.go
var (
    framesTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "openc2_frames_total",
            Help: "Total frames processed",
        },
        []string{"direction", "type", "protocol"},
    )
    
    translationLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "openc2_translation_latency_seconds",
            Help:    "Time to translate frames",
            Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // 100µs to 100ms
        },
        []string{"direction"},
    )
    
    vehiclesOnline = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "openc2_vehicles_online",
            Help: "Number of vehicles currently online",
        },
    )
)
```

---

### Phase 4: Real-Time WebSocket Debug Channel
**Effort**: 2-3 days | **Priority**: High

Live message inspector via WebSocket subscription.

#### Design

Clients connect with `"clientType": "debug"` in the hello message to receive copies of all frames:

```json
{
  "v": 1,
  "type": "hello",
  "vid": "_client",
  "ts": 1710700800000,
  "data": {
    "protocolVersion": 1,
    "clientId": "debug-inspector-01",
    "clientType": "debug"
  }
}
```

Debug clients receive `debug_frame` messages:

```json
{
  "v": 1,
  "type": "debug_frame",
  "vid": "_gateway",
  "ts": 1710700800050,
  "data": {
    "correlation_id": "01HQXYZ...",
    "direction": "inbound",
    "protocol": "udp",
    "original_frame": { "...": "..." },
    "translated_frame": { "...": "..." },
    "latency_us": 1102,
    "errors": []
  }
}
```

#### Debug Inspector UI

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Gateway Inspector                                    [⏸ Pause] [🗑 Clear]│
├───────┬────────────┬───────────┬───────────────┬────────┬───────────────┤
│ DIR   │ TIME       │ TYPE      │ VEHICLE       │ SIZE   │ LATENCY       │
├───────┼────────────┼───────────┼───────────────┼────────┼───────────────┤
│ ← IN  │ 14:32:01.2 │ telemetry │ ugv-husky-07  │ 1.2KB  │ —             │
│ → OUT │ 14:32:01.2 │ telemetry │ ugv-husky-07  │ 890B   │ 1.1ms         │
│ ← IN  │ 14:32:01.5 │ command   │ _client       │ 124B   │ —             │
│ → OUT │ 14:32:01.5 │ command   │ ugv-husky-07  │ 89B    │ 0.8ms         │
│ ← IN  │ 14:32:02.1 │ telemetry │ ugv-husky-07  │ 1.2KB  │ —             │
│ → OUT │ 14:32:02.1 │ telemetry │ ugv-husky-07  │ 891B   │ 1.0ms         │
├───────┴────────────┴───────────┴───────────────┴────────┴───────────────┤
│ ► Selected Frame (14:32:01.234 | telemetry | ugv-husky-07)              │
├─────────────────────────────────────────────────────────────────────────┤
│ ┌─ Original (protobuf) ─────────┐  ┌─ Translated (JSON) ──────────────┐ │
│ │ vehicle_id: "ugv-husky-07"    │  │ {                                │ │
│ │ timestamp: 1710700800000      │  │   "v": 1,                        │ │
│ │ position {                    │  │   "type": "telemetry",           │ │
│ │   latitude: 37.7749           │  │   "vid": "ugv-husky-07",         │ │
│ │   longitude: -122.4194        │  │   "ts": 1710700800000,           │ │
│ │   altitude: 127.45            │  │   "data": {                      │ │
│ │ }                             │  │     "position": {                │ │
│ │ ...                           │  │       "lat": 37.7749,            │ │
│ └───────────────────────────────┘  │       ...                        │ │
│                                    └────────────────────────────────────┤
│ [📋 Copy Original] [📋 Copy Translated] [🔄 Diff] [▶ Replay]             │
└─────────────────────────────────────────────────────────────────────────┘
```

#### Filtering Options

Debug clients can send filter requests:

```json
{
  "v": 1,
  "type": "debug_filter",
  "vid": "_client",
  "ts": 1710700800000,
  "data": {
    "vehicles": ["ugv-husky-07"],
    "types": ["command", "command_ack"],
    "directions": ["inbound"],
    "include_raw": true
  }
}
```

---

### Phase 5: Record & Replay System
**Effort**: 2-3 days | **Priority**: Medium

Capture sessions for offline analysis and bug reproduction.

#### Recording Mode

```bash
# Start gateway with recording enabled
./gateway --record=/var/log/openc2/session-2026-03-17.jsonl

# Or via environment
OPENC2_RECORD_PATH=/var/log/openc2/session-$(date +%Y%m%d-%H%M%S).jsonl ./gateway
```

#### Recording Format (JSONL)

Each line is a self-contained record:

```json
{"ts":1710700800000,"dir":"in","proto":"udp","src":"192.168.1.42:54321","raw":"CgN1Z3YtaHVza3ktMDcSA...","decoded":{"v":1,"type":"telemetry","vid":"ugv-husky-07"}}
{"ts":1710700800001,"dir":"out","proto":"ws","dst":"client-a1b2c3d4","raw":"{\"v\":1,\"type\":\"telemetry\"...}","decoded":{"v":1,"type":"telemetry","vid":"ugv-husky-07"}}
{"ts":1710700800050,"dir":"in","proto":"ws","src":"client-a1b2c3d4","raw":"{\"v\":1,\"type\":\"command\"...}","decoded":{"v":1,"type":"command","vid":"ugv-husky-07"}}
{"ts":1710700800051,"dir":"out","proto":"udp","dst":"239.255.0.2:14551","raw":"CgN1Z3YtaHVza3ktMDcSB...","decoded":{"v":1,"type":"command","vid":"ugv-husky-07"}}
```

#### Replay Mode

```bash
# Replay at original speed
./gateway --replay=/var/log/openc2/session-2026-03-17.jsonl

# Replay at 2x speed
./gateway --replay=/var/log/openc2/session-2026-03-17.jsonl --speed=2.0

# Replay at half speed for detailed analysis
./gateway --replay=/var/log/openc2/session-2026-03-17.jsonl --speed=0.5

# Replay with modified translation logic (for testing fixes)
./gateway --replay=/var/log/openc2/session-2026-03-17.jsonl --compare
```

#### Replay Compare Mode

When `--compare` is enabled, replay shows differences between original and new translation:

```
[14:32:01.234] DIFF in telemetry frame for ugv-husky-07:
  Field "data.orientation.yaw":
    Original: 127.45000076293945
    Current:  127.45
  (Precision fix applied correctly)
```

#### Implementation

```go
// internal/replay/recorder.go
type Recorder struct {
    file    *os.File
    encoder *json.Encoder
    mu      sync.Mutex
}

type Record struct {
    Ts      int64           `json:"ts"`
    Dir     string          `json:"dir"`      // "in" or "out"
    Proto   string          `json:"proto"`    // "udp" or "ws"
    Src     string          `json:"src,omitempty"`
    Dst     string          `json:"dst,omitempty"`
    Raw     string          `json:"raw"`      // base64 for binary, string for JSON
    Decoded json.RawMessage `json:"decoded"`
}

func (r *Recorder) Record(rec Record) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.encoder.Encode(rec)
}
```

---

### Phase 6: Protocol Diff Tool
**Effort**: 1-2 days | **Priority**: Medium

CLI tool for comparing protobuf input vs JSON output.

#### Usage

```bash
# Compare single frame
openc2-debug diff --proto frame.bin --json frame.json

# Compare from recording
openc2-debug diff --record session.jsonl --correlation-id 01HQXYZ...

# Batch compare all frames in recording
openc2-debug diff --record session.jsonl --report diff-report.html
```

#### Output Example

```
Frame: 01HQXYZ... (telemetry, ugv-husky-07)
──────────────────────────────────────────

✓ v: 1 == 1
✓ type: "telemetry" == "telemetry"
✓ vid: "ugv-husky-07" == "ugv-husky-07"
✓ ts: 1710700800000 == 1710700800000

data.position:
  ✓ lat: 37.7749 == 37.7749
  ✓ lon: -122.4194 == -122.4194
  ⚠ alt: 127.45 (float32) → 127.45000076293945 (float64)
      Note: Precision loss from float32→float64 conversion

data.orientation:
  ✓ roll: 0.0 == 0.0
  ✓ pitch: 0.0 == 0.0
  ✗ yaw: 180.0 ≠ -180.0
      ERROR: Sign inversion detected!

Summary: 1 error, 1 warning, 12 fields OK
```

---

### Phase 7: Wireshark Dissector
**Effort**: 1-2 days | **Priority**: Low

Custom Wireshark dissector for the protobuf protocol.

#### Lua Dissector

```lua
-- openc2_dissector.lua
local openc2_proto = Proto("openc2", "OpenC2 Vehicle Protocol")

local f_vehicle_id = ProtoField.string("openc2.vehicle_id", "Vehicle ID")
local f_msg_type = ProtoField.uint32("openc2.msg_type", "Message Type", base.DEC)
local f_timestamp = ProtoField.uint64("openc2.timestamp", "Timestamp", base.DEC)

openc2_proto.fields = { f_vehicle_id, f_msg_type, f_timestamp }

function openc2_proto.dissector(buffer, pinfo, tree)
    pinfo.cols.protocol = "OpenC2"
    local subtree = tree:add(openc2_proto, buffer(), "OpenC2 Protocol Data")
    
    -- Parse protobuf fields (simplified example)
    -- Full implementation would use proto definition
    subtree:add(f_vehicle_id, buffer(0, 16))
    subtree:add(f_msg_type, buffer(16, 4))
    subtree:add(f_timestamp, buffer(20, 8))
end

-- Register for UDP multicast ports
local udp_table = DissectorTable.get("udp.port")
udp_table:add(14550, openc2_proto)
udp_table:add(14551, openc2_proto)
```

#### Installation

```bash
# macOS
cp openc2_dissector.lua ~/.local/lib/wireshark/plugins/

# Linux
cp openc2_dissector.lua ~/.config/wireshark/plugins/

# Then in Wireshark: Analyze → Reload Lua Plugins
```

---

## Configuration Reference

All debug/observability settings:

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `OPENC2_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `OPENC2_LOG_FORMAT` | `json` | Log format: json, text |
| `OPENC2_DEBUG_ENABLED` | `true` | Enable debug HTTP endpoints |
| `OPENC2_DEBUG_PORT` | `9001` | Debug HTTP server port |
| `OPENC2_DEBUG_FRAME_BUFFER` | `1000` | Frames to retain in memory |
| `OPENC2_DEBUG_ERROR_BUFFER` | `500` | Errors to retain in memory |
| `OPENC2_METRICS_ENABLED` | `true` | Enable Prometheus metrics |
| `OPENC2_METRICS_PORT` | `9090` | Metrics HTTP server port |
| `OPENC2_RECORD_PATH` | `""` | Path to record session (empty = disabled) |
| `OPENC2_REPLAY_PATH` | `""` | Path to replay session (empty = disabled) |
| `OPENC2_REPLAY_SPEED` | `1.0` | Replay speed multiplier |

---

## Directory Structure

```
openc2-gateway/
├── internal/
│   ├── observability/
│   │   ├── logger.go         # Structured frame logging
│   │   ├── metrics.go        # Prometheus metrics
│   │   └── correlation.go    # Correlation ID generation
│   ├── debug/
│   │   ├── server.go         # HTTP debug endpoints
│   │   ├── buffer.go         # Ring buffer implementation
│   │   ├── inspector.go      # WebSocket debug channel
│   │   └── handlers.go       # HTTP handlers
│   └── replay/
│       ├── recorder.go       # Session recording
│       ├── player.go         # Session replay
│       └── diff.go           # Protocol diff logic
├── cmd/
│   ├── gateway/
│   │   └── main.go
│   └── openc2-debug/         # CLI debug tool
│       └── main.go
└── tools/
    └── wireshark/
        └── openc2_dissector.lua
```

---

## Summary

| Phase | Component | Effort | Priority |
|-------|-----------|--------|----------|
| 1 | Structured Logging + Correlation IDs | 1 day | Critical |
| 2 | HTTP Debug Endpoints | 1 day | Critical |
| 3 | Prometheus Metrics + Grafana | 1-2 days | High |
| 4 | Real-Time WebSocket Debug Channel | 2-3 days | High |
| 5 | Record & Replay System | 2-3 days | Medium |
| 6 | Protocol Diff Tool | 1-2 days | Medium |
| 7 | Wireshark Dissector | 1-2 days | Low |

**Total estimated effort**: 9-14 days

Start with Phases 1-2 for immediate debugging capability, then layer on metrics and real-time inspection as the system matures.
