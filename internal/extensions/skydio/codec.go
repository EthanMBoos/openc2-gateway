// Package skydio provides the extension codec for Skydio autonomous drones.
//
// This codec decodes Skydio-specific telemetry and encodes commands.
// It registers itself via init() — import this package in cmd/gateway/main.go
// to enable Skydio support.
//
// Usage:
//
//	import _ "github.com/EthanMBoos/openc2-gateway/internal/extensions/skydio"
package skydio

import (
	"errors"
	"fmt"

	"github.com/EthanMBoos/openc2-gateway/internal/extensions"
	"google.golang.org/protobuf/proto"
)

func init() {
	extensions.Register(&Codec{})
	extensions.RegisterManifest(extensions.Manifest{
		Namespace:   "skydio",
		Version:     "1.0",
		DisplayName: "Skydio Drone Controls",
		Commands: []extensions.CommandDefinition{
			{
				Command:     "setFlightMode",
				Label:       "Set Flight Mode",
				Description: "Change flight behavior",
				TargetMode:  "both",
				Parameters: []extensions.CommandParameter{
					{
						Name:     "mode",
						Label:    "Flight Mode",
						Type:     "select",
						Required: true,
						Options: []extensions.ParameterOption{
							{Value: "hover", Label: "Hover"},
							{Value: "manual", Label: "Manual"},
							{Value: "waypoint", Label: "Waypoint"},
							{Value: "orbit", Label: "Orbit"},
						},
					},
				},
			},
			{
				Command:     "setGimbal",
				Label:       "Gimbal Control",
				Description: "Adjust camera gimbal pitch and yaw",
				Parameters: []extensions.CommandParameter{
					{
						Name:    "pitch",
						Label:   "Pitch",
						Type:    "number",
						Min:     floatPtr(-90),
						Max:     floatPtr(30),
						Default: 0,
					},
					{
						Name:    "yaw",
						Label:   "Yaw",
						Type:    "number",
						Min:     floatPtr(-180),
						Max:     floatPtr(180),
						Default: 0,
					},
				},
			},
			{Command: "startRecording", Label: "Start Recording", Description: "Begin video recording"},
			{Command: "stopRecording", Label: "Stop Recording", Description: "End video recording"},
			{Command: "takePhoto", Label: "Take Photo", Description: "Capture a still image", TargetMode: "both"},
			{
				Command:      "orbit",
				Label:        "Orbit Point",
				Description:  "Circle around a point of interest",
				Confirmation: true,
				Parameters: []extensions.CommandParameter{
					{
						Name:        "center",
						Label:       "Center Point",
						Type:        "coordinates",
						Required:    true,
						Description: "Click on map to select orbit center",
					},
					{
						Name:    "radius",
						Label:   "Radius (m)",
						Type:    "number",
						Min:     floatPtr(5),
						Max:     floatPtr(100),
						Default: 20,
					},
				},
			},
			{Command: "track", Label: "Track Subject", Description: "Begin autonomous subject tracking", Confirmation: true},
			{Command: "stopTracking", Label: "Stop Tracking", Description: "End autonomous tracking"},
			{
				Command:     "sendToZone",
				Label:       "Send to Zone",
				Description: "Fly to a designated zone on the map",
				TargetMode:  "both",
				Parameters: []extensions.CommandParameter{
					{
						Name:        "zoneId",
						Label:       "Zone",
						Type:        "zone",
						Required:    true,
						Description: "Select a zone from the map",
					},
					{
						Name:        "altitude",
						Label:       "Altitude (m)",
						Type:        "number",
						Min:         floatPtr(10),
						Max:         floatPtr(120),
						Default:     30,
						Description: "Flight altitude above ground",
					},
				},
			},
		},
	})
}

// floatPtr returns a pointer to the given float64 value.
func floatPtr(v float64) *float64 { return &v }

// Codec implements extensions.Codec for Skydio drones.
type Codec struct{}

var _ extensions.Codec = (*Codec)(nil) // Compile-time interface check

func (c *Codec) Namespace() string           { return "skydio" }
func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

// DecodeTelemetry converts SkydioTelemetry proto bytes to JSON-serializable map.
func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
	if version != 1 {
		return nil, fmt.Errorf("unsupported skydio telemetry version: %d", version)
	}

	var msg SkydioTelemetry
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal skydio telemetry: %w", err)
	}

	result := map[string]any{
		"flightMode":             flightModeToString(msg.FlightMode),
		"gpsFixQuality":          msg.GpsFixQuality,
		"satellites":             msg.Satellites,
		"windSpeedMs":            msg.WindSpeedMs,
		"windDirectionDeg":       msg.WindDirectionDeg,
		"remainingFlightTimeSec": msg.RemainingFlightTimeSec,
	}

	// Motor temperatures
	if len(msg.MotorTempsC) > 0 {
		temps := make([]float64, len(msg.MotorTempsC))
		for i, t := range msg.MotorTempsC {
			temps[i] = float64(t)
		}
		result["motorTempsC"] = temps
	}

	// Gimbal state
	if msg.Gimbal != nil {
		result["gimbal"] = map[string]any{
			"pitchDeg": msg.Gimbal.PitchDeg,
			"yawDeg":   msg.Gimbal.YawDeg,
			"rollDeg":  msg.Gimbal.RollDeg,
		}
	}

	// Obstacle avoidance
	if msg.ObstacleAvoidance != nil {
		result["obstacleAvoidance"] = map[string]any{
			"enabled":          msg.ObstacleAvoidance.Enabled,
			"frontClear":       msg.ObstacleAvoidance.FrontClear,
			"rearClear":        msg.ObstacleAvoidance.RearClear,
			"leftClear":        msg.ObstacleAvoidance.LeftClear,
			"rightClear":       msg.ObstacleAvoidance.RightClear,
			"aboveClear":       msg.ObstacleAvoidance.AboveClear,
			"belowClear":       msg.ObstacleAvoidance.BelowClear,
			"closestObstacleM": msg.ObstacleAvoidance.ClosestObstacleM,
		}
	}

	// Recording state
	if msg.Recording != nil {
		result["recording"] = map[string]any{
			"isRecording":          msg.Recording.IsRecording,
			"recordingDurationSec": msg.Recording.RecordingDurationSec,
			"storageRemainingMb":   msg.Recording.StorageRemainingMb,
			"currentResolution":    msg.Recording.CurrentResolution,
		}
	}

	// Home location
	if msg.Home != nil {
		result["home"] = map[string]any{
			"lat":    msg.Home.Latitude,
			"lng":    msg.Home.Longitude,
			"altMsl": msg.Home.AltitudeMslM,
		}
	}

	// Tracking target
	if msg.TrackingTarget != nil && msg.TrackingTarget.Active {
		result["trackingTarget"] = map[string]any{
			"active":     msg.TrackingTarget.Active,
			"confidence": msg.TrackingTarget.Confidence,
			"distanceM":  msg.TrackingTarget.DistanceM,
			"bearingDeg": msg.TrackingTarget.BearingDeg,
			"targetType": msg.TrackingTarget.TargetType,
		}
	}

	return result, nil
}

// EncodeCommand converts a UI command payload to proto bytes.
func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
	switch action {
	case "setFlightMode":
		return c.encodeSetFlightMode(payload)
	case "setGimbal":
		return c.encodeSetGimbal(payload)
	case "startRecording":
		return c.encodeStartRecording(payload)
	case "stopRecording":
		return c.encodeStopRecording(payload)
	case "takePhoto":
		return c.encodeTakePhoto(payload)
	case "orbit":
		return c.encodeOrbit(payload)
	case "track":
		return c.encodeTrack(payload)
	case "stopTracking":
		return c.encodeStopTracking(payload)
	case "setHome":
		return c.encodeSetHome(payload)
	default:
		return 0, nil, fmt.Errorf("unknown skydio action: %s", action)
	}
}

func (c *Codec) encodeSetFlightMode(payload map[string]any) (uint32, []byte, error) {
	modeStr, ok := payload["mode"].(string)
	if !ok {
		return 0, nil, errors.New("missing or invalid 'mode' field")
	}
	mode, err := stringToFlightMode(modeStr)
	if err != nil {
		return 0, nil, err
	}
	msg := &SetFlightModeCommand{Mode: mode}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetFlightModeCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeSetGimbal(payload map[string]any) (uint32, []byte, error) {
	msg := &SetGimbalCommand{}
	if pitch, ok := payload["pitchDeg"].(float64); ok {
		msg.PitchDeg = float32(pitch)
	}
	if yaw, ok := payload["yawDeg"].(float64); ok {
		msg.YawDeg = float32(yaw)
	}
	if absYaw, ok := payload["useAbsoluteYaw"].(bool); ok {
		msg.UseAbsoluteYaw = absYaw
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetGimbalCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeStartRecording(payload map[string]any) (uint32, []byte, error) {
	msg := &StartRecordingCommand{}
	if res, ok := payload["resolution"].(string); ok {
		msg.Resolution = res
	} else {
		msg.Resolution = "4K30" // Default
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal StartRecordingCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeStopRecording(_ map[string]any) (uint32, []byte, error) {
	msg := &StopRecordingCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal StopRecordingCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeTakePhoto(_ map[string]any) (uint32, []byte, error) {
	msg := &TakePhotoCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal TakePhotoCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeOrbit(payload map[string]any) (uint32, []byte, error) {
	msg := &OrbitCommand{}

	if lat, ok := payload["centerLat"].(float64); ok {
		msg.CenterLat = lat
	} else {
		return 0, nil, errors.New("missing 'centerLat' field")
	}
	if lng, ok := payload["centerLng"].(float64); ok {
		msg.CenterLng = lng
	} else {
		return 0, nil, errors.New("missing 'centerLng' field")
	}
	if radius, ok := payload["radiusM"].(float64); ok {
		msg.RadiusM = float32(radius)
	} else {
		msg.RadiusM = 10.0 // Default 10m orbit
	}
	if alt, ok := payload["altitudeM"].(float64); ok {
		msg.AltitudeM = float32(alt)
	}
	if speed, ok := payload["speedMs"].(float64); ok {
		msg.SpeedMs = float32(speed)
	} else {
		msg.SpeedMs = 2.0 // Default 2 m/s
	}
	if cw, ok := payload["clockwise"].(bool); ok {
		msg.Clockwise = cw
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal OrbitCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeTrack(payload map[string]any) (uint32, []byte, error) {
	msg := &TrackCommand{}
	if x, ok := payload["screenX"].(float64); ok {
		msg.ScreenX = float32(x)
	} else {
		return 0, nil, errors.New("missing 'screenX' field")
	}
	if y, ok := payload["screenY"].(float64); ok {
		msg.ScreenY = float32(y)
	} else {
		return 0, nil, errors.New("missing 'screenY' field")
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal TrackCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeStopTracking(_ map[string]any) (uint32, []byte, error) {
	msg := &StopTrackingCommand{}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal StopTrackingCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeSetHome(payload map[string]any) (uint32, []byte, error) {
	msg := &SetHomeCommand{}
	if lat, ok := payload["lat"].(float64); ok {
		msg.Latitude = lat
	} else {
		return 0, nil, errors.New("missing 'lat' field")
	}
	if lng, ok := payload["lng"].(float64); ok {
		msg.Longitude = lng
	} else {
		return 0, nil, errors.New("missing 'lng' field")
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetHomeCommand: %w", err)
	}
	return 1, data, nil
}

// ── Enum Helpers ──

func flightModeToString(m FlightMode) string {
	switch m {
	case FlightMode_FLIGHT_MODE_IDLE:
		return "idle"
	case FlightMode_FLIGHT_MODE_HOVER:
		return "hover"
	case FlightMode_FLIGHT_MODE_MANUAL:
		return "manual"
	case FlightMode_FLIGHT_MODE_WAYPOINT:
		return "waypoint"
	case FlightMode_FLIGHT_MODE_ORBIT:
		return "orbit"
	case FlightMode_FLIGHT_MODE_TRACK:
		return "track"
	case FlightMode_FLIGHT_MODE_CABLE_CAM:
		return "cableCam"
	case FlightMode_FLIGHT_MODE_RETURN_HOME:
		return "returnHome"
	case FlightMode_FLIGHT_MODE_LANDING:
		return "landing"
	case FlightMode_FLIGHT_MODE_EMERGENCY:
		return "emergency"
	default:
		return "unknown"
	}
}

func stringToFlightMode(s string) (FlightMode, error) {
	switch s {
	case "idle":
		return FlightMode_FLIGHT_MODE_IDLE, nil
	case "hover":
		return FlightMode_FLIGHT_MODE_HOVER, nil
	case "manual":
		return FlightMode_FLIGHT_MODE_MANUAL, nil
	case "waypoint":
		return FlightMode_FLIGHT_MODE_WAYPOINT, nil
	case "orbit":
		return FlightMode_FLIGHT_MODE_ORBIT, nil
	case "track":
		return FlightMode_FLIGHT_MODE_TRACK, nil
	case "cableCam":
		return FlightMode_FLIGHT_MODE_CABLE_CAM, nil
	case "returnHome":
		return FlightMode_FLIGHT_MODE_RETURN_HOME, nil
	case "landing":
		return FlightMode_FLIGHT_MODE_LANDING, nil
	case "emergency":
		return FlightMode_FLIGHT_MODE_EMERGENCY, nil
	default:
		return FlightMode_FLIGHT_MODE_UNKNOWN, fmt.Errorf("unknown flight mode: %s", s)
	}
}
