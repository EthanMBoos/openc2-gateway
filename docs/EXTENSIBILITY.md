# OpenC2 Extensibility Architecture

> **Purpose**: Design document for supporting multiple projects/teams with custom protocols, commands, and telemetry without forking the UI or gateway.

---

## Problem Statement

OpenC2 will support different types of projects with different teams and protocols:
- Teams want to send **custom commands** from the UI (custom Action buttons)
- Teams have **project-specific protos** with domain-specific message types
- Teams want to display **custom state** beyond standard telemetry (e.g., bucket angle for excavators, sonar data for maritime)

**Goal**: One codebase for UI and gateway — teams *extend* without *forking*.

---

## Core Insight

OpenC2 is a **platform**, not an **application**. The architecture must separate:

| Layer | Description |
|-------|-------------|
| **Core protocol** | Position, heading, status, basic commands — universal across all vehicles |
| **Domain-specific extensions** | Custom telemetry fields, custom commands, custom UI panels |

---

## Solution: Schema-Driven Extensibility

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              EXTENSION REPOS                                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐              │
│  │   excavator/    │  │    maritime/    │  │     camera/     │              │
│  │   v1/*.proto    │  │    v1/*.proto   │  │    v1/*.proto   │              │
│  │   manifest.yaml │  │   manifest.yaml │  │   manifest.yaml │              │
│  │   codec.go      │  │    codec.go     │  │    codec.go     │              │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘              │
└───────────────────────────────┬─────────────────────────────────────────────┘
                                │ imported as dependency
                                ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                             GATEWAY (core)                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ api/proto/openc2.proto                                              │    │
│  │   - Core telemetry, commands, status                                │    │
│  │   - Extension envelope: map<string, bytes> extensions               │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ internal/extensions/                                                │    │
│  │   - Extension registry (loads codecs at startup)                    │    │
│  │   - Routes extension commands                                       │    │
│  │   - Translates extension bytes ↔ JSON                               │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└───────────────────────────────┬─────────────────────────────────────────────┘
                                │ WebSocket (JSON)
                                ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                                UI (OpenC2)                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ Manifest-driven rendering                                           │    │
│  │   - Dynamic ActionPanel buttons from manifest                       │    │
│  │   - Dynamic telemetry panels from manifest                          │    │
│  │   - Extension data stored in VehicleInstance.extensions             │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Protocol Layer: Extension Envelope

### Core Proto Additions

Add extension support to `openc2.proto` without breaking existing messages:

```protobuf
// api/proto/openc2.proto

message VehicleTelemetry {
  // ... existing fields 1-10 ...
  
  // Extension data (opaque to gateway core)
  // Key = namespace (e.g., "excavator"), Value = serialized extension proto
  map<string, bytes> extensions = 20;
}

message Command {
  // ... existing fields ...
  
  // For action = "extension"
  ExtensionCommand extension = 20;
}

message ExtensionCommand {
  string namespace = 1;           // e.g., "excavator"
  string action = 2;              // e.g., "setBucketAngle"  
  bytes payload = 3;              // Serialized extension-specific proto
}
```

**Key design principle**: Gateway core doesn't need to understand extension contents — it passes `bytes` through. This decouples gateway releases from extension releases.

### JSON Wire Format

Gateway translates extensions to JSON for UI consumption:

**Telemetry with extensions:**
```json
{
  "v": 1,
  "type": "telemetry",
  "vid": "excavator-01",
  "ts": 1710700800000,
  "gts": 1710700800001,
  "data": {
    "location": {"lat": 37.7749, "lng": -122.4194, "alt_msl": 10.0},
    "speed": 0.5,
    "heading": 45.0,
    "environment": "ground",
    "seq": 12345,
    "extensions": {
      "excavator": {
        "bucketAngle": 45,
        "hydraulicPressure": 1200,
        "armExtension": 3.5,
        "mode": "DIGGING"
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
  "vid": "excavator-01",
  "ts": 1710700800000,
  "data": {
    "commandId": "cmd-abc123",
    "action": "extension",
    "namespace": "excavator",
    "payload": {
      "type": "setBucketAngle",
      "angle": 30
    }
  }
}
```

---

## Project Manifest

Each project ships a **manifest file** that tells the UI and gateway what to expect:

```yaml
# excavator/v1/manifest.yaml

namespace: excavator
version: "1.0"
displayName: "Excavator Controls"

# Telemetry extensions (fields added to vehicle state)
telemetry:
  extensions:
    - name: bucketAngle
      type: number
      unit: degrees
      range: [0, 90]
      description: "Current bucket angle"
      
    - name: hydraulicPressure
      type: number
      unit: psi
      range: [0, 3000]
      warning: 2500
      critical: 2800
      
    - name: armExtension
      type: number
      unit: meters
      range: [0, 10]
      
    - name: mode
      type: enum
      values: ["IDLE", "DIGGING", "DUMPING", "TRAVELING"]

# Custom commands (appear in ActionPanel)
commands:
  - action: setBucketAngle
    label: "Set Bucket"
    icon: "bucket"
    description: "Set the bucket angle"
    params:
      - name: angle
        type: number
        label: "Angle"
        range: [0, 90]
        default: 45
        
  - action: setArmExtension
    label: "Extend Arm"
    icon: "arm"
    params:
      - name: extension
        type: number
        label: "Extension"
        range: [0, 10]
        unit: meters
        
  - action: emergencyRetract
    label: "Emergency Retract"
    icon: "warning"
    confirmation: true
    confirmMessage: "Retract all hydraulics immediately?"

# UI panel configuration
ui:
  panels:
    - type: telemetry
      title: "Excavator Status"
      position: right  # left, right, or bottom
      fields:
        - extension: bucketAngle
          display: gauge
          label: "Bucket"
          
        - extension: hydraulicPressure
          display: bar
          label: "Hydraulics"
          colorScale:
            - [0, 2500, "green"]
            - [2500, 2800, "yellow"]
            - [2800, 3000, "red"]
            
        - extension: mode
          display: badge
          label: "Mode"
```

---

## Extension Proto Files

Each team defines their own proto in the extensions repo:

```protobuf
// openc2-extensions/excavator/v1/excavator.proto

syntax = "proto3";
package openc2.extensions.excavator.v1;

option go_package = "github.com/your-org/openc2-extensions/excavator/v1";

// Telemetry extension (serialized into VehicleTelemetry.extensions["excavator"])
message ExcavatorTelemetry {
  float bucket_angle_deg = 1;
  float hydraulic_pressure_psi = 2;
  float arm_extension_m = 3;
  ExcavatorMode mode = 4;
}

enum ExcavatorMode {
  EXCAVATOR_MODE_IDLE = 0;
  EXCAVATOR_MODE_DIGGING = 1;
  EXCAVATOR_MODE_DUMPING = 2;
  EXCAVATOR_MODE_TRAVELING = 3;
}

// Command payloads (serialized into ExtensionCommand.payload)
message SetBucketAngleCommand {
  float angle_deg = 1;
}

message SetArmExtensionCommand {
  float extension_m = 1;
}

message EmergencyRetractCommand {
  // Empty - just retract
}
```

---

## Gateway Implementation

### Extension Codec Interface

```go
// internal/extensions/codec.go

package extensions

// Codec handles encoding/decoding for a specific extension namespace.
type Codec interface {
    // Namespace returns the extension identifier (e.g., "excavator")
    Namespace() string
    
    // DecodeTelemetry converts proto bytes to JSON-serializable map
    DecodeTelemetry(data []byte) (map[string]any, error)
    
    // DecodeCommand converts command proto bytes to JSON-serializable map
    DecodeCommand(action string, data []byte) (map[string]any, error)
    
    // EncodeCommand converts JSON command payload to proto bytes
    EncodeCommand(action string, data map[string]any) ([]byte, error)
    
    // Manifest returns the parsed manifest for this extension
    Manifest() *Manifest
}
```

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

// DecodeExtensions decodes all extensions in a telemetry message.
func DecodeExtensions(extensions map[string][]byte) (map[string]any, error) {
    result := make(map[string]any)
    
    for namespace, data := range extensions {
        codec := Get(namespace)
        if codec == nil {
            // Unknown extension - pass through as base64 for debugging
            result[namespace] = map[string]any{
                "_raw": data,
                "_error": "unknown extension namespace",
            }
            continue
        }
        
        decoded, err := codec.DecodeTelemetry(data)
        if err != nil {
            result[namespace] = map[string]any{
                "_raw": data,
                "_error": err.Error(),
            }
            continue
        }
        
        result[namespace] = decoded
    }
    
    return result, nil
}
```

### Example Extension Codec

```go
// In openc2-extensions repo: excavator/codec.go

package excavator

import (
    "fmt"
    
    "github.com/your-org/openc2-extensions/excavator/v1/pb"
    "github.com/your-org/openc2-gateway/internal/extensions"
    "google.golang.org/protobuf/proto"
)

func init() {
    extensions.Register(&Codec{})
}

type Codec struct {
    manifest *extensions.Manifest
}

func (c *Codec) Namespace() string { return "excavator" }

func (c *Codec) DecodeTelemetry(data []byte) (map[string]any, error) {
    var msg pb.ExcavatorTelemetry
    if err := proto.Unmarshal(data, &msg); err != nil {
        return nil, fmt.Errorf("unmarshal excavator telemetry: %w", err)
    }
    
    return map[string]any{
        "bucketAngle":       msg.BucketAngleDeg,
        "hydraulicPressure": msg.HydraulicPressurePsi,
        "armExtension":      msg.ArmExtensionM,
        "mode":              msg.Mode.String(),
    }, nil
}

func (c *Codec) EncodeCommand(action string, data map[string]any) ([]byte, error) {
    switch action {
    case "setBucketAngle":
        angle, ok := data["angle"].(float64)
        if !ok {
            return nil, fmt.Errorf("missing or invalid 'angle' field")
        }
        msg := &pb.SetBucketAngleCommand{AngleDeg: float32(angle)}
        return proto.Marshal(msg)
        
    case "setArmExtension":
        ext, ok := data["extension"].(float64)
        if !ok {
            return nil, fmt.Errorf("missing or invalid 'extension' field")
        }
        msg := &pb.SetArmExtensionCommand{ExtensionM: float32(ext)}
        return proto.Marshal(msg)
        
    case "emergencyRetract":
        msg := &pb.EmergencyRetractCommand{}
        return proto.Marshal(msg)
        
    default:
        return nil, fmt.Errorf("unknown excavator action: %s", action)
    }
}

func (c *Codec) DecodeCommand(action string, data []byte) (map[string]any, error) {
    // Decode for logging/debugging - inverse of EncodeCommand
    switch action {
    case "setBucketAngle":
        var msg pb.SetBucketAngleCommand
        if err := proto.Unmarshal(data, &msg); err != nil {
            return nil, err
        }
        return map[string]any{"angle": msg.AngleDeg}, nil
    // ... other actions
    default:
        return nil, fmt.Errorf("unknown action: %s", action)
    }
}

func (c *Codec) Manifest() *extensions.Manifest {
    return c.manifest
}
```

### Gateway Main: Import Extensions

```go
// cmd/gateway/main.go

package main

import (
    // Core packages
    "github.com/your-org/openc2-gateway/internal/config"
    "github.com/your-org/openc2-gateway/internal/websocket"
    // ...
    
    // Extension codecs - blank import to trigger init() registration
    _ "github.com/your-org/openc2-extensions/excavator"
    _ "github.com/your-org/openc2-extensions/maritime"
    _ "github.com/your-org/openc2-extensions/camera"
)

func main() {
    // Extensions are auto-registered via init()
    // ...
}
```

### Manifest Endpoint

Gateway serves manifests to UI clients:

```go
// internal/websocket/server.go

func (s *Server) handleManifests(w http.ResponseWriter, r *http.Request) {
    manifests := make(map[string]any)
    
    for _, codec := range extensions.All() {
        manifests[codec.Namespace()] = codec.Manifest()
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(manifests)
}

// Register in server setup:
// http.HandleFunc("/manifests", s.handleManifests)
```

---

## UI Implementation

### Project Store

```typescript
// stores/projectStore.ts

interface ProjectManifest {
  namespace: string;
  version: string;
  displayName: string;
  telemetry: {
    extensions: ExtensionField[];
  };
  commands: ExtensionCommand[];
  ui: {
    panels: PanelConfig[];
  };
}

interface ProjectState {
  activeProject: string | null;
  manifests: Record<string, ProjectManifest>;
  loadManifests: () => Promise<void>;
}

export const useProjectStore = create<ProjectState>((set) => ({
  activeProject: null,
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
  };
  
  // Extension data by namespace
  extensions: Record<string, unknown>;
}
```

### Dynamic ActionPanel

```typescript
// components/layout/ActionPanel.tsx

function ActionPanel(): React.ReactElement {
  const { manifests, activeProject } = useProjectStore();
  const selectedVehicle = useSelectedVehicle();
  
  // Get extension commands for the active project
  const extensionCommands = activeProject 
    ? manifests[activeProject]?.commands ?? []
    : [];

  return (
    <div>
      {/* Core commands - always present */}
      <ActionButton action="stop" label="STOP" />
      <ActionButton action="return_home" label="RTB" />
      <ActionButton action="goto" label="GO TO" />
      
      {/* Divider if we have extension commands */}
      {extensionCommands.length > 0 && <Divider />}
      
      {/* Extension commands from manifest */}
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

### Dynamic Telemetry Panels

```typescript
// components/panels/ExtensionTelemetryPanel.tsx

function ExtensionTelemetryPanel({ namespace }: { namespace: string }) {
  const manifest = useProjectStore(s => s.manifests[namespace]);
  const vehicle = useSelectedVehicle();
  const extensionData = vehicle?.extensions[namespace];
  
  if (!manifest || !extensionData) return null;
  
  return (
    <div className="extension-panel">
      <h3>{manifest.displayName}</h3>
      
      {manifest.ui.panels.map((panel, idx) => (
        <DynamicPanel 
          key={idx}
          config={panel}
          data={extensionData}
        />
      ))}
    </div>
  );
}

function DynamicPanel({ config, data }: DynamicPanelProps) {
  return (
    <div className="panel-section">
      <h4>{config.title}</h4>
      
      {config.fields.map(field => {
        const value = data[field.extension];
        
        switch (field.display) {
          case 'gauge':
            return <Gauge key={field.extension} value={value} label={field.label} />;
          case 'bar':
            return <ProgressBar key={field.extension} value={value} colorScale={field.colorScale} />;
          case 'badge':
            return <Badge key={field.extension} value={value} label={field.label} />;
          default:
            return <span key={field.extension}>{field.label}: {value}</span>;
        }
      })}
    </div>
  );
}
```

---

## Repository Strategy

### Directory Structure

```
github.com/your-org/
├── openc2-gateway/           # YOU OWN - core gateway
│   ├── api/proto/
│   │   └── openc2.proto      # Core protocol only
│   ├── internal/
│   │   ├── extensions/       # Extension loading/routing
│   │   ├── protocol/
│   │   ├── registry/
│   │   └── ...
│   ├── cmd/gateway/
│   └── docs/
│       └── EXTENSIBILITY.md  # This document
│
├── openc2-extensions/        # SHARED - teams contribute
│   ├── CODEOWNERS            # Each team owns their directory
│   ├── excavator/
│   │   ├── OWNERS
│   │   └── v1/
│   │       ├── excavator.proto
│   │       ├── excavator.pb.go   # generated
│   │       ├── codec.go          # Go codec for gateway
│   │       └── manifest.yaml     # UI schema
│   ├── maritime/
│   │   ├── OWNERS
│   │   └── v1/
│   │       ├── maritime.proto
│   │       ├── codec.go
│   │       └── manifest.yaml
│   └── camera/
│       └── v1/
│           └── ...
│
└── OpenC2/                   # YOU OWN - UI
    └── src/renderer/
        ├── stores/
        │   └── projectStore.ts
        └── components/
            └── extensions/   # Manifest-driven rendering
```

### CODEOWNERS

```
# openc2-extensions/CODEOWNERS

# Default owners (you)
*                    @openc2-core-team

# Team-specific ownership
/excavator/          @excavator-team
/maritime/           @maritime-team
/camera/             @camera-team
```

### Repository Options

| Approach | Pros | Cons | When to Use |
|----------|------|------|-------------|
| **Monorepo** (`openc2-extensions/`) | Single source of truth, atomic updates, easy CI | Teams step on each other, you're the bottleneck | Small org, <5 teams |
| **Polyrepo** (each team owns their repo) | Full autonomy, independent versioning | Coordination hell, diamond dependencies | Large org, strict ownership |
| **Schema registry** (Buf.build) | Versioned schemas, breaking change detection, generated code hosting | Extra infra | Enterprise, strict compatibility needs |

**Recommendation**: Start with monorepo + CODEOWNERS. Graduate to Buf registry if you hit 10+ teams or need strict versioning.

---

## Build & Release Flow

### Extension Repo CI

```yaml
# openc2-extensions/.github/workflows/ci.yml

name: CI
on: [push, pull_request]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install buf
        uses: bufbuild/buf-setup-action@v1
        
      - name: Lint protos
        run: buf lint
        
      - name: Check breaking changes
        run: buf breaking --against '.git#branch=main'
        
      - name: Generate Go code
        run: buf generate
        
      - name: Validate manifests
        run: |
          for manifest in */v*/manifest.yaml; do
            echo "Validating $manifest"
            yq eval '.' "$manifest" > /dev/null
          done
          
      - name: Test codecs
        run: go test ./...
```

### Gateway Integration

```yaml
# openc2-gateway/.github/workflows/ci.yml

jobs:
  build:
    steps:
      # ... existing steps ...
      
      - name: Update extensions
        run: go get github.com/your-org/openc2-extensions@latest
        
      - name: Build
        run: go build ./...
```

### Release Flow

1. Team updates `excavator.proto` and `manifest.yaml` in extensions repo
2. Extensions repo CI runs: lint, breaking change check, tests
3. PR merged → Extensions repo releases `v1.3.0`
4. Gateway bumps dependency: `go get github.com/your-org/openc2-extensions@v1.3.0`
5. Gateway releases with new extension support
6. UI fetches manifests on connect → new buttons appear automatically

---

## Decoding Strategy Options

### Option A: Gateway Translates (Recommended)

Gateway has a registry of known extensions and translates proto bytes → JSON.

| Pros | Cons |
|------|------|
| UI receives ready-to-use JSON | Gateway must be rebuilt for new extensions |
| Type-safe decoding with good error messages | Tight coupling between gateway and extensions |
| No proto dependency in UI | |

### Option B: Passthrough (Simpler)

Gateway passes extension bytes as base64, UI decodes using protobuf-es.

**JSON frame:**
```json
{
  "extensions": {
    "excavator": "CgQtDABA..."
  }
}
```

**UI decoding:**
```typescript
import { ExcavatorTelemetry } from '@your-org/openc2-extensions/excavator';

const decoded = ExcavatorTelemetry.fromBinary(base64ToBytes(data));
```

| Pros | Cons |
|------|------|
| Gateway is fully decoupled | UI needs proto codegen |
| Teams can ship without gateway release | Less debuggable (binary in JSON) |
| | Bundle size increases with proto libs |

**Recommendation**: Start with Option A. Move to Option B for performance-critical or rapidly-changing extensions.

---

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Where does validation live? | **Both** — gateway rejects malformed, UI provides UX | Defense in depth |
| Proto for extensions? | **Yes** — proto for wire, JSON for UI display | Best of both: type-safe wire, easy UI consumption |
| How are manifests deployed? | **Gateway serves them** (`/manifests` endpoint) | Single source of truth, no version skew |
| Multiple namespaces per vehicle? | **Yes** — a vehicle can have `excavator` + `camera` extensions | Composition over inheritance |
| Unknown extensions? | **Pass through with `_error` field** | Graceful degradation, don't break on unknown |

---

## What This Architecture Provides

1. **Single codebase** — no forks for different teams
2. **Teams own their extensions** — they define manifest + proto, you provide the platform
3. **Graceful degradation** — UI ignores unknown extensions (future-proof)
4. **Type safety where it matters** — core protocol is typed, extensions are schema-validated
5. **Testability** — manifests can be validated in CI before deployment
6. **Independent releases** — extension teams can ship without waiting for platform releases (with Option B)

---

## Implementation Phases

| Phase | Gateway | UI | Effort |
|-------|---------|-----|--------|
| 1. Add `extensions` field to proto | Add `map<string, bytes> extensions` to `VehicleTelemetry` and `Command` | Add `extensions` to `VehicleInstance` | 1 day |
| 2. Extension registry | Create `internal/extensions/` package with codec interface and registry | — | 2 days |
| 3. Manifest loader | Load YAML manifests, expose via `/manifests` endpoint | Fetch manifests on connect, create `projectStore` | 2 days |
| 4. First extension | Implement excavator codec as reference | — | 1 day |
| 5. Dynamic ActionPanel | Route extension commands | Render buttons from manifest | 2 days |
| 6. Extension validation | Validate extension commands against manifest schema | Client-side validation before send | 2 days |
| 7. Dynamic telemetry panels | — | Render panels/gauges from manifest schema | 3-4 days |

**Total**: ~2 weeks for MVP extensibility

---

## Future Enhancements

- **Hot reload**: Watch manifest files for changes, push updates via WebSocket
- **Extension marketplace**: Browse/enable extensions from UI
- **Per-vehicle extensions**: Different vehicles in same fleet have different extensions
- **Extension versioning**: Support multiple versions of same extension
- **Buf schema registry**: Migrate to Buf.build for enterprise-grade schema management
