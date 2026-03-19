package protocol

import (
	"sync"
	"testing"
)

func TestSequenceTracker_FirstMessage(t *testing.T) {
	st := NewSequenceTracker()

	// First message from a vehicle should always be accepted
	if !st.Accept("ugv-husky-01", 100) {
		t.Error("first message should be accepted")
	}

	highWaterMark, ok := st.HighWaterMark("ugv-husky-01")
	if !ok || highWaterMark != 100 {
		t.Errorf("expected highWaterMark=100, got highWaterMark=%d, ok=%v", highWaterMark, ok)
	}
}

func TestSequenceTracker_IncrementingSeq(t *testing.T) {
	st := NewSequenceTracker()

	st.Accept("ugv-husky-01", 100)

	// Incrementing sequence should be accepted
	if !st.Accept("ugv-husky-01", 101) {
		t.Error("seq 101 should be accepted after 100")
	}
	if !st.Accept("ugv-husky-01", 102) {
		t.Error("seq 102 should be accepted after 101")
	}
	if !st.Accept("ugv-husky-01", 200) {
		t.Error("seq 200 should be accepted (gaps are fine)")
	}
}

func TestSequenceTracker_Duplicates(t *testing.T) {
	st := NewSequenceTracker()

	st.Accept("ugv-husky-01", 100)

	// Same sequence should be rejected (duplicate)
	if st.Accept("ugv-husky-01", 100) {
		t.Error("duplicate seq 100 should be rejected")
	}

	// Lower sequence should be rejected (stale)
	if st.Accept("ugv-husky-01", 99) {
		t.Error("stale seq 99 should be rejected")
	}
	if st.Accept("ugv-husky-01", 50) {
		t.Error("stale seq 50 should be rejected")
	}
}

func TestSequenceTracker_WrapAround(t *testing.T) {
	st := NewSequenceTracker()

	// Start near max uint32
	st.Accept("ugv-husky-01", 0xFFFFFFFF-10)

	// Continue incrementing through wrap
	for i := uint32(0xFFFFFFFF - 9); i != 10; i++ {
		if !st.Accept("ugv-husky-01", i) {
			t.Errorf("seq %d should be accepted (wrapping)", i)
		}
	}

	// After wrap, old pre-wrap values should be rejected
	if st.Accept("ugv-husky-01", 0xFFFFFFFF-20) {
		t.Error("pre-wrap seq should be rejected after wrap")
	}
}

func TestSequenceTracker_MultipleVehicles(t *testing.T) {
	st := NewSequenceTracker()

	// Different vehicles have independent sequence spaces
	st.Accept("ugv-husky-01", 100)
	st.Accept("ugv-husky-02", 50)

	// Each vehicle's seq is independent
	if !st.Accept("ugv-husky-01", 101) {
		t.Error("vehicle 1 seq 101 should be accepted")
	}
	if !st.Accept("ugv-husky-02", 51) {
		t.Error("vehicle 2 seq 51 should be accepted")
	}

	// Cross-vehicle shouldn't affect each other
	if st.Accept("ugv-husky-01", 50) {
		t.Error("vehicle 1 seq 50 should be rejected (below highWaterMark)")
	}
	if !st.Accept("ugv-husky-02", 100) {
		t.Error("vehicle 2 seq 100 should be accepted (above its highWaterMark)")
	}

	if st.VehicleCount() != 2 {
		t.Errorf("expected 2 vehicles, got %d", st.VehicleCount())
	}
}

func TestSequenceTracker_Reset(t *testing.T) {
	st := NewSequenceTracker()

	st.Accept("ugv-husky-01", 100)
	st.Reset("ugv-husky-01")

	// After reset, any sequence should be accepted
	if !st.Accept("ugv-husky-01", 1) {
		t.Error("seq 1 should be accepted after reset")
	}

	_, ok := st.HighWaterMark("ugv-husky-01")
	if !ok {
		t.Error("vehicle should be tracked after accept")
	}
}

func TestSequenceTracker_Concurrent(t *testing.T) {
	st := NewSequenceTracker()
	var wg sync.WaitGroup

	// Simulate concurrent telemetry from multiple vehicles
	for v := 0; v < 10; v++ {
		wg.Add(1)
		go func(vid int) {
			defer wg.Done()
			vehicleID := "ugv-test-" + string(rune('0'+vid))
			for seq := uint32(0); seq < 1000; seq++ {
				st.Accept(vehicleID, seq)
			}
		}(v)
	}

	wg.Wait()

	if st.VehicleCount() != 10 {
		t.Errorf("expected 10 vehicles, got %d", st.VehicleCount())
	}
}

func TestSeqAfter(t *testing.T) {
	tests := []struct {
		a, b   uint32
		expect bool
	}{
		{1, 0, true},
		{100, 50, true},
		{50, 100, false},
		{0, 0, false},
		{100, 100, false},
		// Wrap-around cases
		{0, 0xFFFFFFFF, true},   // 0 comes after max (wrapped)
		{1, 0xFFFFFFFF, true},   // 1 comes after max (wrapped)
		{10, 0xFFFFFFF0, true},  // Small after near-max (wrapped)
		{0xFFFFFFFF, 0, false},  // max does NOT come after 0
		{0xFFFFFFF0, 10, false}, // near-max does NOT come after small
	}

	for _, tc := range tests {
		got := seqAfter(tc.a, tc.b)
		if got != tc.expect {
			t.Errorf("seqAfter(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.expect)
		}
	}
}
