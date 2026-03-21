package skydio

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestCodec_DecodeTelemetry(t *testing.T) {
	codec := &Codec{}

	msg := &SkydioTelemetry{
		FlightMode:             FlightMode_FLIGHT_MODE_HOVER,
		GpsFixQuality:          4,
		Satellites:             18,
		WindSpeedMs:            3.5,
		WindDirectionDeg:       270,
		RemainingFlightTimeSec: 1200,
		MotorTempsC:            []float32{42.1, 41.8, 43.2, 42.5},
		Gimbal: &GimbalState{
			PitchDeg: -45.0,
			YawDeg:   0.0,
			RollDeg:  0.0,
		},
		ObstacleAvoidance: &ObstacleAvoidance{
			Enabled:          true,
			FrontClear:       true,
			RearClear:        true,
			LeftClear:        true,
			RightClear:       true,
			AboveClear:       true,
			BelowClear:       false,
			ClosestObstacleM: 2.5,
		},
		Recording: &RecordingState{
			IsRecording:          true,
			RecordingDurationSec: 120,
			StorageRemainingMb:   32000,
			CurrentResolution:    "4K60",
		},
		Home: &HomeLocation{
			Latitude:     33.7756,
			Longitude:    -84.3963,
			AltitudeMslM: 320.0,
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

	if result["flightMode"] != "hover" {
		t.Errorf("flightMode: got %v, want hover", result["flightMode"])
	}
	if result["gpsFixQuality"].(uint32) != 4 {
		t.Errorf("gpsFixQuality: got %v, want 4", result["gpsFixQuality"])
	}

	gimbal, ok := result["gimbal"].(map[string]any)
	if !ok {
		t.Fatalf("gimbal not a map")
	}
	if gimbal["pitchDeg"].(float32) != -45.0 {
		t.Errorf("gimbal.pitchDeg: got %v, want -45.0", gimbal["pitchDeg"])
	}

	recording, ok := result["recording"].(map[string]any)
	if !ok {
		t.Fatalf("recording not a map")
	}
	if recording["isRecording"].(bool) != true {
		t.Errorf("recording.isRecording: got %v, want true", recording["isRecording"])
	}
}

func TestCodec_EncodeSetFlightMode(t *testing.T) {
	codec := &Codec{}

	payload := map[string]any{
		"mode": "hover",
	}

	version, data, err := codec.EncodeCommand("setFlightMode", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}

	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}

	var msg SetFlightModeCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}

	if msg.Mode != FlightMode_FLIGHT_MODE_HOVER {
		t.Errorf("mode: got %v, want HOVER", msg.Mode)
	}
}

func TestCodec_EncodeOrbit(t *testing.T) {
	codec := &Codec{}

	payload := map[string]any{
		"centerLat": 33.7756,
		"centerLng": -84.3963,
		"radiusM":   15.0,
		"altitudeM": 30.0,
		"speedMs":   3.0,
		"clockwise": true,
	}

	version, data, err := codec.EncodeCommand("orbit", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}

	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}

	var msg OrbitCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}

	if msg.CenterLat != 33.7756 {
		t.Errorf("centerLat: got %v, want 33.7756", msg.CenterLat)
	}
	if msg.RadiusM != 15.0 {
		t.Errorf("radiusM: got %v, want 15.0", msg.RadiusM)
	}
	if msg.Clockwise != true {
		t.Errorf("clockwise: got %v, want true", msg.Clockwise)
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
