// Package extensions provides manifest registration for UI metadata.
//
// Each extension registers its manifest via RegisterManifest in init().
// The server collects all manifests and includes them in the welcome payload.
package extensions

import (
	"fmt"
	"sync"
)

// Manifest describes an extension's UI integration.
// This is the extensions package's representation — it gets converted to
// protocol.ExtensionManifest when building the welcome message.
type Manifest struct {
	Namespace   string
	Version     string
	DisplayName string
	Commands    []CommandDefinition
}

// CommandDefinition describes a command within an extension.
type CommandDefinition struct {
	Command      string
	Label        string
	Description  string
	Confirmation bool
	Parameters   []CommandParameter // Payload schema for UI input fields
	TargetMode   string             // "single", "broadcast", or "both" (default: "single")
}

// CommandParameter defines an input field for a command payload.
type CommandParameter struct {
	Name        string
	Label       string
	Type        string // string, number, boolean, select, zone, coordinates, vehicle
	Required    bool
	Default     any
	Options     []ParameterOption // For select type
	Description string
	Min         *float64 // For number type
	Max         *float64 // For number type
}

// ParameterOption defines a choice for select-type parameters.
type ParameterOption struct {
	Value string
	Label string
}

var (
	manifestMu sync.RWMutex
	manifests  = make(map[string]Manifest)
)

// RegisterManifest adds a manifest to the registry.
// Call from your extension package's init() alongside Register().
// Panics if a manifest for the same namespace is already registered.
func RegisterManifest(m Manifest) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if _, exists := manifests[m.Namespace]; exists {
		panic(fmt.Sprintf("extensions: manifest already registered for namespace %q", m.Namespace))
	}
	manifests[m.Namespace] = m
}

// GetManifest returns the manifest for a namespace, or nil if not registered.
func GetManifest(namespace string) *Manifest {
	manifestMu.RLock()
	defer manifestMu.RUnlock()
	if m, ok := manifests[namespace]; ok {
		return &m
	}
	return nil
}

// GetAllManifests returns all registered manifests.
func GetAllManifests() map[string]Manifest {
	manifestMu.RLock()
	defer manifestMu.RUnlock()
	result := make(map[string]Manifest, len(manifests))
	for k, v := range manifests {
		result[k] = v
	}
	return result
}
