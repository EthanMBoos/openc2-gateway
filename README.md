# Tower-Server

Communications server for robotic fleets. Bridges commands and telemetry between vehicles and the [Tower](https://github.com/EthanMBoos/Tower) UI.

```
┌─────────────────┐     WebSocket      ┌─────────────────┐    UDP multicast     ┌─────────────────┐
│      Tower      │◀──────────────────▶│  tower-server   │◀────────────────────▶│  Radio Nodes    │
│   (Electron)    │   localhost:9000   │                 │   239.255.0.1:14550  │  (on vehicles)  │
└─────────────────┘                    └─────────────────┘                      └─────────────────┘
```

## Quick Start

```bash
# Clone and install
git clone https://github.com/EthanMBoos/tower-server.git
cd tower-server
go mod download

# Run server with simulated vehicles (demo mode)
./scripts/demo.sh

# Or run manually:
go run ./cmd/tower-server &
go run ./cmd/testsender -vid ugv-test-01 &

# Test connection
go run ./cmd/testclient
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `TOWER_WS_PORT` | `9000` | WebSocket server port |
| `TOWER_MCAST_SOURCES` | `239.255.0.1:14550` | Telemetry multicast sources |
| `TOWER_CMD_MCAST_GROUP` | `239.255.0.2` | Command multicast group |
| `TOWER_CMD_MCAST_PORT` | `14551` | Command multicast port |
| `TOWER_MAX_CLIENTS` | `4` | Max WebSocket clients |
| `TOWER_STANDBY_TIMEOUT` | `3s` | Time before vehicle marked standby |
| `TOWER_OFFLINE_TIMEOUT` | `10s` | Time before vehicle marked offline |
| `TOWER_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |

**Multi-source example:**
```bash
TOWER_MCAST_SOURCES="239.255.0.1:14550:ugv,239.255.1.1:14551:usv" go run ./cmd/tower-server
```

## Documentation

- [PROTOCOL.md](docs/PROTOCOL.md) — Message format specification
- [SERVER_IMPLEMENTATION.md](docs/SERVER_IMPLEMENTATION.md) — Implementation guide

## License

[MIT](LICENSE)
