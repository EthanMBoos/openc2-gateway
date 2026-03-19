// Package protocol provides sequence tracking for telemetry deduplication.
package protocol

import "sync"

// SequenceTracker tracks per-vehicle sequence numbers for deduplication.
// Telemetry with seq <= high-water mark is considered a duplicate or stale.
//
// Thread-safe for concurrent access from multiple goroutines.
type SequenceTracker struct {
	mu       sync.RWMutex
	vehicles map[string]*vehicleSeq
}

type vehicleSeq struct {
	highWaterMark uint32 // High-water mark (last accepted seq)
	seen          bool   // Whether we've seen any message from this vehicle
}

// NewSequenceTracker creates a new sequence tracker.
func NewSequenceTracker() *SequenceTracker {
	return &SequenceTracker{
		vehicles: make(map[string]*vehicleSeq),
	}
}

// Accept checks if a sequence number should be accepted for a vehicle.
// Returns true if the message should be processed, false if it's a duplicate/stale.
//
// Deduplication rules:
//   - First message from a vehicle: always accept, set highWaterMark = seq
//   - seq > highWaterMark: accept, update highWaterMark = seq
//   - seq <= highWaterMark: reject (duplicate or out-of-order old message)
//   - Wrap-around: if seq is much smaller than highWaterMark (diff > 2^31), assume wrap and accept
func (st *SequenceTracker) Accept(vehicleID string, seq uint32) bool {
	st.mu.Lock()
	defer st.mu.Unlock()

	v, exists := st.vehicles[vehicleID]
	if !exists {
		// First message from this vehicle - accept and track
		st.vehicles[vehicleID] = &vehicleSeq{highWaterMark: seq, seen: true}
		return true
	}

	if !v.seen {
		// Shouldn't happen, but handle gracefully
		v.highWaterMark = seq
		v.seen = true
		return true
	}

	// Check for wrap-around: if seq is much smaller than highWaterMark, it likely wrapped
	// We use 2^31 as the threshold - if the difference is larger, assume wrap
	if seqAfter(seq, v.highWaterMark) {
		v.highWaterMark = seq
		return true
	}

	// seq <= highWaterMark (accounting for wrap) - duplicate or stale
	return false
}

// seqAfter returns true if a comes after b in sequence space,
// accounting for wrap-around at 2^32.
//
// Uses the standard technique: treat the difference as signed.
// If (a - b) interpreted as signed int32 is positive, a is "after" b.
func seqAfter(a, b uint32) bool {
	// Cast to int32 to get signed comparison
	// This works because if a wrapped around, (a - b) will be a large positive
	// number which, when cast to int32, becomes negative.
	diff := int32(a - b)
	return diff > 0
}

// HighWaterMark returns the current high-water mark for a vehicle.
// Returns 0 and false if the vehicle hasn't been seen.
func (st *SequenceTracker) HighWaterMark(vehicleID string) (uint32, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	v, exists := st.vehicles[vehicleID]
	if !exists || !v.seen {
		return 0, false
	}
	return v.highWaterMark, true
}

// Reset clears tracking state for a vehicle, allowing any sequence number to be accepted.
//
// IMPORTANT: The vehicle registry MUST call this when a vehicle transitions from
// offline → online (after OPENC2_OFFLINE_TIMEOUT expires and new telemetry arrives).
// This handles the reboot case: if a vehicle restarts, its seq resets to 0, which
// would otherwise be rejected as stale (seq < highWaterMark). The status state machine detects
// the offline→online transition and calls Reset() to re-sync.
//
// Without this, a rebooted vehicle's telemetry is silently dropped until seq exceeds
// the pre-reboot high-water mark.
func (st *SequenceTracker) Reset(vehicleID string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.vehicles, vehicleID)
}

// ResetAll clears all tracking state.
func (st *SequenceTracker) ResetAll() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.vehicles = make(map[string]*vehicleSeq)
}

// VehicleCount returns the number of tracked vehicles.
func (st *SequenceTracker) VehicleCount() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.vehicles)
}
