package command

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
)

func TestTrackAndAcknowledge(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig(), nil)
	defer tracker.CancelAll()

	result := tracker.Track("cmd-001", "ugv-husky-01", "goto")
	if !result.Accepted {
		t.Fatalf("expected command to be accepted, got error: %v", result.Error)
	}

	if tracker.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", tracker.PendingCount())
	}

	// Acknowledge the command
	found := tracker.Acknowledge("cmd-001")
	if !found {
		t.Error("expected Acknowledge to return true for pending command")
	}

	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending after ack, got %d", tracker.PendingCount())
	}

	// Acknowledging again should return false
	found = tracker.Acknowledge("cmd-001")
	if found {
		t.Error("expected Acknowledge to return false for already-acked command")
	}
}

func TestDuplicateCommandRejected(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig(), nil)
	defer tracker.CancelAll()

	// First track succeeds
	result := tracker.Track("cmd-001", "ugv-husky-01", "goto")
	if !result.Accepted {
		t.Fatalf("expected first track to succeed")
	}

	// Second track with same commandID should fail
	result = tracker.Track("cmd-001", "ugv-husky-01", "stop")
	if result.Accepted {
		t.Error("expected duplicate commandID to be rejected")
	}
	if result.RejectionFrame == nil {
		t.Error("expected rejection frame for duplicate")
	}
}

// TestTimeoutEchosCommandID is the critical test: verify that when a command
// times out, the synthetic ack frame contains the original commandId so the
// UI can correlate which command timed out.
func TestTimeoutEchosCommandID(t *testing.T) {
	cfg := TrackerConfig{
		Timeout:    50 * time.Millisecond, // Fast timeout for testing
		RateLimit:  10,
		RateWindow: 1 * time.Second,
	}

	var receivedFrame *protocol.Frame
	var frameReceived sync.WaitGroup
	frameReceived.Add(1)

	tracker := NewTracker(cfg, func(frame *protocol.Frame) {
		receivedFrame = frame
		frameReceived.Done()
	})
	defer tracker.CancelAll()

	// Track a command
	result := tracker.Track("cmd-timeout-test", "ugv-husky-01", "goto")
	if !result.Accepted {
		t.Fatalf("expected command to be accepted")
	}

	// Wait for timeout
	frameReceived.Wait()

	// THE CRITICAL CHECK: The timeout frame must echo the original commandId
	if receivedFrame == nil {
		t.Fatal("expected timeout callback to be called")
	}

	if receivedFrame.Type != protocol.TypeCommandAck {
		t.Errorf("expected type=command_ack, got %s", receivedFrame.Type)
	}

	payload, ok := receivedFrame.Data.(protocol.CommandAckPayload)
	if !ok {
		t.Fatalf("expected CommandAckPayload, got %T", receivedFrame.Data)
	}

	if payload.CommandID != "cmd-timeout-test" {
		t.Errorf("CRITICAL: timeout ack has wrong commandId: expected 'cmd-timeout-test', got '%s'. "+
			"The UI cannot correlate this timeout to the original command!", payload.CommandID)
	}

	if payload.Status != protocol.AckTimeout {
		t.Errorf("expected status=timeout, got %s", payload.Status)
	}

	// Command should be removed from pending
	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending after timeout, got %d", tracker.PendingCount())
	}
}

func TestAcknowledgeCancelsTimeout(t *testing.T) {
	cfg := TrackerConfig{
		Timeout:    100 * time.Millisecond,
		RateLimit:  10,
		RateWindow: 1 * time.Second,
	}

	var timeoutCalled atomic.Bool

	tracker := NewTracker(cfg, func(frame *protocol.Frame) {
		timeoutCalled.Store(true)
	})
	defer tracker.CancelAll()

	// Track a command
	tracker.Track("cmd-ack-test", "ugv-husky-01", "stop")

	// Acknowledge before timeout
	tracker.Acknowledge("cmd-ack-test")

	// Wait past the timeout period
	time.Sleep(150 * time.Millisecond)

	// Timeout should NOT have been called
	if timeoutCalled.Load() {
		t.Error("timeout callback should not be called after Acknowledge")
	}
}

func TestRateLimitPerVehicle(t *testing.T) {
	cfg := TrackerConfig{
		Timeout:    5 * time.Second,
		RateLimit:  3, // Only 3 commands per second
		RateWindow: 1 * time.Second,
	}

	tracker := NewTracker(cfg, nil)
	defer tracker.CancelAll()

	// First 3 commands should succeed
	for i := 0; i < 3; i++ {
		result := tracker.Track(
			"cmd-rate-"+string(rune('a'+i)),
			"ugv-husky-01",
			"goto",
		)
		if !result.Accepted {
			t.Errorf("expected command %d to be accepted", i+1)
		}
	}

	// 4th command should be rate limited
	result := tracker.Track("cmd-rate-d", "ugv-husky-01", "goto")
	if result.Accepted {
		t.Error("expected 4th command to be rate limited")
	}
	if result.RejectionFrame == nil {
		t.Error("expected rejection frame for rate limit")
	}

	// Check the error frame has the commandId for correlation
	errPayload, ok := result.RejectionFrame.Data.(protocol.ErrorPayload)
	if ok && errPayload.CommandID != nil && *errPayload.CommandID != "cmd-rate-d" {
		t.Errorf("rejection frame should contain commandId 'cmd-rate-d', got '%s'", *errPayload.CommandID)
	}
}

func TestRateLimitPerVehicleIsolation(t *testing.T) {
	cfg := TrackerConfig{
		Timeout:    5 * time.Second,
		RateLimit:  2,
		RateWindow: 1 * time.Second,
	}

	tracker := NewTracker(cfg, nil)
	defer tracker.CancelAll()

	// 2 commands to vehicle A
	tracker.Track("cmd-a1", "vehicle-a", "goto")
	tracker.Track("cmd-a2", "vehicle-a", "stop")

	// Vehicle A is now rate limited
	result := tracker.Track("cmd-a3", "vehicle-a", "goto")
	if result.Accepted {
		t.Error("expected vehicle-a to be rate limited")
	}

	// But vehicle B should NOT be affected
	result = tracker.Track("cmd-b1", "vehicle-b", "goto")
	if !result.Accepted {
		t.Error("expected vehicle-b to NOT be rate limited")
	}
}

func TestRateLimitWindowExpiry(t *testing.T) {
	cfg := TrackerConfig{
		Timeout:    5 * time.Second,
		RateLimit:  2,
		RateWindow: 50 * time.Millisecond, // Fast window for testing
	}

	tracker := NewTracker(cfg, nil)
	defer tracker.CancelAll()

	// Fill the rate limit
	tracker.Track("cmd-1", "vehicle-a", "goto")
	tracker.Track("cmd-2", "vehicle-a", "stop")

	// Should be rate limited
	result := tracker.Track("cmd-3", "vehicle-a", "goto")
	if result.Accepted {
		t.Error("expected to be rate limited")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed now
	result = tracker.Track("cmd-4", "vehicle-a", "goto")
	if !result.Accepted {
		t.Error("expected command after window expiry to be accepted")
	}
}

func TestPendingCountForVehicle(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig(), nil)
	defer tracker.CancelAll()

	tracker.Track("cmd-a1", "vehicle-a", "goto")
	tracker.Track("cmd-a2", "vehicle-a", "stop")
	tracker.Track("cmd-b1", "vehicle-b", "goto")

	if tracker.PendingCountForVehicle("vehicle-a") != 2 {
		t.Errorf("expected 2 pending for vehicle-a")
	}
	if tracker.PendingCountForVehicle("vehicle-b") != 1 {
		t.Errorf("expected 1 pending for vehicle-b")
	}
	if tracker.PendingCountForVehicle("vehicle-c") != 0 {
		t.Errorf("expected 0 pending for unknown vehicle")
	}
}

func TestGetPending(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig(), nil)
	defer tracker.CancelAll()

	tracker.Track("cmd-001", "ugv-husky-01", "goto")

	pc := tracker.GetPending("cmd-001")
	if pc == nil {
		t.Fatal("expected to find pending command")
	}
	if pc.CommandID != "cmd-001" {
		t.Errorf("wrong commandID: %s", pc.CommandID)
	}
	if pc.VehicleID != "ugv-husky-01" {
		t.Errorf("wrong vehicleID: %s", pc.VehicleID)
	}
	if pc.Type != "goto" {
		t.Errorf("wrong type: %s", pc.Type)
	}

	// Non-existent
	if tracker.GetPending("cmd-999") != nil {
		t.Error("expected nil for non-existent command")
	}
}

func TestCancelAll(t *testing.T) {
	var timeoutCount atomic.Int32

	cfg := TrackerConfig{
		Timeout:    50 * time.Millisecond,
		RateLimit:  10,
		RateWindow: 1 * time.Second,
	}

	tracker := NewTracker(cfg, func(frame *protocol.Frame) {
		timeoutCount.Add(1)
	})

	// Track several commands
	tracker.Track("cmd-1", "vehicle-a", "goto")
	tracker.Track("cmd-2", "vehicle-a", "stop")
	tracker.Track("cmd-3", "vehicle-b", "goto")

	// Cancel all
	tracker.CancelAll()

	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending after CancelAll, got %d", tracker.PendingCount())
	}

	// Wait past timeout
	time.Sleep(100 * time.Millisecond)

	// No timeouts should have fired
	if timeoutCount.Load() != 0 {
		t.Errorf("expected 0 timeouts after CancelAll, got %d", timeoutCount.Load())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tracker := NewTracker(DefaultTrackerConfig(), nil)
	defer tracker.CancelAll()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const commandsPerGoroutine = 100

	// Concurrent Track and Acknowledge
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < commandsPerGoroutine; j++ {
				cmdID := "cmd-" + string(rune('a'+workerID)) + "-" + string(rune('0'+j%10))
				vehicleID := "vehicle-" + string(rune('0'+workerID%5))

				result := tracker.Track(cmdID, vehicleID, "goto")
				if result.Accepted {
					// Acknowledge some of them
					if j%2 == 0 {
						tracker.Acknowledge(cmdID)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Just verify no panics and we can still query
	_ = tracker.PendingCount()
}
