// Package extensions provides YAML manifest loading for UI metadata.
//
// Manifests are YAML files that define how the UI renders extension commands.
// They live alongside each codec in internal/extensions/<name>/manifest.yaml.
//
// To add a new extension:
// 1. Create internal/extensions/<name>/manifest.yaml
// 2. Create internal/extensions/<name>/codec.go with init() that calls extensions.Register()
// 3. Import the extension in cmd/gateway/main.go
//
// The manifest is loaded automatically when LoadManifest is called.
package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Valid parameter types for command parameters.
var validParamTypes = map[string]bool{
	"string":      true,
	"number":      true,
	"boolean":     true,
	"select":      true,
	"zone":        true,
	"coordinates": true,
	"vehicle":     true,
}

// Valid target modes for commands.
var validTargetModes = map[string]bool{
	"":          true, // empty means single (default)
	"single":    true,
	"broadcast": true,
	"both":      true,
}

// manifestYAML is the YAML structure for manifest files.
// Field names use yaml tags to match the YAML file format.
type manifestYAML struct {
	Namespace   string        `yaml:"namespace"`
	Version     string        `yaml:"version"`
	DisplayName string        `yaml:"displayName"`
	Commands    []commandYAML `yaml:"commands"`
}

type commandYAML struct {
	Command      string          `yaml:"command"`
	Label        string          `yaml:"label"`
	Description  string          `yaml:"description"`
	Confirmation bool            `yaml:"confirmation"`
	TargetMode   string          `yaml:"targetMode"`
	Parameters   []parameterYAML `yaml:"parameters"`
}

type parameterYAML struct {
	Name        string       `yaml:"name"`
	Label       string       `yaml:"label"`
	Type        string       `yaml:"type"`
	Required    bool         `yaml:"required"`
	Default     any          `yaml:"default"`
	Description string       `yaml:"description"`
	Min         *float64     `yaml:"min"`
	Max         *float64     `yaml:"max"`
	Options     []optionYAML `yaml:"options"`
}

type optionYAML struct {
	Value string `yaml:"value"`
	Label string `yaml:"label"`
}

// validateManifest checks the manifest for common mistakes.
// Returns a list of validation errors (empty if valid).
func validateManifest(m *manifestYAML, filePath string) []string {
	var errs []string
	loc := func(context string) string {
		return fmt.Sprintf("%s: %s", filePath, context)
	}

	// Required top-level fields
	if m.Namespace == "" {
		errs = append(errs, loc("missing required field 'namespace'"))
	}
	if m.Version == "" {
		errs = append(errs, loc("missing required field 'version'"))
	}
	if len(m.Commands) == 0 {
		errs = append(errs, loc("manifest has no commands"))
	}

	// Validate each command
	seenCommands := make(map[string]bool)
	for i, cmd := range m.Commands {
		cmdLoc := func(msg string) string {
			return loc(fmt.Sprintf("commands[%d] (%s): %s", i, cmd.Command, msg))
		}

		if cmd.Command == "" {
			errs = append(errs, loc(fmt.Sprintf("commands[%d]: missing required field 'command'", i)))
			continue
		}
		if seenCommands[cmd.Command] {
			errs = append(errs, cmdLoc("duplicate command name"))
		}
		seenCommands[cmd.Command] = true

		if cmd.Label == "" {
			errs = append(errs, cmdLoc("missing required field 'label'"))
		}

		// Validate targetMode
		if !validTargetModes[cmd.TargetMode] {
			errs = append(errs, cmdLoc(fmt.Sprintf("invalid targetMode '%s' (valid: single, broadcast, both)", cmd.TargetMode)))
		}

		// Validate parameters
		seenParams := make(map[string]bool)
		for j, param := range cmd.Parameters {
			paramLoc := func(msg string) string {
				return cmdLoc(fmt.Sprintf("parameters[%d] (%s): %s", j, param.Name, msg))
			}

			if param.Name == "" {
				errs = append(errs, cmdLoc(fmt.Sprintf("parameters[%d]: missing required field 'name'", j)))
				continue
			}
			if seenParams[param.Name] {
				errs = append(errs, paramLoc("duplicate parameter name"))
			}
			seenParams[param.Name] = true

			if param.Label == "" {
				errs = append(errs, paramLoc("missing required field 'label'"))
			}

			// Validate parameter type
			if param.Type == "" {
				errs = append(errs, paramLoc("missing required field 'type'"))
			} else if !validParamTypes[param.Type] {
				errs = append(errs, paramLoc(fmt.Sprintf("invalid type '%s' (valid: %s)", param.Type, strings.Join(validParamTypeList(), ", "))))
			}

			// Select parameters must have options
			if param.Type == "select" && len(param.Options) == 0 {
				errs = append(errs, paramLoc("select parameter must have at least one option"))
			}

			// Validate options
			for k, opt := range param.Options {
				if opt.Value == "" {
					errs = append(errs, paramLoc(fmt.Sprintf("options[%d]: missing required field 'value'", k)))
				}
				if opt.Label == "" {
					errs = append(errs, paramLoc(fmt.Sprintf("options[%d]: missing required field 'label'", k)))
				}
			}

			// Validate min/max
			if param.Min != nil && param.Max != nil && *param.Min > *param.Max {
				errs = append(errs, paramLoc(fmt.Sprintf("min (%v) is greater than max (%v)", *param.Min, *param.Max)))
			}
		}
	}

	return errs
}

func validParamTypeList() []string {
	types := make([]string, 0, len(validParamTypes))
	for t := range validParamTypes {
		types = append(types, t)
	}
	return types
}

// LoadManifest loads a manifest from the given YAML file path and registers it.
// Returns an error if the file cannot be read or parsed.
func LoadManifest(yamlPath string) error {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", yamlPath, err)
	}

	var m manifestYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse manifest %s: %w", yamlPath, err)
	}

	// Validate manifest before registering
	if errs := validateManifest(&m, yamlPath); len(errs) > 0 {
		return fmt.Errorf("invalid manifest:\n  %s", strings.Join(errs, "\n  "))
	}

	// Convert YAML structure to internal Manifest type
	manifest := Manifest{
		Namespace:   m.Namespace,
		Version:     m.Version,
		DisplayName: m.DisplayName,
		Commands:    make([]CommandDefinition, len(m.Commands)),
	}

	for i, cmd := range m.Commands {
		manifest.Commands[i] = CommandDefinition{
			Command:      cmd.Command,
			Label:        cmd.Label,
			Description:  cmd.Description,
			Confirmation: cmd.Confirmation,
			TargetMode:   cmd.TargetMode,
			Parameters:   make([]CommandParameter, len(cmd.Parameters)),
		}

		for j, param := range cmd.Parameters {
			manifest.Commands[i].Parameters[j] = CommandParameter{
				Name:        param.Name,
				Label:       param.Label,
				Type:        param.Type,
				Required:    param.Required,
				Default:     param.Default,
				Description: param.Description,
				Min:         param.Min,
				Max:         param.Max,
				Options:     make([]ParameterOption, len(param.Options)),
			}

			for k, opt := range param.Options {
				manifest.Commands[i].Parameters[j].Options[k] = ParameterOption{
					Value: opt.Value,
					Label: opt.Label,
				}
			}
		}
	}

	RegisterManifest(manifest)
	return nil
}

// LoadManifestsFromDir loads all manifest.yaml files from extension subdirectories.
// It expects a directory structure like:
//
//	extensionsDir/
//	  husky/manifest.yaml
//	  skydio/manifest.yaml
//
// Returns a list of loaded namespaces and any errors encountered.
func LoadManifestsFromDir(extensionsDir string) ([]string, error) {
	entries, err := os.ReadDir(extensionsDir)
	if err != nil {
		return nil, fmt.Errorf("read extensions dir %s: %w", extensionsDir, err)
	}

	var loaded []string
	var loadErrors []error

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(extensionsDir, entry.Name(), "manifest.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			// No manifest.yaml in this directory — skip
			continue
		}

		if err := LoadManifest(manifestPath); err != nil {
			loadErrors = append(loadErrors, err)
			continue
		}

		loaded = append(loaded, entry.Name())
	}

	if len(loadErrors) > 0 {
		return loaded, fmt.Errorf("failed to load %d manifests: %v", len(loadErrors), loadErrors)
	}

	return loaded, nil
}
