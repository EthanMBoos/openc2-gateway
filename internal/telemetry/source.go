// Package telemetry provides telemetry data sources for the server.
package telemetry

import (
	"context"

	"github.com/EthanMBoos/tower-server/internal/protocol"
)

// Source provides telemetry frames to the server.
// Implementations include mock sources (for testing) and multicast listeners.
type Source interface {
	// Start begins producing telemetry frames.
	// Frames are sent to the provided channel until ctx is cancelled.
	// The source owns the channel and closes it when done.
	Start(ctx context.Context, frames chan<- *protocol.Frame) error
}
