# Tower Server Protocol Specification

> **Purpose**: Behavioral contracts and architecture for server ↔ UI communication.  
> **Source of truth for types**: See [`internal/protocol/`](../internal/protocol/) for Go types, validation, and translation.  
> **Terminology**: See [GLOSSARY.md](GLOSSARY.md) for definitions of `seq`, `gts`, `HWM`, and other key terms.

---

## Overview

```
Radio Node ◀───protobuf/UDP multicast───▶ Server ◀───JSON/WebSocket───▶ UI Client
```

| Direction | Format | Transport | Address |
|-----------|--------|-----------|---------|
| Vehicle → Server | protobuf | UDP multicast | `239.255.0.1:14550` |
| Server → Vehicle | protobuf | UDP multicast | `239.255.0.2:14551` |
| Server ↔ UI | JSON | WebSocket | `ws://localhost:9000` |

---

## Security Assumptions (MVP)

**Trusted LAN only.** No authentication, authorization, or encryption.

| Future Feature | Description |
|----------------|-------------|
| API Key Auth | `Authorization` header on WebSocket upgrade |
| TLS | `wss://` with cert validation |
| Command ACL | Per-vehicle permissions by client identity |

---

## Connection Lifecycle

```
                        VEHICLE ↔ SERVER                    SERVER ↔ UI
                       (protobuf/UDP multicast)              (JSON/WebSocket)
                       
┌─────────────┐                           ┌─────────────┐                    ┌─────────────┐
│   Vehicle   │                           │   Server   │                    │     UI      │
└──────┬──────┘                           └──────┬──────┘                    └──────┬──────┘
       │                                         │                                  │
       │                                         │◀────────── hello ────────────────│
       │                                         │─────────── welcome ─────────────▶│
       │                                         │            (fleet, manifests,    │
       │                                         │             availableExtensions) │
       │                                         │                                  │
       │────── VehicleTelemetry ────────────────▶│─────────── telemetry ───────────▶│
       │────── Heartbeat (capabilities) ────────▶│─────────── heartbeat ───────────▶│
       │                                         │                                  │
       │◀───── Command ──────────────────────────│◀────────── command ──────────────│
       │────── CommandAck ──────────────────────▶│─────────── command_ack ─────────▶│
```

- Client MUST send `hello` as first message
- Server responds with `welcome` containing full fleet state, available extensions, and manifests
- Server broadcasts subsequent vehicle `heartbeat` frames to all handshaked UI clients, including live capability updates
- Vehicles do NOT receive the `welcome` message — it's UI-only
- `ServerHeartbeat` is defined for future vehicle-side loss detection, but the current server does not broadcast it yet

---

## Client Reconnection

On disconnect (network drop, server restart, client refresh), the client MUST:

1. **Reconnect** via new WebSocket connection
2. **Send `hello`** to re-handshake
3. **Replace local fleet state** with the `welcome` snapshot — **do NOT merge**

### Why Replace, Not Merge?

| Scenario | Merge Behavior (WRONG) | Replace Behavior (CORRECT) |
|----------|------------------------|---------------------------|
| Vehicle removed from fleet while disconnected | Ghost vehicle persists locally | Vehicle disappears |
| Vehicle status changed during disconnect | Stale status shown | Correct status from snapshot |
| Server restarted, registry cleared | Client shows vehicles server doesn't know | Clean slate |

**Implementation hint:**
```typescript
function onWelcome(msg: WelcomeMessage) {
  // WRONG: vehicles = { ...vehicles, ...msg.data.fleet }
  // RIGHT:
  vehicles = {};
  for (const v of msg.data.fleet) {
    vehicles[v.id] = v;
  }
}
```

### Partial State During Reconnect

Between disconnect and receiving `welcome`, the UI SHOULD:
- Show a "reconnecting" indicator
- Disable command buttons (commands would fail anyway)
- Optionally gray out vehicle markers to indicate stale data

---

## Message Types

### Server → UI

| Type | Frequency | Droppable | Description |
|------|-----------|-----------|-------------|
| `telemetry` | 10-100Hz | Yes | Position, velocity, heading, battery |
| `status` | On change | No | Online/offline/standby transitions |
| `heartbeat` | 1Hz | Yes | Connection health |
| `command_ack` | On demand | No | Command response |
| `alert` | On demand | No | Warnings, errors, geofence |
| `fleet_status` | On demand | No | Fleet summary |
| `welcome` | Once | No | Handshake response |
| `error` | On demand | No | Protocol/system error |

### UI → Server

| Type | Description |
|------|-------------|
| `hello` | Handshake (required first message) |
| `command` | Vehicle command (goto, stop, return_home, set_mode, set_speed) |

---

## Behavioral Contracts

### Message Ordering & Deduplication

- **Telemetry**: May arrive out-of-order (UDP). Use `seq` (sequence number) for ordering, NOT timestamp.
  - `seq` is monotonic per-vehicle, wraps at 2^32
  - Vehicle timestamps (`ts`) are **untrusted** — use for display only
  - Server timestamps (`gts`) are authoritative for cross-vehicle correlation
- **Commands**: Processed in order received.
- **Status/Acks**: Delivered reliably in order (WebSocket).

#### Server Deduplication (Sequence Tracking)

The server maintains a **high-water mark (HWM)** per vehicle for sequence numbers:

| Condition | Action | Reason |
|-----------|--------|--------|
| First message from vehicle | Accept, set HWM = seq | Initialize tracking |
| `seq > HWM` | Accept, update HWM = seq | Normal forward progress |
| `seq <= HWM` | **DROP** | Duplicate or stale (radio retransmit, reordering) |
| `seq` wraps (near 2^32 → 0) | Accept if `seq` is "after" HWM | Handle uint32 wrap-around |

**Wrap-around detection**: Uses signed difference — if `(seq - hwm)` as `int32` is positive, `seq` comes after `hwm`.

```
Example wrap-around:
  HWM = 0xFFFFFFF0 (near max)
  seq = 5 (wrapped)
  diff = 5 - 0xFFFFFFF0 = 0x00000015 (as int32: +21)
  Result: Accept (5 comes after 0xFFFFFFF0 in sequence space)
```

**Implementation**: See [`internal/protocol/sequence.go`](../internal/protocol/sequence.go)

> **Why untrusted timestamps?** Vehicles often lack RTC batteries, NTP connectivity, or stable GPS fix at boot. 
> Assume vehicle clocks can be hours or days off. The server's local clock is the single source of truth.

#### Vehicle Reboot Detection (Registry ↔ Sequence Tracker Contract)

**Problem:** When a vehicle reboots, its sequence number resets to 0. If the server's high-water mark (HWM) is still at the pre-reboot value (e.g., 50000), all new telemetry with `seq < 50000` is silently dropped as "stale."

**Solution:** The vehicle registry MUST call `SequenceTracker.Reset(vehicleID)` when a vehicle transitions from `offline` → `online`.

```
Timeline:
  1. Vehicle sending telemetry (seq=50000, HWM=50000)
  2. Vehicle goes silent for >10s → Registry marks OFFLINE
  3. Vehicle reboots, starts sending (seq=0)
  4. First telemetry arrives → Registry detects offline→online transition
  5. Registry calls SequenceTracker.Reset("ugv-husky-07")  ← CRITICAL
  6. SequenceTracker accepts seq=0, sets HWM=0
  7. Normal operation resumes
```

**Integration point:** [`internal/registry/registry.go`](../internal/registry/registry.go) owns status transitions and MUST hold a reference to the `SequenceTracker`. On any `offline → online` transition:

```go
// In registry.go, when processing telemetry for an offline vehicle:
if vehicle.Status == StatusOffline {
    r.sequenceTracker.Reset(vehicleID)  // Allow any seq number
    vehicle.Status = StatusOnline
    r.emitStatusChange(vehicleID, StatusOnline)
}
```

**Why not auto-detect reboot via large seq regression?** A vehicle might legitimately have `seq` jump backward due to:
- Wrap-around (handled separately)
- Multi-node vehicle with unsynchronized seq counters
- Replay attack (security concern for future)

Tying reset to the offline→online transition is unambiguous and matches the physical reality of "vehicle went away and came back."

#### UI Staleness Detection (Client Responsibility)

The server does **NOT** filter telemetry by timestamp due to clock skew risk. Instead, the UI MUST detect stale data using sequence gaps:

| Condition | Meaning | UI Action |
|-----------|---------|-----------|
| `seq` increments by 1 | Normal | Display telemetry |
| `seq` gap > 1 | Packet loss | Display telemetry, optionally warn operator |
| `seq` gap > 100 | Major backlog or partition recovery | Consider discarding or showing "recovering" state |
| No telemetry for `gts` > 3s | Standby/offline | Use status state machine (server handles this) |

**Why not filter at server?** Vehicle timestamps are untrusted. If the server rejected messages where `gts - ts > threshold`, a vehicle with a mis-set clock would have all telemetry dropped. The sequence number is the only reliable ordering mechanism.

**UI implementation hint:**
```typescript
// Track last seq per vehicle
if (msg.data.seq - lastSeq[msg.vid] > 100) {
  console.warn(`Large seq gap for ${msg.vid}: ${msg.data.seq - lastSeq[msg.vid]}`);
  // Optionally: show "recovering" indicator, don't animate position jumps
}
lastSeq[msg.vid] = msg.data.seq;
```

### Protocol Versioning

- All messages include `v` field (currently `1`)
- Minor changes (new optional fields): No version bump
- Breaking changes: Increment version
- Server supports current + one prior version
- Clients ignore unknown fields

### Transport Profiles (Planned)

If future deployments need to optimize for **spotty links**, **bandwidth caps**, or **smaller protobuf frames**, the recommended path is to add a **transport profile** rather than mutating the core Pidgin object model.

**Design rule:** Keep the semantic contract stable (`VehicleTelemetry`, `Heartbeat`, `Command`, `CommandAck`, extension manifests/capabilities). Optimize how often fields are sent and, only if necessary, add a profile-specific compact representation that the server immediately translates back into the canonical model.

#### Why a Transport Profile Instead of Ad-Hoc Shrinking?

Ad-hoc changes tend to leak network concerns into every message definition:

| Bad Pattern | Why It Causes Long-Term Damage |
|-------------|--------------------------------|
| Renaming/removing fields to save bytes | Breaks tooling, tests, and downstream integrations |
| Using extensions to encode link behavior | Conflates domain extensibility with transport concerns |
| Compressing every UDP datagram by default | Adds CPU/latency cost and makes loss recovery worse for small packets |
| Forking a separate "low-bandwidth protocol" | Creates two semantic contracts to maintain forever |

The safer approach is:

1. Keep one canonical protocol model
2. Negotiate a link profile per session or deployment
3. Let the server normalize constrained-link input back to the canonical JSON/UI model

#### Recommended Profiles

| Profile | Intended Network | Contract |
|---------|------------------|----------|
| `default` | Healthy LAN / wired multicast | Current behavior |
| `constrained` | Lossy WiFi, low-bitrate radios, intermittent links | Same semantics, stricter emission rules |

The `constrained` profile is the recommended answer to a future "make it work on bad links" ask.

#### `constrained` Profile: Phase 1 (No New Proto Messages)

Start with sender behavior changes before introducing any new wire shape.

**Emission rules** means sending different classes of data at different frequencies instead of lowering the rate for everything equally.

**Example constrained-link rule:**

- Send motion-critical telemetry on every fast frame: `vehicle_id`, `sequence_num`, `location`, `speed_ms`, `heading_deg`.
- Send sticky state only when it changes, plus a low-rate refresh: `battery_pct`, `signal_strength`, `status`, `environment`.
- Send capability metadata on heartbeat only: supported commands, extension capabilities, sensor inventory.
- Send rich extension payloads on change or at a lower rate than core motion telemetry.

**Example:** if a vehicle normally emits telemetry at 20 Hz, the constrained profile could send:

- At 20 Hz: `vehicle_id`, `sequence_num`, `location`, `speed_ms`, `heading_deg`
- At 1 Hz or on change: `battery_pct`, `signal_strength`, `status`, `environment`
- On heartbeat only: `Heartbeat.capabilities`
- At 1 Hz or on change: extension telemetry in the `extensions` map

**Normative guidance for constrained links:**

- Vehicles SHOULD send only motion-critical telemetry on the fast path.
- Vehicles SHOULD move capability advertisement to `Heartbeat.capabilities` only.
- Vehicles SHOULD send sticky fields only on change, plus a periodic refresh to heal packet loss.
- Vehicles SHOULD send extension telemetry at a lower rate than core motion telemetry unless the extension is mission-critical.
- The server MUST continue translating received data into the same canonical UI JSON shape.

This profile avoids wire breakage and usually captures most of the bandwidth win, because the largest recurring costs are repeated strings and extension maps, not protobuf tag overhead.

#### `constrained` Profile: Phase 2 (If Phase 1 Is Not Enough)

If emission rules are still insufficient, add a **profile-gated compact frame** in a future protocol version rather than rewriting existing messages.

Recommended constraints for that future work:

- Add a new compact telemetry payload alongside existing `VehicleTelemetry`; do not redefine the meaning of existing fields.
- Keep `Command`, `CommandAck`, and error semantics unchanged.
- Make compact frames server-facing only; the server rehydrates them into canonical telemetry before forwarding to UI clients.
- Require periodic full snapshots so a receiver can recover after packet loss without an out-of-band reset.
- Negotiate profile/version explicitly; never infer compact-mode from missing fields.

This keeps the complexity at the protocol edge instead of forcing every UI/tooling consumer to understand multiple partial representations.

#### What Not to Optimize First

If asked to reduce wire size, do **not** start with these:

- Per-packet compression of UDP datagrams
- Bit-packing booleans into custom binary formats
- Removing `vehicle_id` or other identity fields from the canonical contract
- Making the extension system responsible for bandwidth adaptation

Those changes buy less than expected early, while making debugging and future compatibility materially worse.

#### Presentable Recommendation

If this comes up after release, the recommended answer is:

> Tower will keep one stable semantic protocol. For degraded networks, we will introduce a negotiated transport profile called `constrained` that first reduces bandwidth through emission rules and lower-rate extension state, and only if needed adds a compact server-facing telemetry frame in a future version. The server will normalize that back into the existing canonical model so the UI and tooling do not fork.

#### Version Negotiation (Hello/Welcome)

The `hello` → `welcome` handshake performs version negotiation:

1. Client sends `hello` with `data.protocolVersion` set to the version it wants
2. Server checks if that version is in its `SupportedVersions` list
3. If supported: Server responds with `welcome`, using that version for the session
4. If unsupported: Server sends `error` with `PROTOCOL_VERSION_UNSUPPORTED` and the list of supported versions

**Welcome message includes:**
```json
{
  "protocolVersion": 1,
  "type": "welcome",
  "vehicleId": "_server",
  "timestampMs": 1710700800000,
  "data": {
    "serverVersion": "0.1.0",
    "protocolVersion": 1,
    "supportedVersions": [1],
    "fleet": [...],
    "config": {...},
    "availableExtensions": [
      { "namespace": "husky", "version": 1 },
      { "namespace": "camera", "version": 1 }
    ],
    "manifests": {
      "husky": {
        "namespace": "husky",
        "version": "1.0",
        "displayName": "Husky UGV Controls",
        "commands": [
          { "action": "setDriveMode", "label": "Set Drive Mode" },
          { "action": "triggerEStop", "label": "Trigger E-Stop", "confirmation": true }
        ]
      }
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `protocolVersion` | The negotiated version for this session |
| `supportedVersions` | All versions this server can speak (for client diagnostics) |
| `availableExtensions` | Extensions the server has codecs for (namespace + version) |
| `manifests` | Extension manifests with UI metadata (commands, labels, confirmations) |

**Client implementation hint:** If you receive `PROTOCOL_VERSION_UNSUPPORTED`, check `supportedVersions` in the error payload and downgrade if possible.

### Rate Limiting

Commands throttled to **10/second per vehicle**. This limit is **global per vehicle**, shared across all connected clients — if Client A sends 6 commands and Client B sends 5 commands to the same vehicle within 1 second, the 11th is rejected regardless of which client sent it. This protects the vehicle's radio link and processor from being overwhelmed.

Excess commands are rejected with an error frame sent back to the UI.

```
UI ── command (1st-10th) ──▶ Server ── multicast ──▶ Vehicle
UI ◀── command_ack {status: "accepted"} ──┘

UI ── command (11th in 1 sec) ──▶ Server
UI ◀── error {code: "RATE_LIMITED", message: "..."} ──┘
       (command NOT broadcast to vehicle)
```

**Error frame format:**
```json
{
  "protocolVersion": 1,
  "type": "error",
  "vehicleId": "_server",
  "timestampMs": 1710700800000,
  "serverTimestampMs": 1710700800000,
  "data": {
    "code": "RATE_LIMITED",
    "message": "Command rate limit exceeded for ugv-husky-07 (10/sec)",
    "commandId": "abc123"
  }
}
```

The UI SHOULD display this error to the operator so they know the command was not sent.

### Idempotency

- Commands include `commandId` for deduplication
- Vehicles filter by `vehicle_id` from multicast
- UI generates unique `commandId` per action (UUID)

---

## Vehicle Status State Machine

```
                    telemetry
                  ┌───────────┐
                  ▼           │
┌─────────┐   telemetry   ┌─────────┐
│ OFFLINE │──────────────▶│ ONLINE  │◀─── [first telemetry from new vehicle]
└─────────┘               └─────────┘
     ▲                         │
     │ 10s no telemetry        │ 3s no telemetry
     │                         ▼
     │                    ┌─────────┐
     └────────────────────│ STANDBY │
                          └─────────┘
```

**Initial state:** When the server receives the first telemetry from a previously-unknown vehicle, it creates that vehicle in the `ONLINE` state. There is no explicit "first seen" state.

Configurable via:
```bash
TOWER_STANDBY_TIMEOUT=3s
TOWER_OFFLINE_TIMEOUT=10s
```

### Heartbeat Interval

Vehicles SHOULD send heartbeat every **1 second ± 100ms** when not actively sending telemetry.
Telemetry packets reset the heartbeat timer (no need to send both simultaneously).

---

## Server Loss Detection (Vehicle-Side)

> **STATUS: NOT IMPLEMENTED** — This section describes planned behavior. The `ServerHeartbeat` message is not yet defined in `pidgin.proto` and the server does not yet broadcast heartbeats. Vehicle-side failsafe is the vehicle's responsibility for MVP.

Vehicles SHOULD detect server connectivity loss and trigger appropriate failsafe behavior. The server will broadcast `ServerHeartbeat` on multicast `239.255.0.2:14551` every **1 second** once implemented.

### Detection

| Condition | Action |
|-----------|--------|
| No `ServerHeartbeat` received for `TOWER_HEARTBEAT_TIMEOUT` (default 5s) | Enter failsafe mode |
| `ServerHeartbeat` received after failsafe | Resume normal operation |
| `ServerHeartbeat.sequence_num` gap > 3 | Log warning (packet loss), continue |

### Failsafe Behaviors

Vehicles MUST implement one of the following failsafe modes, configured via `TOWER_FAILSAFE_MODE`:

| Mode | Behavior | Use Case |
|------|----------|----------|
| `RTL` | Return to home/launch position | Default for most vehicles |
| `HOLD` | Stop and hold current position | Ground vehicles, close-range ops |
| `CONTINUE` | Continue current mission autonomously | Pre-planned autonomous missions |
| `LAND` | Immediate landing (aerial only) | Low-battery or critical ops |

**Default:** `RTL` for aerial vehicles, `HOLD` for ground/marine vehicles.

### State Diagram (Vehicle Perspective)

```
                          ServerHeartbeat
                        ┌─────────────────┐
                        ▼                 │
┌────────────────┐  heartbeat rx   ┌──────────────┐
│ SERVER_LOST   │◀────────────────│ CONNECTED    │
│ (failsafe mode)│                 │              │
└────────────────┘                 └──────────────┘
        │                                 ▲
        │ ServerHeartbeat rx             │
        └─────────────────────────────────┘
              (resume normal ops)
```

### Configuration

```bash
# Vehicle-side environment variables
TOWER_HEARTBEAT_TIMEOUT=5s      # Time without heartbeat before failsafe
TOWER_FAILSAFE_MODE=RTL       # RTL, HOLD, CONTINUE, or LAND
```

### Implementation Notes

1. **Hysteresis**: Vehicles SHOULD require 2-3 consecutive heartbeats before exiting failsafe to avoid flapping on unreliable links.

2. **Logging**: Vehicles MUST log server loss/recovery events for post-mission analysis.

3. **Alert**: On server loss, vehicles SHOULD queue an `Alert` message (severity `WARNING`, code `SERVER_LOST`) to be sent when connectivity resumes.

4. **No Time Sync**: Vehicles SHOULD NOT use `ServerHeartbeat.timestamp_ms` for clock sync — the server clock is authoritative for the UI only, not for vehicle autonomy.

---

## Command Delivery

```
UI ── command ──▶ Server ── multicast 239.255.0.2 ──▶ All Radio Nodes
                     │                                    (filter by vid)
UI ◀── ack ─────────┘ (immediate)
```

### Server ACK (Immediate)

| Ack Status | Meaning |
|------------|---------|
| `accepted` | Broadcast successful |
| `rejected` | Invalid command or vehicle not in registry |
| `failed` | Network error |

### Vehicle ACK (Async, Best-Effort)

Vehicles MAY send `CommandAck` back via the `VehicleMessage` envelope. The server:
1. Tracks pending commands per-vehicle with `TOWER_CMD_TIMEOUT` (default 5s)
2. If vehicle ACK received within timeout → forward to UI
3. If timeout expires → send synthetic `command_ack` with `status: "timeout"` to UI

```
UI ── command ──────────────────────▶ Server ── multicast ──▶ Vehicle
UI ◀── command_ack {accepted} ──────┘ (immediate)

... 5 seconds pass, no vehicle response ...

UI ◀── command_ack {timeout} ───────┘ (synthetic, from server)
```

**Timeout ack format:**
```json
{
  "protocolVersion": 1,
  "type": "command_ack",
  "vehicleId": "ugv-husky-07",
  "timestampMs": 1710700805000,
  "serverTimestampMs": 1710700805000,
  "data": {
    "commandId": "abc123",
    "status": "timeout",
    "message": "No response from vehicle within 5s"
  }
}
```

The UI SHOULD treat `timeout` as "command may or may not have been received" — distinct from `rejected` (definitely not sent) or `failed` (definitely not executed).

### Command Lifecycle States

| Status | Source | Meaning | UI Action |
|--------|--------|---------|------------|
| `accepted` | Server | Command broadcast to network | Show pending indicator |
| `rejected` | Server | Invalid command or vehicle | Show error, command not sent |
| `timeout` | Server | No vehicle response within timeout | Show warning, outcome unknown |
| `accepted` | Vehicle | Vehicle received and will execute | Update to "executing" |
| `completed` | Vehicle | Action finished successfully | Clear pending, show success |
| `failed` | Vehicle | Execution failed | Show error with message |

### Completion Status

`ACK_COMPLETED` comes from vehicle when the action finishes (e.g., arrived at destination).
This may arrive seconds/minutes after the initial `ACK_ACCEPTED`.

### Protobuf ↔ JSON Status Mapping

The server translates between protobuf enum values and JSON strings. This table is the source of truth:

| Protobuf Enum (`AckStatus`) | JSON `status` String | Notes |
|-----------------------------|----------------------|-------|
| `ACK_ACCEPTED` (0) | `"accepted"` | Vehicle acknowledged receipt |
| `ACK_REJECTED` (1) | `"rejected"` | Vehicle refused command |
| `ACK_COMPLETED` (2) | `"completed"` | Action finished successfully |
| `ACK_FAILED` (3) | `"failed"` | Execution failed |
| *(synthetic)* | `"timeout"` | Server-generated when vehicle doesn't respond |

**Implementation**: See [`internal/protocol/translate.go`](../internal/protocol/translate.go) for the mapping function.

**Note**: The `timeout` status has no protobuf equivalent — it's synthesized by the server when `TOWER_CMD_TIMEOUT` expires without a vehicle ack.

### Field Validation: ENV_UNKNOWN

`VehicleEnvironment.ENV_UNKNOWN` (protobuf value `0`) is **allowed but logged**.

| Behavior | Rationale |
|----------|----------|
| Accept telemetry | Graceful degradation > hard failure |
| Log warning (rate-limited) | Catch misconfigurations during development |
| Translate to `"unknown"` in JSON | UI can display generic icon |

**Why not reject?**
- Proto3 semantics: unset enum fields default to `0`. Rejecting would cause silent failures.
- Sensors fail. A vehicle with a malfunctioning environment sensor should still report position.
- New platforms being tested may not have environment classification yet.

**UI guidance**: Display vehicles with `"unknown"` environment using a generic icon. Optionally show a subtle indicator that the environment is unclassified.

**Implementation**: See [`internal/protocol/validate.go`](../internal/protocol/validate.go) — warning logged via `warnEnvUnknown()`.

---

## Vehicle Discovery

**Dynamic**: Server creates registry entry on first telemetry, emits `status: online`.

**Static** (optional): `vehicles.yaml` pre-configures known fleet with display names.

---

## Vehicle ID Convention

```
{type}-{platform}-{identifier}
```

| Type | Example |
|------|---------|
| `ugv` | `ugv-husky-07` |
| `uav` | `uav-skydio-x2d-03` |
| `usv` | `usv-wam-v-02` |
| `uuv` | `uuv-bluefin-01` |

Special: `_server`, `_fleet`, `_client`

---

## Timestamps

- Unix epoch **milliseconds** (not seconds)
- int64 wire format

### Vehicle Timestamps (`ts` from vehicle)

**UNTRUSTED.** Vehicle clocks may be wrong (no RTC, no NTP, GPS cold start). Use for:
- Relative ordering within a single vehicle session (combined with `seq`)
- Display/logging only

**Never use for:**
- Cross-vehicle correlation
- Absolute time calculations
- Timeout/expiry logic

### Server Timestamps (`serverTimestampMs`)

The server stamps each frame with its local time on receipt. This is the **authoritative timestamp** for:
- Cross-vehicle event correlation
- Replay/logging with accurate wall-clock time
- Latency measurements

```json
{"protocolVersion":1, "type":"telemetry", "vehicleId":"ugv-husky-07", "timestampMs":1710700800000, "serverTimestampMs":1710700800123, "data":{...}}
```

| Field | Source | Trust Level | Use For |
|-------|--------|-------------|---------|
| `timestampMs` | Vehicle | Untrusted | Display, per-vehicle ordering |
| `serverTimestampMs` | Server | Authoritative | Correlation, logging, latency |

---

## Error Codes

| Code | Cause |
|------|-------|
| `INVALID_MESSAGE` | Malformed JSON |
| `UNKNOWN_COMMAND` | Unknown action |
| `VEHICLE_NOT_FOUND` | Vehicle not in registry |
| `RATE_LIMITED` | Too many commands |
| `PROTOCOL_VERSION_UNSUPPORTED` | Version mismatch |
| `COMMAND_SEND_FAILED` | Multicast network error |

---

## Implementation Reference

| File | Purpose |
|------|---------|
| [`internal/protocol/frame.go`](../internal/protocol/frame.go) | JSON wire types |
| [`internal/protocol/translate.go`](../internal/protocol/translate.go) | Proto ↔ JSON translation |
| [`internal/protocol/validate.go`](../internal/protocol/validate.go) | Message validation |
| [`internal/protocol/builders.go`](../internal/protocol/builders.go) | Frame constructors |
| [`internal/protocol/sequence.go`](../internal/protocol/sequence.go) | Sequence tracking & deduplication |
| [`api/proto/pidgin.proto`](../api/proto/pidgin.proto) | Protobuf schema |
