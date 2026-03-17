# OpenC2-Gateway

Communications gateway for robotic fleets. Bridges commands and telemetry between vehicles and the [OpenC2](https://github.com/EthanMBoos/OpenC2) UI.

```
┌─────────────────┐     WebSocket      ┌─────────────────┐    UDP multicast     ┌─────────────────┐
│   OpenC2 UI     │◀──────────────────▶│  openc2-gateway │◀────────────────────▶│  Radio Nodes    │
│   (Electron)    │   localhost:9000   │                 │   239.255.0.1:14550  │  (on vehicles)  │
└─────────────────┘                    └─────────────────┘                      └─────────────────┘
```

## Quick Start

```bash
# Clone and install
git clone https://github.com/EthanMBoos/openc2-gateway.git
cd openc2-gateway
go mod download

# Run gateway with simulated vehicles (demo mode)
./scripts/demo.sh

# Or run manually:
go run ./cmd/gateway &
go run ./cmd/testsender -vid ugv-test-01 &

# Test connection
go run ./cmd/testclient
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OPENC2_WS_PORT` | `9000` | WebSocket server port |
| `OPENC2_MCAST_GROUP` | `239.255.0.1` | Telemetry multicast group |
| `OPENC2_MCAST_PORT` | `14550` | Telemetry multicast port |
| `OPENC2_CMD_MCAST_GROUP` | `239.255.0.2` | Command multicast group |
| `OPENC2_CMD_MCAST_PORT` | `14551` | Command multicast port |
| `OPENC2_MAX_CLIENTS` | `4` | Max WebSocket clients |
| `OPENC2_STANDBY_TIMEOUT` | `3s` | Time before vehicle marked standby |
| `OPENC2_OFFLINE_TIMEOUT` | `10s` | Time before vehicle marked offline |
| `OPENC2_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |

## Documentation

- [PROTOCOL.md](docs/PROTOCOL.md) — Message format specification
- [GATEWAY_IMPLEMENTATION.md](docs/GATEWAY_IMPLEMENTATION.md) — Implementation guide

## License

[MIT](LICENSE)
