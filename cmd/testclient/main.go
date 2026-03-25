// Package main provides a simple integration test for the gateway.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/EthanMBoos/openc2-gateway/internal/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func main() {
	// Test modes
	duration := flag.Duration("duration", 0, "Stay connected for this duration (e.g., 30s). If 0, reads 5 frames and exits.")
	badVersion := flag.Bool("bad-version", false, "Send hello with invalid protocol version (test error handling)")
	skipHello := flag.Bool("skip-hello", false, "Send command without hello first (test error handling)")

	// Command mode
	cmd := flag.String("cmd", "", "Send command: stop, goto, return_home, set_mode, set_speed, ext")
	vid := flag.String("vid", "ugv-husky-01", "Vehicle ID for command")
	lat := flag.Float64("lat", 37.7749, "Latitude for goto command")
	lng := flag.Float64("lng", -122.4194, "Longitude for goto command")
	speed := flag.Float64("speed", 5.0, "Speed for goto/set_speed commands (m/s)")
	mode := flag.String("mode", "autonomous", "Mode for set_mode/ext command (manual/autonomous/guided)")

	// Extension commands (currently only Husky is implemented)
	ext := flag.String("ext", "husky", "Extension namespace (currently only 'husky' supported)")
	action := flag.String("action", "", "Extension action: setDriveMode, setBumperSensitivity, triggerEStop")
	sensitivity := flag.Int("sensitivity", 75, "Bumper sensitivity for setBumperSensitivity (0-100)")

	// Rate limit testing
	burst := flag.Int("burst", 0, "Send N commands rapidly to test rate limiting (use with -cmd)")

	// Timeout testing
	wait := flag.Duration("wait", 0, "After initial ack, wait this long for timeout ack (e.g., 6s)")

	flag.Parse()

	// Connect to gateway
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:9000", nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	fmt.Println("✓ Connected to gateway")

	// Error test mode: bad protocol version
	if *badVersion {
		testBadVersion(conn)
		return
	}

	// Error test mode: skip hello
	if *skipHello {
		testSkipHello(conn)
		return
	}

	// Command mode
	if *cmd != "" {
		if *burst > 0 {
			testBurst(conn, *cmd, *vid, *burst)
		} else {
			sendCommand(conn, *cmd, *vid, *lat, *lng, *speed, *mode, *ext, *action, *sensitivity, *wait)
		}
		return
	}

	// Normal mode: send hello and read telemetry
	normalMode(conn, *duration)
}

func testBadVersion(conn *websocket.Conn) {
	fmt.Println("Testing: bad protocol version...")

	hello := protocol.Frame{
		ProtocolVersion: 99, // Invalid version
		Type:            protocol.TypeHello,
		VehicleID:       protocol.VehicleIDClient,
		TimestampMs:     time.Now().UnixMilli(),
		Data: protocol.HelloPayload{
			ProtocolVersion: 99, // Invalid version
			ClientID:        "test-client",
		},
	}

	helloBytes, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		log.Fatalf("Failed to send hello: %v", err)
	}
	fmt.Println("  Sent hello with v=99")

	// Read error response
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	var frame protocol.Frame
	if err := json.Unmarshal(msg, &frame); err != nil {
		log.Fatalf("Failed to parse response: %v", err)
	}

	fmt.Printf("  Response: %s\n", string(msg))

	if frame.Type == protocol.TypeError {
		data, ok := frame.Data.(map[string]interface{})
		if ok && data["code"] == "PROTOCOL_VERSION_UNSUPPORTED" {
			fmt.Println("✓ Received expected PROTOCOL_VERSION_UNSUPPORTED error")
			os.Exit(0)
		}
	}

	fmt.Println("✗ Did not receive expected error")
	os.Exit(1)
}

func testSkipHello(conn *websocket.Conn) {
	fmt.Println("Testing: command without hello...")

	// Send command directly without hello
	command := protocol.Frame{
		ProtocolVersion: protocol.ProtocolVersion,
		Type:            protocol.TypeCommand,
		VehicleID:       "ugv-test-01",
		TimestampMs:     time.Now().UnixMilli(),
		Data: map[string]interface{}{
			"action":    "stop",
			"commandId": "test-cmd-001",
		},
	}

	cmdBytes, _ := json.Marshal(command)
	if err := conn.WriteMessage(websocket.TextMessage, cmdBytes); err != nil {
		log.Fatalf("Failed to send command: %v", err)
	}
	fmt.Println("  Sent command without hello")

	// Read error response
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	var frame protocol.Frame
	if err := json.Unmarshal(msg, &frame); err != nil {
		log.Fatalf("Failed to parse response: %v", err)
	}

	fmt.Printf("  Response: %s\n", string(msg))

	if frame.Type == protocol.TypeError {
		data, ok := frame.Data.(map[string]interface{})
		if ok && data["code"] == "INVALID_MESSAGE" {
			fmt.Println("✓ Received expected INVALID_MESSAGE error")
			os.Exit(0)
		}
	}

	fmt.Println("✗ Did not receive expected error")
	os.Exit(1)
}

func normalMode(conn *websocket.Conn, duration time.Duration) {
	// Send hello
	hello := protocol.Frame{
		ProtocolVersion: protocol.ProtocolVersion,
		Type:            protocol.TypeHello,
		VehicleID:       protocol.VehicleIDClient,
		TimestampMs:     time.Now().UnixMilli(),
		Data: protocol.HelloPayload{
			ProtocolVersion: protocol.ProtocolVersion,
			ClientID:        "test-client",
		},
	}

	helloBytes, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		log.Fatalf("Failed to send hello: %v", err)
	}
	fmt.Println("✓ Sent hello")

	// Read welcome response
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read welcome: %v", err)
	}

	var welcome protocol.Frame
	if err := json.Unmarshal(msg, &welcome); err != nil {
		log.Fatalf("Failed to parse welcome: %v", err)
	}

	if welcome.Type != protocol.TypeWelcome {
		log.Fatalf("Expected welcome, got %s", welcome.Type)
	}
	fmt.Printf("✓ Received welcome (type=%s)\n", welcome.Type)

	// Read telemetry frames
	fmt.Println("✓ Reading telemetry frames...")

	if duration > 0 {
		// Duration mode: read until time expires
		fmt.Printf("  (staying connected for %s)\n", duration)
		deadline := time.Now().Add(duration)
		conn.SetReadDeadline(deadline)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break // timeout or error
			}

			var frame protocol.Frame
			if err := json.Unmarshal(msg, &frame); err != nil {
				continue
			}

			if frame.Type == protocol.TypeTelemetry {
				data, _ := json.Marshal(frame.Data)
				fmt.Printf("  [%s] %s\n", frame.VehicleID, string(data))
			} else {
				fmt.Printf("  [%s] type=%s\n", frame.VehicleID, frame.Type)
			}
		}
		fmt.Println("\n✓ Duration expired, disconnecting")
	} else {
		// Default mode: read 5 frames
		for i := 0; i < 5; i++ {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Fatalf("Failed to read telemetry: %v", err)
			}

			var frame protocol.Frame
			if err := json.Unmarshal(msg, &frame); err != nil {
				log.Fatalf("Failed to parse frame: %v", err)
			}

			if frame.Type == protocol.TypeTelemetry {
				data, _ := json.Marshal(frame.Data)
				fmt.Printf("  [%s] %s\n", frame.VehicleID, string(data))
			} else {
				fmt.Printf("  [%s] type=%s\n", frame.VehicleID, frame.Type)
			}
		}
		fmt.Println("\n✓ Phase 1 test passed!")
	}
}

func sendCommand(conn *websocket.Conn, cmd, vid string, lat, lng, speed float64, mode, ext, action string, sensitivity int, wait time.Duration) {
	// Send hello first
	hello := protocol.Frame{
		ProtocolVersion: protocol.ProtocolVersion,
		Type:            protocol.TypeHello,
		VehicleID:       protocol.VehicleIDClient,
		TimestampMs:     time.Now().UnixMilli(),
		Data: protocol.HelloPayload{
			ProtocolVersion: protocol.ProtocolVersion,
			ClientID:        "testclient-cmd",
		},
	}

	helloBytes, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		log.Fatalf("Failed to send hello: %v", err)
	}

	// Read welcome
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read welcome: %v", err)
	}
	var welcome protocol.Frame
	json.Unmarshal(msg, &welcome)
	if welcome.Type != protocol.TypeWelcome {
		log.Fatalf("Expected welcome, got %s", welcome.Type)
	}

	// Build command payload
	commandID := fmt.Sprintf("%s-%s", cmd, uuid.New().String()[:8])
	var data interface{}

	switch cmd {
	case "stop":
		data = map[string]interface{}{
			"commandId": commandID,
		}
	case "goto":
		data = map[string]interface{}{
			"commandId":   commandID,
			"destination": map[string]float64{"lat": lat, "lng": lng},
			"speed":       speed,
		}
	case "return_home":
		data = map[string]interface{}{
			"commandId": commandID,
		}
	case "set_mode":
		data = map[string]interface{}{
			"commandId": commandID,
			"mode":      mode,
		}
	case "set_speed":
		data = map[string]interface{}{
			"commandId": commandID,
			"speed":     speed,
		}
	case "ext":
		// Extension command (currently only Husky supported)
		if ext != "husky" {
			log.Fatalf("Unsupported extension: %s (only 'husky' is implemented)", ext)
		}
		cmd = "extension" // Wire format uses "extension"
		var payload map[string]interface{}
		switch action {
		case "setDriveMode":
			payload = map[string]interface{}{"type": "setDriveMode", "mode": mode}
		case "setBumperSensitivity":
			payload = map[string]interface{}{"type": "setBumperSensitivity", "sensitivity": sensitivity}
		case "triggerEStop":
			payload = map[string]interface{}{"type": "triggerEStop"}
		default:
			log.Fatalf("Unknown Husky action: %s (use: setDriveMode, setBumperSensitivity, triggerEStop)", action)
		}
		data = map[string]interface{}{
			"commandId": commandID,
			"namespace": ext,
			"payload":   payload,
		}
	default:
		log.Fatalf("Unknown command: %s (use: stop, goto, return_home, set_mode, set_speed, ext)", cmd)
	}

	// Send command
	cmdFrame := protocol.Frame{
		ProtocolVersion: protocol.ProtocolVersion,
		Type:            protocol.TypeCommand,
		VehicleID:       vid,
		TimestampMs:     time.Now().UnixMilli(),
		Command:         cmd,
		Data:            data,
	}

	cmdBytes, _ := json.Marshal(cmdFrame)
	if err := conn.WriteMessage(websocket.TextMessage, cmdBytes); err != nil {
		log.Fatalf("Failed to send command: %v", err)
	}
	fmt.Printf("→ Sent %s to %s (id=%s)\n", cmd, vid, commandID)

	// Read response (skip telemetry, look for ack/error)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("Timeout waiting for response: %v", err)
		}

		var frame protocol.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}

		// Skip telemetry
		if frame.Type == protocol.TypeTelemetry {
			continue
		}

		// Handle ack or error
		if frame.Type == protocol.TypeCommandAck {
			dataMap, _ := frame.Data.(map[string]interface{})
			status := dataMap["status"]
			fmt.Printf("✓ Command %s: %s\n", status, commandID)

			// If -wait specified and we got "accepted", wait for timeout ack
			if wait > 0 && status == "accepted" {
				fmt.Printf("→ Waiting %s for timeout ack...\n", wait)
				conn.SetReadDeadline(time.Now().Add(wait))
				for {
					_, msg, err := conn.ReadMessage()
					if err != nil {
						fmt.Println("✗ No timeout ack received (connection timeout)")
						return
					}

					var f protocol.Frame
					if err := json.Unmarshal(msg, &f); err != nil {
						continue
					}

					if f.Type == protocol.TypeTelemetry {
						continue
					}

					if f.Type == protocol.TypeCommandAck {
						dm, _ := f.Data.(map[string]interface{})
						st := dm["status"]
						if st == "timeout" {
							fmt.Printf("✓ Received timeout ack: %s\n", dm["message"])
							return
						}
						fmt.Printf("  Got ack status=%s\n", st)
					}
				}
			}
			return
		}

		if frame.Type == protocol.TypeError {
			dataMap, _ := frame.Data.(map[string]interface{})
			code := dataMap["code"]
			message := dataMap["message"]
			fmt.Printf("✗ Error [%s]: %s\n", code, message)
			os.Exit(1)
		}
	}
}

func testBurst(conn *websocket.Conn, cmd, vid string, count int) {
	// Send hello first
	hello := protocol.Frame{
		ProtocolVersion: protocol.ProtocolVersion,
		Type:            protocol.TypeHello,
		VehicleID:       protocol.VehicleIDClient,
		TimestampMs:     time.Now().UnixMilli(),
		Data: protocol.HelloPayload{
			ProtocolVersion: protocol.ProtocolVersion,
			ClientID:        "testclient-burst",
		},
	}

	helloBytes, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		log.Fatalf("Failed to send hello: %v", err)
	}

	// Read welcome
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read welcome: %v", err)
	}
	var welcome protocol.Frame
	json.Unmarshal(msg, &welcome)
	if welcome.Type != protocol.TypeWelcome {
		log.Fatalf("Expected welcome, got %s", welcome.Type)
	}

	fmt.Printf("→ Sending %d %s commands rapidly to %s...\n", count, cmd, vid)

	// Send N commands as fast as possible
	accepted := 0
	rateLimited := 0

	for i := 0; i < count; i++ {
		commandID := fmt.Sprintf("%s-burst-%d", cmd, i+1)
		cmdFrame := protocol.Frame{
			ProtocolVersion: protocol.ProtocolVersion,
			Type:            protocol.TypeCommand,
			VehicleID:       vid,
			TimestampMs:     time.Now().UnixMilli(),
			Command:         cmd,
			Data: map[string]interface{}{
				"commandId": commandID,
			},
		}

		cmdBytes, _ := json.Marshal(cmdFrame)
		if err := conn.WriteMessage(websocket.TextMessage, cmdBytes); err != nil {
			log.Fatalf("Failed to send command %d: %v", i+1, err)
		}
	}

	// Read responses
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	responsesNeeded := count
	for responsesNeeded > 0 {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var frame protocol.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}

		// Skip telemetry
		if frame.Type == protocol.TypeTelemetry {
			continue
		}

		if frame.Type == protocol.TypeCommandAck {
			accepted++
			responsesNeeded--
		} else if frame.Type == protocol.TypeError {
			dataMap, _ := frame.Data.(map[string]interface{})
			if dataMap["code"] == "RATE_LIMITED" {
				rateLimited++
			}
			responsesNeeded--
		}
	}

	fmt.Printf("✓ Results: %d accepted, %d rate-limited\n", accepted, rateLimited)
	if rateLimited > 0 {
		fmt.Println("✓ Rate limiting is working!")
	}
}
