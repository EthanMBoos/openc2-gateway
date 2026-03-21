// Package husky provides the extension codec for Clearpath Husky A200 UGV.
//
// This codec decodes Husky-specific telemetry and encodes commands.
// It registers itself via init() — import this package in cmd/gateway/main.go
// to enable Husky support.
//
// Usage:
//
//	import _ "github.com/EthanMBoos/openc2-gateway/internal/extensions/husky"
package husky

import (
	"errors"
	"fmt"

	"github.com/EthanMBoos/openc2-gateway/internal/extensions"
	"google.golang.org/protobuf/proto"
)

func init() {
	extensions.Register(&Codec{})
	extensions.RegisterManifest(extensions.Manifest{
		Namespace:   "husky",
		Version:     "1.0",
		DisplayName: "Husky UGV Controls",
		Commands: []extensions.CommandDefinition{
			{
				Command:     "setDriveMode",
				Label:       "Set Drive Mode",
				Description: "Change between manual, autonomous, or guided control",
				TargetMode:  "both", // Can target single or all huskies
				Parameters: []extensions.CommandParameter{
					{
						Name:     "mode",
						Label:    "Drive Mode",
						Type:     "select",
						Required: true,
						Options: []extensions.ParameterOption{
							{Value: "manual", Label: "Manual"},
							{Value: "autonomous", Label: "Autonomous"},
							{Value: "guided", Label: "Guided"},
						},
					},
				},
			},
			{
				Command:     "setBumperSensitivity",
				Label:       "Bumper Sensitivity",
				Description: "Adjust collision detection threshold",
				Parameters: []extensions.CommandParameter{
					{
						Name:     "sensitivity",
						Label:    "Sensitivity",
						Type:     "number",
						Required: true,
						Min:      floatPtr(0),
						Max:      floatPtr(100),
						Default:  50,
					},
				},
			},
			{
				Command:      "triggerEStop",
				Label:        "Emergency Stop",
				Description:  "Immediately halt all motion (software E-Stop)",
				Confirmation: true,
				TargetMode:   "both", // E-Stop all vehicles is a valid use case
			},
			{
				Command:      "releaseEStop",
				Label:        "Release E-Stop",
				Description:  "Clear software E-Stop (hardware E-Stop must be cleared manually)",
				Confirmation: true,
			},
			{
				Command:     "sendToZone",
				Label:       "Send to Zone",
				Description: "Navigate to a designated zone on the map",
				TargetMode:  "both", // Send one or all vehicles to a zone
				Parameters: []extensions.CommandParameter{
					{
						Name:        "zoneId",
						Label:       "Zone",
						Type:        "zone",
						Required:    true,
						Description: "Select a zone from the map",
					},
				},
			},
		},
	})
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(v float64) *float64 { return &v }

// Codec implements extensions.Codec for Husky UGV.
type Codec struct{}

var _ extensions.Codec = (*Codec)(nil) // Compile-time interface check

func (c *Codec) Namespace() string { return "husky" }

func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

// DecodeTelemetry converts HuskyTelemetry proto bytes to JSON-serializable map.
func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
	if version != 1 {
		return nil, fmt.Errorf("unsupported husky telemetry version: %d", version)
	}

	// Unmarshal the proto
	var msg HuskyTelemetry
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal husky telemetry: %w", err)
	}

	// Convert to JSON-friendly map
	result := map[string]any{
		"driveMode":       driveModeToString(msg.DriveMode),
		"batteryVoltage":  msg.BatteryVoltage,
		"motorTempLeftC":  msg.MotorTempLeftC,
		"motorTempRightC": msg.MotorTempRightC,
		"odometryLeftM":   msg.OdometryLeftM,
		"odometryRightM":  msg.OdometryRightM,
		"estopEngaged":    msg.EstopEngaged,
	}

	// Nested bumper contacts
	if msg.BumperContacts != nil {
		result["bumperContacts"] = map[string]any{
			"frontLeft":  msg.BumperContacts.FrontLeft,
			"frontRight": msg.BumperContacts.FrontRight,
			"rearLeft":   msg.BumperContacts.RearLeft,
			"rearRight":  msg.BumperContacts.RearRight,
		}
	}

	return result, nil
}

// EncodeCommand converts a UI command payload to proto bytes.
func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
	switch action {
	case "setDriveMode":
		return c.encodeSetDriveMode(payload)
	case "setBumperSensitivity":
		return c.encodeSetBumperSensitivity(payload)
	case "triggerEStop":
		return c.encodeTriggerEStop(payload)
	case "releaseEStop":
		return c.encodeReleaseEStop(payload)
	default:
		return 0, nil, fmt.Errorf("unknown husky action: %s", action)
	}
}

func (c *Codec) encodeSetDriveMode(payload map[string]any) (uint32, []byte, error) {
	modeStr, ok := payload["mode"].(string)
	if !ok {
		return 0, nil, errors.New("missing or invalid 'mode' field")
	}

	mode, err := stringToDriveMode(modeStr)
	if err != nil {
		return 0, nil, err
	}

	msg := &SetDriveModeCommand{Mode: mode}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetDriveModeCommand: %w", err)
	}

	return 1, data, nil
}

func (c *Codec) encodeSetBumperSensitivity(payload map[string]any) (uint32, []byte, error) {
	// JSON numbers come as float64
	sensFloat, ok := payload["sensitivity"].(float64)
	if !ok {
		return 0, nil, errors.New("missing or invalid 'sensitivity' field")
	}

	msg := &SetBumperSensitivityCommand{Sensitivity: uint32(sensFloat)}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetBumperSensitivityCommand: %w", err)
	}

	return 1, data, nil
}

func (c *Codec) encodeTriggerEStop(_ map[string]any) (uint32, []byte, error) {
	msg := &TriggerEStopCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal TriggerEStopCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeReleaseEStop(_ map[string]any) (uint32, []byte, error) {
	msg := &ReleaseEStopCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal ReleaseEStopCommand: %w", err)
	}
	return 1, data, nil
}

// ── Enum Helpers ──

func driveModeToString(m DriveMode) string {
	switch m {
	case DriveMode_DRIVE_MODE_MANUAL:
		return "manual"
	case DriveMode_DRIVE_MODE_AUTONOMOUS:
		return "autonomous"
	case DriveMode_DRIVE_MODE_GUIDED:
		return "guided"
	default:
		return "unknown"
	}
}

func stringToDriveMode(s string) (DriveMode, error) {
	switch s {
	case "manual":
		return DriveMode_DRIVE_MODE_MANUAL, nil
	case "autonomous":
		return DriveMode_DRIVE_MODE_AUTONOMOUS, nil
	case "guided":
		return DriveMode_DRIVE_MODE_GUIDED, nil
	default:
		return DriveMode_DRIVE_MODE_UNKNOWN, fmt.Errorf("unknown drive mode: %s", s)
	}
}
