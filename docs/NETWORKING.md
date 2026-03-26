# Networking for OpenC2

> A practical guide to understanding how OpenC2 communicates with vehicles.  
> No networking background required.

---

## The Big Picture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        FIELD NETWORK (same subnet)                      │
│                                                                         │
│   ┌─────────────────┐         ┌─────────────────┐         ┌──────────┐  │
│   │  Ground Station │         │   Robot (UGV)   │         │  Robot   │  │
│   │  (Gateway + UI) │         │                 │         │  (USV)   │  │
│   │                 │         │                 │         │          │  │
│   │  eth0:          │         │  eth0:          │         │  wlan0:  │  │
│   │  192.168.1.10   │         │  192.168.1.50   │         │  192.168.│  │
│   │                 │         │                 │         │   1.51   │  │
│   └────────┬────────┘         └────────┬────────┘         └────┬─────┘  │
│            │                           │                       │        │
│            └───────────────────────────┴───────────────────────┘        │
│                              Switch / WiFi AP                           │
└─────────────────────────────────────────────────────────────────────────┘
```

**Key insight:** Every device has a regular IP address (`192.168.1.x`), but OpenC2 uses **multicast** — a special addressing scheme where messages go to a *group*, not a specific device.

---

## What is Multicast?

Think of it like radio channels:

| Concept | Radio Analogy | Network Equivalent |
|---------|---------------|-------------------|
| **Unicast** | Phone call (one-to-one) | Send to `192.168.1.50` |
| **Broadcast** | PA system (one-to-all) | Send to `192.168.1.255` |
| **Multicast** | Radio station (one-to-subscribers) | Send to `239.255.0.1` |

With multicast:
- Senders publish to a **group address** (e.g., `239.255.0.1`)
- Receivers **subscribe** to groups they care about
- Network hardware delivers packets only to subscribers

---

## How OpenC2 Uses Multicast

```
                    TELEMETRY FLOW (Vehicle → Gateway)
                    
    UGV                                              Gateway
    ┌─────────┐                                     ┌─────────┐
    │ Sends   │──── UDP to 239.255.0.1:14550 ──────▶│ Joined  │
    │ to group│                                     │ group   │
    └─────────┘                                     └─────────┘
                                                         │
    USV                                                  │
    ┌─────────┐                                          │
    │ Sends   │──── UDP to 239.255.0.1:14550 ────────────┘
    │ to group│         (same group)
    └─────────┘


                    COMMAND FLOW (Gateway → Vehicles)
                    
    Gateway                                        UGV
    ┌─────────┐                                   ┌─────────┐
    │ Sends   │──── UDP to 239.255.0.2:14551 ────▶│ Joined  │
    │ to group│                                   │ group   │
    └─────────┘                                   └─────────┘
         │                                              
         │                                         USV
         │                                        ┌─────────┐
         └───────────────────────────────────────▶│ Joined  │
                      (all vehicles receive)      │ group   │
                                                  └─────────┘
```

| Direction | Group Address | Port | Who Joins | Who Sends |
|-----------|---------------|------|-----------|-----------|
| Vehicle → Gateway | `239.255.0.1` | `14550` | Gateway | Vehicles |
| Gateway → Vehicles | `239.255.0.2` | `14551` | Vehicles | Gateway |

---

## Why Multicast Addresses Look Weird

Multicast addresses are **not device addresses** — they're group identifiers.

The range `224.0.0.0` to `239.255.255.255` is reserved at the protocol level (IPv4, RFC 1112). When network hardware sees a destination in this range, it knows to:

1. Use IGMP (Internet Group Management Protocol) to track subscribers
2. Replicate packets to all subscribers
3. Map to a special Ethernet MAC address (`01:00:5e:xx:xx:xx`)

**You cannot assign a multicast address to a device.** Trying to set your NIC to `239.255.0.1` will fail.

---

## Practical Setup

### What You Need

| Requirement | Notes |
|-------------|-------|
| Same subnet | All devices on `192.168.1.x/24` (or whatever your network uses) |
| Layer 2 connectivity | All devices reachable at the switch/WiFi level |
| UDP ports open | `14550` and `14551` not blocked by firewalls |
| Multicast-capable NICs | All modern network adapters support this |

### Ground Station Setup

```bash
# Start the gateway (joins multicast group automatically)
go run ./cmd/gateway

# Or with explicit sources
OPENC2_MCAST_SOURCES="239.255.0.1:14550" ./gateway
```

The gateway calls `IP_ADD_MEMBERSHIP` under the hood — this tells your NIC "deliver packets for `239.255.0.1` to me."

### Robot Setup (Firmware Side)

Your vehicle firmware needs to:

```python
# Python example (robot side)
import socket
import struct

# --- SENDING TELEMETRY ---
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
# Send to multicast group (NOT the gateway's IP!)
sock.sendto(telemetry_bytes, ("239.255.0.1", 14550))

# --- RECEIVING COMMANDS ---
cmd_sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
cmd_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
cmd_sock.bind(("", 14551))

# Join multicast group
mreq = struct.pack("4sl", socket.inet_aton("239.255.0.2"), socket.INADDR_ANY)
cmd_sock.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)

# Now cmd_sock.recv() will get commands from gateway
```

---

## Testing Locally (No Hardware)

OpenC2 includes a test sender that simulates vehicles on your development machine:

```bash
# Terminal 1: Start gateway
go run ./cmd/gateway

# Terminal 2: Simulate a vehicle
go run ./cmd/testsender -vid ugv-husky-01

# Terminal 3: Simulate another vehicle
go run ./cmd/testsender -vid usv-blueboat-02 -env marine
```

This works because multicast loopback is enabled — your machine receives its own multicast packets.

---

## Multiple Multicast Groups

If different vehicle types use different groups (e.g., UGVs on `239.255.0.1`, USVs on `239.255.1.1`):

```bash
# Gateway joins both groups
OPENC2_MCAST_SOURCES="239.255.0.1:14550:ugv,239.255.1.1:14550:usv" ./gateway
```

Each robot broadcasts to its designated group. The gateway listens to all.

---

## Common Problems

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| Gateway doesn't see packets | Firewall blocking UDP | `sudo ufw allow 14550/udp` |
| Works on laptop, fails on Pi | Multiple NICs, joined wrong one | The gateway joins on all interfaces by default |
| WiFi doesn't work | AP blocks multicast | Enable multicast forwarding on access point |
| Works locally, not across machines | Different subnets | Move to same subnet, or configure multicast routing |
| Intermittent packet loss | WiFi multicast rate limiting | Some APs throttle multicast; use wired or tune AP settings |

### Debugging with tcpdump

```bash
# See multicast traffic on the telemetry group
sudo tcpdump -i any host 239.255.0.1 and udp port 14550

# See all multicast traffic
sudo tcpdump -i any 'ip multicast'
```

---

## Comparison: Multicast vs Alternatives

### Option 1: Unicast (Point-to-Point)

```
    Vehicle A ────────────▶ Gateway
    Vehicle B ────────────▶ Gateway  
    Vehicle C ────────────▶ Gateway
```

| Pros | Cons |
|------|------|
| Simple to understand | Gateway IP must be known/configured on each vehicle |
| Works across subnets | Adding vehicles requires config on vehicle side |
| Firewall-friendly | Commands must be sent to each vehicle individually |

**When to use:** Internet-connected fleets, NAT traversal needed, or multicast-hostile networks.

### Option 2: Broadcast

```
    Vehicle A ────┐
    Vehicle B ────┼───▶ 192.168.1.255 (everyone on subnet)
    Vehicle C ────┘
```

| Pros | Cons |
|------|------|
| Zero config | ALL devices receive ALL packets (wasteful) |
| Simple | Doesn't cross subnets |
| | Scales poorly (network flooded) |

**When to use:** Rarely. Multicast is almost always better.

### Option 3: Multicast (OpenC2's Choice)

```
    Vehicle A ────┐
    Vehicle B ────┼───▶ 239.255.0.1 (only subscribers receive)
    Vehicle C ────┘
              Gateway subscribes
```

| Pros | Cons |
|------|------|
| Zero-config discovery | Doesn't route across subnets by default |
| Efficient (only subscribers get packets) | Some WiFi APs handle it poorly |
| Scales well | Slightly harder to debug |
| Commands reach all vehicles with one send | |

**When to use:** Field deployments on a local network (exactly what OpenC2 targets).

### Option 4: Message Broker (MQTT, ROS2 DDS)

```
    Vehicle A ────┐            ┌───▶ Gateway
    Vehicle B ────┼───▶ Broker ┼───▶ Logger
    Vehicle C ────┘            └───▶ Cloud
```

| Pros | Cons |
|------|------|
| Decoupled pub/sub | Extra infrastructure (broker) |
| Works across networks | Latency from broker hop |
| Persistence, QoS, auth | Complexity |
| Multiple consumers easy | |

**When to use:** Complex multi-system architectures, cloud integration, long-term data logging.

---

## Summary

| Concept | OpenC2 Value |
|---------|--------------|
| Telemetry group | `239.255.0.1:14550` |
| Command group | `239.255.0.2:14551` |
| Device IPs | Normal unicast (e.g., `192.168.1.x`) — not directly used by protocol |
| Transport | UDP (fast, tolerates loss) |
| Requirement | All devices on same Layer 2 network |

**The key mental model:** Vehicles don't send to the gateway's IP. They send to a *group*, and the gateway subscribes to that group. This is why you don't need to configure gateway addresses on robots — they just broadcast to the well-known group.
