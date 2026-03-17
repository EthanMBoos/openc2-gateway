package registry

import (
	"testing"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
)

func TestNewVehicleStartsOnline(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	r := New(seqTracker, DefaultConfig())

	transition := r.RecordTelemetry("ugv-husky-01", "ground")

	if transition == nil {
		t.Fatal("expected transition for new vehicle")
	}
	if transition.From != "" {
		t.Errorf("expected empty From for new vehicle, got %q", transition.From)
	}
	if transition.To != StatusOnline {
		t.Errorf("expected To=online, got %q", transition.To)
	}

	v := r.Get("ugv-husky-01")
	if v == nil {
		t.Fatal("vehicle not found in registry")
	}
	if v.Status != StatusOnline {
		t.Errorf("expected status=online, got %q", v.Status)
	}
}

func TestOnlineToStandbyTransition(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 500 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	// Record initial telemetry
	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }
	r.RecordTelemetry("ugv-husky-01", "ground")

	// Advance time past standby timeout
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	transitions := r.CheckTimeouts()

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].From != StatusOnline {
		t.Errorf("expected From=online, got %q", transitions[0].From)
	}
	if transitions[0].To != StatusStandby {
		t.Errorf("expected To=standby, got %q", transitions[0].To)
	}
}

func TestStandbyToOfflineTransition(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 300 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }
	r.RecordTelemetry("ugv-husky-01", "ground")

	// First timeout: ONLINE → STANDBY
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	r.CheckTimeouts()

	// Second timeout: STANDBY → OFFLINE
	fakeTime = fakeTime.Add(200 * time.Millisecond) // Total: 350ms > 300ms
	transitions := r.CheckTimeouts()

	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].From != StatusStandby {
		t.Errorf("expected From=standby, got %q", transitions[0].From)
	}
	if transitions[0].To != StatusOffline {
		t.Errorf("expected To=offline, got %q", transitions[0].To)
	}
}

// TestOfflineToOnlineResetsSequenceTracker verifies the critical bug fix:
// When a vehicle transitions from OFFLINE → ONLINE, the sequence tracker MUST
// be reset. Otherwise, a rebooted vehicle with seq=0 would have all its
// telemetry dropped until seq exceeds the old high-water mark.
func TestOfflineToOnlineResetsSequenceTracker(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 300 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	vehicleID := "ugv-husky-01"
	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }

	// Step 1: Vehicle sends telemetry with high sequence number
	r.RecordTelemetry(vehicleID, "ground")
	// Simulate the decoder accepting seq=1000
	seqTracker.Accept(vehicleID, 1000)

	// Verify high-water mark is set
	hwm, ok := seqTracker.HighWaterMark(vehicleID)
	if !ok {
		t.Fatal("expected high-water mark to be set")
	}
	if hwm != 1000 {
		t.Errorf("expected hwm=1000, got %d", hwm)
	}

	// Step 2: Vehicle goes offline
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	r.CheckTimeouts() // ONLINE → STANDBY

	fakeTime = fakeTime.Add(200 * time.Millisecond)
	r.CheckTimeouts() // STANDBY → OFFLINE

	v := r.Get(vehicleID)
	if v.Status != StatusOffline {
		t.Errorf("expected status=offline, got %q", v.Status)
	}

	// Step 3: Vehicle reboots and sends telemetry with seq=0
	// BEFORE the fix, this would be dropped because 0 < 1000
	fakeTime = fakeTime.Add(100 * time.Millisecond)
	transition := r.RecordTelemetry(vehicleID, "ground")

	// Verify transition occurred
	if transition == nil {
		t.Fatal("expected transition OFFLINE → ONLINE")
	}
	if transition.From != StatusOffline || transition.To != StatusOnline {
		t.Errorf("expected OFFLINE→ONLINE, got %s→%s", transition.From, transition.To)
	}

	// THE CRITICAL CHECK: Sequence tracker should be reset
	// A new message with seq=0 should be accepted
	accepted := seqTracker.Accept(vehicleID, 0)
	if !accepted {
		t.Error("CRITICAL: seq=0 was rejected after OFFLINE→ONLINE transition! " +
			"This means the sequence tracker was NOT reset, and a rebooted " +
			"vehicle's telemetry would be silently dropped.")
	}

	// Verify HWM is now 0 (or cleared)
	hwm, ok = seqTracker.HighWaterMark(vehicleID)
	if !ok {
		// Reset deletes the entry, so this is fine
		return
	}
	if hwm != 0 {
		t.Errorf("expected hwm=0 after reset, got %d", hwm)
	}
}

// TestStandbyToOnlineDoesNotResetSequence verifies that STANDBY → ONLINE
// does NOT reset the sequence tracker. This is intentional: standby just
// means brief telemetry gap, not a vehicle reboot.
func TestStandbyToOnlineDoesNotResetSequence(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 500 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	vehicleID := "ugv-husky-01"
	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }

	// Vehicle sends telemetry with seq=100
	r.RecordTelemetry(vehicleID, "ground")
	seqTracker.Accept(vehicleID, 100)

	// Vehicle goes to standby (not offline)
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	r.CheckTimeouts() // ONLINE → STANDBY

	v := r.Get(vehicleID)
	if v.Status != StatusStandby {
		t.Errorf("expected status=standby, got %q", v.Status)
	}

	// Vehicle sends telemetry again (STANDBY → ONLINE)
	fakeTime = fakeTime.Add(10 * time.Millisecond)
	r.RecordTelemetry(vehicleID, "ground")

	// Sequence tracker should NOT be reset - old seq should still be rejected
	accepted := seqTracker.Accept(vehicleID, 50) // 50 < 100
	if accepted {
		t.Error("seq=50 was accepted after STANDBY→ONLINE, " +
			"expected it to be rejected (sequence not reset for standby)")
	}
}

func TestTransitionCallback(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 300 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	var receivedTransitions []StatusTransition
	done := make(chan struct{})

	r.SetTransitionCallback(func(t StatusTransition) {
		receivedTransitions = append(receivedTransitions, t)
		if len(receivedTransitions) == 2 {
			close(done)
		}
	})

	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }
	r.RecordTelemetry("ugv-husky-01", "ground")

	// Trigger ONLINE → STANDBY
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	r.CheckTimeouts()

	// Wait for callback (it's async)
	select {
	case <-done:
		// All expected transitions received
	case <-time.After(100 * time.Millisecond):
		// May not get all, but that's ok for this test
	}

	// At minimum, we should have the initial ONLINE transition from RecordTelemetry
	if len(receivedTransitions) == 0 {
		t.Error("expected at least one transition callback")
	}
}

func TestGetFleetSummary(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	r := New(seqTracker, DefaultConfig())

	r.RecordTelemetry("ugv-husky-01", "ground")
	r.RecordTelemetry("uav-skydio-03", "air")

	summary := r.GetFleetSummary()

	if len(summary) != 2 {
		t.Errorf("expected 2 vehicles, got %d", len(summary))
	}

	// Check that both vehicles are present (order not guaranteed)
	found := make(map[string]bool)
	for _, v := range summary {
		found[v.ID] = true
		if v.Status != "online" {
			t.Errorf("expected status=online for %s, got %s", v.ID, v.Status)
		}
	}

	if !found["ugv-husky-01"] || !found["uav-skydio-03"] {
		t.Error("missing expected vehicles in summary")
	}
}

func TestCountByStatus(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	cfg := Config{
		StandbyTimeout: 100 * time.Millisecond,
		OfflineTimeout: 300 * time.Millisecond,
	}
	r := New(seqTracker, cfg)

	fakeTime := time.Now()
	r.now = func() time.Time { return fakeTime }

	r.RecordTelemetry("ugv-husky-01", "ground")
	r.RecordTelemetry("uav-skydio-03", "air")

	counts := r.CountByStatus()
	if counts[StatusOnline] != 2 {
		t.Errorf("expected 2 online, got %d", counts[StatusOnline])
	}

	// Make one go to standby
	fakeTime = fakeTime.Add(150 * time.Millisecond)
	r.CheckTimeouts()

	counts = r.CountByStatus()
	if counts[StatusOnline] != 0 {
		t.Errorf("expected 0 online, got %d", counts[StatusOnline])
	}
	if counts[StatusStandby] != 2 {
		t.Errorf("expected 2 standby, got %d", counts[StatusStandby])
	}
}

func TestRemoveClearsSequenceTracker(t *testing.T) {
	seqTracker := protocol.NewSequenceTracker()
	r := New(seqTracker, DefaultConfig())

	vehicleID := "ugv-husky-01"
	r.RecordTelemetry(vehicleID, "ground")
	seqTracker.Accept(vehicleID, 500)

	// Verify HWM is set
	_, ok := seqTracker.HighWaterMark(vehicleID)
	if !ok {
		t.Fatal("expected high-water mark to be set")
	}

	// Remove vehicle
	r.Remove(vehicleID)

	// Verify vehicle is gone
	if r.Get(vehicleID) != nil {
		t.Error("expected vehicle to be removed from registry")
	}

	// Verify sequence tracker is cleared
	_, ok = seqTracker.HighWaterMark(vehicleID)
	if ok {
		t.Error("expected sequence tracker to be cleared for removed vehicle")
	}
}
