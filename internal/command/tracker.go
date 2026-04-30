// Package command provides command routing, tracking, and rate limiting.
//
// The tracker monitors pending commands and generates synthetic timeout acks
// when vehicles don't respond within the configured timeout. This ensures the
// UI always gets a response for every command it sends.
//
// Rate limiting prevents command spam that could overwhelm vehicles or the
// radio link. Limits are per-vehicle to allow commanding multiple vehicles
// simultaneously while preventing runaway automation on a single target.
package command

import (
	"fmt"
	"sync"
	"time"

	"github.com/EthanMBoos/tower-server/internal/protocol"
)

// PendingCommand represents a command awaiting vehicle acknowledgment.
type PendingCommand struct {
	CommandID string
	VehicleID string
	Type      string // "goto", "stop", "return_home", "set_mode", "set_speed"
	SentAt    time.Time
	timer     *time.Timer
}

// TrackerConfig holds configuration for the command tracker.
type TrackerConfig struct {
	// Timeout is how long to wait for a vehicle ack before sending synthetic timeout.
	// Default: 5 seconds (TOWER_CMD_TIMEOUT)
	Timeout time.Duration

	// RateLimit is max commands per second per vehicle.
	// Default: 10 (TOWER_CMD_RATE_LIMIT)
	RateLimit int

	// RateWindow is the sliding window for rate limiting.
	// Default: 1 second
	RateWindow time.Duration
}

// DefaultTrackerConfig returns sensible defaults matching PROTOCOL.md.
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		Timeout:    5 * time.Second,
		RateLimit:  10,
		RateWindow: 1 * time.Second,
	}
}

// TimeoutCallback is called when a command times out.
// The Frame contains the synthetic timeout ack with the original commandId.
type TimeoutCallback func(frame *protocol.Frame)

// Tracker manages pending commands and generates timeout acks.
type Tracker struct {
	mu       sync.Mutex
	pending  map[string]*PendingCommand // key = commandID
	config   TrackerConfig
	onTimeout TimeoutCallback

	// Rate limiting: track command timestamps per vehicle
	rateLimiter map[string][]time.Time // key = vehicleID, value = recent command times

	// Time function for testing
	now func() time.Time
}

// NewTracker creates a new command tracker.
func NewTracker(cfg TrackerConfig, onTimeout TimeoutCallback) *Tracker {
	return &Tracker{
		pending:     make(map[string]*PendingCommand),
		config:      cfg,
		onTimeout:   onTimeout,
		rateLimiter: make(map[string][]time.Time),
		now:         time.Now,
	}
}

// SetTimeoutCallback sets or updates the timeout callback function.
// This allows wiring the callback after construction when there are
// circular dependencies (e.g., tracker needs server, server needs tracker).
func (t *Tracker) SetTimeoutCallback(cb TimeoutCallback) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onTimeout = cb
}

// TrackResult indicates whether a command was accepted for tracking.
type TrackResult struct {
	Accepted bool
	Error    error
	// If rate limited, this contains the rejection frame to send to the client
	RejectionFrame *protocol.Frame
}

// Track starts tracking a command and sets up the timeout timer.
// Returns an error if rate limited or if commandID is already being tracked.
//
// The caller should:
// 1. Call Track() before broadcasting the command
// 2. If Track() returns error, send the rejection frame to the client and don't broadcast
// 3. If Track() succeeds, broadcast the command to the vehicle
func (t *Tracker) Track(commandID, vehicleID, commandType string) TrackResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()

	// Check for duplicate commandID
	if _, exists := t.pending[commandID]; exists {
		return TrackResult{
			Accepted: false,
			Error:    fmt.Errorf("command %s already pending", commandID),
			RejectionFrame: protocol.NewServerCommandAckFrame(
				vehicleID, commandID, protocol.AckRejected,
				fmt.Sprintf("Duplicate commandId: %s", commandID),
			),
		}
	}

	// Check rate limit
	if err := t.checkRateLimitLocked(vehicleID, now); err != nil {
		return TrackResult{
			Accepted: false,
			Error:    err,
			RejectionFrame: protocol.NewCommandErrorFrame(
				"RATE_LIMITED",
				fmt.Sprintf("Command rate limit exceeded for %s (%d/sec)", vehicleID, t.config.RateLimit),
				commandID,
			),
		}
	}

	// Record this command for rate limiting
	t.recordCommandLocked(vehicleID, now)

	// Create pending command with timeout timer
	pc := &PendingCommand{
		CommandID: commandID,
		VehicleID: vehicleID,
		Type:      commandType,
		SentAt:    now,
	}

	// Set up timeout timer
	// CRITICAL: Capture commandID and vehicleID in closure for the timeout callback
	pc.timer = time.AfterFunc(t.config.Timeout, func() {
		t.handleTimeout(commandID, vehicleID)
	})

	t.pending[commandID] = pc

	return TrackResult{Accepted: true}
}

// handleTimeout is called when a command's timer expires.
func (t *Tracker) handleTimeout(commandID, vehicleID string) {
	t.mu.Lock()
	pc, exists := t.pending[commandID]
	if exists {
		delete(t.pending, commandID)
	}
	t.mu.Unlock()

	if !exists {
		// Command was already acknowledged, no timeout needed
		return
	}

	// Generate synthetic timeout ack with the ORIGINAL commandID
	// This is critical - the UI needs this to correlate the timeout
	frame := protocol.NewTimeoutAckFrame(vehicleID, commandID, int(t.config.Timeout.Seconds()))

	if t.onTimeout != nil {
		t.onTimeout(frame)
	}

	// Log for observability
	_ = pc // Could log pc.Type, pc.SentAt for debugging
}

// Acknowledge removes a command from pending tracking.
// Call this when a vehicle ack (accepted, rejected, completed, failed) is received.
// Returns true if the command was pending, false if it wasn't found (already timed out or never tracked).
func (t *Tracker) Acknowledge(commandID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	pc, exists := t.pending[commandID]
	if !exists {
		return false
	}

	// Stop the timeout timer
	pc.timer.Stop()
	delete(t.pending, commandID)

	return true
}

// checkRateLimitLocked checks if a vehicle has exceeded its rate limit.
// Must be called with t.mu held.
func (t *Tracker) checkRateLimitLocked(vehicleID string, now time.Time) error {
	times := t.rateLimiter[vehicleID]
	windowStart := now.Add(-t.config.RateWindow)

	// Count commands in the window
	count := 0
	for _, ts := range times {
		if ts.After(windowStart) {
			count++
		}
	}

	if count >= t.config.RateLimit {
		return fmt.Errorf("rate limit exceeded: %d commands in %v", count, t.config.RateWindow)
	}

	return nil
}

// recordCommandLocked records a command timestamp for rate limiting.
// Also prunes old entries outside the window.
// Must be called with t.mu held.
func (t *Tracker) recordCommandLocked(vehicleID string, now time.Time) {
	windowStart := now.Add(-t.config.RateWindow)

	// Prune old entries and add new one
	times := t.rateLimiter[vehicleID]
	filtered := make([]time.Time, 0, len(times)+1)
	for _, ts := range times {
		if ts.After(windowStart) {
			filtered = append(filtered, ts)
		}
	}
	filtered = append(filtered, now)
	t.rateLimiter[vehicleID] = filtered
}

// PendingCount returns the number of commands awaiting acknowledgment.
func (t *Tracker) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// PendingCountForVehicle returns pending command count for a specific vehicle.
func (t *Tracker) PendingCountForVehicle(vehicleID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, pc := range t.pending {
		if pc.VehicleID == vehicleID {
			count++
		}
	}
	return count
}

// GetPending returns info about a pending command, or nil if not found.
func (t *Tracker) GetPending(commandID string) *PendingCommand {
	t.mu.Lock()
	defer t.mu.Unlock()

	pc, exists := t.pending[commandID]
	if !exists {
		return nil
	}

	// Return a copy without the timer
	return &PendingCommand{
		CommandID: pc.CommandID,
		VehicleID: pc.VehicleID,
		Type:      pc.Type,
		SentAt:    pc.SentAt,
	}
}

// CancelAll cancels all pending command timers.
// Call this during shutdown to prevent timer goroutine leaks.
func (t *Tracker) CancelAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, pc := range t.pending {
		pc.timer.Stop()
	}
	t.pending = make(map[string]*PendingCommand)
}
