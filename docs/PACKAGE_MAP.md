# Gateway Package Map

One-page reference for what each package in `internal/` owns, its key types, and what it does not do. Use this to orient quickly before editing code.

---

## `internal/protocol`

**Owns:** The JSON wire format between gateway and UI clients.

| File | Responsibility |
|---|---|
| `frame.go` | All JSON types: `Frame`, payloads, command structs |
| `translate.go` | Protobuf → `Frame` (inbound) and `Frame` → protobuf (outbound) |
| `validate.go` | Frame validation (version, required fields, type checks) |
| `builders.go` | Factory functions for gateway-originated frames (status, heartbeat, etc.) |
| `sequence.go` | `SequenceTracker` — deduplicates repeated UDP telemetry by sequence number |

**Key types:** `Frame`, `TelemetryPayload`, `CommandPayload` (interface), `GotoCommand`, `StopCommand`, `ReturnHomeCommand`, `SetModeCommand`, `SetSpeedCommand`, `HelloPayload`, `WelcomePayload`, `ErrorPayload`, `SequenceTracker`

**Does not:** manage connections, hold state, or route messages.

---

## `internal/registry`

**Owns:** Vehicle state machine — tracking which vehicles exist and their online/standby/offline status.

| File | Responsibility |
|---|---|
| `registry.go` | `Registry` — add/update/query vehicles; `Vehicle` and `Status` types; status transitions |

**Key types:** `Registry`, `Vehicle`, `Status` (`online` / `standby` / `offline`), `StatusTransition`

**Does not:** receive telemetry directly, send anything over the network, or know about WebSocket clients.

---

## `internal/command`

**Owns:** Command issuance from UI to vehicles — routing and ACK tracking.

| File | Responsibility |
|---|---|
| `router.go` | `Router` — receives command frames from UI, validates capabilities, rate-limits, and sends via multicast |
| `tracker.go` | `Tracker` — tracks in-flight commands by correlation ID; matches incoming ACKs; handles timeouts |

**Key types:** `Router`, `Tracker`

**Does not:** define command wire formats (those live in `protocol`), manage vehicle state (that's `registry`).

---

## `internal/telemetry`

**Owns:** Inbound UDP multicast listener — receives raw protobuf from vehicles.

| File | Responsibility |
|---|---|
| `multicast.go` | `MulticastConfig`, UDP listener — reads datagrams, deserializes protobuf, pushes to a channel |
| `source.go` | `Source` interface — abstraction over the listener, used for testing |

**Key types:** `MulticastConfig`, `Source` (interface)

**Does not:** translate frames (that's `protocol`), update vehicle state (that's `registry`), or broadcast to clients (that's `websocket`).

---

## `internal/websocket`

**Owns:** WebSocket server and connected UI client lifecycle.

| File | Responsibility |
|---|---|
| `server.go` | `Server` — HTTP upgrade, client registration/deregistration, broadcast loop |
| `client.go` | `Client` — per-connection read/write pumps, hello handshake |

**Key types:** `Server`, `Client`, `ServerConfig`

**Does not:** translate protocol formats, track vehicles, or issue commands directly (delegates to `command.Router`).

---

## `internal/extensions`

**Owns:** Codec plugin registry for non-standard vehicle protocols.

| File | Responsibility |
|---|---|
| `codec.go` | `Codec` interface — implement this to add a custom vehicle protocol |
| `registry.go` | `Register()` / `Lookup()` — global registry of named codecs |

**Key types:** `Codec` (interface)

**Does not:** contain any built-in codecs — see `ADDING_A_ROBOT_TYPE.md` for how to implement one.

---

## `internal/config`

**Owns:** Environment variable parsing and validation.

| File | Responsibility |
|---|---|
| `config.go` | `Config` struct, `Default()`, `Load()`, `Validate()` |

**Key types:** `Config`

**Does not:** watch for config changes at runtime — configuration is loaded once at startup.

---

## `internal/observability`

**Owns:** Prometheus metrics and the `/metrics` + `/healthz` HTTP handlers.

| File | Responsibility |
|---|---|
| `metrics.go` | `Metrics` struct — counters and gauges registered with Prometheus; handler for `/metrics` |

**Key types:** `Metrics`

**Does not:** structured logging (use `log/slog` directly) or debug endpoints (see `DEBUG_OBSERVABILITY_ROADMAP.md`).

---

## Data Flow Summary

```
Vehicle (UDP multicast)
    ↓ raw protobuf
internal/telemetry        — deserialize datagram
    ↓ pb.Telemetry
internal/protocol         — translate to Frame, deduplicate via SequenceTracker
    ↓ *protocol.Frame
internal/registry         — update vehicle status state machine
    ↓
internal/websocket        — broadcast Frame JSON to all connected UI clients

UI client (WebSocket)
    ↓ command Frame JSON
internal/websocket        — receive, parse, validate
    ↓
internal/command/Router   — capability check, rate limit, serialize to protobuf
    ↓ UDP multicast
Vehicle
    ↓ ACK Frame (via telemetry path)
internal/command/Tracker  — match ACK to in-flight command, resolve or timeout
```
