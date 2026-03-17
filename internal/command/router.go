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
	if frame.Vid == "" || frame.Vid == protocol.VidClient || frame.Vid == protocol.VidGateway {
		return r.errorResult(protocol.ErrInvalidMessage, "invalid target vehicle ID")
	}

	// Verify vehicle exists in registry
	vehicle := r.registry.Get(frame.Vid)
	if vehicle == nil {
		return RouteResult{
			Success: false,
			Frame: protocol.NewCommandErrorFrame(
				protocol.ErrVehicleNotFound,
				fmt.Sprintf("vehicle %s not found in registry", frame.Vid),
				actionData.CommandID,
			),
		}
	}

	// Check rate limit via tracker
	trackResult := r.tracker.Track(actionData.CommandID, frame.Vid, actionData.Action)
	if !trackResult.Accepted {
		return RouteResult{
			Success: false,
			Frame:   trackResult.RejectionFrame,
		}
	}

	// Build protobuf command
	pbCmd, err := r.buildProtoCommand(frame.Vid, actionData.CommandID, actionData.Action, dataBytes)
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
		"vid", frame.Vid,
		"action", actionData.Action,
		"command_id", actionData.CommandID,
	)

	// Return immediate gateway ack
	return RouteResult{
		Success: true,
		Frame: protocol.NewGatewayCommandAckFrame(
			frame.Vid,
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
