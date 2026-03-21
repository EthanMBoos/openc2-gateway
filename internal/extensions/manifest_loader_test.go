package extensions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifest_Valid(t *testing.T) {
	// Create temp manifest
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
displayName: Test Extension
commands:
  - command: doSomething
    label: Do Something
    description: Does something useful
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := LoadManifest(manifestPath); err != nil {
		t.Errorf("expected valid manifest to load, got error: %v", err)
	}
}

func TestLoadManifest_MissingNamespace(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
version: "1.0"
displayName: Test Extension
commands:
  - command: doSomething
    label: Do Something
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for missing namespace")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("error should mention 'namespace', got: %v", err)
	}
}

func TestLoadManifest_InvalidTargetMode(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
commands:
  - command: doSomething
    label: Do Something
    targetMode: invalid_mode
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for invalid targetMode")
	}
	if !strings.Contains(err.Error(), "targetMode") {
		t.Errorf("error should mention 'targetMode', got: %v", err)
	}
}

func TestLoadManifest_InvalidParamType(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
commands:
  - command: doSomething
    label: Do Something
    parameters:
      - name: foo
        label: Foo
        type: not_a_real_type
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for invalid param type")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Errorf("error should mention 'invalid type', got: %v", err)
	}
}

func TestLoadManifest_SelectWithoutOptions(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
commands:
  - command: doSomething
    label: Do Something
    parameters:
      - name: choice
        label: Choice
        type: select
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for select without options")
	}
	if !strings.Contains(err.Error(), "select parameter must have") {
		t.Errorf("error should mention select options requirement, got: %v", err)
	}
}

func TestLoadManifest_MinGreaterThanMax(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
commands:
  - command: doSomething
    label: Do Something
    parameters:
      - name: value
        label: Value
        type: number
        min: 100
        max: 10
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for min > max")
	}
	if !strings.Contains(err.Error(), "min") && !strings.Contains(err.Error(), "max") {
		t.Errorf("error should mention min/max issue, got: %v", err)
	}
}

func TestLoadManifest_DuplicateCommand(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	content := `
namespace: test
version: "1.0"
commands:
  - command: doSomething
    label: Do Something
  - command: doSomething
    label: Do Something Again
`
	if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadManifest(manifestPath)
	if err == nil {
		t.Error("expected error for duplicate command")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention 'duplicate', got: %v", err)
	}
}
