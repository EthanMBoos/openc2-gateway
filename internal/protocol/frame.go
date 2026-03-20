// Package protocol defines the JSON wire types for gateway ↔ UI communication.
// These types implement the PROTOCOL.md specification and are decoupled from
// the protobuf types used for vehicle ↔ gateway communication.
//
// NAMING CONVENTION:
// - Protobuf (vehicle ↔ gateway): snake_case (e.g., battery_pct, vehicle_id)
// - JSON (gateway ↔ UI): camelCase (e.g., batteryPct, vehicleId)
//
// This is intentional and follows industry conventions for each format.
// The translate.go file handles the mapping between these conventions.
package protocol

// Frame is the envelope for all JSON messages over WebSocket.
// All messages follow this structure regardless of type.
type Frame struct {
	ProtocolVersion    int         `json:"protocolVersion"`              // Protocol version (currently 1)
	Type               string      `json:"type"`                         // Message type identifier
	VehicleID          string      `json:"vehicleId"`                    // Vehicle ID (source or target)
	TimestampMs        int64       `json:"timestampMs"`                  // Vehicle timestamp (UNTRUSTED - display only)
	GatewayTimestampMs int64       `json:"gatewayTimestampMs,omitempty"` // Gateway timestamp (authoritative)
	Data               interface{} `json:"data"`                         // Type-specific payload
}

// ProtocolVersion is the current protocol version.
const ProtocolVersion = 1

// ----------------------------------------------------------------------------
// Message Types
// ----------------------------------------------------------------------------

const (
	TypeTelemetry   = "telemetry"
	TypeStatus      = "status"
	TypeHeartbeat   = "heartbeat"
	TypeCommandAck  = "command_ack"
	TypeAlert       = "alert"
	TypeFleetStatus = "fleet_status"
	TypeCommand     = "command"
	TypeHello       = "hello"
	TypeWelcome     = "welcome"
	TypeError       = "error"
)

// Special vehicle IDs used for system messages
const (
	VehicleIDGateway = "_gateway" // Messages originating from the gateway
	VehicleIDClient  = "_client"  // Messages originating from UI clients
	VehicleIDFleet   = "_fleet"   // Fleet-wide broadcast messages
)

// ----------------------------------------------------------------------------
// Location
// ----------------------------------------------------------------------------

// Location represents a geographic position in WGS84 coordinates.
type Location struct {
	Lat    float64  `json:"lat"`               // Latitude in degrees (-90 to 90)
	Lng    float64  `json:"lng"`               // Longitude in degrees (-180 to 180)
	AltMsl *float64 `json:"alt_msl,omitempty"` // Altitude MSL in meters
}

// ----------------------------------------------------------------------------
// Telemetry
// ----------------------------------------------------------------------------

// TelemetryPayload contains position, velocity, and state data.
type TelemetryPayload struct {
	Location            Location       `json:"location"`
	Speed               float64        `json:"speed"`                         // Speed in m/s
	Heading             float64        `json:"heading"`                       // Heading in degrees [0, 360)
	Environment         string         `json:"environment"`                   // air, ground, surface
	Seq                 uint32         `json:"seq"`                           // Monotonic sequence number for ordering
	BatteryPercent      *int           `json:"batteryPct,omitempty"`          // 0-100, nil if unknown
	SignalStrength      *int           `json:"signalStrength,omitempty"`      // 0-5 bars, nil if unknown
	SupportedExtensions []string       `json:"supportedExtensions,omitempty"` // Namespaces this vehicle supports
	Extensions          map[string]any `json:"extensions,omitempty"`          // Decoded extension telemetry
}

// ----------------------------------------------------------------------------
// Status
// ----------------------------------------------------------------------------

// StatusPayload contains vehicle operational status.
type StatusPayload struct {
	Status         string `json:"status"`                   // online, offline, standby
	SignalStrength *int   `json:"signalStrength,omitempty"` // 0-5 bars, nil if unknown
	Source         string `json:"source"`                   // Telemetry source identifier
}

// Status values
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusStandby = "standby"
)

// ----------------------------------------------------------------------------
// Heartbeat
// ----------------------------------------------------------------------------

// HeartbeatPayload contains connection health data.
type HeartbeatPayload struct {
	UptimeMs     int64                `json:"uptimeMs"`               // Vehicle uptime in milliseconds
	Capabilities *VehicleCapabilities `json:"capabilities,omitempty"` // What this vehicle supports
}

// ----------------------------------------------------------------------------
// Vehicle Capabilities
// ----------------------------------------------------------------------------

// VehicleCapabilities advertises what commands/features a vehicle supports.
// This prevents the UI from showing buttons for unsupported actions.
type VehicleCapabilities struct {
	// Core commands this vehicle supports: "goto", "stop", "return_home", "set_mode", "set_speed"
	SupportedCommands []string `json:"supportedCommands"`

	// Extension capabilities with specific supported actions
	Extensions []ExtensionCapability `json:"extensions"`

	// Whether vehicle accepts mission waypoint sequences
	SupportsMissions bool `json:"supportsMissions"`

	// Sensors attached to this vehicle
	Sensors []SensorCapability `json:"sensors,omitempty"`
}

// ExtensionCapability advertises which actions a vehicle supports within an extension.
type ExtensionCapability struct {
	// Extension namespace (e.g., "husky", "camera")
	Namespace string `json:"namespace"`

	// Schema version this vehicle implements
	Version uint32 `json:"version"`

	// Specific actions this vehicle supports within the extension.
	// Empty means all actions; populated means only these specific actions.
	SupportedActions []string `json:"supportedActions"`
}

// SensorCapability describes an attached sensor with stream info.
type SensorCapability struct {
	SensorID  string            `json:"sensorId"`            // Unique sensor ID on this vehicle
	Type      string            `json:"type"`                // camera_rgb, camera_thermal, lidar_3d, etc.
	StreamURL string            `json:"streamUrl,omitempty"` // rtsp://, http://, ws://
	Mount     *SensorMount      `json:"mount,omitempty"`     // Physical mounting position
	Metadata  map[string]string `json:"metadata,omitempty"`  // Type-specific metadata
}

// SensorMount describes the physical mounting of a sensor.
type SensorMount struct {
	X     float64 `json:"x"`     // Position offset in meters (forward)
	Y     float64 `json:"y"`     // Position offset in meters (left)
	Z     float64 `json:"z"`     // Position offset in meters (up)
	Roll  float64 `json:"roll"`  // Euler angle in degrees
	Pitch float64 `json:"pitch"` // Euler angle in degrees
	Yaw   float64 `json:"yaw"`   // Euler angle in degrees
}

// Sensor type values
const (
	SensorUnknown       = "unknown"
	SensorCameraRGB     = "camera_rgb"
	SensorCameraThermal = "camera_thermal"
	SensorCameraDepth   = "camera_depth"
	SensorLidar2D       = "lidar_2d"
	SensorLidar3D       = "lidar_3d"
	SensorRadar         = "radar"
	SensorIMU           = "imu"
	SensorGPS           = "gps"
)

// ----------------------------------------------------------------------------
// Command Acknowledgment
// ----------------------------------------------------------------------------

// CommandAckPayload contains command response data.
type CommandAckPayload struct {
	CommandID string  `json:"commandId"`         // ID of the acknowledged command
	Status    string  `json:"status"`            // accepted, rejected, completed, failed
	Message   *string `json:"message,omitempty"` // Human-readable status message
}

// Ack status values
const (
	AckAccepted  = "accepted"
	AckRejected  = "rejected"
	AckCompleted = "completed"
	AckFailed    = "failed"
	AckTimeout   = "timeout" // Synthetic: gateway sends when vehicle doesn't respond
)

// ----------------------------------------------------------------------------
// Alert
// ----------------------------------------------------------------------------

// AlertPayload contains warning/error event data.
type AlertPayload struct {
	Severity string    `json:"severity"`           // info, warning, error, critical
	Code     string    `json:"code"`               // Machine-readable alert code
	Message  string    `json:"message"`            // Human-readable description
	Location *Location `json:"location,omitempty"` // Where the alert occurred
}

// Alert severity values
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

// ----------------------------------------------------------------------------
// Fleet Status
// ----------------------------------------------------------------------------

// FleetStatusPayload contains fleet summary data.
type FleetStatusPayload struct {
	Vehicles     []VehicleSummary `json:"vehicles"`
	TotalOnline  int              `json:"totalOnline"`
	TotalOffline int              `json:"totalOffline"`
}

// VehicleSummary is a brief vehicle overview for fleet status.
type VehicleSummary struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Status       string               `json:"status"`                 // online, offline, standby
	Environment  string               `json:"environment"`            // air, ground, surface
	LastSeen     int64                `json:"lastSeen"`               // Unix timestamp (ms)
	Capabilities *VehicleCapabilities `json:"capabilities,omitempty"` // What this vehicle supports
}

// ----------------------------------------------------------------------------
// Commands (UI → Gateway)
// ----------------------------------------------------------------------------

// CommandPayload is the base for all command types.
// Use GotoCommand, StopCommand, etc. for specific actions.
type CommandPayload interface {
	Action() string
	GetCommandID() string
}

// GotoCommand navigates to a destination.
type GotoCommand struct {
	CommandID   string   `json:"commandId"`
	Destination Location `json:"destination"`
	Speed       *float64 `json:"speed,omitempty"` // Target speed in m/s
}

func (c GotoCommand) Action() string       { return "goto" }
func (c GotoCommand) GetCommandID() string { return c.CommandID }

// StopCommand issues an emergency stop.
type StopCommand struct {
	CommandID string `json:"commandId"`
}

func (c StopCommand) Action() string       { return "stop" }
func (c StopCommand) GetCommandID() string { return c.CommandID }

// ReturnHomeCommand returns to home/launch position.
type ReturnHomeCommand struct {
	CommandID string `json:"commandId"`
}

func (c ReturnHomeCommand) Action() string       { return "return_home" }
func (c ReturnHomeCommand) GetCommandID() string { return c.CommandID }

// SetModeCommand changes the operational mode.
type SetModeCommand struct {
	CommandID string `json:"commandId"`
	Mode      string `json:"mode"` // manual, autonomous, guided
}

func (c SetModeCommand) Action() string       { return "set_mode" }
func (c SetModeCommand) GetCommandID() string { return c.CommandID }

// SetSpeedCommand changes the target speed.
type SetSpeedCommand struct {
	CommandID string  `json:"commandId"`
	Speed     float64 `json:"speed"` // Speed in m/s
}

func (c SetSpeedCommand) Action() string       { return "set_speed" }
func (c SetSpeedCommand) GetCommandID() string { return c.CommandID }

// Mode values
const (
	ModeManual     = "manual"
	ModeAutonomous = "autonomous"
	ModeGuided     = "guided"
)

// ExtensionCommandInput is the parsed form of an extension command from the UI.
// Wire format: {"action":"extension","namespace":"husky","payload":{"type":"setDriveMode","mode":"autonomous"}}
// The payload.type field is the action routed to the codec's EncodeCommand method.
type ExtensionCommandInput struct {
	CommandID string         `json:"commandId"`
	Namespace string         `json:"namespace"`
	Payload   map[string]any `json:"payload"` // Must contain "type" key identifying the action
}

// ExtensionAction returns the action name from the payload (payload.type).
// Returns empty string if the payload is nil or "type" is not a string.
func (e ExtensionCommandInput) ExtensionAction() string {
	if e.Payload == nil {
		return ""
	}
	t, _ := e.Payload["type"].(string)
	return t
}

// ----------------------------------------------------------------------------
// Hello / Welcome (Handshake)
// ----------------------------------------------------------------------------

// HelloPayload is sent by clients to initiate connection.
type HelloPayload struct {
	ProtocolVersion int     `json:"protocolVersion"`
	ClientID        string  `json:"clientId"`
	ClientType      *string `json:"clientType,omitempty"` // ui, monitor, replay
}

// WelcomePayload is sent by gateway in response to hello.
type WelcomePayload struct {
	GatewayVersion  string           `json:"gatewayVersion"`
	ProtocolVersion int              `json:"protocolVersion"`
	Fleet           []VehicleSummary `json:"fleet"`
	Config          WelcomeConfig    `json:"config"`
}

// WelcomeConfig contains gateway configuration shared with clients.
type WelcomeConfig struct {
	TelemetryRateHz     int `json:"telemetryRateHz"`
	HeartbeatIntervalMs int `json:"heartbeatIntervalMs"`
}

// ----------------------------------------------------------------------------
// Error
// ----------------------------------------------------------------------------

// ErrorPayload contains error details.
type ErrorPayload struct {
	Code      string  `json:"code"`                // Machine-readable error code
	Message   string  `json:"message"`             // Human-readable description
	CommandID *string `json:"commandId,omitempty"` // Associated command ID (for command errors)
}

// Error codes
const (
	ErrInvalidMessage             = "INVALID_MESSAGE"
	ErrUnknownCommand             = "UNKNOWN_COMMAND"
	ErrVehicleNotFound            = "VEHICLE_NOT_FOUND"
	ErrRateLimited                = "RATE_LIMITED" // Per-vehicle limit (10/sec). Global limit not implemented — 1-2 operators won't saturate multicast. Add if multi-operator deployments need it.
	ErrProtocolVersionUnsupported = "PROTOCOL_VERSION_UNSUPPORTED"
	ErrCommandSendFailed          = "COMMAND_SEND_FAILED"
	ErrCommandNotSupported        = "COMMAND_NOT_SUPPORTED" // Vehicle doesn't support this command (per capabilities)
)

// Environment values
const (
	EnvAir     = "air"
	EnvGround  = "ground"
	EnvSurface = "surface"
	EnvUnknown = "unknown"
)
