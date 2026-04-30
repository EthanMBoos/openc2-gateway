// Package config provides configuration management for the server.
// Configuration is loaded from environment variables with sensible defaults.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// MulticastSourceConfig defines a single multicast telemetry source.
type MulticastSourceConfig struct {
	Group string `yaml:"group" json:"group"` // Multicast group address
	Port  int    `yaml:"port"  json:"port"`  // UDP port
	Label string `yaml:"label" json:"label"` // Human-readable label for logging
}

// Config holds all server configuration.
type Config struct {
	// WebSocket settings
	WSPort int // TOWER_WS_PORT (default: 9000)

	// Multicast telemetry sources (inbound from vehicles)
	// TOWER_MCAST_SOURCES="239.255.0.1:14550,239.255.1.1:14551"
	// Default: 239.255.0.1:14550
	MulticastSources []MulticastSourceConfig

	// Multicast settings for commands (outbound)
	CmdMulticastGroup string // TOWER_CMD_MCAST_GROUP (default: 239.255.0.2)
	CmdMulticastPort  int    // TOWER_CMD_MCAST_PORT (default: 14551)

	// Vehicle status timeouts
	StandbyTimeout time.Duration // TOWER_STANDBY_TIMEOUT (default: 3s)
	OfflineTimeout time.Duration // TOWER_OFFLINE_TIMEOUT (default: 10s)

	// Command settings
	CmdTimeout   time.Duration // TOWER_CMD_TIMEOUT (default: 5s)
	CmdRateLimit int           // TOWER_CMD_RATE_LIMIT (default: 10 per second per vehicle)

	// Server metadata
	ServerVersion string // Injected at build time
}

// Default multicast addresses per PROTOCOL.md
const (
	DefaultMulticastGroup = "239.255.0.1"
	DefaultMulticastPort  = 14550
)

// Default returns a Config with sensible defaults matching PROTOCOL.md.
func Default() Config {
	return Config{
		WSPort: 9000,
		MulticastSources: []MulticastSourceConfig{{
			Group: DefaultMulticastGroup,
			Port:  DefaultMulticastPort,
			Label: "default",
		}},
		CmdMulticastGroup: "239.255.0.2",
		CmdMulticastPort:  14551,
		StandbyTimeout:    3 * time.Second,
		OfflineTimeout:    10 * time.Second,
		CmdTimeout:        5 * time.Second,
		CmdRateLimit:      10,
		ServerVersion:    "0.1.0",
	}
}

// Load reads configuration from environment variables.
// Unset variables use defaults from Default().
func Load() (Config, error) {
	cfg := Default()
	var err error

	// WebSocket port
	if v := os.Getenv("TOWER_WS_PORT"); v != "" {
		cfg.WSPort, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_WS_PORT: %w", err)
		}
	}

	// Multicast settings (commands)
	if v := os.Getenv("TOWER_CMD_MCAST_GROUP"); v != "" {
		cfg.CmdMulticastGroup = v
	}
	if v := os.Getenv("TOWER_CMD_MCAST_PORT"); v != "" {
		cfg.CmdMulticastPort, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_CMD_MCAST_PORT: %w", err)
		}
	}

	// Timeouts
	if v := os.Getenv("TOWER_STANDBY_TIMEOUT"); v != "" {
		cfg.StandbyTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_STANDBY_TIMEOUT: %w", err)
		}
	}
	if v := os.Getenv("TOWER_OFFLINE_TIMEOUT"); v != "" {
		cfg.OfflineTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_OFFLINE_TIMEOUT: %w", err)
		}
	}
	if v := os.Getenv("TOWER_CMD_TIMEOUT"); v != "" {
		cfg.CmdTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_CMD_TIMEOUT: %w", err)
		}
	}

	// Command rate limit
	if v := os.Getenv("TOWER_CMD_RATE_LIMIT"); v != "" {
		cfg.CmdRateLimit, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_CMD_RATE_LIMIT: %w", err)
		}
	}

	// Multicast telemetry sources
	if v := os.Getenv("TOWER_MCAST_SOURCES"); v != "" {
		cfg.MulticastSources, err = parseMulticastSources(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid TOWER_MCAST_SOURCES: %w", err)
		}
	}
	// Otherwise keep default from Default()

	return cfg, nil
}

// parseMulticastSources parses "239.255.0.1:14550,239.255.1.1:14551" format.
// Optional labels: "239.255.0.1:14550:ugv,239.255.1.1:14551:usv"
func parseMulticastSources(s string) ([]MulticastSourceConfig, error) {
	var sources []MulticastSourceConfig
	seen := make(map[string]bool) // Detect duplicates

	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check for optional label suffix: "group:port:label"
		var label string
		if colonCount := strings.Count(part, ":"); colonCount == 2 {
			lastColon := strings.LastIndex(part, ":")
			label = strings.TrimSpace(part[lastColon+1:])
			part = part[:lastColon]
		}

		host, portStr, err := net.SplitHostPort(part)
		if err != nil {
			return nil, fmt.Errorf("source %d (%q): %w", i, part, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("source %d: invalid port %q: %w", i, portStr, err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("source %d: port must be 1-65535, got %d", i, port)
		}

		// Validate multicast address range (224.0.0.0 - 239.255.255.255)
		if !isMulticastAddress(host) {
			return nil, fmt.Errorf("source %d: %q is not a valid multicast address (must be 224.0.0.0-239.255.255.255)", i, host)
		}

		// Check for duplicates
		key := fmt.Sprintf("%s:%d", host, port)
		if seen[key] {
			return nil, fmt.Errorf("duplicate source: %s", key)
		}
		seen[key] = true

		// Default label if empty or not provided
		if label == "" {
			label = fmt.Sprintf("source-%d", len(sources))
		}

		sources = append(sources, MulticastSourceConfig{
			Group: host,
			Port:  port,
			Label: label,
		})
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no valid sources specified")
	}

	return sources, nil
}

// isMulticastAddress checks if the IP is in the multicast range 224.0.0.0-239.255.255.255.
func isMulticastAddress(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsMulticast()
}

// Validate checks configuration for invalid combinations.
func (c Config) Validate() error {
	if c.WSPort < 1 || c.WSPort > 65535 {
		return fmt.Errorf("invalid WSPort: must be 1-65535, got %d", c.WSPort)
	}
	if len(c.MulticastSources) == 0 {
		return fmt.Errorf("no multicast sources configured")
	}
	if c.CmdMulticastPort < 1 || c.CmdMulticastPort > 65535 {
		return fmt.Errorf("invalid CmdMulticastPort: must be 1-65535, got %d", c.CmdMulticastPort)
	}
	if c.StandbyTimeout <= 0 {
		return fmt.Errorf("StandbyTimeout must be positive")
	}
	if c.OfflineTimeout <= c.StandbyTimeout {
		return fmt.Errorf("OfflineTimeout (%v) must be greater than StandbyTimeout (%v)",
			c.OfflineTimeout, c.StandbyTimeout)
	}
	if c.CmdTimeout <= 0 {
		return fmt.Errorf("CmdTimeout must be positive")
	}
	if c.CmdRateLimit <= 0 {
		return fmt.Errorf("CmdRateLimit must be positive")
	}
	return nil
}
