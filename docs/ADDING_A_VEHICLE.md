# Integrating a New Vehicle or Protocol

How to add support for a new robot type, vehicle platform, or custom telemetry protocol to the OpenC2 gateway.

There are two levels of integration depending on how different your vehicle is from the standard protocol:

1. **Standard vehicle** — your robot sends the core OpenC2 protobuf schema. You only need to configure `testsender` to simulate it and define capabilities. No code changes required.
2. **Custom protocol / extension** — your robot sends additional proprietary telemetry or accepts custom commands beyond the core set. You need to implement a `Codec` in `internal/extensions/`.

---

## Level 1: Standard Vehicle Integration

If your vehicle speaks the standard OpenC2 protobuf schema (`api/proto/openc2.proto`), it works with the gateway out of the box. The key decisions are:

### 1. Choose an environment

| Environment | Proto enum | `-env` flag |
|---|---|---|
| Ground robot (UGV) | `ENV_GROUND` | `ground` |
| Aerial vehicle (UAV) | `ENV_AIR` | `air` |
| Surface vessel (USV) | `ENV_SURFACE` | `surface` |

### 2. Define capabilities

Capabilities are advertised on every heartbeat. The gateway uses them to validate commands before forwarding — a command for an unsupported action is rejected at the gateway, not forwarded to the vehicle.

Core capability flags:

| Field | Description |
|---|---|
| `supported_commands` | List of command types: `goto`, `stop`, `return_home`, `set_mode`, `set_speed` |
| `supports_missions` | Whether the vehicle accepts waypoint mission sequences |
| `extensions` | Custom extension namespaces and their supported actions (see Level 2) |

### 3. Simulate with `testsender`

Use `testsender` to stand in for hardware during development:

```bash
# Standard UGV
go run ./cmd/testsender -vid ugv-myrobot-01 -env ground

# UAV with no stop command (e.g., fixed-wing that can't hover)
go run ./cmd/testsender -vid uav-fixedwing-01 -env air -caps no-stop

# Observation-only sensor platform (no commands)
go run ./cmd/testsender -vid sensor-mast-01 -env ground -caps none
```

The vehicle will appear in the UI's fleet view once the gateway receives the first heartbeat frame.

### 4. Implement the vehicle-side sender

Your vehicle firmware/software must:

1. Serialize telemetry as a `Telemetry` protobuf message (see `api/proto/openc2.proto`)
2. Send it via UDP multicast to `239.255.0.1:14550` (configurable via `OPENC2_MCAST_GROUP` / `OPENC2_MCAST_PORT`)
3. Listen for commands on `239.255.0.2:14551` (configurable via `OPENC2_CMD_MCAST_GROUP` / `OPENC2_CMD_MCAST_PORT`)
4. Send back a `CommandAck` frame when a command is received

Refer to `testsender` (`cmd/testsender/main.go`) as a working reference implementation.

---

## Level 2: Custom Protocol / Extension Codec

Use this when your vehicle sends proprietary data (drive mode, bumper contacts, depth readings, etc.) that doesn't fit the standard telemetry fields, or accepts custom commands beyond the core set.

### How extensions work

The `Telemetry` proto message has an `extensions` map (`map<string, ExtensionData>`). Each entry is a namespace key (e.g., `"husky"`) pointing to a versioned opaque byte payload. The gateway passes each payload to the registered `Codec` for that namespace, which decodes it to a JSON map that gets forwarded to the UI.

Commands work in reverse: the UI sends an `extension_command` frame specifying a namespace and action, the gateway looks up the codec, calls `EncodeCommand`, and forwards the serialized bytes to the vehicle.

### Step 1: Define your proto schema

Create `api/proto/<yournamespace>.proto`:

```proto
syntax = "proto3";
package openc2.<yournamespace>;
option go_package = "github.com/EthanMBoos/openc2-gateway/api/proto/<yournamespace>";

// Telemetry-specific message for your vehicle
message MyRobotTelemetry {
  string drive_mode = 1;        // e.g., "MANUAL", "AUTONOMOUS"
  bool e_stop_active = 2;
  float battery_voltage = 3;
  bool front_bumper_contact = 4;
  bool rear_bumper_contact = 5;
  // ...
}

// Command-specific messages
message SetDriveModeCommand {
  string mode = 1;  // "MANUAL" or "AUTONOMOUS"
}
```

Regenerate after editing:
```bash
protoc --go_out=. --go_opt=paths=source_relative api/proto/<yournamespace>/<yournamespace>.proto
```

### Step 2: Implement the `Codec` interface

Create `internal/extensions/<yournamespace>/codec.go`:

```go
package <yournamespace>

import (
    "fmt"

    "google.golang.org/protobuf/proto"

    "github.com/EthanMBoos/openc2-gateway/internal/extensions"
    pb "github.com/EthanMBoos/openc2-gateway/api/proto/<yournamespace>"
)

func init() {
    extensions.Register(&Codec{})
}

type Codec struct{}

func (c *Codec) Namespace() string { return "<yournamespace>" }

func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
    switch version {
    case 1:
        var msg pb.MyRobotTelemetry
        if err := proto.Unmarshal(data, &msg); err != nil {
            return nil, fmt.Errorf("unmarshal v1: %w", err)
        }
        return map[string]any{
            "drive_mode":           msg.DriveMode,
            "e_stop_active":        msg.EStopActive,
            "battery_voltage":      msg.BatteryVoltage,
            "front_bumper_contact": msg.FrontBumperContact,
            "rear_bumper_contact":  msg.RearBumperContact,
        }, nil
    default:
        return nil, fmt.Errorf("unsupported version: %d", version)
    }
}

func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
    switch action {
    case "setDriveMode":
        mode, ok := payload["mode"].(string)
        if !ok {
            return 0, nil, fmt.Errorf("setDriveMode: missing or invalid mode")
        }
        msg := &pb.SetDriveModeCommand{Mode: mode}
        data, err := proto.Marshal(msg)
        return 1, data, err
    default:
        return 0, nil, fmt.Errorf("unknown action: %q", action)
    }
}
```

**Version compatibility contract:**
- `DecodeTelemetry` must handle all versions ever shipped — old vehicles in the field may still be sending v1 after you ship v2.
- `EncodeCommand` always encodes at the latest version.
- Return an error for unknown versions; never silently corrupt or drop data.

### Step 3: Register the codec at startup

Import the package for its `init()` side effect in `cmd/gateway/main.go`:

```go
import (
    // ...
    _ "github.com/EthanMBoos/openc2-gateway/internal/extensions/<yournamespace>"
)
```

The `_` import triggers `init()`, which calls `extensions.Register()`. The gateway will now decode telemetry and route commands for your namespace automatically.

### Step 4: Advertise capabilities from the vehicle

Your vehicle's heartbeat must include extension capabilities so the gateway and UI know which custom commands are valid:

```proto
// In your Telemetry heartbeat:
capabilities: {
  supported_commands: ["goto", "stop"],
  extensions: [
    {
      namespace: "<yournamespace>",
      supported_actions: ["setDriveMode", "triggerEstop"]
    }
  ]
}
```

The gateway rejects extension commands for actions not listed here.

### Step 5: Simulate with `testsender`

`testsender` doesn't yet support custom extension payloads — use a minimal standalone test sender instead. Copy `cmd/testsender/main.go` as a starting point, add your extension payload to the `extensions` map of each `Telemetry` message, and run it alongside the gateway:

```bash
# In one terminal
go run ./cmd/gateway

# In another
go run ./cmd/<yournamespace>sender -vid myrobot-01
```

### Step 6: Write a unit test for the codec

Create `internal/extensions/<yournamespace>/codec_test.go`:

```go
package <yournamespace>

import (
    "testing"

    "google.golang.org/protobuf/proto"
    pb "github.com/EthanMBoos/openc2-gateway/api/proto/<yournamespace>"
)

func TestDecodeTelemetry(t *testing.T) {
    msg := &pb.MyRobotTelemetry{DriveMode: "AUTONOMOUS", EStopActive: false, BatteryVoltage: 25.6}
    data, _ := proto.Marshal(msg)

    c := &Codec{}
    got, err := c.DecodeTelemetry(1, data)
    if err != nil {
        t.Fatal(err)
    }
    if got["drive_mode"] != "AUTONOMOUS" {
        t.Errorf("drive_mode: got %v", got["drive_mode"])
    }
}

func TestEncodeCommand(t *testing.T) {
    c := &Codec{}
    version, data, err := c.EncodeCommand("setDriveMode", map[string]any{"mode": "MANUAL"})
    if err != nil {
        t.Fatal(err)
    }
    if version != 1 {
        t.Errorf("expected version 1, got %d", version)
    }
    if len(data) == 0 {
        t.Error("expected non-empty encoded command")
    }
}

func TestUnknownVersion(t *testing.T) {
    c := &Codec{}
    _, err := c.DecodeTelemetry(99, []byte{})
    if err == nil {
        t.Error("expected error for unknown version")
    }
}
```

Run with:
```bash
go test ./internal/extensions/<yournamespace>/...
```

---

## Checklist

**Standard vehicle:**
- [ ] Chose environment (`ground` / `air` / `surface`)
- [ ] Defined capabilities (supported commands, missions)
- [ ] Verified with `testsender` + `testclient`
- [ ] Vehicle firmware sends protobuf telemetry to correct multicast address
- [ ] Vehicle firmware listens for commands and sends `CommandAck`

**Custom protocol / extension:**
- [ ] Defined `.proto` schema for telemetry and command messages
- [ ] Implemented `Codec` interface (`Namespace`, `SupportedVersions`, `DecodeTelemetry`, `EncodeCommand`)
- [ ] Registered codec via blank import in `cmd/gateway/main.go`
- [ ] Vehicle heartbeat advertises extension capabilities
- [ ] Unit tests for codec v1 round-trip, unknown version error, unknown action error
- [ ] Verified end-to-end with gateway + test sender
