# Protocol Test Fixtures

Shared JSON fixtures for validating Tower ↔ Server protocol conformance.

**Source of truth:** `internal/protocol/frame.go`

These fixtures are **identical in both repos** to catch protocol drift early. Both TypeScript and Go tests validate against the same JSON shapes.

## Files

| File | Description |
|------|-------------|
| `telemetry.json` | Telemetry frame (server → UI) with position, speed, extensions |
| `heartbeat.json` | Heartbeat frame with full capability advertisement |
| `welcome.json` | Welcome response containing fleet bootstrap data |
| `commands.json` | All command types (hello, goto, stop, etc.) from UI → server |
| `responses.json` | Acks, alerts, errors, status transitions from server → UI |

## Usage

### Go Tests

```go
package protocol_test

import (
    "encoding/json"
    "os"
    "testing"
    
    "github.com/EthanMBoos/tower-server/internal/protocol"
)

func TestTelemetryFixture(t *testing.T) {
    data, err := os.ReadFile("../../testdata/protocol/telemetry.json")
    if err != nil {
        t.Fatal(err)
    }
    
    var frame protocol.Frame
    if err := json.Unmarshal(data, &frame); err != nil {
        t.Fatal(err)
    }
    
    if frame.Type != protocol.TypeTelemetry {
        t.Errorf("expected %q, got %q", protocol.TypeTelemetry, frame.Type)
    }
}
```

### Round-Trip Validation

Test that Frame → JSON → Frame produces identical output:

```go
func TestFrameRoundTrip(t *testing.T) {
    data, _ := os.ReadFile("../../testdata/protocol/telemetry.json")
    
    var frame protocol.Frame
    json.Unmarshal(data, &frame)
    
    encoded, _ := json.Marshal(frame)
    
    var decoded protocol.Frame
    json.Unmarshal(encoded, &decoded)
    
    // Compare
    if frame.VehicleID != decoded.VehicleID {
        t.Error("round-trip mismatch")
    }
}
```

## Keeping Fixtures in Sync

When modifying the protocol:

1. **Update frame.go first** — Go structs are authoritative
2. **Update fixtures here** — Adjust JSON to match new schema
3. **Copy fixtures to Tower** — Keep them identical
4. **Run tests in both repos** — Catch any drift

```bash
# Quick sync check
diff -r ./testdata/protocol ../Tower/testdata/protocol
```

## Integration with translate_test.go

The fixtures complement unit tests in `internal/protocol/translate_test.go`:

- **Unit tests**: Test individual translation functions with Go-constructed inputs
- **Fixture tests**: Validate that Go types can unmarshal real-world JSON shapes

Both are necessary — unit tests ensure logic correctness, fixture tests ensure wire compatibility.
