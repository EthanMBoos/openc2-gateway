// Package protocol provides translation between protobuf and JSON wire types.
package protocol

import (
	"errors"
	"fmt"
	"time"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
	"github.com/EthanMBoos/openc2-gateway/internal/extensions"
	"google.golang.org/protobuf/proto"
)

// nowMs returns the current time in milliseconds since Unix epoch.
// Extracted for testing (can be overridden in tests).
var nowMs = func() int64 {
	return time.Now().UnixMilli()
}

// ----------------------------------------------------------------------------
// Raw Bytes → JSON Frame (Primary Entry Point for UDP)
// ----------------------------------------------------------------------------

// DecodeVehicleMessage is the primary entry point for processing raw UDP datagrams.
// It performs size validation, protobuf unmarshal, message validation, and JSON
// translation in one call. Use this instead of calling individual functions.
//
// Returns the translated JSON Frame ready for WebSocket broadcast.
func DecodeVehicleMessage(data []byte) (*Frame, error) {
	// 1. Size validation (fail fast on oversized frames)
	if err := ValidateFrameSize(data); err != nil {
		return nil, fmt.Errorf("frame size: %w", err)
	}

	// 2. Protobuf unmarshal
	var msg pb.VehicleMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("protobuf unmarshal: %w", err)
	}

	// 3. Message validation (required fields, value constraints)
	if err := ValidateVehicleMessage(&msg); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	// 4. Translate to JSON frame
	return VehicleMessageToFrame(&msg)
}

// ----------------------------------------------------------------------------
// Proto → JSON Translation (Vehicle → UI)
// ----------------------------------------------------------------------------

// VehicleMessageToFrame converts a protobuf VehicleMessage envelope to a JSON Frame.
// This is the primary entry point for processing inbound vehicle messages.
func VehicleMessageToFrame(msg *pb.VehicleMessage) (*Frame, error) {
	if msg == nil {
		return nil, errors.New("nil vehicle message")
	}

	switch p := msg.Payload.(type) {
	case *pb.VehicleMessage_Telemetry:
		return TelemetryToFrame(p.Telemetry)
	case *pb.VehicleMessage_Heartbeat:
		return HeartbeatToFrame(p.Heartbeat)
	case *pb.VehicleMessage_Alert:
		return AlertToFrame(p.Alert)
	case *pb.VehicleMessage_CommandAck:
		return CommandAckToFrame(p.CommandAck)
	default:
		return nil, fmt.Errorf("unknown payload type: %T", msg.Payload)
	}
}

// TelemetryToFrame converts a protobuf VehicleTelemetry to a JSON Frame.
func TelemetryToFrame(msg *pb.VehicleTelemetry) (*Frame, error) {
	if msg == nil {
		return nil, errors.New("nil telemetry message")
	}
	if msg.VehicleId == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if msg.Location == nil {
		return nil, errors.New("missing location")
	}

	payload := TelemetryPayload{
		Location: Location{
			Lat: msg.Location.Latitude,
			Lng: msg.Location.Longitude,
		},
		Speed:       float64(msg.SpeedMs),
		Heading:     float64(msg.HeadingDeg),
		Environment: environmentToString(msg.Environment),
		Seq:         msg.SequenceNum,
	}

	// Optional altitude
	if msg.Location.AltitudeMslM != 0 {
		alt := float64(msg.Location.AltitudeMslM)
		payload.Location.AltMsl = &alt
	}

	// Optional battery (absence means unknown)
	if msg.BatteryPct != nil {
		pct := int(*msg.BatteryPct)
		payload.BatteryPct = &pct
	}

	// Optional signal strength (absence means unknown)
	if msg.SignalStrength != nil {
		strength := int(*msg.SignalStrength)
		payload.SignalStrength = &strength
	}

	// Extension capabilities advertised by the vehicle
	if len(msg.SupportedExtensions) > 0 {
		payload.SupportedExtensions = msg.SupportedExtensions
	}

	// Decode extension telemetry via registered codecs
	if len(msg.Extensions) > 0 {
		payload.Extensions = extensions.DecodeAll(msg.Extensions)
	}

	return &Frame{
		V:    ProtocolVersion,
		Type: TypeTelemetry,
		Vid:  msg.VehicleId,
		Ts:   msg.TimestampMs,
		Gts:  nowMs(),
		Data: payload,
	}, nil
}

// HeartbeatToFrame converts a protobuf Heartbeat to a JSON Frame.
func HeartbeatToFrame(msg *pb.Heartbeat) (*Frame, error) {
	if msg == nil {
		return nil, errors.New("nil heartbeat message")
	}
	if msg.VehicleId == "" {
		return nil, errors.New("missing vehicle_id")
	}

	payload := HeartbeatPayload{
		UptimeMs: msg.UptimeMs,
	}

	// Translate capabilities if present
	if msg.Capabilities != nil {
		payload.Capabilities = capabilitiesToJSON(msg.Capabilities)
	}

	return &Frame{
		V:    ProtocolVersion,
		Type: TypeHeartbeat,
		Vid:  msg.VehicleId,
		Ts:   msg.TimestampMs,
		Gts:  nowMs(),
		Data: payload,
	}, nil
}

// AlertToFrame converts a protobuf Alert to a JSON Frame.
func AlertToFrame(msg *pb.Alert) (*Frame, error) {
	if msg == nil {
		return nil, errors.New("nil alert message")
	}
	if msg.VehicleId == "" {
		return nil, errors.New("missing vehicle_id")
	}

	payload := AlertPayload{
		Severity: severityToString(msg.Severity),
		Code:     msg.Code,
		Message:  msg.Message,
	}

	// Optional location
	if msg.Location != nil {
		loc := Location{
			Lat: msg.Location.Latitude,
			Lng: msg.Location.Longitude,
		}
		if msg.Location.AltitudeMslM != 0 {
			alt := float64(msg.Location.AltitudeMslM)
			loc.AltMsl = &alt
		}
		payload.Location = &loc
	}

	return &Frame{
		V:    ProtocolVersion,
		Type: TypeAlert,
		Vid:  msg.VehicleId,
		Ts:   msg.TimestampMs,
		Gts:  nowMs(),
		Data: payload,
	}, nil
}

// CommandAckToFrame converts a protobuf CommandAck to a JSON Frame.
func CommandAckToFrame(msg *pb.CommandAck) (*Frame, error) {
	if msg == nil {
		return nil, errors.New("nil command_ack message")
	}
	if msg.VehicleId == "" {
		return nil, errors.New("missing vehicle_id")
	}

	payload := CommandAckPayload{
		CommandID: msg.CommandId,
		Status:    ackStatusToString(msg.Status),
	}

	if msg.Message != "" {
		payload.Message = &msg.Message
	}

	return &Frame{
		V:    ProtocolVersion,
		Type: TypeCommandAck,
		Vid:  msg.VehicleId,
		Ts:   msg.TimestampMs,
		Gts:  nowMs(),
		Data: payload,
	}, nil
}

// ----------------------------------------------------------------------------
// JSON → Proto Translation (UI → Vehicle)
// ----------------------------------------------------------------------------

// GotoCommandToProto converts a JSON GotoCommand to a protobuf Command.
func GotoCommandToProto(vid string, ts int64, cmd GotoCommand) (*pb.Command, error) {
	if vid == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if cmd.CommandID == "" {
		return nil, errors.New("missing command_id")
	}

	dest := &pb.Location{
		Latitude:  cmd.Destination.Lat,
		Longitude: cmd.Destination.Lng,
	}
	if cmd.Destination.AltMsl != nil {
		dest.AltitudeMslM = float32(*cmd.Destination.AltMsl)
	}

	gotoCmd := &pb.GotoCommand{
		Destination: dest,
	}
	if cmd.Speed != nil {
		gotoCmd.SpeedMs = float32(*cmd.Speed)
	}

	return &pb.Command{
		CommandId:   cmd.CommandID,
		VehicleId:   vid,
		TimestampMs: ts,
		Payload:     &pb.Command_Goto{Goto: gotoCmd},
	}, nil
}

// StopCommandToProto converts a JSON StopCommand to a protobuf Command.
func StopCommandToProto(vid string, ts int64, cmd StopCommand) (*pb.Command, error) {
	if vid == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if cmd.CommandID == "" {
		return nil, errors.New("missing command_id")
	}

	return &pb.Command{
		CommandId:   cmd.CommandID,
		VehicleId:   vid,
		TimestampMs: ts,
		Payload:     &pb.Command_Stop{Stop: &pb.StopCommand{}},
	}, nil
}

// ReturnHomeCommandToProto converts a JSON ReturnHomeCommand to a protobuf Command.
func ReturnHomeCommandToProto(vid string, ts int64, cmd ReturnHomeCommand) (*pb.Command, error) {
	if vid == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if cmd.CommandID == "" {
		return nil, errors.New("missing command_id")
	}

	return &pb.Command{
		CommandId:   cmd.CommandID,
		VehicleId:   vid,
		TimestampMs: ts,
		Payload:     &pb.Command_ReturnHome{ReturnHome: &pb.ReturnHomeCommand{}},
	}, nil
}

// SetModeCommandToProto converts a JSON SetModeCommand to a protobuf Command.
func SetModeCommandToProto(vid string, ts int64, cmd SetModeCommand) (*pb.Command, error) {
	if vid == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if cmd.CommandID == "" {
		return nil, errors.New("missing command_id")
	}

	mode, err := stringToMode(cmd.Mode)
	if err != nil {
		return nil, err
	}

	return &pb.Command{
		CommandId:   cmd.CommandID,
		VehicleId:   vid,
		TimestampMs: ts,
		Payload:     &pb.Command_SetMode{SetMode: &pb.SetModeCommand{Mode: mode}},
	}, nil
}

// SetSpeedCommandToProto converts a JSON SetSpeedCommand to a protobuf Command.
func SetSpeedCommandToProto(vid string, ts int64, cmd SetSpeedCommand) (*pb.Command, error) {
	if vid == "" {
		return nil, errors.New("missing vehicle_id")
	}
	if cmd.CommandID == "" {
		return nil, errors.New("missing command_id")
	}

	return &pb.Command{
		CommandId:   cmd.CommandID,
		VehicleId:   vid,
		TimestampMs: ts,
		Payload:     &pb.Command_SetSpeed{SetSpeed: &pb.SetSpeedCommand{SpeedMs: float32(cmd.Speed)}},
	}, nil
}

// ----------------------------------------------------------------------------
// Enum Conversions
// ----------------------------------------------------------------------------

func environmentToString(e pb.VehicleEnvironment) string {
	switch e {
	case pb.VehicleEnvironment_ENV_AIR:
		return EnvAir
	case pb.VehicleEnvironment_ENV_GROUND:
		return EnvGround
	case pb.VehicleEnvironment_ENV_SURFACE:
		return EnvSurface
	case pb.VehicleEnvironment_ENV_SUBSURFACE:
		return EnvSubsurface
	default:
		return EnvUnknown
	}
}

func stringToEnvironment(s string) pb.VehicleEnvironment {
	switch s {
	case EnvAir:
		return pb.VehicleEnvironment_ENV_AIR
	case EnvGround:
		return pb.VehicleEnvironment_ENV_GROUND
	case EnvSurface:
		return pb.VehicleEnvironment_ENV_SURFACE
	case EnvSubsurface:
		return pb.VehicleEnvironment_ENV_SUBSURFACE
	default:
		return pb.VehicleEnvironment_ENV_UNKNOWN
	}
}

func severityToString(s pb.AlertSeverity) string {
	switch s {
	case pb.AlertSeverity_SEVERITY_INFO:
		return SeverityInfo
	case pb.AlertSeverity_SEVERITY_WARNING:
		return SeverityWarning
	case pb.AlertSeverity_SEVERITY_ERROR:
		return SeverityError
	case pb.AlertSeverity_SEVERITY_CRITICAL:
		return SeverityCritical
	default:
		return SeverityInfo
	}
}

func ackStatusToString(s pb.AckStatus) string {
	switch s {
	case pb.AckStatus_ACK_ACCEPTED:
		return AckAccepted
	case pb.AckStatus_ACK_REJECTED:
		return AckRejected
	case pb.AckStatus_ACK_COMPLETED:
		return AckCompleted
	case pb.AckStatus_ACK_FAILED:
		return AckFailed
	default:
		return AckFailed
	}
}

func stringToMode(s string) (pb.OperationalMode, error) {
	switch s {
	case ModeManual:
		return pb.OperationalMode_MODE_MANUAL, nil
	case ModeAutonomous:
		return pb.OperationalMode_MODE_AUTONOMOUS, nil
	case ModeGuided:
		return pb.OperationalMode_MODE_GUIDED, nil
	default:
		return pb.OperationalMode_MODE_MANUAL, fmt.Errorf("unknown mode: %s", s)
	}
}

func statusToString(s pb.VehicleStatus) string {
	switch s {
	case pb.VehicleStatus_STATUS_ONLINE:
		return StatusOnline
	case pb.VehicleStatus_STATUS_OFFLINE:
		return StatusOffline
	case pb.VehicleStatus_STATUS_STANDBY:
		return StatusStandby
	default:
		return StatusOffline
	}
}

// ----------------------------------------------------------------------------
// Capability Conversions
// ----------------------------------------------------------------------------

// capabilitiesToJSON converts protobuf VehicleCapabilities to JSON struct.
func capabilitiesToJSON(caps *pb.VehicleCapabilities) *VehicleCapabilities {
	if caps == nil {
		return nil
	}

	result := &VehicleCapabilities{
		SupportedCommands: caps.SupportedCommands,
		SupportsMissions:  caps.SupportsMissions,
	}

	// Ensure slices are non-nil for JSON serialization ([] not null)
	if result.SupportedCommands == nil {
		result.SupportedCommands = []string{}
	}

	// Translate extension capabilities
	if len(caps.Extensions) > 0 {
		result.Extensions = make([]ExtensionCapability, 0, len(caps.Extensions))
		for _, ext := range caps.Extensions {
			result.Extensions = append(result.Extensions, extensionCapToJSON(ext))
		}
	} else {
		result.Extensions = []ExtensionCapability{}
	}

	// Translate sensors
	if len(caps.Sensors) > 0 {
		result.Sensors = make([]SensorCapability, 0, len(caps.Sensors))
		for _, s := range caps.Sensors {
			result.Sensors = append(result.Sensors, sensorToJSON(s))
		}
	}

	return result
}

// extensionCapToJSON converts protobuf ExtensionCapability to JSON struct.
func extensionCapToJSON(ext *pb.ExtensionCapability) ExtensionCapability {
	cap := ExtensionCapability{
		Namespace: ext.Namespace,
		Version:   ext.Version,
	}

	if len(ext.SupportedActions) > 0 {
		cap.SupportedActions = ext.SupportedActions
	} else {
		cap.SupportedActions = []string{} // empty = all actions
	}

	return cap
}

// sensorToJSON converts a protobuf SensorCapability to JSON struct.
func sensorToJSON(s *pb.SensorCapability) SensorCapability {
	sensor := SensorCapability{
		SensorID:  s.SensorId,
		Type:      sensorTypeToString(s.Type),
		StreamURL: s.StreamUrl,
		Metadata:  s.Metadata,
	}

	// Translate mount if present
	if s.Mount != nil {
		sensor.Mount = &SensorMount{
			X:     float64(s.Mount.X),
			Y:     float64(s.Mount.Y),
			Z:     float64(s.Mount.Z),
			Roll:  float64(s.Mount.Roll),
			Pitch: float64(s.Mount.Pitch),
			Yaw:   float64(s.Mount.Yaw),
		}
	}

	return sensor
}

// sensorTypeToString converts protobuf SensorType to JSON string.
func sensorTypeToString(t pb.SensorType) string {
	switch t {
	case pb.SensorType_SENSOR_CAMERA_RGB:
		return SensorCameraRGB
	case pb.SensorType_SENSOR_CAMERA_THERMAL:
		return SensorCameraThermal
	case pb.SensorType_SENSOR_CAMERA_DEPTH:
		return SensorCameraDepth
	case pb.SensorType_SENSOR_LIDAR_2D:
		return SensorLidar2D
	case pb.SensorType_SENSOR_LIDAR_3D:
		return SensorLidar3D
	case pb.SensorType_SENSOR_SONAR:
		return SensorSonar
	case pb.SensorType_SENSOR_RADAR:
		return SensorRadar
	case pb.SensorType_SENSOR_IMU:
		return SensorIMU
	case pb.SensorType_SENSOR_GPS:
		return SensorGPS
	default:
		return SensorUnknown
	}
}
