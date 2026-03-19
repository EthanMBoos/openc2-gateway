# OpenC2 Extensibility

> **Purpose**: Extension codec/manifest spec, wire format, and implementation details.  
> For system topology, platform philosophy, and repository strategy see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Protocol Layer: Extension Envelope

### Core Proto Additions

Add extension support to `openc2.proto` without breaking existing messages:

```protobuf
// api/proto/openc2.proto

message VehicleTelemetry {
  // ... existing fields 1-10 ...
  
  // List of extension namespaces this vehicle supports (e.g., ["husky", "camera"])
  // Used by UI to filter ActionPanel buttons - only show commands the vehicle can handle.
  repeated string supported_extensions = 19;
  
  // Extension data by namespace. Each extension is versioned independently.
  // Key = namespace (e.g., "husky"), Value = versioned extension payload.
  map<string, ExtensionData> extensions = 20;
}

// ExtensionData wraps extension payloads with version metadata.
// This allows schema evolution per-extension without breaking the gateway.
message ExtensionData {
  // Schema version for this extension's payload format.
  // Codecs use this to select the correct decoder.
  uint32 version = 1;
  
  // Serialized extension-specific proto (e.g., HuskyTelemetry).
  bytes payload = 2;
}

message Command {
  // ... existing fields ...
  
  // For action = "extension"
  ExtensionCommand extension = 20;
}

message ExtensionCommand {
  string namespace = 1;           // e.g., "husky"
  string action = 2;              // e.g., "setDriveMode"
  uint32 version = 3;             // Schema version for command payload
  bytes payload = 4;              // Serialized extension-specific proto
}
```

**Key design principles**:
- **Gateway core doesn't parse extension contents** — it routes versioned bytes to codecs. This decouples gateway releases from extension releases.
- **Per-vehicle capabilities** — `supported_extensions` lets the UI filter actions based on what each vehicle actually supports.
- **Independent versioning** — each extension evolves its schema independently; codecs handle version negotiation.

### JSON Wire Format

Gateway translates extensions to JSON for UI consumption.

**Welcome Snapshot:** The `welcome` message contains core telemetry only — no extension data. Clients receive extension state on the first telemetry frame after handshake.

**Telemetry with extensions:**
```json
{
  "v": 1,
  "type": "telemetry",
  "vid": "husky-01",
  "ts": 1710700800000,
  "gts": 1710700800001,
  "data": {
    "location": {"lat": 37.7749, "lng": -122.4194, "alt_msl": 10.0},
    "speed": 0.5,
    "heading": 45.0,
    "environment": "ground",
    "seq": 12345,
    "supportedExtensions": ["husky", "camera"],
    "extensions": {
      "husky": {
        "_version": 1,
        "driveMode": "AUTONOMOUS",
        "eStopActive": false,
        "batteryVoltage": 25.6,
        "frontBumperContact": false,
        "rearBumperContact": false
      }
    }
  }
}
```

**Extension command:**
```json
{
  "v": 1,
  "type": "command",
  "vid": "husky-01",
  "ts": 1710700800000,
  "data": {
    "commandId": "cmd-abc123",
    "action": "extension",
    "namespace": "husky",
    "version": 1,
    "payload": {
      "type": "setDriveMode",
      "mode": "autonomous"
    }
  }
}
```

**Extension command ACK:**

Extension commands use the same `command_ack` structure as core commands. The gateway validates extension actions against `VehicleCapabilities.extensions[].supportedActions` before broadcasting:

```json
{
  "v": 1,
  "type": "command_ack",
  "vid": "husky-01",
  "ts": 1710700800005,
  "gts": 1710700800006,
  "data": {
    "commandId": "cmd-abc123",
    "status": "accepted",
    "message": ""
  }
}
```

**Extension capability validation:** The gateway rejects extension commands that the vehicle doesn't support:

```json
{
  "v": 1,
  "type": "command_ack",
  "vid": "husky-01",
  "ts": 1710700800005,
  "gts": 1710700800006,
  "data": {
    "commandId": "cmd-abc123",
    "status": "rejected",
    "message": "Vehicle does not support extension action 'setArmExtension' (supported actions for 'husky': setDriveMode, triggerEStop, clearEStop, resetFaults)"
  }
}
```

| ACK Status | Meaning for Extension Commands |
|------------|-------------------------------|
| `accepted` | Command validated; broadcast to vehicle |
| `rejected` | Extension namespace unknown OR action not in `supportedActions` |
| `timeout` | No vehicle response within `OPENC2_CMD_TIMEOUT` (default 5s) |
| `completed` | Vehicle confirms action executed |
| `failed` | Vehicle reports execution error |

**UI filtering:** The `supportedExtensions` array enables the UI to show only relevant commands:
```typescript
// ActionPanel.tsx - filter extension commands by vehicle capabilities
const availableCommands = extensionCommands.filter(cmd => 
  selectedVehicle?.telemetry?.supportedExtensions?.includes(cmd.namespace)
);
```

---

## Project Manifest (MVP)

Each extension ships a **manifest file** declaring its namespace and commands. For MVP, manifests are minimal — just enough for the UI to render ActionPanel buttons.

> **Phase 2:** Dynamic telemetry panels with gauges, badges, and color scales. For MVP, hard-code extension-specific panels in the UI.

```yaml
# internal/extensions/husky/manifest.yaml

namespace: husky
version: "1.0"
displayName: "Husky UGV Controls"

# Custom commands (appear in ActionPanel when vehicle supports this extension)
commands:
  - action: setDriveMode
    label: "Set Drive Mode"
    description: "Switch between manual and autonomous drive"
        
  - action: clearEStop
    label: "Clear E-Stop"
        
  - action: triggerEStop
    label: "Trigger E-Stop"
    confirmation: true
```

**MVP scope:**
- `namespace`, `version`, `displayName` — required metadata
- `commands` — list of actions the UI can send
- No `telemetry` field (extension data is opaque to manifest for now)
- No `ui.panels` (hard-code panels for first 1-2 extensions)

---

## Extension Proto Files

Each extension defines its own proto. For MVP, these live in-tree under `internal/extensions/`:

```protobuf
// internal/extensions/husky/husky.proto

syntax = "proto3";
package openc2.extensions.husky;

option go_package = "github.com/EthanMBoos/openc2-gateway/internal/extensions/husky";

// Telemetry extension (serialized into VehicleTelemetry.extensions["husky"])
message HuskyTelemetry {
  float battery_voltage_v = 1;
  bool e_stop_active = 2;
  bool front_bumper_contact = 3;
  bool rear_bumper_contact = 4;
  HuskyDriveMode drive_mode = 5;
}

enum HuskyDriveMode {
  HUSKY_DRIVE_MODE_IDLE = 0;
  HUSKY_DRIVE_MODE_MANUAL = 1;
  HUSKY_DRIVE_MODE_AUTONOMOUS = 2;
  HUSKY_DRIVE_MODE_FAULT = 3;
}

// Command payloads (serialized into ExtensionCommand.payload)
message SetDriveModeCommand {
  HuskyDriveMode mode = 1;
}

message TriggerEStopCommand {
  // Empty - activates emergency stop
}

message ClearEStopCommand {
  // Empty - clears emergency stop
}

message ResetFaultsCommand {
  // Empty - reset drive system faults
}
```

---

## Gateway Implementation

### Extension Codec Interface

```go
// internal/extensions/codec.go

package extensions

// Codec handles encoding/decoding for a specific extension namespace.
// Each codec must handle version negotiation for its extension's schema.
type Codec interface {
    // Namespace returns the extension identifier (e.g., "husky")
    Namespace() string
    
    // SupportedVersions returns the schema versions this codec can decode.
    // Used for diagnostics and version mismatch errors.
    SupportedVersions() []uint32
    
    // DecodeTelemetry converts versioned proto bytes to JSON-serializable map.
    // Returns error if version is unsupported by this codec.
    DecodeTelemetry(version uint32, data []byte) (map[string]any, error)
    
    // DecodeCommand converts versioned command proto bytes to JSON-serializable map.
    DecodeCommand(action string, version uint32, data []byte) (map[string]any, error)
    
    // EncodeCommand converts JSON command payload to proto bytes.
    // Returns the version used for encoding (latest supported version).
    EncodeCommand(action string, data map[string]any) (version uint32, payload []byte, err error)
    
    // Manifest returns the parsed manifest for this extension
    Manifest() *Manifest
}
```

**Version Compatibility Contract:** Codecs MUST decode all schema versions they have ever shipped (backward compatibility). When encoding commands, use the latest version. If a vehicle sends a version the codec doesn't recognize, gateway passes through with `_error` metadata — the UI shows the debug panel, operations continue.

### Extension Registry

```go
// internal/extensions/registry.go

package extensions

import (
    "fmt"
    "sync"
)

var (
    mu     sync.RWMutex
    codecs = make(map[string]Codec)
)

// Register adds a codec to the registry. Called at init() by extension packages.
func Register(c Codec) {
    mu.Lock()
    defer mu.Unlock()
    codecs[c.Namespace()] = c
}

// Get returns the codec for a namespace, or nil if not found.
func Get(namespace string) Codec {
    mu.RLock()
    defer mu.RUnlock()
    return codecs[namespace]
}

// All returns all registered codecs.
func All() []Codec {
    mu.RLock()
    defer mu.RUnlock()
    result := make([]Codec, 0, len(codecs))
    for _, c := range codecs {
        result = append(result, c)
    }
    return result
}

// DecodeExtensions decodes all versioned extensions in a telemetry message.
func DecodeExtensions(extensions map[string]*ExtensionData) (map[string]any, error) {
    result := make(map[string]any)
    
    for namespace, ext := range extensions {
        codec := Get(namespace)
        if codec == nil {
            // Unknown extension - pass through as base64 for debugging
            result[namespace] = map[string]any{
                "_raw":     ext.Payload,
                "_version": ext.Version,
                "_error":   "unknown extension namespace",
            }
            continue
        }
        
        decoded, err := codec.DecodeTelemetry(ext.Version, ext.Payload)
        if err != nil {
            result[namespace] = map[string]any{
                "_raw":     ext.Payload,
                "_version": ext.Version,
                "_error":   err.Error(),
            }
            continue
        }
        
        // Include version in decoded output for debugging/display
        decoded["_version"] = ext.Version
        result[namespace] = decoded
    }
    
    return result, nil
}
```

### Example Extension Codec (MVP)

```go
// internal/extensions/husky/codec.go

package husky

import (
    "fmt"
    
    pb "github.com/EthanMBoos/openc2-gateway/internal/extensions/husky"
    "github.com/EthanMBoos/openc2-gateway/internal/extensions"
    "google.golang.org/protobuf/proto"
)

func init() {
    extensions.Register(&Codec{})
}

type Codec struct{}

func (c *Codec) Namespace() string { return "husky" }

func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
    if version != 1 {
        return nil, fmt.Errorf("unsupported husky telemetry version %d", version)
    }
    
    var msg pb.HuskyTelemetry
    if err := proto.Unmarshal(data, &msg); err != nil {
        return nil, fmt.Errorf("unmarshal husky telemetry: %w", err)
    }
    return map[string]any{
        "batteryVoltage":    msg.BatteryVoltageV,
        "eStopActive":       msg.EStopActive,
        "frontBumperContact": msg.FrontBumperContact,
        "rearBumperContact":  msg.RearBumperContact,
        "driveMode":         msg.DriveMode.String(),
    }, nil
}

func (c *Codec) EncodeCommand(action string, data map[string]any) (uint32, []byte, error) {
    switch action {
    case "setDriveMode":
        modeStr, ok := data["mode"].(string)
        if !ok {
            return 0, nil, fmt.Errorf("missing or invalid 'mode' field")
        }
        modeVal, ok := pb.HuskyDriveMode_value["HUSKY_DRIVE_MODE_"+strings.ToUpper(modeStr)]
        if !ok {
            return 0, nil, fmt.Errorf("unknown husky drive mode: %s", modeStr)
        }
        msg := &pb.SetDriveModeCommand{Mode: pb.HuskyDriveMode(modeVal)}
        payload, err := proto.Marshal(msg)
        return 1, payload, err
        
    case "triggerEStop":
        msg := &pb.TriggerEStopCommand{}
        payload, err := proto.Marshal(msg)
        return 1, payload, err
        
    case "clearEStop":
        msg := &pb.ClearEStopCommand{}
        payload, err := proto.Marshal(msg)
        return 1, payload, err

    case "resetFaults":
        msg := &pb.ResetFaultsCommand{}
        payload, err := proto.Marshal(msg)
        return 1, payload, err
        
    default:
        return 0, nil, fmt.Errorf("unknown husky action: %s", action)
    }
}

func (c *Codec) DecodeCommand(action string, data []byte) (map[string]any, error) {
    // Decode for logging/debugging - inverse of EncodeCommand
    switch action {
    case "setDriveMode":
        var msg pb.SetDriveModeCommand
        if err := proto.Unmarshal(data, &msg); err != nil {
            return nil, err
        }
        return map[string]any{"mode": msg.Mode.String()}, nil
    // ... other actions
    default:
        return nil, fmt.Errorf("unknown action: %s", action)
    }
}

func (c *Codec) Manifest() *extensions.Manifest {
    return c.manifest
}
```

### Gateway Main: Import Extensions (MVP)

For MVP, extensions live **in-tree** under `internal/extensions/`. No separate repo yet.

```go
// cmd/gateway/main.go

package main

import (
    // Core packages
    "github.com/EthanMBoos/openc2-gateway/internal/config"
    "github.com/EthanMBoos/openc2-gateway/internal/websocket"
    // ...
    
    // Extension codecs - in-tree for MVP
    _ "github.com/EthanMBoos/openc2-gateway/internal/extensions/husky"
)

func main() {
    // Extensions are auto-registered via init()
    // ...
}
```

### Manifest Endpoint (MVP)

For MVP, serve a static JSON file bundled at build time:

```go
// internal/websocket/server.go

//go:embed manifests.json
var manifestsJSON []byte

func (s *Server) handleManifests(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write(manifestsJSON)
}

// Register in server setup:
// http.HandleFunc("/manifests", s.handleManifests)
```

**Phase 2:** Dynamically collect manifests from registered codecs at runtime.

---

## UI Implementation

### Project Store (MVP)

```typescript
// stores/projectStore.ts

interface ProjectManifest {
  namespace: string;
  version: string;
  displayName: string;
  commands: ExtensionCommand[];
}

interface ProjectState {
  manifests: Record<string, ProjectManifest>;
  loadManifests: () => Promise<void>;
}

export const useProjectStore = create<ProjectState>((set) => ({
  manifests: {},
  
  loadManifests: async () => {
    const response = await fetch('http://localhost:9000/manifests');
    const manifests = await response.json();
    set({ manifests });
  },
}));
```

### Vehicle Instance Extensions

```typescript
// stores/vehicleStore.ts

interface VehicleInstance {
  id: string;
  name: string;
  status: 'online' | 'offline' | 'standby';
  telemetry: {
    location: { lat: number; lng: number };
    speed: number;
    heading: number;
    // ... core fields
    
    // Per-vehicle extension capabilities
    supportedExtensions: string[];  // e.g., ["husky", "camera"]
  };
  
  // Extension data by namespace (decoded from versioned payloads)
  extensions: Record<string, unknown>;
}
```

### Dynamic ActionPanel

```typescript
// components/layout/ActionPanel.tsx

function ActionPanel(): React.ReactElement {
  const { manifests, activeProject } = useProjectStore();
  const selectedVehicle = useSelectedVehicle();
  
  // Get extension commands that the selected vehicle actually supports
  const extensionCommands = useMemo(() => {
    if (!activeProject || !selectedVehicle) return [];
    
    const projectCommands = manifests[activeProject]?.commands ?? [];
    const vehicleCapabilities = selectedVehicle.telemetry?.supportedExtensions ?? [];
    
    // Filter to commands whose namespace the vehicle supports
    return projectCommands.filter(cmd => 
      vehicleCapabilities.includes(cmd.namespace ?? activeProject)
    );
  }, [activeProject, manifests, selectedVehicle]);

  return (
    <div>
      {/* Core commands - always present */}
      <ActionButton action="stop" label="STOP" />
      <ActionButton action="return_home" label="RTB" />
      <ActionButton action="goto" label="GO TO" />
      
      {/* Divider if we have extension commands */}
      {extensionCommands.length > 0 && <Divider />}
      
      {/* Extension commands filtered by vehicle capabilities */}
      {extensionCommands.map(cmd => (
        <ExtensionActionButton 
          key={cmd.action}
          namespace={activeProject!}
          command={cmd}
          vehicleId={selectedVehicle?.id}
        />
      ))}
    </div>
  );
}

function ExtensionActionButton({ 
  namespace, 
  command, 
  vehicleId 
}: ExtensionActionButtonProps) {
  const sendCommand = useCommandSender();
  
  const handleClick = () => {
    if (command.confirmation) {
      // Show confirmation dialog
      if (!confirm(command.confirmMessage ?? `Execute ${command.label}?`)) {
        return;
      }
    }
    
    sendCommand({
      type: 'command',
      vid: vehicleId,
      data: {
        commandId: crypto.randomUUID(),
        action: 'extension',
        namespace,
        payload: {
          type: command.action,
          // TODO: Collect params from dialog if command.params exists
        },
      },
    });
  };

  return (
    <button onClick={handleClick} title={command.description}>
      {command.icon && <Icon name={command.icon} />}
      {command.label}
    </button>
  );
}
```

### Extension Telemetry Panels (Phase 2)

> **MVP:** Hard-code extension panels for your first 1-2 projects. Extract to manifest-driven rendering once you have 3+ extensions and understand common patterns.

```typescript
// components/panels/HuskyPanel.tsx (MVP - hard-coded)

function HuskyPanel() {
  const vehicle = useSelectedVehicle();
  const data = vehicle?.extensions?.husky;
  
  if (!data) return null;
  
  return (
    <div className="extension-panel">
      <h3>Husky UGV Status</h3>
      <div>Drive Mode: {data.driveMode}</div>
      <div>E-Stop: {data.eStopActive ? "ACTIVE" : "Clear"}</div>
      <div>Battery: {data.batteryVoltage?.toFixed(1)} V</div>
    </div>
  );
}
```

---

## Extension Decoding

**Gateway always decodes extensions to JSON.** The WebSocket carries clean, human-readable JSON — no binary blobs, no base64.

| Benefit | Why It Matters |
|---------|----------------|
| UI receives ready-to-use JSON | No protobuf runtime in the browser, no bundle bloat |
| Single point of debugging | WebSocket traffic is readable; errors logged in one place |
| Consistent behavior | All clients see identical decoded data |
| Type-safe decoding | Gateway codec catches malformed extension data with clear errors |

### Unknown Extensions (Fail-Fast)

For MVP, the gateway **rejects unknown extensions** with a clear error:

```json
{
  "extensions": {
    "maritime": {
      "_version": 1,
      "_error": "unknown extension namespace: maritime"
    }
  }
}
```

**UI handling:** Display the error in a debug panel. This tells the operator "extension codec not integrated" without breaking the rest of telemetry.

**Development workflow:**
1. Team defines their `maritime.proto` and implements a codec
2. Team adds codec to `internal/extensions/maritime/` (in-tree for MVP)
3. Team runs local gateway with their codec
4. Once validated, PR to main branch

**Key point:** All roads lead to `openc2.proto`. Extension protos define what goes *inside* the `extensions` bytes field — they're payloads nested in the OpenC2 envelope, not alternatives to it.

---

## Implementation Phases

### MVP (This Release)

| Phase | Gateway | UI | Effort |
|-------|---------|-----|--------|
| 1. Extension envelope | `extensions` field already in proto ✓ | Add `extensions` to `VehicleInstance` | 0.5 day |
| 2. Extension registry | Create `internal/extensions/` with codec interface | — | 1 day |
| 3. Telemetry translation | Decode extensions via codecs, include in JSON | Store extension data in vehicle state | 1 day |
| 4. First codec | Implement husky codec in-tree | Hard-coded HuskyPanel | 1 day |
| 5. Extension commands | Route `action: "extension"` through codecs | Extension buttons in ActionPanel | 1 day |
| 6. Static manifests | `/manifests` returns static JSON | Fetch on connect | 0.5 day |

**Total**: ~5 days for MVP extensibility

### Phase 2 (Post-MVP)

| Feature | Description |
|---------|-------------|
| Dynamic manifest loading | Collect manifests from registered codecs at runtime |
| Manifest-driven UI panels | Render gauges/badges from manifest schema |
| External extensions repo | Split to `openc2-extensions/` with CODEOWNERS |
| Manifest validation CI | JSON Schema validation in CI |

---

## Future Enhancements

- **Hot reload**: Watch manifest files for changes, push updates via WebSocket
- **Extension marketplace**: Browse/enable extensions from UI
- **Buf schema registry**: Migrate to Buf.build for enterprise-grade schema management
- **Plugin architecture**: Load extension codecs at runtime without gateway recompilation

---

## Open Design Gaps

### Manifest Schema Validation

> **STATUS: NOT YET IMPLEMENTED** — This is a known gap that should be addressed before production use with multiple teams.

**Current state:** The example CI validates YAML syntax only:

```yaml
- name: Validate manifests
  run: |
    for manifest in */v*/manifest.yaml; do
      yq eval '.' "$manifest" > /dev/null  # Just checks valid YAML
    done
```

**The problem:** When a team submits a malformed manifest (wrong field types, missing required fields, invalid `display` values), nothing rejects it until runtime — possibly in production.

**Recommended approach:**

1. **Publish a JSON Schema** for `manifest.yaml` in the extensions repo:
   - Define required fields: `namespace`, `version`, `displayName`
   - Validate `telemetry.extensions[].type` against allowed values
   - Validate `commands[].params[].type` and `range` structure
   - Validate `ui.panels[].fields[].display` against known display types

2. **Validate in CI** alongside proto linting:
   ```yaml
   - name: Validate manifest schema
     run: |
       npm install -g ajv-cli ajv-formats
       for manifest in */v*/manifest.yaml; do
         ajv validate -s manifest.schema.json -d "$manifest" --strict
       done
   ```

3. **Gateway validation** at startup:
   - Parse and validate all manifests against schema
   - Log warnings for invalid manifests but don't crash
   - `/manifests` endpoint only serves validated manifests

4. **UI graceful degradation**:
   - Ignore malformed manifest entries (log warning)
   - Show "extension unavailable" placeholder instead of crashing

**Priority:** HIGH — Without this, a single bad PR from any team can break the UI for all operators.

### Namespace Governance

> **STATUS: GOVERNANCE RULES DEFINED** — CI enforcement not yet implemented.  
> Namespace tiers, rules, and the namespace registry are documented in [ARCHITECTURE.md](ARCHITECTURE.md#extension-namespace-governance).

#### When to Promote to Core

An extension should become a core protocol field when:

| Criterion | Threshold |
|-----------|-----------|
| Adoption | >80% of projects use it |
| Stability | No breaking changes for 6+ months |
| Cross-cutting | Multiple domains need it (not domain-specific) |
| Abstraction | Clear, generic interface (not team-specific quirks) |

Example: Sensors started as a potential extension, but every robotics project has cameras/LiDAR. Now it's `VehicleCapabilities.sensors` in core.

**Priority:** MEDIUM — Enforce before onboarding third team.

---

## Protocol Evolution Roadmap

> **Purpose**: First-class abstractions that prevent custom forks. These are universal needs across robotics projects that warrant inclusion in the core protocol rather than forcing teams to reinvent them as extensions.

### Problem: Why Extensions Aren't Enough

The extension system handles **project-specific** needs well. But some abstractions are **universal** — 90% of teams need them, and treating them as extensions means:

1. Every team reinvents the same wheel
2. Incompatible implementations prevent cross-project tooling
3. UI must special-case "common extensions" anyway

The following should be **core protocol features** with well-defined semantics.

---

### 1. Vehicle Capabilities (Priority: HIGH) — ✅ IMPLEMENTED

> **STATUS**: Implemented in `openc2.proto`, gateway, and testsender as of March 2026.

**The Problem (Solved):**

Previously, all vehicles implicitly accepted all core commands (`goto`, `stop`, `return_home`, etc.). This failed for:

| Vehicle Type | Issue |
|--------------|-------|
| Stationary sensor | Cannot `goto` or `stop` (not moving) |
| Fixed-wing UAV | Cannot `stop` mid-flight (stall) |
| Tethered ROV | Cannot `return_home` (would destroy tether) |
| Observation-only | No commands at all (telemetry publisher only) |

**Solution:**

Capabilities are now advertised via `Heartbeat` messages. See `api/proto/openc2.proto` for the full schema:

- `VehicleCapabilities` — lists supported commands, extensions, mission support, and sensors
- `ExtensionCapability` — granular extension support with namespace, version, and specific action list
- `SensorCapability` — describes attached sensors with stream URLs and mounting info
- Gateway validates commands against capabilities (fail-fast with `COMMAND_NOT_SUPPORTED` error)
- Capabilities included in `welcome` message fleet snapshot

**Extension Capability Structure:**

```protobuf
message ExtensionCapability {
  string namespace = 1;           // e.g., "husky"
  uint32 version = 2;             // Schema version vehicle implements
  repeated string supported_actions = 3;  // Empty = all actions supported
}
```

The `supported_actions` field enables **granular capability advertisement**:

| `supported_actions` value | Meaning |
|--------------------------|---------|
| Empty `[]` | Vehicle supports all actions in extending namespace |
| `["setBucketAngle", "setArmExtension"]` | Vehicle only supports these specific actions |

This prevents the UI from showing "Deploy Blade" for a vehicle that doesn't have a blade attachment.

**Testing:**

```bash
# Full capability vehicle (default)
go run ./cmd/testsender -vid ugv-husky-07

# Fixed-wing (no stop command)
go run ./cmd/testsender -vid fixed-wing-01 -env air -caps no-stop

# Ground vehicle with Husky extension
go run ./cmd/testsender -vid husky-01 -env ground

# Observation-only sensor (no commands accepted)
go run ./cmd/testsender -vid sensor-01 -caps none
```

**UI Impact:**

```typescript
// ActionPanel dynamically shows/hides buttons
function ActionPanel({ vehicle }: Props) {
  const caps = vehicle.capabilities;
  
  return (
    <>
      {caps.supportedCommands.includes('stop') && <StopButton />}
      {caps.supportedCommands.includes('goto') && <GotoButton />}
      {caps.supportedCommands.includes('return_home') && <RTBButton />}
      
      {/* Extension buttons filtered by caps.extensions */}
      {caps.extensions.map(ext => (
        ext.supportedActions.map(action => (
          <ExtensionButton key={`${ext.namespace}:${action}`} 
            namespace={ext.namespace} action={action} />
        ))
      ))}
    </>
  );
}
```

**Gateway Changes:**

1. Parse `capabilities` from Heartbeat
2. Store in registry alongside vehicle state
3. Include in `welcome` snapshot fleet data
4. Reject commands not in vehicle's `supported_commands` (fail-fast, don't forward)

---

### 2. Sensor Registry (Priority: MEDIUM) — ✅ IMPLEMENTED

> **STATUS**: Implemented as part of `VehicleCapabilities.sensors` in `openc2.proto` as of March 2026.

**The Problem (Solved):**

Cameras, LiDAR, sonar, and thermal sensors appear across nearly every robotics project. Treating them as extensions means:

- Every team defines their own camera proto
- Stream URL formats vary wildly
- No standard way to display sensor feeds in UI
- Mounting transforms (where sensor points) are either missing or incompatible

**Solution:**

Sensors are now a first-class field in `VehicleCapabilities`. See `api/proto/openc2.proto` for the full schema:

```protobuf
message SensorCapability {
  string sensor_id = 1;              // "front_camera", "lidar_1"
  SensorType type = 2;
  string stream_url = 3;             // rtsp://, http://, ws://
  SensorMount mount = 4;             // Where/how sensor is mounted
  map<string, string> metadata = 10; // Type-specific (resolution, FOV, etc.)
}

enum SensorType {
  SENSOR_UNKNOWN = 0;
  SENSOR_CAMERA_RGB = 1;
  SENSOR_CAMERA_THERMAL = 2;
  SENSOR_CAMERA_DEPTH = 3;
  SENSOR_LIDAR_2D = 4;
  SENSOR_LIDAR_3D = 5;
  SENSOR_SONAR = 6;
  SENSOR_RADAR = 7;
  SENSOR_IMU = 8;
  SENSOR_GPS = 9;
}

message SensorMount {
  // Position offset from vehicle origin (meters, body frame)
  float x = 1;
  float y = 2;
  float z = 3;
  
  // Orientation (Euler angles in degrees, body frame)
  // Roll/pitch/yaw convention: X-forward, Y-left, Z-up
  float roll = 4;
  float pitch = 5;
  float yaw = 6;
}
```

**UI Capability:**

- Render camera feeds in a standard video panel
- Visualize sensor FOV cones on map (using mount orientation)
- Show sensor health indicators without extension-specific code

**Why Not Extension?**

Sensors are **cross-cutting** — a vehicle might have husky extensions AND cameras. Making sensors an extension would require every domain extension to depend on a "camera extension," creating coupling that defeats the purpose.

---

### 3. Mission Abstraction (Priority: MEDIUM-LOW)

**The Problem:**

Core commands are point-to-point: go here, stop, return. Real operations need:

- Waypoint sequences ("visit these 5 points")
- Conditional tasks ("survey area, then return")
- Progress tracking ("waypoint 3/5 complete")
- Pause/resume/abort semantics

**Without a standard mission format:**

- Teams build incompatible mission planners
- No shared tooling for mission visualization
- Progress tracking requires extension-specific UI

**Proto Addition:**

```protobuf
message Command {
  // ... existing fields ...
  
  oneof payload {
    GotoCommand goto = 10;
    StopCommand stop = 11;
    ReturnHomeCommand return_home = 12;
    SetModeCommand set_mode = 13;
    SetSpeedCommand set_speed = 14;
    
    // NEW: High-level mission control
    MissionCommand mission = 20;
  }
}

message MissionCommand {
  string mission_id = 1;
  MissionAction action = 2;
  MissionDefinition definition = 3;  // Only for START action
}

enum MissionAction {
  MISSION_START = 0;
  MISSION_PAUSE = 1;
  MISSION_RESUME = 2;
  MISSION_ABORT = 3;
}

message MissionDefinition {
  repeated Waypoint waypoints = 1;
  string mission_type = 2;           // "patrol", "survey", "delivery", or extension namespace
  map<string, bytes> parameters = 10; // Extension point for mission-type-specific params
}

message Waypoint {
  Location location = 1;
  float speed_ms = 2;                // Target speed for this leg
  float loiter_time_s = 3;           // Time to hold at waypoint (0 = fly-through)
  string action = 4;                 // "none", "photograph", or extension:namespace/action
  map<string, bytes> action_params = 5;
}
```

**Telemetry Addition:**

```protobuf
message VehicleTelemetry {
  // ... existing fields ...
  
  // NEW: Mission progress (optional, only when executing mission)
  optional MissionProgress mission_progress = 16;
}

message MissionProgress {
  string mission_id = 1;
  MissionState state = 2;
  uint32 current_waypoint = 3;       // 0-indexed
  uint32 total_waypoints = 4;
  float completion_pct = 5;          // 0.0-100.0
  string current_action = 6;         // What vehicle is doing right now
}

enum MissionState {
  MISSION_PENDING = 0;
  MISSION_ACTIVE = 1;
  MISSION_PAUSED = 2;
  MISSION_COMPLETED = 3;
  MISSION_ABORTED = 4;
  MISSION_FAILED = 5;
}
```

**Design Philosophy:**

The core provides the **envelope** (waypoints, lifecycle, progress). Domain-specific behavior ("what does a survey mission do?") lives in the `parameters` map and is decoded by extensions. This follows the same pattern as telemetry extensions.

---

### 4. Coordinate Frames (Priority: LOW)

**The Problem:**

Robotics systems use multiple coordinate frames:

| Frame | Use Case |
|-------|----------|
| WGS84 (lat/lng) | GPS, mapping, UI display |
| UTM | Local planning, obstacle avoidance |
| Body-fixed | Sensor mounts, manipulator control |
| Map-relative | Indoor/GPS-denied navigation |

Currently, `Location` is implicitly WGS84. This works for outdoor vehicles but breaks for:

- Indoor robots (no GPS)
- Underwater vehicles (acoustic positioning)
- Warehouse robots (local grid coordinates)

**Proto Addition:**

```protobuf
message Location {
  double latitude = 1;   // WGS84 or local Y
  double longitude = 2;  // WGS84 or local X  
  float altitude_msl_m = 3;
  
  // NEW: Frame identifier (default = WGS84)
  string frame_id = 4;   // "wgs84", "utm:10N", "local", or custom
}
```

**Why Low Priority:**

Most OpenC2 deployments are outdoor with GPS. The frame issue primarily affects indoor/industrial robotics, which often have existing local tooling. Implement when a concrete use case demands it.

---

### Implementation Priority

| Feature | Priority | Effort | Status |
|---------|----------|--------|--------|
| Vehicle Capabilities | **HIGH** | 3-4 days | ✅ **DONE** — Proto, gateway, and testsender implemented |
| Adapter Layer | **HIGH** | 1 week | Teams own translation (Integration Contract) |
| Sensor Registry | MEDIUM | 2-3 days | ✅ **DONE** — Part of VehicleCapabilities |
| Mission Abstraction | MEDIUM | 1 week | Not started |
| Coordinate Frames | LOW | 1 day | Not started |

**Recommendation:** Capabilities and sensor registry are shipped. The Integration Contract handles the adapter layer — teams own their translation code.

---

## Integration Contract

> **This is the most important architectural decision in the project.**

OpenC2 does not maintain bridges, adapters, or translators for external protocols. Teams who want to integrate with OpenC2 must emit OpenC2 proto on the multicast groups.

### The Contract

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           YOUR SYSTEM (Team-Owned)                          │
│                                                                             │
│  ┌─────────────────────────────────────┐      ┌─────────────────────────┐   │
│  │            Your Robot               │      │   Your Ground Station   │   │
│  │                                     │      │                         │   │
│  │  ┌─────────────┐  ┌──────────────┐  │      │  ┌─────────────────┐    │   │
│  │  │ Your State  │  │ Radio Node   │  │ radio│  │  Radio Receiver │    │   │
│  │  │ (ROS2/DDS/  │─▶│ + OpenC2     │──┼──────┼─▶│  (passthrough)  │    │   │
│  │  │  custom)    │  │ Translation  │  │ link │  │                 │    │   │
│  │  └─────────────┘  │ (~50 lines)  │  │      │  └────────┬────────┘    │   │
│  │                   └──────────────┘  │      │           │             │   │
│  └─────────────────────────────────────┘      │           ▼             │   │
│                                               │  UDP Multicast          │   │
│                                               │  239.255.0.1:14550      │   │
│                                               └───────────┬─────────────┘   │
└───────────────────────────────────────────────────────────┼─────────────────┘
                                                            │
┌───────────────────────────────────────────────────────────┼─────────────────┐
│                        OPENC2 (We Own)                    │                 │
│                                                           ▼                 │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                         OpenC2 Gateway                               │   │
│  │                    (speaks OpenC2 proto ONLY)                        │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                      │                                      │
│                                      ▼                                      │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                           OpenC2 UI                                  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key insight:** Translation happens **on the robot**, in the radio node that transmits. The ground station receiver is a passthrough — it just forwards OpenC2 proto to the local multicast group. This keeps the ground station generic across all robot types.

### What OpenC2 Provides

| Asset | Description |
|-------|-------------|
| `openc2.proto` | The protocol definition — your translation target |
| Reference implementation | `cmd/testsender/` shows exactly how to emit telemetry |
| Field documentation | Every field explained with units and semantics |
| Validation | Gateway validates incoming protos, gives clear errors |
| This document | Architecture guidance for teams |

### What Teams Own

| Responsibility | Why |
|----------------|-----|
| Translation code | You know your state model — map it to `VehicleTelemetry` on the robot |
| Correctness | You verify your translation emits valid protos |
| Radio link | Your robot transmits OpenC2 proto; your ground station forwards it |
| Latency | Translation runs on the robot — you control timing and bandwidth |

### Why This Matters

**The alternative was bridges.** We would write `openc2-project1-bridge`, `openc2-project2-bridge` etc. This fails:

| Problem | Impact |
|---------|--------|
| N teams = N adapters | Maintenance scales linearly with adoption |
| Blame games | "Your bridge broke our data" vs "your data broke our bridge" |
| Ownership ambiguity | Who fixes bugs? Who tests? Who upgrades? |

**The precedent is clear.** This is how integration protocols scale:

- **MAVLink**: The ecosystem defined the protocol. Vehicles that want ground station compatibility emit MAVLink. No adapters per flight controller.
- **ROS2**: Standard message types (`geometry_msgs/Pose`, `nav_msgs/Odometry`) are ecosystem-defined. Robots conform to them to use ecosystem tooling.
- **NMEA (GPS)**: Devices emit NMEA sentences. Mapping software doesn't write parsers for each GPS chip's internal format.
- **OpenTelemetry**: Services emit OTLP. Observability platforms don't adapt to each service's internal metrics format.

### The Pitch to Teams

> "Add ~50 lines to your robot's radio node. Map your `Odometry` to `VehicleTelemetry`, serialize it, and transmit. Your ground station receiver forwards to multicast. You show up in our UI."

**Example (Go):**

```go
// On the robot: translate + serialize in your radio node
func translateToOpenC2(odom *YourOdometry) *openc2.VehicleTelemetry {
    return &openc2.VehicleTelemetry{
        VehicleId:   "your-vehicle-id",
        TimestampMs: time.Now().UnixMilli(),
        Location: &openc2.Location{
            Latitude:   odom.Pose.Position.Latitude,
            Longitude:  odom.Pose.Position.Longitude,
            AltitudeMslM: float32(odom.Pose.Position.Altitude),
        },
        HeadingDeg:   float32(odom.Pose.Orientation.Yaw * 180 / math.Pi),
        SpeedMs:      float32(math.Sqrt(odom.Twist.Linear.X*odom.Twist.Linear.X + odom.Twist.Linear.Y*odom.Twist.Linear.Y)),
        Environment:  openc2.Environment_GROUND,
        Status:       openc2.VehicleStatus_ACTIVE,
    }
}
```

**Example (C++):**

```cpp
// On the robot: translate + serialize in your radio node
#include "openc2.pb.h"
#include <cmath>

openc2::VehicleTelemetry translateToOpenC2(const YourOdometry& odom) {
    openc2::VehicleTelemetry telem;
    telem.set_vehicle_id("your-vehicle-id");
    telem.set_timestamp_ms(std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::system_clock::now().time_since_epoch()).count());
    
    auto* loc = telem.mutable_location();
    loc->set_latitude(odom.pose.position.latitude);
    loc->set_longitude(odom.pose.position.longitude);
    loc->set_altitude_msl_m(odom.pose.position.altitude);
    
    telem.set_heading_deg(odom.pose.orientation.yaw * 180.0 / M_PI);
    telem.set_speed_ms(std::sqrt(
        odom.twist.linear.x * odom.twist.linear.x + 
        odom.twist.linear.y * odom.twist.linear.y));
    telem.set_environment(openc2::ENVIRONMENT_GROUND);
    telem.set_status(openc2::VEHICLE_STATUS_ACTIVE);
    
    return telem;
}
```

### Integration Checklist

For teams integrating with OpenC2:

- [ ] Clone the proto: `api/proto/openc2.proto`
- [ ] Generate code for your language (`protoc`, `buf generate`, etc.)
- [ ] Add translation function to your robot's radio node (~50 lines)
- [ ] Serialize `VehicleTelemetry` and transmit over your radio link
- [ ] Configure ground station receiver to forward to telemetry multicast group (default: `239.255.0.1:14550`)
- [ ] (Optional) Subscribe to command multicast group (default: `239.255.0.2:14551`) and relay commands to robot
- [ ] Verify with `cmd/testclient/` — you should see your vehicle

### What If Teams Push Back?

| Objection | Response |
|-----------|----------|
| "Can't you just write a bridge for us?" | "You own your robot's radio node. You know your state model. The translation is 50 lines — you'll write it faster than explaining your format to us." |
| "We don't want to maintain proto code" | "The proto is stable. Once your translation works, it works. We version the proto carefully — no breaking changes." |
| "Our robot's radio node is frozen/legacy" | "Add a small sidecar process on the robot that subscribes to your existing output and emits OpenC2 proto. Still your code, your responsibility." |

### The Result

- **Gateway stays pure**: Only speaks OpenC2 proto. No protocol zoo.
- **Clear ownership**: Teams own their translation. We own the platform.
- **No maintenance scaling**: Adding team N+1 requires zero work from us.
- **Predictable debugging**: If data looks wrong, it's the translation. If gateway breaks, it's us.
