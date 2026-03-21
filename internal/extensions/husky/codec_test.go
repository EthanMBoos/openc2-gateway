package husky

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestCodec_DecodeTelemetry(t *testing.T) {
	codec := &Codec{}

	msg := &HuskyTelemetry{
		DriveMode:       DriveMode_DRIVE_MODE_AUTONOMOUS,
		BatteryVoltage:  25.6,
		MotorTempLeftC:  45.2,
		MotorTempRightC: 44.8,
		OdometryLeftM:   123.45,
		OdometryRightM:  123.50,
		EstopEngaged:    false,
		BumperContacts: &BumperContacts{
			FrontLeft:  false,
			FrontRight: false,
			RearLeft:   false,
			RearRight:  false,
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

	if result["driveMode"] != "autonomous" {
		t.Errorf("driveMode: got %v, want autonomous", result["driveMode"])
	}
	if result["batteryVoltage"].(float32) != 25.6 {
		t.Errorf("batteryVoltage: got %v, want 25.6", result["batteryVoltage"])
	}

	bumpers, ok := result["bumperContacts"].(map[string]any)
	if !ok {
		t.Fatalf("bumperContacts not a map")
	}
	if bumpers["frontLeft"].(bool) != false {
		t.Errorf("bumperContacts.frontLeft: got %v, want false", bumpers["frontLeft"])
	}
}

func TestCodec_EncodeSetDriveMode(t *testing.T) {
	codec := &Codec{}

	payload := map[string]any{
		"mode": "autonomous",
	}

	version, data, err := codec.EncodeCommand("setDriveMode", payload)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}

	if version != 1 {
		t.Errorf("version: got %d, want 1", version)
	}

	var msg SetDriveModeCommand
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}

	if msg.Mode != DriveMode_DRIVE_MODE_AUTONOMOUS {
		t.Errorf("mode: got %v, want AUTONOMOUS", msg.Mode)
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
