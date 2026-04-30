#!/bin/bash
# Demo script - starts server with simulated vehicles
#
# Usage:
#   ./scripts/demo.sh          # 3 vehicles (default)
#   ./scripts/demo.sh 5        # 5 vehicles
#
# Cleanup:
#   Ctrl+C to stop all processes

set -e

VEHICLE_COUNT=${1:-3}

echo "Starting Tower Server demo with $VEHICLE_COUNT vehicles..."
echo ""

# Cleanup on exit
cleanup() {
    echo ""
    echo "Shutting down..."
    pkill -f "go run ./cmd/tower-server" 2>/dev/null || true
    pkill -f "go run ./cmd/testsender" 2>/dev/null || true
    echo "Done."
}
trap cleanup EXIT

# Start server
echo "→ Starting server on :9000..."
go run ./cmd/tower-server &
SERVER_PID=$!
sleep 1

# Start test senders
VEHICLE_TYPES=("ugv" "uav" "usv" "ugv" "uav")
ENVS=("ground" "air" "marine" "ground" "air")

for i in $(seq 1 $VEHICLE_COUNT); do
    idx=$((($i - 1) % 5))
    VID="${VEHICLE_TYPES[$idx]}-demo-$(printf '%02d' $i)"
    ENV="${ENVS[$idx]}"
    echo "→ Starting vehicle: $VID ($ENV)"
    go run ./cmd/testsender -vid "$VID" -env "$ENV" -rate 10 &
    sleep 0.2
done

echo ""
echo "════════════════════════════════════════════════════════════"
echo "  Demo running!"
echo ""
echo "  WebSocket:  ws://localhost:9000"
echo "  Health:     http://localhost:9000/healthz"
echo "  Metrics:    http://localhost:9000/metrics"
echo ""
echo "  Connect a client:"
echo "    go run ./cmd/testclient"
echo ""
echo "  Press Ctrl+C to stop"
echo "════════════════════════════════════════════════════════════"
echo ""

# Wait for server
wait $SERVER_PID
