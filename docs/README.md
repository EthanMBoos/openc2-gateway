# OpenC2 Gateway Documentation Index

> Start here to quickly understand the openc2-gateway codebase.

---

## Quick Reference

| Document | Purpose |
|----------|---------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | System topology, platform philosophy, deployment models |
| [PROTOCOL.md](PROTOCOL.md) | Wire format, behavioral contracts, message types |
| [GLOSSARY.md](GLOSSARY.md) | Key term definitions (`seq`, `gts`, `HWM`, naming conventions) |
| [DEVELOPMENT.md](DEVELOPMENT.md) | Build, run, test, simulate vehicles |
| [PACKAGE_MAP.md](PACKAGE_MAP.md) | What each `internal/` package owns and does |
| [EXTENSIBILITY.md](EXTENSIBILITY.md) | Extension codec/manifest spec, wire format |
| [ADDING_A_VEHICLE.md](ADDING_A_VEHICLE.md) | Step-by-step guide for new vehicle/protocol integration |
| [GATEWAY_IMPLEMENTATION.md](GATEWAY_IMPLEMENTATION.md) | Build phases, testing procedures |
| [DEBUG_OBSERVABILITY_ROADMAP.md](DEBUG_OBSERVABILITY_ROADMAP.md) | Planned observability features (not yet implemented) |
| [WHY_GO.md](WHY_GO.md) | Language choice justification |

---

## What is openc2-gateway?

**openc2-gateway** is a Go application that bridges robotic vehicles to the OpenC2 operator UI. It handles protocol translation, telemetry aggregation, and command routing.

> **Important Architecture Note**: The gateway is a **standalone process** — it has no dependency on the UI. Multiple UI clients can connect to a single gateway. Vehicles never communicate directly with the UI.

```
┌──────────────┐    UDP multicast    ┌──────────────┐    WebSocket     ┌──────────────┐
│  50+ Robots  │ ◀─────────────────▶ │   Gateway    │ ◀───────────────▶│  N Operator  │
│  10-100Hz    │   239.255.0.1:14550 │              │   localhost:9000 │     UIs      │
│  protobuf    │                     │              │   JSON frames    │              │
└──────────────┘                     └──────────────┘                  └──────────────┘
```

It provides four core capabilities:

1. **Protocol Translation** — Decodes protobuf from vehicles, encodes JSON for UIs
2. **Fleet Registry** — Tracks vehicle state (online/standby/offline) via telemetry gaps
3. **Command Routing** — Validates, rate-limits, and forwards commands to vehicles
4. **Extensibility** — Codec plugin system for custom vehicle protocols

---

## Key Technologies

| Category | Stack |
|----------|-------|
| **Language** | Go 1.23+ |
| **Transport (vehicles)** | UDP multicast, protobuf |
| **Transport (UI)** | WebSocket, JSON |
| **Dependencies** | gorilla/websocket, google/protobuf, golang.org/x/net |
| **Testing** | Standard library only |

---

## Project Structure (Key Paths)

```
openc2-gateway/
├── cmd/
│   ├── gateway/        # Main entry point
│   ├── testsender/     # Vehicle simulator
│   └── testclient/     # WebSocket test client
│
├── api/proto/
│   └── openc2.proto    # Core protocol schema
│
├── internal/
│   ├── config/         # Environment variable parsing
│   ├── protocol/       # Frame types, translation, validation, sequence tracking
│   ├── registry/       # Vehicle state machine (online/standby/offline)
│   ├── command/        # Command routing, rate limiting, ACK tracking
│   ├── telemetry/      # UDP multicast listener
│   ├── websocket/      # WebSocket server, client management
│   ├── extensions/     # Codec plugin registry
│   └── observability/  # Metrics, health endpoints
│
├── scripts/
│   └── demo.sh         # Multi-vehicle demo launcher
│
└── docs/               # You are here
```

---

## Architecture Highlights

### Sequence-Based Deduplication
Vehicles send monotonic sequence numbers (`seq`). The gateway tracks a high-water mark (HWM) per vehicle — any `seq ≤ HWM` is dropped as a duplicate or stale retransmit. This handles UDP packet reordering without relying on untrusted vehicle clocks.

### Untrusted Vehicle Timestamps
Vehicle clocks (`ts`) are treated as untrusted — no RTC, no NTP, GPS cold starts. The gateway adds its own authoritative timestamp (`gts`) when translating to JSON. Never filter telemetry by `ts`.

### Extension Codec System
Custom vehicle protocols implement the `Codec` interface. The gateway routes extension payloads to registered codecs by namespace, decodes to JSON, and forwards to the UI. Unknown extensions pass through with `_error` metadata for graceful degradation.

### Zero-Config Deployment
`CGO_ENABLED=0 go build` produces a single static binary (~13MB) that runs on any target without dependencies. No container runtime, no package manager, no library conflicts.

---

## Getting Started

```bash
# Run gateway + simulated vehicle
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-husky-01

# Connect test client
go run ./cmd/testclient
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for full setup instructions.
