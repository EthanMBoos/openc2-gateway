// Package blueboat provides the extension codec for BlueRobotics BlueBoat USV.
//
// This codec decodes BlueBoat-specific telemetry and encodes commands.
// It registers itself via init() — import this package in cmd/gateway/main.go
// to enable BlueBoat support.
//
// Usage:
//
//	import _ "github.com/EthanMBoos/openc2-gateway/internal/extensions/blueboat"
package blueboat

import (
	"errors"
	"fmt"

	"github.com/EthanMBoos/openc2-gateway/internal/extensions"
	"google.golang.org/protobuf/proto"
)

func init() {
	extensions.Register(&Codec{})
}

// Codec implements extensions.Codec for BlueBoat USV.
type Codec struct{}

var _ extensions.Codec = (*Codec)(nil)

func (c *Codec) Namespace() string           { return "blueboat" }
func (c *Codec) SupportedVersions() []uint32 { return []uint32{1} }

func (c *Codec) DecodeTelemetry(version uint32, data []byte) (map[string]any, error) {
	if version != 1 {
		return nil, fmt.Errorf("unsupported blueboat telemetry version: %d", version)
	}
	var msg BlueboatTelemetry
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal blueboat telemetry: %w", err)
	}
	result := map[string]any{
		"navMode":         navModeToString(msg.NavMode),
		"waterDepthM":     msg.WaterDepthM,
		"gpsFixQuality":   msg.GpsFixQuality,
		"satellites":      msg.Satellites,
		"currentDrawA":    msg.CurrentDrawA,
		"rangeRemainingM": msg.RangeRemainingM,
		"windSpeedMs":     msg.WindSpeedMs,
	}
	if msg.Battery != nil {
		result["battery"] = map[string]any{
			"voltage":    msg.Battery.Voltage,
			"percentage": msg.Battery.Percentage,
			"tempC":      msg.Battery.TempC,
			"cycles":     msg.Battery.Cycles,
		}
	}
	if msg.Thrusters != nil {
		result["thrusters"] = map[string]any{
			"leftRpm":    msg.Thrusters.LeftRpm,
			"rightRpm":   msg.Thrusters.RightRpm,
			"leftTempC":  msg.Thrusters.LeftTempC,
			"rightTempC": msg.Thrusters.RightTempC,
			"leftFault":  msg.Thrusters.LeftFault,
			"rightFault": msg.Thrusters.RightFault,
		}
	}
	if msg.Dock != nil {
		result["dock"] = map[string]any{
			"lat": msg.Dock.Latitude,
			"lng": msg.Dock.Longitude,
		}
	}
	return result, nil
}

func (c *Codec) EncodeCommand(action string, payload map[string]any) (uint32, []byte, error) {
	switch action {
	case "setNavMode":
		return c.encodeSetNavMode(payload)
	case "startSurvey":
		return c.encodeStartSurvey(payload)
	case "setDock":
		return c.encodeSetDock(payload)
	default:
		return 0, nil, fmt.Errorf("unknown blueboat action: %s", action)
	}
}

func (c *Codec) encodeSetNavMode(payload map[string]any) (uint32, []byte, error) {
	modeStr, ok := payload["mode"].(string)
	if !ok {
		return 0, nil, errors.New("missing mode field")
	}
	mode, err := stringToNavMode(modeStr)
	if err != nil {
		return 0, nil, err
	}
	msg := &SetNavModeCommand{Mode: mode}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetNavModeCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeStartSurvey(payload map[string]any) (uint32, []byte, error) {
	zoneId, ok := payload["zoneId"].(string)
	if !ok {
		return 0, nil, errors.New("missing zoneId field")
	}
	laneSpacing := float32(10.0)
	if v, ok := payload["laneSpacingM"].(float64); ok {
		laneSpacing = float32(v)
	}
	speed := float32(1.0)
	if v, ok := payload["speedMs"].(float64); ok {
		speed = float32(v)
	}
	msg := &StartSurveyCommand{
		ZoneId:       zoneId,
		LaneSpacingM: laneSpacing,
		SpeedMs:      speed,
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal StartSurveyCommand: %w", err)
	}
	return 1, data, nil
}

func (c *Codec) encodeSetDock(payload map[string]any) (uint32, []byte, error) {
	lat, ok := payload["dockLocationLat"].(float64)
	if !ok {
		return 0, nil, errors.New("missing 'dockLocationLat' field")
	}
	lng, ok := payload["dockLocationLng"].(float64)
	if !ok {
		return 0, nil, errors.New("missing 'dockLocationLng' field")
	}
	msg := &SetDockCommand{Latitude: lat, Longitude: lng}
	data, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal SetDockCommand: %w", err)
	}
	return 1, data, nil
}

func navModeToString(m NavMode) string {
	switch m {
	case NavMode_NAV_MODE_MANUAL:
		return "manual"
	case NavMode_NAV_MODE_HOLD:
		return "hold"
	case NavMode_NAV_MODE_WAYPOINT:
		return "waypoint"
	case NavMode_NAV_MODE_SURVEY:
		return "survey"
	case NavMode_NAV_MODE_RETURN_HOME:
		return "returnHome"
	case NavMode_NAV_MODE_LOITER:
		return "loiter"
	case NavMode_NAV_MODE_DOCKING:
		return "docking"
	default:
		return "unknown"
	}
}

func stringToNavMode(s string) (NavMode, error) {
	switch s {
	case "manual":
		return NavMode_NAV_MODE_MANUAL, nil
	case "hold":
		return NavMode_NAV_MODE_HOLD, nil
	case "waypoint":
		return NavMode_NAV_MODE_WAYPOINT, nil
	case "survey":
		return NavMode_NAV_MODE_SURVEY, nil
	case "returnHome":
		return NavMode_NAV_MODE_RETURN_HOME, nil
	case "loiter":
		return NavMode_NAV_MODE_LOITER, nil
	case "docking":
		return NavMode_NAV_MODE_DOCKING, nil
	default:
		return NavMode_NAV_MODE_UNKNOWN, fmt.Errorf("unknown nav mode: %s", s)
	}
}
