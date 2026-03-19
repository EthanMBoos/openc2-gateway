// Package protocol provides frame builders for gateway-originated messages.
package protocol

import "fmt"

// ----------------------------------------------------------------------------
// Frame Builders (Gateway → UI)
// ----------------------------------------------------------------------------

// NewStatusFrame creates a status change frame.
// signalStrength can be nil if unknown.
func NewStatusFrame(vehicleID string, status string, signalStrength *int, source string) *Frame {
	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeStatus,
		VehicleID:          vehicleID,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: StatusPayload{
			Status:         status,
			SignalStrength: signalStrength,
			Source:         source,
		},
	}
}

// NewWelcomeFrame creates a welcome response frame.
func NewWelcomeFrame(gatewayVersion string, fleet []VehicleSummary, telemetryRateHz, heartbeatIntervalMs int) *Frame {
	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeWelcome,
		VehicleID:          VehicleIDGateway,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: WelcomePayload{
			GatewayVersion:  gatewayVersion,
			ProtocolVersion: ProtocolVersion,
			Fleet:           fleet,
			Config: WelcomeConfig{
				TelemetryRateHz:     telemetryRateHz,
				HeartbeatIntervalMs: heartbeatIntervalMs,
			},
		},
	}
}

// NewErrorFrame creates an error response frame.
func NewErrorFrame(code string, message string) *Frame {
	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeError,
		VehicleID:          VehicleIDGateway,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: ErrorPayload{
			Code:    code,
			Message: message,
		},
	}
}

// NewCommandErrorFrame creates an error response frame for command-related errors.
// Includes the commandId so the UI can correlate which command failed.
func NewCommandErrorFrame(code string, message string, commandID string) *Frame {
	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeError,
		VehicleID:          VehicleIDGateway,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: ErrorPayload{
			Code:      code,
			Message:   message,
			CommandID: &commandID,
		},
	}
}

// NewFleetStatusFrame creates a fleet status frame.
func NewFleetStatusFrame(vehicles []VehicleSummary) *Frame {
	online := 0
	offline := 0
	for _, v := range vehicles {
		switch v.Status {
		case StatusOnline:
			online++
		case StatusOffline, StatusStandby:
			offline++
		}
	}

	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeFleetStatus,
		VehicleID:          VehicleIDFleet,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: FleetStatusPayload{
			Vehicles:     vehicles,
			TotalOnline:  online,
			TotalOffline: offline,
		},
	}
}

// NewGatewayCommandAckFrame creates an ack from the gateway (not vehicle).
func NewGatewayCommandAckFrame(vehicleID, commandID, status, message string) *Frame {
	payload := CommandAckPayload{
		CommandID: commandID,
		Status:    status,
	}
	if message != "" {
		payload.Message = &message
	}

	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeCommandAck,
		VehicleID:          vehicleID,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data:               payload,
	}
}

// NewTimeoutAckFrame creates a synthetic timeout ack when a vehicle doesn't respond.
// This tells the UI the command outcome is unknown (vehicle may or may not have received it).
func NewTimeoutAckFrame(vehicleID, commandID string, timeoutSeconds int) *Frame {
	msg := fmt.Sprintf("No response from vehicle within %ds", timeoutSeconds)
	return NewGatewayCommandAckFrame(vehicleID, commandID, AckTimeout, msg)
}

// NewGatewayAlertFrame creates an alert from the gateway (system alerts).
func NewGatewayAlertFrame(vehicleID, severity, code, message string, location *Location) *Frame {
	return &Frame{
		ProtocolVersion:    ProtocolVersion,
		Type:               TypeAlert,
		VehicleID:          vehicleID,
		TimestampMs:        nowMs(),
		GatewayTimestampMs: nowMs(),
		Data: AlertPayload{
			Severity: severity,
			Code:     code,
			Message:  message,
			Location: location,
		},
	}
}
