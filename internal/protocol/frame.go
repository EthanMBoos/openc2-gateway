// Package protocol defines the JSON wire types for server ↔ UI communication.
// These types implement the PROTOCOL.md specification and are decoupled from
// the protobuf types used for vehicle ↔ server communication.
//
// NAMING CONVENTION:
// - Protobuf (vehicle ↔ server): snake_case (e.g., battery_pct, vehicle_id)
// - JSON (server ↔ UI): camelCase (e.g., batteryPercent, vehicleId)
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
	ServerTimestampMs int64       `json:"serverTimestampMs,omitempty"` // Server timestamp (authoritative)
	Command            string      `json:"command,omitempty"`            // Command type (for type="command" frames)
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
	VehicleIDServer = "_server" // Messages originating from the server
	VehicleIDClient  = "_client"  // Messages originating from UI clients
	VehicleIDFleet   = "_fleet"   // Fleet-wide broadcast messages
)

// ----------------------------------------------------------------------------
// Location
// ----------------------------------------------------------------------------

// Location represents a geographic position in WGS84 coordinates.
type Location struct {
	Lat    float64  `json:"lat"`              // Latitude in degrees (-90 to 90)
	Lng    float64  `json:"lng"`              // Longitude in degrees (-180 to 180)
	AltMsl *float64 `json:"altMsl,omitempty"` // Altitude MSL in meters
}

// ----------------------------------------------------------------------------
// Telemetry
// ----------------------------------------------------------------------------

// TelemetryPayload contains position, velocity, and state data.
type TelemetryPayload struct {
	Location            Location       `json:"location"`
	Speed               float64        `json:"speed"`                         // Speed in m/s
	Heading             float64        `json:"heading"`                       // Heading in degrees [0, 360)
	Environment         string         `json:"environment"`                   // air, ground, marine
	Seq                 uint32         `json:"seq"`                           // Monotonic sequence number for ordering
	BatteryPercent      *int           `json:"batteryPercent,omitempty"`      // 0-100, nil if unknown
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
	VehicleType  string               `json:"vehicleType,omitempty"`  // Type identifier for UI auto-matching (e.g., "clearpath-husky-a200")
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
	AckTimeout   = "timeout" // Synthetic: server sends when vehicle doesn't respond
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
	Environment  string               `json:"environment"`            // air, ground, marine
	LastSeen     int64                `json:"lastSeen"`               // Unix timestamp (ms)
	Capabilities *VehicleCapabilities `json:"capabilities,omitempty"` // What this vehicle supports
}

// ----------------------------------------------------------------------------
// Commands (UI → Server)
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

// WelcomePayload is sent by server in response to hello.
//
// EXTENSION DISCOVERY FLOW:
// The server is the single source of truth for what extensions exist. On startup,
// codecs self-register via init() and the server collects their namespaces/versions.
// When a UI connects:
//
//  1. Server sends `welcome` with:
//     - AvailableExtensions: namespaces the server can decode (from registered codecs)
//     - Manifests: UI metadata per extension (commands, labels, confirmation flags)
//
//  2. Vehicles advertise which extensions they support via `supportedExtensions` in telemetry
//
//  3. UI filters command buttons by: server.available ∩ vehicle.supported
//     - A button only appears if BOTH server can route it AND vehicle can execute it
//
// This decouples extension releases from server releases — adding a new extension
// is a single codec import in cmd/tower-server/main.go, no config files needed.
//
// See docs/EXTENSIBILITY.md for the full extension architecture.
type WelcomePayload struct {
	ServerVersion      string                       `json:"serverVersion"`
	ProtocolVersion     int                          `json:"protocolVersion"`
	SupportedVersions   []int                        `json:"supportedVersions"` // All protocol versions server can speak
	Fleet               []VehicleSummary             `json:"fleet"`
	Config              WelcomeConfig                `json:"config"`
	AvailableExtensions []AvailableExtension         `json:"availableExtensions,omitempty"` // Extensions the server can decode
	Manifests           map[string]ExtensionManifest `json:"manifests,omitempty"`           // Full manifest per extension namespace
}

// AvailableExtension describes an extension the server can decode.
// Collected from registered codecs at startup.
type AvailableExtension struct {
	Namespace string `json:"namespace"` // Extension namespace (e.g., "husky")
	Version   uint32 `json:"version"`   // Schema version supported by this codec
}

// ExtensionManifest describes an extension's UI integration.
// Sent to UI so it can render command buttons without hardcoding.
type ExtensionManifest struct {
	Namespace   string                       `json:"namespace"`
	Version     string                       `json:"version"`     // Semantic version (e.g., "1.0.0")
	DisplayName string                       `json:"displayName"` // Human-readable name
	Commands    []ExtensionCommandDefinition `json:"commands"`    // Available commands
}

// ExtensionCommandDefinition describes a command within an extension.
type ExtensionCommandDefinition struct {
	Command      string             `json:"command"`                // Command identifier (e.g., "setDriveMode")
	Label        string             `json:"label"`                  // UI button label
	Description  *string            `json:"description,omitempty"`  // Tooltip text
	Confirmation bool               `json:"confirmation,omitempty"` // Requires user confirmation before sending
	Parameters   []CommandParameter `json:"parameters,omitempty"`   // Payload schema for UI input fields
	TargetMode   string             `json:"targetMode,omitempty"`   // "single", "broadcast", or "both" (default: "single")
}

// CommandParameter defines an input field for a command payload.
// The UI uses this to render appropriate form controls.
type CommandParameter struct {
	Name        string            `json:"name"`                  // Key in the payload (e.g., "mode", "zoneId")
	Label       string            `json:"label"`                 // UI label
	Type        string            `json:"type"`                  // Parameter type (see ParameterType* constants)
	Required    bool              `json:"required,omitempty"`    // Whether the parameter is required
	Default     any               `json:"default,omitempty"`     // Default value
	Options     []ParameterOption `json:"options,omitempty"`     // For "select" type: available choices
	Description *string           `json:"description,omitempty"` // Help text
	Min         *float64          `json:"min,omitempty"`         // For "number" type: minimum value
	Max         *float64          `json:"max,omitempty"`         // For "number" type: maximum value
}

// ParameterOption defines a choice for "select" type parameters.
type ParameterOption struct {
	Value string `json:"value"` // Value sent in payload
	Label string `json:"label"` // Display label in UI
}

// Parameter types for CommandParameter.Type
const (
	ParamTypeString      = "string"      // Free text input
	ParamTypeNumber      = "number"      // Numeric input (optionally bounded by min/max)
	ParamTypeBoolean     = "boolean"     // Toggle/checkbox
	ParamTypeSelect      = "select"      // Dropdown from Options
	ParamTypeZone        = "zone"        // Zone picker from map features
	ParamTypeCoordinates = "coordinates" // Lat/lng picker from map
	ParamTypeVehicle     = "vehicle"     // Vehicle instance picker
)

// Target modes for ExtensionCommandDefinition.TargetMode
// This is manifest-level metadata that tells the UI what targeting options to present.
const (
	TargetModeSingle    = "single"    // Command targets one specific vehicle instance
	TargetModeBroadcast = "broadcast" // Command targets all vehicles supporting this extension
	TargetModeBoth      = "both"      // UI shows a toggle letting operator choose single OR broadcast at command time
)

// WelcomeConfig contains server configuration shared with clients.
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
	EnvMarine  = "marine"
	EnvUnknown = "unknown"
)
