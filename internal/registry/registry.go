// Package registry provides vehicle tracking with status state machine.
//
// The registry maintains the current state of all known vehicles and detects
// status transitions based on telemetry gaps. It integrates with the sequence
// tracker to reset deduplication state when vehicles reconnect after being offline.
//
// Status State Machine:
//
//	                    telemetry
//	                  ┌───────────┐
//	                  ▼           │
//	┌─────────┐   telemetry   ┌─────────┐
//	│ OFFLINE │──────────────▶│ ONLINE  │
//	└─────────┘               └─────────┘
//	     ▲                         │
//	     │ OfflineTimeout          │ StandbyTimeout
//	     │                         ▼
//	     │                    ┌─────────┐
//	     └────────────────────│ STANDBY │
//	                          └─────────┘
package registry

import (
	"sync"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
)

// Status represents the vehicle operational status.
type Status string

const (
	StatusOnline  Status = "online"
	StatusStandby Status = "standby"
	StatusOffline Status = "offline"
)

// Vehicle represents a tracked vehicle in the registry.
type Vehicle struct {
	ID           string
	Name         string                        // Display name (from config or derived from ID)
	Environment  string                        // air, ground, marine
	Status       Status                        // Current operational status
	LastSeen     time.Time                     // Last telemetry received
	FirstSeen    time.Time                     // When vehicle was first discovered
	Capabilities *protocol.VehicleCapabilities // Advertised capabilities (from Heartbeat)
}

// Config holds registry configuration.
type Config struct {
	StandbyTimeout time.Duration // Time without telemetry before ONLINE → STANDBY
	OfflineTimeout time.Duration // Time without telemetry before STANDBY → OFFLINE
}

// DefaultConfig returns sensible defaults matching PROTOCOL.md.
func DefaultConfig() Config {
	return Config{
		StandbyTimeout: 3 * time.Second,
		OfflineTimeout: 10 * time.Second,
	}
}

// StatusTransition represents a vehicle status change event.
type StatusTransition struct {
	VehicleID string
	From      Status
	To        Status
	Timestamp time.Time
}

// Registry tracks all known vehicles and their status.
type Registry struct {
	mu              sync.RWMutex
	vehicles        map[string]*Vehicle
	sequenceTracker *protocol.SequenceTracker
	config          Config

	// Callback for status transitions (optional)
	onTransition func(StatusTransition)

	// Time function for testing
	now func() time.Time
}

// New creates a new vehicle registry.
func New(seqTracker *protocol.SequenceTracker, cfg Config) *Registry {
	return &Registry{
		vehicles:        make(map[string]*Vehicle),
		sequenceTracker: seqTracker,
		config:          cfg,
		now:             time.Now,
	}
}

// SetTransitionCallback sets a callback for status transitions.
// Useful for emitting status frames to connected clients.
func (r *Registry) SetTransitionCallback(cb func(StatusTransition)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onTransition = cb
}

// RecordTelemetry updates the vehicle registry when telemetry is received.
// This is the main entry point called by the telemetry pipeline.
//
// Returns the previous status if a transition occurred, or empty string if no change.
// The caller should emit a status frame to clients if a transition occurred.
func (r *Registry) RecordTelemetry(vehicleID, environment string) (transition *StatusTransition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	v, exists := r.vehicles[vehicleID]

	if !exists {
		// New vehicle discovered - always starts as ONLINE
		r.vehicles[vehicleID] = &Vehicle{
			ID:          vehicleID,
			Name:        vehicleID, // Default to ID, can be overridden
			Environment: environment,
			Status:      StatusOnline,
			LastSeen:    now,
			FirstSeen:   now,
		}
		// First telemetry from this vehicle - sequence tracker will auto-initialize
		return &StatusTransition{
			VehicleID: vehicleID,
			From:      "", // New vehicle, no previous status
			To:        StatusOnline,
			Timestamp: now,
		}
	}

	prevStatus := v.Status
	v.LastSeen = now
	v.Environment = environment // Update in case it changed

	// Check for status transition
	if prevStatus == StatusOffline {
		// OFFLINE → ONLINE: Vehicle came back!
		// CRITICAL: Reset sequence tracker to accept any seq from rebooted vehicle
		r.sequenceTracker.Reset(vehicleID)

		v.Status = StatusOnline
		t := StatusTransition{
			VehicleID: vehicleID,
			From:      StatusOffline,
			To:        StatusOnline,
			Timestamp: now,
		}
		r.notifyTransition(t)
		return &t
	}

	if prevStatus == StatusStandby {
		// STANDBY → ONLINE: Vehicle resumed sending telemetry
		v.Status = StatusOnline
		t := StatusTransition{
			VehicleID: vehicleID,
			From:      StatusStandby,
			To:        StatusOnline,
			Timestamp: now,
		}
		r.notifyTransition(t)
		return &t
	}

	// Status unchanged (was already ONLINE)
	return nil
}

// CheckTimeouts scans all vehicles and transitions them to STANDBY or OFFLINE
// based on time since last telemetry. Call this periodically (e.g., every 1s).
//
// Returns all transitions that occurred.
func (r *Registry) CheckTimeouts() []StatusTransition {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	var transitions []StatusTransition

	for _, v := range r.vehicles {
		elapsed := now.Sub(v.LastSeen)
		prevStatus := v.Status

		switch v.Status {
		case StatusOnline:
			if elapsed >= r.config.StandbyTimeout {
				v.Status = StatusStandby
				t := StatusTransition{
					VehicleID: v.ID,
					From:      prevStatus,
					To:        StatusStandby,
					Timestamp: now,
				}
				r.notifyTransition(t)
				transitions = append(transitions, t)
			}
		case StatusStandby:
			if elapsed >= r.config.OfflineTimeout {
				v.Status = StatusOffline
				t := StatusTransition{
					VehicleID: v.ID,
					From:      prevStatus,
					To:        StatusOffline,
					Timestamp: now,
				}
				r.notifyTransition(t)
				transitions = append(transitions, t)
			}
		}
		// OFFLINE vehicles stay OFFLINE until telemetry arrives
	}

	return transitions
}

// notifyTransition calls the transition callback if set.
// Must be called with lock held.
func (r *Registry) notifyTransition(t StatusTransition) {
	if r.onTransition != nil {
		// Call without lock to avoid deadlocks
		cb := r.onTransition
		go cb(t)
	}
}

// Get returns a vehicle by ID, or nil if not found.
func (r *Registry) Get(vehicleID string) *Vehicle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, exists := r.vehicles[vehicleID]
	if !exists {
		return nil
	}
	// Return a copy to avoid race conditions
	copy := *v
	return &copy
}

// GetFleetSummary returns a snapshot of all vehicles for the welcome message.
func (r *Registry) GetFleetSummary() []protocol.VehicleSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]protocol.VehicleSummary, 0, len(r.vehicles))
	for _, v := range r.vehicles {
		result = append(result, protocol.VehicleSummary{
			ID:           v.ID,
			Name:         v.Name,
			Status:       string(v.Status),
			Environment:  v.Environment,
			LastSeen:     v.LastSeen.UnixMilli(),
			Capabilities: v.Capabilities,
		})
	}
	return result
}

// Count returns the number of tracked vehicles.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vehicles)
}

// CountByStatus returns counts of vehicles in each status.
func (r *Registry) CountByStatus() map[Status]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := map[Status]int{
		StatusOnline:  0,
		StatusStandby: 0,
		StatusOffline: 0,
	}
	for _, v := range r.vehicles {
		counts[v.Status]++
	}
	return counts
}

// SetName updates the display name for a vehicle.
func (r *Registry) SetName(vehicleID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, exists := r.vehicles[vehicleID]
	if !exists {
		return false
	}
	v.Name = name
	return true
}

// UpdateCapabilities updates the advertised capabilities for a vehicle.
// Called when processing Heartbeat messages.
// Returns false if vehicle doesn't exist.
func (r *Registry) UpdateCapabilities(vehicleID string, caps *protocol.VehicleCapabilities) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, exists := r.vehicles[vehicleID]
	if !exists {
		return false
	}
	v.Capabilities = caps
	return true
}

// GetCapabilities returns the capabilities for a vehicle, or nil if unknown.
func (r *Registry) GetCapabilities(vehicleID string) *protocol.VehicleCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, exists := r.vehicles[vehicleID]
	if !exists {
		return nil
	}
	return v.Capabilities
}

// Remove deletes a vehicle from the registry.
// Also clears its sequence tracking state.
func (r *Registry) Remove(vehicleID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles, vehicleID)
	r.sequenceTracker.Reset(vehicleID)
}
