// Package command provides command routing from UI to vehicles.
// Commands are validated, rate-limited, converted to protobuf, and broadcast
// via UDP multicast.
package command

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
	"github.com/EthanMBoos/openc2-gateway/internal/extensions"
	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
	"github.com/EthanMBoos/openc2-gateway/internal/registry"
	"google.golang.org/protobuf/proto"
)

// RouterConfig configures the command router.
type RouterConfig struct {
	MulticastGroup string // Command multicast group (e.g., "239.255.0.2")
	MulticastPort  int    // Command port (e.g., 14551)
}

// DefaultRouterConfig returns defaults per PROTOCOL.md.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		MulticastGroup: "239.255.0.2",
		MulticastPort:  14551,
	}
}

// Router handles command routing from UI clients to vehicles.
type Router struct {
	config   RouterConfig
	registry *registry.Registry
	tracker  *Tracker

	// Multicast connection for sending commands
	mu   sync.RWMutex
	conn *net.UDPConn
	addr *net.UDPAddr
}

// NewRouter creates a new command router.
func NewRouter(cfg RouterConfig, reg *registry.Registry, tracker *Tracker) *Router {
	return &Router{
		config:   cfg,
		registry: reg,
		tracker:  tracker,
	}
}

// Start initializes the multicast connection for sending commands.
func (r *Router) Start() error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", r.config.MulticastGroup, r.config.MulticastPort))
	if err != nil {
		return fmt.Errorf("resolve command multicast addr: %w", err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dial command multicast: %w", err)
	}

	r.mu.Lock()
	r.conn = conn
	r.addr = addr
	r.mu.Unlock()

	slog.Info("command router started",
		"group", r.config.MulticastGroup,
		"port", r.config.MulticastPort,
	)

	return nil
}

// Stop closes the multicast connection.
func (r *Router) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		err := r.conn.Close()
		r.conn = nil
		return err
	}
	return nil
}

// RouteResult is returned by Route to indicate success or failure.
type RouteResult struct {
	Success bool
	Frame   *protocol.Frame // Response frame to send back to client
}

// Route processes a command from a UI client.
// Returns a response frame (ack or error) to send back to the client.
func (r *Router) Route(frame *protocol.Frame) RouteResult {
	// Parse the command data
	dataBytes, err := json.Marshal(frame.Data)
	if err != nil {
		return r.errorResult(protocol.ErrInvalidMessage, "failed to marshal command data")
	}

	// Determine command type from the "action" field
	var actionData struct {
		Action    string `json:"action"`
		CommandID string `json:"commandId"`
	}
	if err := json.Unmarshal(dataBytes, &actionData); err != nil {
		return r.errorResult(protocol.ErrInvalidMessage, "failed to parse command action")
	}

	// Validate required fields
	if actionData.CommandID == "" {
		return r.errorResult(protocol.ErrInvalidMessage, "missing commandId")
	}
	if frame.VehicleID == "" || frame.VehicleID == protocol.VehicleIDClient || frame.VehicleID == protocol.VehicleIDGateway {
		return r.errorResult(protocol.ErrInvalidMessage, "invalid target vehicle ID")
	}

	// Verify vehicle exists in registry
	vehicle := r.registry.Get(frame.VehicleID)
	if vehicle == nil {
		return RouteResult{
			Success: false,
			Frame: protocol.NewCommandErrorFrame(
				protocol.ErrVehicleNotFound,
				fmt.Sprintf("vehicle %s not found in registry", frame.VehicleID),
				actionData.CommandID,
			),
		}
	}

	// Validate command against vehicle capabilities (fail-fast)
	if !r.isCommandSupported(vehicle, actionData.Action, dataBytes) {
		errMsg := r.buildCapabilityErrorMessage(vehicle, actionData.Action, dataBytes)
		return RouteResult{
			Success: false,
			Frame: protocol.NewCommandErrorFrame(
				protocol.ErrCommandNotSupported,
				errMsg,
				actionData.CommandID,
			),
		}
	}

	// Check rate limit via tracker
	trackResult := r.tracker.Track(actionData.CommandID, frame.VehicleID, actionData.Action)
	if !trackResult.Accepted {
		return RouteResult{
			Success: false,
			Frame:   trackResult.RejectionFrame,
		}
	}

	// Build protobuf command
	pbCmd, err := r.buildProtoCommand(frame.VehicleID, actionData.CommandID, actionData.Action, dataBytes)
	if err != nil {
		// Un-track the command since we couldn't build it
		r.tracker.Acknowledge(actionData.CommandID)
		return r.errorResult(protocol.ErrInvalidMessage, err.Error())
	}

	// Wrap in GatewayMessage envelope
	gwMsg := &pb.GatewayMessage{
		Payload: &pb.GatewayMessage_Command{
			Command: pbCmd,
		},
	}

	// Marshal and send
	data, err := proto.Marshal(gwMsg)
	if err != nil {
		r.tracker.Acknowledge(actionData.CommandID)
		return r.errorResult(protocol.ErrCommandSendFailed, "failed to marshal command")
	}

	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()

	if conn == nil {
		r.tracker.Acknowledge(actionData.CommandID)
		return r.errorResult(protocol.ErrCommandSendFailed, "command router not started")
	}

	if _, err := conn.Write(data); err != nil {
		r.tracker.Acknowledge(actionData.CommandID)
		return r.errorResult(protocol.ErrCommandSendFailed, fmt.Sprintf("failed to send: %v", err))
	}

	slog.Debug("command sent",
		"vehicle_id", frame.VehicleID,
		"action", actionData.Action,
		"command_id", actionData.CommandID,
	)

	// Return immediate gateway ack
	return RouteResult{
		Success: true,
		Frame: protocol.NewGatewayCommandAckFrame(
			frame.VehicleID,
			actionData.CommandID,
			protocol.AckAccepted,
			"",
		),
	}
}

// buildProtoCommand builds a protobuf Command from parsed JSON.
func (r *Router) buildProtoCommand(vehicleID, commandID, action string, dataBytes []byte) (*pb.Command, error) {
	cmd := &pb.Command{
		CommandId:   commandID,
		VehicleId:   vehicleID,
		TimestampMs: time.Now().UnixMilli(),
	}

	switch action {
	case "goto":
		var gotoCmd protocol.GotoCommand
		if err := json.Unmarshal(dataBytes, &gotoCmd); err != nil {
			return nil, fmt.Errorf("invalid goto command: %w", err)
		}
		cmd.Payload = &pb.Command_Goto{
			Goto: &pb.GotoCommand{
				Destination: &pb.Location{
					Latitude:  gotoCmd.Destination.Lat,
					Longitude: gotoCmd.Destination.Lng,
				},
				SpeedMs: float32(valueOrDefault(gotoCmd.Speed, 5.0)),
			},
		}

	case "stop":
		cmd.Payload = &pb.Command_Stop{
			Stop: &pb.StopCommand{},
		}

	case "return_home":
		cmd.Payload = &pb.Command_ReturnHome{
			ReturnHome: &pb.ReturnHomeCommand{},
		}

	case "set_mode":
		var modeCmd protocol.SetModeCommand
		if err := json.Unmarshal(dataBytes, &modeCmd); err != nil {
			return nil, fmt.Errorf("invalid set_mode command: %w", err)
		}
		pbMode := pb.OperationalMode_MODE_MANUAL
		switch modeCmd.Mode {
		case protocol.ModeManual:
			pbMode = pb.OperationalMode_MODE_MANUAL
		case protocol.ModeAutonomous:
			pbMode = pb.OperationalMode_MODE_AUTONOMOUS
		case protocol.ModeGuided:
			pbMode = pb.OperationalMode_MODE_GUIDED
		default:
			return nil, fmt.Errorf("invalid mode: %s", modeCmd.Mode)
		}
		cmd.Payload = &pb.Command_SetMode{
			SetMode: &pb.SetModeCommand{
				Mode: pbMode,
			},
		}

	case "set_speed":
		var speedCmd protocol.SetSpeedCommand
		if err := json.Unmarshal(dataBytes, &speedCmd); err != nil {
			return nil, fmt.Errorf("invalid set_speed command: %w", err)
		}
		cmd.Payload = &pb.Command_SetSpeed{
			SetSpeed: &pb.SetSpeedCommand{
				SpeedMs: float32(speedCmd.Speed),
			},
		}

	case "extension":
		var extCmd protocol.ExtensionCommandInput
		if err := json.Unmarshal(dataBytes, &extCmd); err != nil {
			return nil, fmt.Errorf("invalid extension command: %w", err)
		}
		if extCmd.Namespace == "" {
			return nil, fmt.Errorf("extension command missing namespace")
		}
		extAction := extCmd.ExtensionAction()
		if extAction == "" {
			return nil, fmt.Errorf("extension command missing payload.type")
		}

		codec := extensions.Get(extCmd.Namespace)
		if codec == nil {
			return nil, fmt.Errorf("no codec registered for extension namespace %q", extCmd.Namespace)
		}

		version, payload, err := codec.EncodeCommand(extAction, extCmd.Payload)
		if err != nil {
			return nil, fmt.Errorf("encode extension command %s/%s: %w", extCmd.Namespace, extAction, err)
		}

		cmd.Payload = &pb.Command_Extension{
			Extension: &pb.ExtensionCommand{
				Namespace: extCmd.Namespace,
				Action:    extAction,
				Version:   version,
				Payload:   payload,
			},
		}

	default:
		return nil, fmt.Errorf("unknown command action: %s", action)
	}

	return cmd, nil
}

// errorResult creates an error RouteResult.
func (r *Router) errorResult(code, message string) RouteResult {
	return RouteResult{
		Success: false,
		Frame:   protocol.NewErrorFrame(code, message),
	}
}

// valueOrDefault returns the value if non-nil, otherwise the default.
func valueOrDefault(v *float64, def float64) float64 {
	if v != nil {
		return *v
	}
	return def
}

// coreCommands lists all core protocol commands.
// If a vehicle has capabilities but no supported_commands list, it's observation-only.
var coreCommands = []string{"goto", "stop", "return_home", "set_mode", "set_speed"}

// isCommandSupported checks if a vehicle supports a given command action.
// Returns true if:
//   - Vehicle has no capabilities advertised (legacy compatibility - allow all)
//   - Vehicle capabilities include this command in supported_commands
//   - For extension commands: vehicle supports that extension namespace + specific action
func (r *Router) isCommandSupported(vehicle *registry.Vehicle, action string, dataBytes []byte) bool {
	caps := vehicle.Capabilities

	// No capabilities advertised = legacy vehicle, allow all commands
	// This maintains backward compatibility with vehicles that don't send capabilities yet.
	if caps == nil {
		return true
	}

	// Check if it's a core command
	for _, coreCmd := range coreCommands {
		if action == coreCmd {
			// Must be in supported_commands
			for _, supported := range caps.SupportedCommands {
				if supported == action {
					return true
				}
			}
			// Core command not supported by this vehicle
			return false
		}
	}

	// Extension command - validate namespace and action
	if action == "extension" {
		return r.isExtensionCommandSupported(caps, dataBytes)
	}

	// Unknown command type - let buildProtoCommand reject it
	return true
}

// extensionCommandData is used to parse extension command payloads for validation.
type extensionCommandData struct {
	Namespace string `json:"namespace"`
	Payload   struct {
		Type string `json:"type"` // The specific action within the extension
	} `json:"payload"`
}

// isExtensionCommandSupported checks if vehicle supports a specific extension command.
func (r *Router) isExtensionCommandSupported(caps *protocol.VehicleCapabilities, dataBytes []byte) bool {
	// Parse extension command details
	var extCmd extensionCommandData
	if err := json.Unmarshal(dataBytes, &extCmd); err != nil {
		// Can't parse - let buildProtoCommand handle it
		return true
	}

	if extCmd.Namespace == "" {
		// No namespace specified - let buildProtoCommand reject it
		return true
	}

	// Find matching extension capability
	for _, ext := range caps.Extensions {
		if ext.Namespace == extCmd.Namespace {
			// Found the extension namespace
			// If SupportedActions is empty, vehicle supports all actions in this extension
			if len(ext.SupportedActions) == 0 {
				return true
			}

			// Check if specific action is supported
			actionType := extCmd.Payload.Type
			if actionType == "" {
				// No action type specified - let buildProtoCommand handle validation
				return true
			}

			for _, supportedAction := range ext.SupportedActions {
				if supportedAction == actionType {
					return true
				}
			}

			// Extension found but action not supported
			return false
		}
	}

	// Extension namespace not found in vehicle capabilities
	return false
}

// buildCapabilityErrorMessage creates a descriptive error for capability rejection.
func (r *Router) buildCapabilityErrorMessage(vehicle *registry.Vehicle, action string, dataBytes []byte) string {
	vid := vehicle.ID

	// For extension commands, provide specific namespace/action info
	if action == "extension" {
		var extCmd extensionCommandData
		if err := json.Unmarshal(dataBytes, &extCmd); err == nil && extCmd.Namespace != "" {
			actionType := extCmd.Payload.Type
			if actionType != "" {
				return fmt.Sprintf("vehicle %s does not support extension command '%s/%s'", vid, extCmd.Namespace, actionType)
			}
			return fmt.Sprintf("vehicle %s does not support extension '%s'", vid, extCmd.Namespace)
		}
	}

	return fmt.Sprintf("vehicle %s does not support command '%s'", vid, action)
}
