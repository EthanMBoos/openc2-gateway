# Gateway Development Guide

How to build, run, test, and simulate vehicles against the OpenC2 gateway.

---

## Prerequisites

- Go 1.23+
- A multicast-capable network interface (loopback works for local dev)

```bash
go version   # should be 1.23+
```

---

## Build & Run

```bash
# Run the gateway (defaults: WS on :9000, multicast on 239.255.0.1:14550)
go run ./cmd/gateway

# Build a binary
go build -o gateway ./cmd/gateway

# Build with version injected
go build -ldflags "-X main.Version=1.0.0" -o gateway ./cmd/gateway
```

The gateway exposes two endpoints on startup:
- `ws://localhost:9000` — WebSocket endpoint for UI clients
- `http://localhost:9000/healthz` — Health check (returns `ok`)
- `http://localhost:9000/metrics` — Prometheus metrics

---

## Configuration

All configuration is via environment variables. Unset variables use defaults.

| Variable | Default | Description |
|---|---|---|
| `OPENC2_WS_PORT` | `9000` | WebSocket server port |
| `OPENC2_MCAST_GROUP` | `239.255.0.1` | Multicast group for inbound telemetry |
| `OPENC2_MCAST_PORT` | `14550` | UDP port for inbound telemetry |
| `OPENC2_CMD_MCAST_GROUP` | `239.255.0.2` | Multicast group for outbound commands |
| `OPENC2_CMD_MCAST_PORT` | `14551` | UDP port for outbound commands |
| `OPENC2_STANDBY_TIMEOUT` | `3s` | Time with no telemetry before vehicle goes standby |
| `OPENC2_OFFLINE_TIMEOUT` | `10s` | Time with no telemetry before vehicle goes offline |
| `OPENC2_CMD_TIMEOUT` | `5s` | Time to wait for a command ACK |
| `OPENC2_CMD_RATE_LIMIT` | `10` | Max commands per second per vehicle |

`OPENC2_OFFLINE_TIMEOUT` must be greater than `OPENC2_STANDBY_TIMEOUT` — the gateway will reject invalid combinations at startup.

Example:
```bash
OPENC2_WS_PORT=8080 OPENC2_STANDBY_TIMEOUT=5s go run ./cmd/gateway
```

---

## Simulating Vehicles (`testsender`)

`testsender` broadcasts mock vehicle telemetry via UDP multicast, simulating a real robot. Run it alongside the gateway to develop and test without hardware.

```bash
# Basic UGV (ground vehicle)
go run ./cmd/testsender -vid ugv-husky-07

# UAV at 20Hz
go run ./cmd/testsender -vid uav-quad-01 -env air -rate 20

# Surface vehicle
go run ./cmd/testsender -vid usv-boat-01 -env surface

# Observation-only vehicle (no commands accepted)
go run ./cmd/testsender -vid sensor-01 -caps none

# Vehicle that accepts goto but not stop
go run ./cmd/testsender -vid ugv-custom -caps no-stop
```

**Flags:**

| Flag | Default | Options |
|---|---|---|
| `-vid` | `ugv-test-01` | Any string — used as vehicle ID |
| `-env` | `ground` | `ground`, `air`, `surface`, `subsurface` |
| `-group` | `239.255.0.1` | Multicast group to send on |
| `-port` | `14550` | UDP port to send on |
| `-rate` | `10` | Telemetry Hz |
| `-caps` | `all` | `all`, `no-stop`, `no-goto`, `none` |

---

## Connecting a Test Client (`testclient`)

`testclient` connects to the gateway via WebSocket, sends a hello, and reads telemetry frames. Useful for verifying the full pipeline.

```bash
# Read 5 frames then exit
go run ./cmd/testclient

# Stay connected for 30 seconds
go run ./cmd/testclient -duration 30s

# Test error handling: bad protocol version
go run ./cmd/testclient -bad-version

# Test error handling: skip hello frame
go run ./cmd/testclient -skip-hello
```

`testclient` connects to `ws://localhost:9000` by default.

---

## Full Demo (Multiple Vehicles)

The demo script starts the gateway and several simulated vehicles in one command:

```bash
# 3 vehicles (default)
./scripts/demo.sh

# 5 vehicles
./scripts/demo.sh 5
```

Ctrl+C stops everything. Then connect hotpaths:
```bash
go run ./cmd/testclient -duration 60s
```

---

## Testing

```bash
# Run all unit tests
go test ./...

# Run with verbose output
go test -v ./...

# Run a specific package
go test ./internal/protocol/...
go test ./internal/command/...
go test ./internal/registry/...

# Run with race detector (recommended before committing)
go test -race ./...
```

Tests live alongside source in `_test.go` files. There are no external test dependencies — all tests use the standard library only.

---

## Regenerating Protobuf

If you modify `api/proto/openc2.proto`:

```bash
# Install protoc-gen-go if needed
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

# Regenerate
protoc --go_out=. --go_opt=paths=source_relative api/proto/openc2.proto
```

The generated file is `api/proto/openc2.pb.go` — commit it alongside the `.proto` source.

---

## Connecting the OpenC2 UI

> **STATUS: NOT YET IMPLEMENTED** — Gateway/UI integration is in progress. This section will be updated once the connection is wired up.

The UI lives in the sibling `OpenC2/` repo and will connect to the gateway WebSocket. See the UI's `DEVELOPMENT.md` for its current setup steps.
