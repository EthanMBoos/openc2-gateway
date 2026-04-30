# Cross-Repository Reference

> **Purpose**: Quick lookup for working across both Tower repositories.  
> **Canonical location**: `tower-server/docs/CROSS_REPO_REFERENCE.md` (mirrored to Tower)

---

## Repository Roles

| Repo | Language | Purpose |
|------|----------|---------|
| `tower-server` | Go | Protocol bridge: vehicles ↔ UI. Owns wire format. |
| `Tower` | TypeScript/Electron | Operator UI. Pure WebSocket client. |

---

## File Mapping

| Server (Go) | UI (TypeScript) | Notes |
|--------------|-----------------|-------|
| `api/proto/pidgin.proto` | — | Protobuf schema (vehicle↔server only) |
| `internal/protocol/frame.go` | `src/types/index.ts` | **SOURCE OF TRUTH** for JSON types |
| `internal/protocol/translate.go` | — | Proto→JSON conversion |
| `internal/protocol/builders.go` | — | Server-originated frames (welcome, error) |
| `internal/websocket/server.go` | `src/main/telemetryBridge.ts` | WebSocket endpoints |
| `internal/websocket/client.go` | — | Client connection handling |
| `internal/registry/registry.go` | `src/renderer/stores/vehicleStore.ts` | Fleet state management |
| `internal/extensions/*.go` | `src/types/index.ts` (ExtensionManifest) | Extension codec + manifest |
| `testdata/protocol/*.json` | `testdata/protocol/*.json` | **MUST BE IDENTICAL** |

---

## Data Flow

```
┌──────────┐    UDP multicast     ┌──────────┐    WebSocket     ┌──────────┐
│ Vehicle  │──────────────────────│ Server  │──────────────────│    UI    │
│ (proto)  │  239.255.0.1:14550   │  (Go)    │ localhost:9000   │ (TS/React)│
└──────────┘                      └──────────┘                  └──────────┘
```

### Inbound (Vehicle → UI)

```
1. Vehicle sends VehicleTelemetry (protobuf)
   └─▶ internal/telemetry/multicast.go:Start()

2. Server decodes protobuf
   └─▶ internal/protocol/translate.go:DecodeVehicleMessage()

3. Server validates & translates to JSON Frame
   └─▶ internal/protocol/translate.go:TelemetryToFrame()

4. Server broadcasts to all clients
   └─▶ internal/websocket/server.go:Broadcast()

5. UI main process receives JSON, buffers, batches
   └─▶ src/main/telemetryBridge.ts (NOT YET IMPLEMENTED)

6. UI renderer updates store
   └─▶ src/renderer/stores/vehicleStore.ts:updateInstance()
```

### Outbound (UI → Vehicle)

```
1. Operator clicks command button
   └─▶ src/renderer/components/ (CommandPanel)

2. UI sends JSON command frame
   └─▶ src/main/telemetryBridge.ts (NOT YET IMPLEMENTED)

3. Server validates, rate-limits, converts to protobuf
   └─▶ internal/command/router.go:Route()

4. Server broadcasts on vehicle multicast
   └─▶ UDP 239.255.0.2:14551
```

---

## Type Correspondence

### Frame Envelope

| Go (`frame.go`) | TypeScript (`types/index.ts`) | JSON Key |
|-----------------|-------------------------------|----------|
| `Frame.ProtocolVersion` | `ServerFrame.protocolVersion` | `protocolVersion` |
| `Frame.Type` | `ServerFrame.type` | `type` |
| `Frame.VehicleID` | `ServerFrame.vehicleId` | `vehicleId` |
| `Frame.TimestampMs` | `ServerFrame.timestampMs` | `timestampMs` |
| `Frame.ServerTimestampMs` | `ServerFrame.serverTimestampMs` | `serverTimestampMs` |
| `Frame.Data` | `ServerFrame.data` | `data` |

### Telemetry Payload

| Go | TypeScript | JSON Key |
|----|------------|----------|
| `TelemetryPayload.Location.Lat` | `location.lat` | `data.location.lat` |
| `TelemetryPayload.Location.Lng` | `location.lng` | `data.location.lng` |
| `TelemetryPayload.Speed` | `speed` | `data.speed` |
| `TelemetryPayload.Heading` | `heading` | `data.heading` |
| `TelemetryPayload.Seq` | `seq` | `data.seq` |
| `TelemetryPayload.BatteryPercent` | `batteryPct` | `data.batteryPct` |

### Status Values

| Concept | Go const | TypeScript | JSON value |
|---------|----------|------------|------------|
| Online | `StatusOnline` | `'online'` | `"online"` |
| Offline | `StatusOffline` | `'offline'` | `"offline"` |
| Standby | `StatusStandby` | `'standby'` | `"standby"` |

### Environment Values

| Concept | Protobuf | JSON |
|---------|----------|------|
| Aerial | `ENV_AIR` | `"air"` |
| Ground | `ENV_GROUND` | `"ground"` |
| Marine | `ENV_MARINE` | `"marine"` |

---

## Message Types

| Type | Direction | Go const | Droppable |
|------|-----------|----------|-----------|
| `telemetry` | Vehicle→UI | `TypeTelemetry` | Yes |
| `heartbeat` | Vehicle→UI | `TypeHeartbeat` | Yes |
| `status` | Server→UI | `TypeStatus` | No |
| `command_ack` | Vehicle→UI | `TypeCommandAck` | No |
| `alert` | Vehicle→UI | `TypeAlert` | No |
| `welcome` | Server→UI | `TypeWelcome` | No |
| `error` | Server→UI | `TypeError` | No |
| `hello` | UI→Server | `TypeHello` | No |
| `command` | UI→Vehicle | `TypeCommand` | No |

---

## Extension System

| Concept | Server Location | UI Location |
|---------|------------------|-------------|
| Codec registration | `internal/extensions/registry.go` | — |
| Manifest YAML | `internal/extensions/{name}/manifest.yaml` | — |
| Manifest types | `internal/extensions/manifest.go` | `ExtensionManifest` in types |
| Decoded telemetry | `extensions` map in Frame | `VehicleInstance.extensions` |
| Capability advertisement | `VehicleCapabilities.Extensions` | `VehicleCapabilities.extensions` |

---

## Configuration

| Setting | Server Env Var | Default |
|---------|-----------------|---------|
| WebSocket port | `TOWER_WS_PORT` | `9000` |
| Telemetry multicast | `TOWER_MCAST_SOURCES` | `239.255.0.1:14550` |
| Command multicast | `TOWER_CMD_MCAST_GROUP` | `239.255.0.2:14551` |
| Standby timeout | `TOWER_STANDBY_TIMEOUT` | `3s` |
| Offline timeout | `TOWER_OFFLINE_TIMEOUT` | `10s` |
| Command rate limit | `TOWER_CMD_RATE_LIMIT` | `10/sec/vehicle` |

---

## Test Data Contract

Files in `testdata/protocol/` **MUST be identical** across both repos:

```
testdata/protocol/
├── commands.json      # UI→Server command examples
├── heartbeat.json     # Vehicle heartbeat with capabilities
├── responses.json     # Command ack examples
├── telemetry.json     # Vehicle telemetry frame
└── welcome.json       # Server handshake response
```

**Validation**: Run `diff -r` between repos before release.

---

## MVP Implementation Gaps

| Component | Status | Location |
|-----------|--------|----------|
| Server WebSocket server | ✅ Done | `internal/websocket/` |
| Server telemetry listener | ✅ Done | `internal/telemetry/` |
| Server command routing | ✅ Done | `internal/command/` |
| UI WebSocket client | ❌ Stub only | `src/main/telemetryBridge.ts` |
| UI preload API | ❌ Stub only | `src/main/preload.ts` |
| UI telemetry store updates | ❌ Partial | `vehicleStore.ts` |
