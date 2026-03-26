// Package main provides a test sender that broadcasts mock vehicle telemetry
// via UDP multicast for integration testing the gateway.
//
// Usage:
//
//	go run ./cmd/testsender -vid ugv-husky-07
//	go run ./cmd/testsender -vid uav-quad-01 -env air -rate 20
//	go run ./cmd/testsender -vid sensor-01 -caps none  # observation-only (no commands)
//	go run ./cmd/testsender -vid fixed-wing-01 -caps no-stop  # no stop command
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	pb "github.com/EthanMBoos/openc2-gateway/api/proto"
	"google.golang.org/protobuf/proto"
)

func main() {
	// Command-line flags
	// Note: Short flag names (-vid, -env) are used for CLI brevity.
	// Full names: vehicle-id, environment, multicast-group, multicast-port, telemetry-rate, capabilities
	vid := flag.String("vid", "ugv-test-01", "Vehicle ID (e.g., ugv-husky-01, uav-quad-02)")
	env := flag.String("env", "ground", "Vehicle environment: ground, air, or marine")
	group := flag.String("group", "239.255.0.1", "Multicast group address for telemetry broadcast")
	port := flag.Int("port", 14550, "Multicast UDP port for telemetry broadcast")
	rate := flag.Int("rate", 10, "Telemetry transmission rate in Hz (messages per second)")
	capsMode := flag.String("caps", "all", "Capability mode: all (full commands), no-stop, no-goto, or none (observation-only)")
	flag.Parse()

	// Build capabilities based on mode
	caps := buildCapabilities(*capsMode, *env)

	// Validate environment
	envEnum := pb.VehicleEnvironment_ENV_UNKNOWN
	switch *env {
	case "ground":
		envEnum = pb.VehicleEnvironment_ENV_GROUND
	case "air":
		envEnum = pb.VehicleEnvironment_ENV_AIR
	case "marine":
		envEnum = pb.VehicleEnvironment_ENV_MARINE
	default:
		log.Printf("Warning: unknown environment %q, using unknown", *env)
	}

	// Create multicast UDP connection
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", *group, *port))
	if err != nil {
		log.Fatalf("Failed to resolve address: %v", err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Fatalf("Failed to create UDP connection: %v", err)
	}
	defer conn.Close()

	log.Printf("Sending telemetry for %s to %s:%d at %dHz (caps=%s)", *vid, *group, *port, *rate, *capsMode)
	if caps != nil {
		log.Printf("  Supported commands: %v", caps.SupportedCommands)
	}

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start sending telemetry
	ticker := time.NewTicker(time.Duration(float64(time.Second) / float64(*rate)))
	defer ticker.Stop()

	// Heartbeat ticker (every 2 seconds)
	heartbeatTicker := time.NewTicker(2 * time.Second)
	defer heartbeatTicker.Stop()

	startTime := time.Now()

	// Initial vehicle state
	state := &vehicleState{
		lat:         37.7749,
		lng:         -122.4194,
		alt:         50,
		heading:     rand.Float64() * 360,
		speed:       5.0,
		batteryPct:  90,
		signalBars:  4,
		seq:         0,
		environment: envEnum,
	}

	for {
		select {
		case <-sigCh:
			log.Println("Shutting down...")
			return
		case <-heartbeatTicker.C:
			// Send heartbeat with capabilities
			uptimeMs := time.Since(startTime).Milliseconds()
			hb := buildHeartbeat(*vid, uptimeMs, caps)
			data, err := proto.Marshal(hb)
			if err != nil {
				log.Printf("Failed to marshal heartbeat: %v", err)
				continue
			}
			if _, err := conn.Write(data); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
			}
		case <-ticker.C:
			// Update state
			state.update(1.0 / float64(*rate))

			// Build protobuf message
			msg := state.toProto(*vid)
			data, err := proto.Marshal(msg)
			if err != nil {
				log.Printf("Failed to marshal: %v", err)
				continue
			}

			// Send
			if _, err := conn.Write(data); err != nil {
				log.Printf("Failed to send: %v", err)
				continue
			}

			// Log every 10 messages
			if state.seq%uint32(*rate) == 0 {
				log.Printf("[%s] seq=%d lat=%.6f lng=%.6f heading=%.1f",
					*vid, state.seq, state.lat, state.lng, state.heading)
			}
		}
	}
}

// buildCapabilities creates capabilities based on the mode flag.
func buildCapabilities(mode, env string) *pb.VehicleCapabilities {
	allCommands := []string{"goto", "stop", "return_home", "set_mode", "set_speed"}

	caps := &pb.VehicleCapabilities{
		SupportsMissions: true,
	}

	switch strings.ToLower(mode) {
	case "all":
		caps.SupportedCommands = allCommands
	case "no-stop":
		// Fixed-wing aircraft can't stop mid-flight
		caps.SupportedCommands = []string{"goto", "return_home", "set_mode", "set_speed"}
	case "no-goto":
		// Stationary sensor
		caps.SupportedCommands = []string{}
		caps.SupportsMissions = false
	case "none":
		// Observation-only vehicle
		caps.SupportedCommands = []string{}
		caps.SupportsMissions = false
	default:
		// Custom comma-separated list
		caps.SupportedCommands = strings.Split(mode, ",")
	}

	// Add extension capabilities based on environment
	caps.Extensions = buildExtensionCapabilities(env)

	// Add a sample sensor for non-air vehicles
	if env != "air" {
		caps.Sensors = []*pb.SensorCapability{
			{
				SensorId:  "front_camera",
				Type:      pb.SensorType_SENSOR_CAMERA_RGB,
				StreamUrl: "rtsp://localhost:8554/front",
				Mount: &pb.SensorMount{
					X:     0.5,
					Z:     0.3,
					Pitch: -15,
				},
			},
		}
	}

	return caps
}

// buildExtensionCapabilities creates extension capabilities based on environment.
func buildExtensionCapabilities(env string) []*pb.ExtensionCapability {
	var extensions []*pb.ExtensionCapability

	switch env {
	case "ground":
		// Husky UGV extension for ground vehicles
		extensions = append(extensions, &pb.ExtensionCapability{
			Namespace:        "husky",
			Version:          1, // Semver major version
			SupportedActions: []string{"setDriveMode", "triggerEStop", "clearEStop", "resetFaults"},
		})
	case "water":
		// Marine-specific extensions
		extensions = append(extensions, &pb.ExtensionCapability{
			Namespace:        "marine",
			Version:          1,
			SupportedActions: []string{"deployAnchor", "retractAnchor", "setRudderAngle", "setThrottle"},
		})
	case "air":
		// UAV payload extensions
		extensions = append(extensions, &pb.ExtensionCapability{
			Namespace:        "uav_payload",
			Version:          1,
			SupportedActions: []string{"captureImage", "startRecording", "stopRecording", "setGimbalAngle"},
		})
	}

	return extensions
}

// buildHeartbeat creates a heartbeat message with capabilities.
func buildHeartbeat(vid string, uptimeMs int64, caps *pb.VehicleCapabilities) *pb.VehicleMessage {
	return &pb.VehicleMessage{
		Payload: &pb.VehicleMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				VehicleId:    vid,
				TimestampMs:  time.Now().UnixMilli(),
				Status:       pb.VehicleStatus_STATUS_ONLINE,
				UptimeMs:     uptimeMs,
				Capabilities: caps,
			},
		},
	}
}

type vehicleState struct {
	lat, lng, alt float64
	heading       float64
	speed         float64
	batteryPct    uint32
	signalBars    uint32
	seq           uint32
	environment   pb.VehicleEnvironment
}

func (s *vehicleState) update(dt float64) {
	// Random walk heading
	s.heading += (rand.Float64() - 0.5) * 20 * dt
	for s.heading >= 360 {
		s.heading -= 360
	}
	for s.heading < 0 {
		s.heading += 360
	}

	// Update position
	headingRad := s.heading * math.Pi / 180
	vx := s.speed * math.Sin(headingRad)
	vy := s.speed * math.Cos(headingRad)

	metersPerDegLat := 111000.0
	metersPerDegLng := metersPerDegLat * math.Cos(s.lat*math.Pi/180)

	s.lat += (vy * dt) / metersPerDegLat
	s.lng += (vx * dt) / metersPerDegLng

	// Slowly drain battery
	if rand.Float64() < 0.0001 {
		s.batteryPct--
		if s.batteryPct < 20 {
			s.batteryPct = 20
		}
	}

	// Increment sequence
	s.seq++
}

func (s *vehicleState) toProto(vid string) *pb.VehicleMessage {
	now := time.Now().UnixMilli()

	// Copy values for pointer fields
	batteryPct := s.batteryPct
	signalStrength := s.signalBars

	telemetry := &pb.VehicleTelemetry{
		VehicleId:   vid,
		TimestampMs: now,
		Location: &pb.Location{
			Latitude:     s.lat,
			Longitude:    s.lng,
			AltitudeMslM: float32(s.alt),
		},
		SpeedMs:        float32(s.speed),
		HeadingDeg:     float32(s.heading),
		Environment:    s.environment,
		SequenceNum:    s.seq,
		BatteryPct:     &batteryPct,
		SignalStrength: &signalStrength,
	}

	return &pb.VehicleMessage{
		Payload: &pb.VehicleMessage_Telemetry{
			Telemetry: telemetry,
		},
	}
}
