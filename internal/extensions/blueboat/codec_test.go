package blueboat

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestCodec_DecodeTelemetry(t *testing.T) {
	codec := &Codec{}
	msg := &BlueboatTelemetry{
		NavMode:         NavMode_NAV_MODE_WAYPOINT,
		WaterDepthM:     5.2,
		GpsFixQuality:   4,
		Satellites:      12,
		CurrentDrawA:    8.5,
		RangeRemainingM: 5000,
		WindSpeedMs:     3.2,
		Battery: &BatteryState{
			Voltage:    25.6,
			Percentage: 85.0,
			TempC:      32.0,
			Cycles:     42,
		},
		Thrusters: &ThrusterStatus{
			LeftRpm:    1200,
			RightRpm:   1180,
			LeftTempC:  45.0,
			RightTempC: 44.0,
			LeftFault:  false,
			RightFault: false,
		},
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal test message: %v", err)
	}
	result, err := codec.DecodeTelemetry(1, data)
	if err != nil {
		t.Fatalf("decode telemetry: %v", err)
	}
	if result["navMode"] != "waypoint" {
		t.Errorf("navMode: got %v, want waypoint", result["navMode"])
	}
	if result["waterDepthM"].(float32) != 5.2 {
		t.Errorf("waterDepthM: got %v, want 5.2", result["waterDepthM"])
	}
	battery, ok := result["battery"].(map[string]any)
	if !ok {
		t.Fatalf("battery not a map")
	}
	if battery["percentage"].(float32) != 85.0 {
		t.Errorf("battery.percentage: got %v, want 85.0", battery["percentage"])
	}
}

func TestCodec_EncodeSetNavMode(t *testing.T) {
	codec := &Codec{}
	payload := map[string]any{"mode": "hold"}
	version, data, err := codec.EncodeCommand("setNavMode", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}
	var msg SetNavModeCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if msg.Mode != NavMode_NAV_MODE_HOLD {
		t.Errorf("mode: got %v, want HOLD", msg.Mode)
	}
}

func TestCodec_EncodeStartSurvey(t *testing.T) {
	codec := &Codec{}
	payload := map[string]any{
		"zoneId":       "zone-alpha",
		"laneSpacingM": 15.0,
		"speedMs":      2.0,
	}
	version, data, err := codec.EncodeCommand("startSurvey", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}
	var msg StartSurveyCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if msg.ZoneId != "zone-alpha" {
		t.Errorf("zoneId: got %v, want zone-alpha", msg.ZoneId)
	}
	if msg.LaneSpacingM != 15.0 {
		t.Errorf("laneSpacingM: got %v, want 15.0", msg.LaneSpacingM)
	}
	if msg.SpeedMs != 2.0 {
		t.Errorf("speedMs: got %v, want 2.0", msg.SpeedMs)
	}
}

func TestCodec_EncodeStartSurveyMissingZone(t *testing.T) {
	codec := &Codec{}
	payload := map[string]any{"laneSpacingM": 10.0}
	_, _, err := codec.EncodeCommand("startSurvey", payload)
	if err == nil {
		t.Error("expected error for missing zoneId")
	}
}

func TestCodec_EncodeSetDock(t *testing.T) {
	codec := &Codec{}
	payload := map[string]any{
		"dockLocationLat": 33.9425,
		"dockLocationLng": -84.1352,
	}
	version, data, err := codec.EncodeCommand("setDock", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}
	var msg SetDockCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if msg.Latitude != 33.9425 {
		t.Errorf("latitude: got %v, want 33.9425", msg.Latitude)
	}
	if msg.Longitude != -84.1352 {
		t.Errorf("longitude: got %v, want -84.1352", msg.Longitude)
	}
}

func TestCodec_EncodeSetDockMissingCoords(t *testing.T) {
	codec := &Codec{}
	// Missing dockLocationLng
	payload := map[string]any{"dockLocationLat": 33.9425}
	_, _, err := codec.EncodeCommand("setDock", payload)
	if err == nil {
		t.Error("expected error for missing dockLocationLng")
	}
}

func TestCodec_UnsupportedVersion(t *testing.T) {
	codec := &Codec{}
	_, err := codec.DecodeTelemetry(99, []byte{})
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestCodec_UnknownAction(t *testing.T) {
	codec := &Codec{}
	_, _, err := codec.EncodeCommand("unknownAction", nil)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
