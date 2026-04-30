package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseMulticastSources(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr string
	}{
		{
			name:  "single source",
			input: "239.255.0.1:14550",
			want:  1,
		},
		{
			name:  "multiple sources",
			input: "239.255.0.1:14550,239.255.1.1:14551",
			want:  2,
		},
		{
			name:  "with labels",
			input: "239.255.0.1:14550:ugv,239.255.1.1:14551:usv",
			want:  2,
		},
		{
			name:  "whitespace handling",
			input: " 239.255.0.1:14550 , 239.255.1.1:14551 ",
			want:  2,
		},
		{
			name:  "empty entries skipped",
			input: "239.255.0.1:14550,,239.255.1.1:14551",
			want:  2,
		},
		{
			name:  "empty label defaults",
			input: "239.255.0.1:14550:",
			want:  1,
		},
		{
			name:    "duplicate source",
			input:   "239.255.0.1:14550,239.255.0.1:14550",
			wantErr: "duplicate source",
		},
		{
			name:    "non-multicast IP",
			input:   "192.168.1.1:14550",
			wantErr: "not a valid multicast address",
		},
		{
			name:    "invalid IP",
			input:   "not-an-ip:14550",
			wantErr: "not a valid multicast address",
		},
		{
			name:    "missing port",
			input:   "239.255.0.1",
			wantErr: "missing port",
		},
		{
			name:    "invalid port",
			input:   "239.255.0.1:abc",
			wantErr: "invalid port",
		},
		{
			name:    "port out of range",
			input:   "239.255.0.1:99999",
			wantErr: "port must be 1-65535",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: "no valid sources",
		},
		{
			name:    "only whitespace",
			input:   "   ,  ,  ",
			wantErr: "no valid sources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMulticastSources(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(got) != tt.want {
				t.Errorf("expected %d sources, got %d", tt.want, len(got))
			}
		})
	}
}

func TestParseMulticastSourcesLabels(t *testing.T) {
	sources, err := parseMulticastSources("239.255.0.1:14550:ugv-fleet,239.255.1.1:14551")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sources[0].Label != "ugv-fleet" {
		t.Errorf("expected label 'ugv-fleet', got %q", sources[0].Label)
	}
	if sources[1].Label != "source-1" {
		t.Errorf("expected default label 'source-1', got %q", sources[1].Label)
	}
}

func TestLoadMulticastSources(t *testing.T) {
	os.Setenv("TOWER_MCAST_SOURCES", "239.255.0.1:14550:test")
	defer os.Unsetenv("TOWER_MCAST_SOURCES")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.MulticastSources) != 1 {
		t.Errorf("expected 1 source, got %d", len(cfg.MulticastSources))
	}
	if cfg.MulticastSources[0].Label != "test" {
		t.Errorf("expected label 'test', got %q", cfg.MulticastSources[0].Label)
	}
}

func TestLoadFallbackToSingleSource(t *testing.T) {
	os.Unsetenv("TOWER_MCAST_SOURCES")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.MulticastSources) != 1 {
		t.Errorf("expected 1 source (fallback), got %d", len(cfg.MulticastSources))
	}
	if cfg.MulticastSources[0].Group != "239.255.0.1" {
		t.Errorf("expected default group, got %q", cfg.MulticastSources[0].Group)
	}
}

func TestValidateMulticastSources(t *testing.T) {
	cfg := Default()
	cfg.MulticastSources = nil

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty MulticastSources")
	}
}
