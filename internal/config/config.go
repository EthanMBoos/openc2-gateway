// Package config provides configuration management for the gateway.
// Configuration is loaded from environment variables with sensible defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all gateway configuration.
type Config struct {
	// WebSocket settings
	WSPort int // OPENC2_WS_PORT (default: 9000)

	// Multicast settings for vehicle telemetry (inbound)
	MulticastGroup string // OPENC2_MCAST_GROUP (default: 239.255.0.1)
	MulticastPort  int    // OPENC2_MCAST_PORT (default: 14550)

	// Multicast settings for commands (outbound)
	CmdMulticastGroup string // OPENC2_CMD_MCAST_GROUP (default: 239.255.0.2)
	CmdMulticastPort  int    // OPENC2_CMD_MCAST_PORT (default: 14551)

	// Vehicle status timeouts
	StandbyTimeout time.Duration // OPENC2_STANDBY_TIMEOUT (default: 3s)
	OfflineTimeout time.Duration // OPENC2_OFFLINE_TIMEOUT (default: 10s)

	// Command settings
	CmdTimeout   time.Duration // OPENC2_CMD_TIMEOUT (default: 5s)
	CmdRateLimit int           // OPENC2_CMD_RATE_LIMIT (default: 10 per second per vehicle)

	// Gateway metadata
	GatewayVersion string // Injected at build time
}

// Default returns a Config with sensible defaults matching PROTOCOL.md.
func Default() Config {
	return Config{
		WSPort:            9000,
		MulticastGroup:    "239.255.0.1",
		MulticastPort:     14550,
		CmdMulticastGroup: "239.255.0.2",
		CmdMulticastPort:  14551,
		StandbyTimeout:    3 * time.Second,
		OfflineTimeout:    10 * time.Second,
		CmdTimeout:        5 * time.Second,
		CmdRateLimit:      10,
		GatewayVersion:    "0.1.0",
	}
}

// Load reads configuration from environment variables.
// Unset variables use defaults from Default().
func Load() (Config, error) {
	cfg := Default()
	var err error

	// WebSocket port
	if v := os.Getenv("OPENC2_WS_PORT"); v != "" {
		cfg.WSPort, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_WS_PORT: %w", err)
		}
	}

	// Multicast settings (telemetry)
	if v := os.Getenv("OPENC2_MCAST_GROUP"); v != "" {
		cfg.MulticastGroup = v
	}
	if v := os.Getenv("OPENC2_MCAST_PORT"); v != "" {
		cfg.MulticastPort, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_MCAST_PORT: %w", err)
		}
	}

	// Multicast settings (commands)
	if v := os.Getenv("OPENC2_CMD_MCAST_GROUP"); v != "" {
		cfg.CmdMulticastGroup = v
	}
	if v := os.Getenv("OPENC2_CMD_MCAST_PORT"); v != "" {
		cfg.CmdMulticastPort, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_CMD_MCAST_PORT: %w", err)
		}
	}

	// Timeouts
	if v := os.Getenv("OPENC2_STANDBY_TIMEOUT"); v != "" {
		cfg.StandbyTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_STANDBY_TIMEOUT: %w", err)
		}
	}
	if v := os.Getenv("OPENC2_OFFLINE_TIMEOUT"); v != "" {
		cfg.OfflineTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_OFFLINE_TIMEOUT: %w", err)
		}
	}
	if v := os.Getenv("OPENC2_CMD_TIMEOUT"); v != "" {
		cfg.CmdTimeout, err = time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_CMD_TIMEOUT: %w", err)
		}
	}

	// Command rate limit
	if v := os.Getenv("OPENC2_CMD_RATE_LIMIT"); v != "" {
		cfg.CmdRateLimit, err = strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid OPENC2_CMD_RATE_LIMIT: %w", err)
		}
	}

	return cfg, nil
}

// Validate checks configuration for invalid combinations.
func (c Config) Validate() error {
	if c.WSPort < 1 || c.WSPort > 65535 {
		return fmt.Errorf("invalid WSPort: must be 1-65535, got %d", c.WSPort)
	}
	if c.MulticastPort < 1 || c.MulticastPort > 65535 {
		return fmt.Errorf("invalid MulticastPort: must be 1-65535, got %d", c.MulticastPort)
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
