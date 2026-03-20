package protocol

import (
	"encoding/json"
	"testing"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
	"google.golang.org/protobuf/proto"
)

func TestTelemetryToFrame(t *testing.T) {
	tests := []struct {
		name    string
		input   *pb.VehicleTelemetry
		wantErr bool
		check   func(t *testing.T, f *Frame)
	}{
		{
			name: "valid telemetry",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location: &pb.Location{
					Latitude:     37.7749,
					Longitude:    -122.4194,
					AltitudeMslM: 10.5,
				},
				SpeedMs:     1.5,
				HeadingDeg:  145.0,
				Environment: pb.VehicleEnvironment_ENV_GROUND,
				BatteryPct:  proto.Uint32(85),
			},
			wantErr: false,
			check: func(t *testing.T, f *Frame) {
				if f.ProtocolVersion != 1 {
					t.Errorf("expected v=1, got %d", f.ProtocolVersion)
				}
				if f.Type != "telemetry" {
					t.Errorf("expected type=telemetry, got %s", f.Type)
				}
				if f.VehicleID != "ugv-husky-07" {
					t.Errorf("expected vid=ugv-husky-07, got %s", f.VehicleID)
				}

				data := f.Data.(TelemetryPayload)
				if data.Location.Lat != 37.7749 {
					t.Errorf("expected lat=37.7749, got %f", data.Location.Lat)
				}
				if data.Location.Lng != -122.4194 {
					t.Errorf("expected lng=-122.4194, got %f", data.Location.Lng)
				}
				if data.Location.AltMsl == nil || *data.Location.AltMsl != 10.5 {
					t.Errorf("expected alt_msl=10.5, got %v", data.Location.AltMsl)
				}
				if data.Environment != "ground" {
					t.Errorf("expected environment=ground, got %s", data.Environment)
				}
				if data.BatteryPercent == nil || *data.BatteryPercent != 85 {
					t.Errorf("expected batteryPct=85, got %v", data.BatteryPercent)
				}
			},
		},
		{
			name:    "nil message",
			input:   nil,
			wantErr: true,
		},
		{
			name: "missing location",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    nil,
			},
			wantErr: true,
		},
		{
			name: "missing vehicle_id",
			input: &pb.VehicleTelemetry{
				VehicleId:   "",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -122.4194},
			},
			wantErr: true,
		},
		{
			name: "unknown battery (nil)",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -122.4194},
				// BatteryPercent omitted = unknown
			},
			wantErr: false,
			check: func(t *testing.T, f *Frame) {
				data := f.Data.(TelemetryPayload)
				if data.BatteryPercent != nil {
					t.Errorf("expected batteryPct=nil for unknown, got %d", *data.BatteryPercent)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := TelemetryToFrame(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("TelemetryToFrame() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && frame != nil {
				tt.check(t, frame)
			}
		})
	}
}

func TestEnvironmentToString(t *testing.T) {
	cases := []struct {
		input pb.VehicleEnvironment
		want  string
	}{
		{pb.VehicleEnvironment_ENV_AIR, "air"},
		{pb.VehicleEnvironment_ENV_GROUND, "ground"},
		{pb.VehicleEnvironment_ENV_SURFACE, "surface"},
		{pb.VehicleEnvironment_ENV_UNKNOWN, "unknown"},
	}

	for _, tc := range cases {
		got := environmentToString(tc.input)
		if got != tc.want {
			t.Errorf("environmentToString(%v) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestGotoCommandToProto(t *testing.T) {
	alt := 50.0
	speed := 5.0
	cmd := GotoCommand{
		CommandID: "cmd-123",
		Destination: Location{
			Lat:    37.7749,
			Lng:    -122.4194,
			AltMsl: &alt,
		},
		Speed: &speed,
	}

	proto, err := GotoCommandToProto("ugv-husky-07", 1710700800000, cmd)
	if err != nil {
		t.Fatalf("GotoCommandToProto() error = %v", err)
	}

	if proto.CommandId != "cmd-123" {
		t.Errorf("expected command_id=cmd-123, got %s", proto.CommandId)
	}
	if proto.VehicleId != "ugv-husky-07" {
		t.Errorf("expected vehicle_id=ugv-husky-07, got %s", proto.VehicleId)
	}

	gotoPayload := proto.GetGoto()
	if gotoPayload == nil {
		t.Fatal("expected goto payload, got nil")
	}
	if gotoPayload.Destination.Latitude != 37.7749 {
		t.Errorf("expected lat=37.7749, got %f", gotoPayload.Destination.Latitude)
	}
	if gotoPayload.Destination.AltitudeMslM != 50.0 {
		t.Errorf("expected altitude_msl_m=50.0, got %f", gotoPayload.Destination.AltitudeMslM)
	}
	if gotoPayload.SpeedMs != 5.0 {
		t.Errorf("expected speed_ms=5.0, got %f", gotoPayload.SpeedMs)
	}
}

func TestFrameJSONMarshaling(t *testing.T) {
	frame := &Frame{
		ProtocolVersion: 1,
		Type:            "telemetry",
		VehicleID:       "ugv-husky-07",
		TimestampMs:     1710700800000,
		Data: TelemetryPayload{
			Location: Location{
				Lat: 37.7749,
				Lng: -122.4194,
			},
			Speed:       1.5,
			Heading:     145.0,
			Environment: "ground",
		},
	}

	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Verify JSON structure
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if result["protocolVersion"] != float64(1) {
		t.Errorf("expected protocolVersion=1, got %v", result["protocolVersion"])
	}
	if result["type"] != "telemetry" {
		t.Errorf("expected type=telemetry, got %v", result["type"])
	}
	if result["vehicleId"] != "ugv-husky-07" {
		t.Errorf("expected vehicleId=ugv-husky-07, got %v", result["vehicleId"])
	}

	dataMap := result["data"].(map[string]interface{})
	locMap := dataMap["location"].(map[string]interface{})
	if locMap["lat"] != 37.7749 {
		t.Errorf("expected lat=37.7749, got %v", locMap["lat"])
	}
	if locMap["lng"] != -122.4194 {
		t.Errorf("expected lng=-122.4194, got %v", locMap["lng"])
	}

	// alt_msl should be omitted when nil
	if _, exists := locMap["alt_msl"]; exists {
		t.Errorf("expected alt_msl to be omitted, but it was present")
	}
}

func TestValidateTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		input   *pb.VehicleTelemetry
		wantErr bool
	}{
		{
			name: "valid",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -122.4194},
				SpeedMs:     1.5,
				HeadingDeg:  180,
			},
			wantErr: false,
		},
		{
			name: "invalid latitude",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 91, Longitude: -122.4194},
			},
			wantErr: true,
		},
		{
			name: "invalid longitude",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -181},
			},
			wantErr: true,
		},
		{
			name: "negative speed",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -122.4194},
				SpeedMs:     -1,
			},
			wantErr: true,
		},
		{
			name: "heading out of range",
			input: &pb.VehicleTelemetry{
				VehicleId:   "ugv-husky-07",
				TimestampMs: 1710700800000,
				Location:    &pb.Location{Latitude: 37.7749, Longitude: -122.4194},
				HeadingDeg:  361,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTelemetry(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTelemetry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
