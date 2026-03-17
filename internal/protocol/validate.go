// Package protocol provides validation for protocol messages.
package protocol

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
)

// ----------------------------------------------------------------------------
// Rate-limited warnings
// ----------------------------------------------------------------------------

var (
	envUnknownLastWarn   time.Time
	envUnknownWarnMu     sync.Mutex
	envUnknownWarnPeriod = time.Second // Max 1 warning per second
)

// warnEnvUnknown logs a rate-limited warning for ENV_UNKNOWN.
func warnEnvUnknown(vehicleID string) {
	envUnknownWarnMu.Lock()
	defer envUnknownWarnMu.Unlock()
	if time.Since(envUnknownLastWarn) > envUnknownWarnPeriod {
		envUnknownLastWarn = time.Now()
		slog.Warn("telemetry.env_unknown",
			"vid", vehicleID,
			"hint", "Vehicle did not set environment field; using 'unknown'",
		)
	}
}

// ----------------------------------------------------------------------------
// Frame Size Validation (UDP Datagram Limits)
// ----------------------------------------------------------------------------

// MaxUDPFrameSize is the maximum recommended frame size for UDP datagrams.
// Staying under 1400 bytes avoids IP fragmentation on typical networks
// (1500 byte MTU minus IP/UDP headers). Fragmented UDP is unreliable —
// if any fragment is lost, the entire datagram is dropped.
const MaxUDPFrameSize = 1400

// ValidateFrameSize checks that raw UDP payload doesn't exceed the safe limit.
// Call this BEFORE attempting protobuf unmarshal to fail fast on oversized frames.
func ValidateFrameSize(data []byte) error {
	if len(data) > MaxUDPFrameSize {
		return fmt.Errorf("frame size %d exceeds maximum %d bytes", len(data), MaxUDPFrameSize)
	}
	if len(data) == 0 {
		return fmt.Errorf("empty frame")
	}
	return nil
}

// ----------------------------------------------------------------------------
// Protobuf Validation (Inbound from Vehicles)
// ----------------------------------------------------------------------------

// ValidateVehicleMessage validates the VehicleMessage envelope and its payload.
// This is the primary entry point for validating inbound vehicle messages.
func ValidateVehicleMessage(msg *pb.VehicleMessage) error {
	if msg == nil {
		return fmt.Errorf("nil vehicle message")
	}

	switch p := msg.Payload.(type) {
	case *pb.VehicleMessage_Telemetry:
		return ValidateTelemetry(p.Telemetry)
	case *pb.VehicleMessage_Heartbeat:
		return ValidateHeartbeat(p.Heartbeat)
	case *pb.VehicleMessage_Alert:
		return ValidateAlert(p.Alert)
	case *pb.VehicleMessage_CommandAck:
		return ValidateCommandAck(p.CommandAck)
	case nil:
		return fmt.Errorf("empty payload")
	default:
		return fmt.Errorf("unknown payload type: %T", msg.Payload)
	}
}

// ValidateTelemetry checks a VehicleTelemetry message for required fields and constraints.
func ValidateTelemetry(msg *pb.VehicleTelemetry) error {
	if msg == nil {
		return fmt.Errorf("nil telemetry message")
	}
	if msg.VehicleId == "" {
		return fmt.Errorf("missing vehicle_id")
	}
	if err := validateTimestamp(msg.TimestampMs); err != nil {
		return err
	}
	if msg.Location == nil {
		return fmt.Errorf("missing location")
	}
	if err := validateLocation(msg.Location); err != nil {
		return fmt.Errorf("invalid location: %w", err)
	}
	if msg.SpeedMs < 0 {
		return fmt.Errorf("speed_ms cannot be negative: %f", msg.SpeedMs)
	}
	if msg.HeadingDeg < 0 || msg.HeadingDeg >= 360 {
		return fmt.Errorf("heading_deg must be [0, 360): %f", msg.HeadingDeg)
	}
	if msg.SignalStrength != nil && *msg.SignalStrength > 5 {
		return fmt.Errorf("signal_strength must be 0-5: %d", *msg.SignalStrength)
	}
	if msg.BatteryPct != nil && *msg.BatteryPct > 100 {
		return fmt.Errorf("battery_pct must be 0-100: %d", *msg.BatteryPct)
	}

	// ENV_UNKNOWN is valid (proto3 default) but log a warning to catch misconfigurations.
	// We don't reject because: (1) graceful degradation > hard failure, (2) sensors fail,
	// (3) new platforms may not have environment classification yet.
	if msg.Environment == pb.VehicleEnvironment_ENV_UNKNOWN {
		warnEnvUnknown(msg.VehicleId)
	}

	return nil
}

// ValidateHeartbeat checks a Heartbeat message for required fields.
func ValidateHeartbeat(msg *pb.Heartbeat) error {
	if msg == nil {
		return fmt.Errorf("nil heartbeat message")
	}
	if msg.VehicleId == "" {
		return fmt.Errorf("missing vehicle_id")
	}
	if err := validateTimestamp(msg.TimestampMs); err != nil {
		return err
	}
	return nil
}

// ValidateAlert checks an Alert message for required fields.
func ValidateAlert(msg *pb.Alert) error {
	if msg == nil {
		return fmt.Errorf("nil alert message")
	}
	if msg.VehicleId == "" {
		return fmt.Errorf("missing vehicle_id")
	}
	if err := validateTimestamp(msg.TimestampMs); err != nil {
		return err
	}
	if msg.Code == "" {
		return fmt.Errorf("missing alert code")
	}
	if msg.Location != nil {
		if err := validateLocation(msg.Location); err != nil {
			return fmt.Errorf("invalid location: %w", err)
		}
	}
	return nil
}

// ValidateCommandAck checks a CommandAck message for required fields.
func ValidateCommandAck(msg *pb.CommandAck) error {
	if msg == nil {
		return fmt.Errorf("nil command_ack message")
	}
	if msg.VehicleId == "" {
		return fmt.Errorf("missing vehicle_id")
	}
	if msg.CommandId == "" {
		return fmt.Errorf("missing command_id")
	}
	if err := validateTimestamp(msg.TimestampMs); err != nil {
		return err
	}
	return nil
}

// Timestamp bounds: 2020-01-01 to 2100-01-01 (milliseconds)
const (
	minTimestampMs = 1577836800000 // 2020-01-01 00:00:00 UTC
	maxTimestampMs = 4102444800000 // 2100-01-01 00:00:00 UTC
)

func validateTimestamp(ts int64) error {
	if ts < minTimestampMs || ts > maxTimestampMs {
		return fmt.Errorf("timestamp_ms out of valid range (2020-2100): %d", ts)
	}
	return nil
}

func validateLocation(loc *pb.Location) error {
	if loc.Latitude < -90 || loc.Latitude > 90 {
		return fmt.Errorf("latitude must be -90 to 90: %f", loc.Latitude)
	}
	if loc.Longitude < -180 || loc.Longitude > 180 {
		return fmt.Errorf("longitude must be -180 to 180: %f", loc.Longitude)
	}
	return nil
}

// ----------------------------------------------------------------------------
// JSON Validation (Inbound from UI)
// ----------------------------------------------------------------------------

// ValidateGotoCommand checks a GotoCommand for required fields and constraints.
func ValidateGotoCommand(cmd GotoCommand) error {
	if cmd.CommandID == "" {
		return fmt.Errorf("missing commandId")
	}
	if err := validateJSONLocation(cmd.Destination); err != nil {
		return fmt.Errorf("invalid destination: %w", err)
	}
	if cmd.Speed != nil && *cmd.Speed < 0 {
		return fmt.Errorf("speed cannot be negative: %f", *cmd.Speed)
	}
	return nil
}

// ValidateStopCommand checks a StopCommand for required fields.
func ValidateStopCommand(cmd StopCommand) error {
	if cmd.CommandID == "" {
		return fmt.Errorf("missing commandId")
	}
	return nil
}

// ValidateReturnHomeCommand checks a ReturnHomeCommand for required fields.
func ValidateReturnHomeCommand(cmd ReturnHomeCommand) error {
	if cmd.CommandID == "" {
		return fmt.Errorf("missing commandId")
	}
	return nil
}

// ValidateSetModeCommand checks a SetModeCommand for required fields and valid mode.
func ValidateSetModeCommand(cmd SetModeCommand) error {
	if cmd.CommandID == "" {
		return fmt.Errorf("missing commandId")
	}
	switch cmd.Mode {
	case ModeManual, ModeAutonomous, ModeGuided:
		// valid
	default:
		return fmt.Errorf("invalid mode: %s (expected: manual, autonomous, guided)", cmd.Mode)
	}
	return nil
}

// ValidateSetSpeedCommand checks a SetSpeedCommand for required fields and constraints.
func ValidateSetSpeedCommand(cmd SetSpeedCommand) error {
	if cmd.CommandID == "" {
		return fmt.Errorf("missing commandId")
	}
	if cmd.Speed < 0 {
		return fmt.Errorf("speed cannot be negative: %f", cmd.Speed)
	}
	return nil
}

// ValidateHelloPayload checks a HelloPayload for required fields.
func ValidateHelloPayload(payload HelloPayload) error {
	if payload.ProtocolVersion < 1 {
		return fmt.Errorf("invalid protocolVersion: %d", payload.ProtocolVersion)
	}
	if payload.ClientID == "" {
		return fmt.Errorf("missing clientId")
	}
	return nil
}

func validateJSONLocation(loc Location) error {
	if loc.Lat < -90 || loc.Lat > 90 {
		return fmt.Errorf("lat must be -90 to 90: %f", loc.Lat)
	}
	if loc.Lng < -180 || loc.Lng > 180 {
		return fmt.Errorf("lng must be -180 to 180: %f", loc.Lng)
	}
	return nil
}
