# cam-proxy

[![CI](https://github.com/smford/cam-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/smford/cam-proxy/actions/workflows/ci.yml)
[![Release](https://github.com/smford/cam-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/smford/cam-proxy/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**An ultra-lightweight, zero-stress edge gateway for IP security cameras.**

Have you ever tried displaying snapshots from your IP cameras on a Home Assistant wall tablet, MagicMirror, or web dashboard—only to have the camera freeze, disconnect, or crash under the connection load? Or spent hours wrestling with complex ONVIF SOAP protocols and RTSP credentials just to pan a camera or receive a motion alert?

`cam-proxy` solves this. It acts as an intelligent, featherweight (< 20MB RAM) buffer between your cameras and your smart home or custom applications. Instead of running heavy 24/7 video decoders or exposing fragile camera hardware to multiple concurrent clients, `cam-proxy` gives you simple HTTP snapshot URLs, clean REST PTZ controls, and automatic MQTT motion alerts—without burning CPU or overloading your cameras.

---

### 💡 What Problems Does `cam-proxy` Solve?

- **Camera Freezes & RTSP Socket Flooding**: Budget IP cameras (Tapo, Reolink, Hikvision, Dahua, Amcrest) have limited microcontrollers. When a dashboard refreshes, multiple tablets load simultaneously, or an automation requests snapshots, the camera's RTSP connection crashes. `cam-proxy` uses **single-flight request coalescing** and an **in-memory TTL frame cache**: 20 simultaneous snapshot requests hit the camera as a *single* connection.
- **Resource Exhaustion on Low-Power Edge Hardware**: Running a full NVR (like Frigate or Blue Iris) requires gigabytes of RAM and constant CPU/GPU decoding just to grab periodic pictures. `cam-proxy` idles at **< 15MB RAM** and **0% CPU**, connecting to the camera *only* when a snapshot or PTZ action is requested.
- **Complex Protocols (ONVIF SOAP & Digest Auth)**: Controlling a PTZ camera or pulling an ONVIF snapshot normally requires crafting complex XML SOAP envelopes with WS-Security timestamps and HTTP Digest handshakes. `cam-proxy` translates all of this into standard, browser-friendly JSON REST endpoints: `GET /snapshot` and `POST /ptz`.
- **Cloud-Free Camera Alerts in Home Assistant**: Subscribes directly to local camera event queues (motion detection, tampering, line crossing) and publishes them to MQTT with **Home Assistant MQTT Auto-Discovery**—no cloud subscriptions, vendor apps, or flaky webhooks required.
- **Web Dashboard & Browser Security**: Browsers cannot connect directly to camera RTSP streams or digest-authenticated HTTP endpoints due to CORS restrictions. `cam-proxy` provides configurable CORS middleware and standard security headers (`X-Content-Type-Options: nosniff`) out of the box.

---

## Why `cam-proxy`? (Comparison with Alternatives)

If you already use tools like **Frigate**, **go2rtc**, or **ffmpeg scripts**, where does `cam-proxy` fit?

| Feature / Capability | `cam-proxy` | Full NVRs (Frigate, Blue Iris, Shinobi) | Streaming Servers (go2rtc, MediaMTX) | DIY Scripts (`ffmpeg` / `curl` cron) |
| :--- | :--- | :--- | :--- | :--- |
| **Primary Focus** | **Lightweight Snapshots, PTZ & MQTT Events** | 24/7 Video Recording & AI Detection | Low-Latency Live Video Streaming | Ad-hoc Frame Grabbing |
| **Idle Memory (RAM)** | **< 15–20 MB** | 500 MB – 4+ GB | 50 MB – 200 MB | Transient (spikes per execution) |
| **Idle CPU Usage** | **~0%** (no continuous decoding) | High (constant decoding/AI) | Low–Moderate | 0% until script triggers |
| **Hardware Strain Protection** | **Yes** (Single-flight coalescing + cache) | None (pulls continuous 24/7 stream) | None (passes connections through) | **No** (concurrent runs crash camera) |
| **ONVIF PTZ REST API** | **Yes** (Simple JSON `POST /ptz`) | Rare / Complex integration | Limited / None | Manual SOAP XML scripting |
| **Camera Event Bridge** | **Yes** (ONVIF PullPoint $\rightarrow$ MQTT) | Via internal events | RTSP metadata only | Not supported |
| **Home Assistant Discovery** | **Zero-Config** (MQTT Auto-Discovery) | Via custom integration | Via go2rtc integration | Manual YAML configuration |
| **CORS & Web Browser Ready** | **Yes** (Built-in CORS middleware) | Usually behind reverse proxy | WebRTC / WS focused | Requires reverse proxy |
| **Dependency Footprint** | **Zero** (Single static binary, CGO-free) | Heavy (Python, OpenCV, TensorRT) | Moderate | Requires `ffmpeg`, `bash`, `curl` |

### When should you use `cam-proxy`?
- **You want snapshots on dashboards & smart displays**: Wall tablets, Home Assistant Lovelace cards, MagicMirrors, e-ink picture frames, or smart watch notifications.
- **You want camera events without cloud lock-in**: Instant motion, tamper, and line-crossing alerts pushed to Home Assistant or Node-RED via MQTT.
- **You run on low-power hardware**: Raspberry Pi Zero/3/4/5, thin clients, NAS devices, or edge routers where every megabyte of RAM matters.
- **You want to avoid camera crashes**: Your camera drops offline when multiple devices request snapshots simultaneously.

### When should you use something else?
- **Continuous 24/7 video recording**: If you need to store weeks of continuous video footage on hard drives, use a dedicated NVR like Frigate, Scrypted, or Blue Iris (you can still run `cam-proxy` alongside them for lightweight dashboard snapshots!).
- **Low-latency live video streaming**: If you want to stream 30fps live video in a browser over WebRTC, tools like `go2rtc` or `MediaMTX` are purpose-built for video re-streaming.

---

## Installation

Choose your preferred installation method:

### 1. Homebrew (macOS & Linux)
Install directly from the official [smford/homebrew-tap](https://github.com/smford/homebrew-tap):
```bash
brew tap smford/tap
brew install cam-proxy
```

### 2. Raspberry Pi & Linux Edge (ARM64 / ARMv7)

`cam-proxy` is optimized for edge hardware (Raspberry Pi 3/4/5 and Zero 2 W) running Raspberry Pi OS (64-bit `arm64` or 32-bit `armv7l`), idling at < 15MB RAM and 0% CPU.

#### Option A: Pre-Compiled Binary & Systemd (Recommended)
Download and install the latest `linux/arm64` release directly onto your Raspberry Pi:

```bash
# 1. Fetch latest release archive for ARM64
LATEST=$(curl -s https://api.github.com/repos/smford/cam-proxy/releases/latest | grep "tag_name" | cut -d '"' -f 4)
curl -sSL "https://github.com/smford/cam-proxy/releases/download/${LATEST}/cam-proxy_${LATEST#v}_linux_arm64.tar.gz" | sudo tar -xz -C /usr/local/bin cam-proxy

# 2. Set up configuration directory and template
sudo mkdir -p /etc/cam-proxy
sudo curl -sSL https://raw.githubusercontent.com/smford/cam-proxy/main/config.example.yaml -o /etc/cam-proxy/cam-proxy.yaml

# 3. Discover local cameras on your network
cam-proxy --scan --generate-config | sudo tee -a /etc/cam-proxy/cam-proxy.yaml

# 4. Install and enable the hardened production systemd service
sudo curl -sSL https://raw.githubusercontent.com/smford/cam-proxy/main/systemd/cam-proxy.service -o /etc/systemd/system/cam-proxy.service
sudo systemctl daemon-reload
sudo systemctl enable --now cam-proxy

# 5. Verify service status
sudo systemctl status cam-proxy
```

#### Option B: Docker on Raspberry Pi
Run the official multi-arch container (`linux/arm64`):

```bash
docker run -d \
  --name cam-proxy \
  --restart unless-stopped \
  --network host \
  -v /etc/cam-proxy/cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro \
  ghcr.io/smford/cam-proxy:latest
```

#### Option C: Cross-Compile from Development Machine
If developing on macOS or Linux x86_64, build the static ARM64 binary locally and deploy over SSH:

```bash
# Build static ARM64 binary locally
make build-linux-arm64

# Copy binary to Raspberry Pi
scp cam-proxy-linux-arm64 pi@raspberrypi.local:/tmp/cam-proxy
ssh pi@raspberrypi.local "sudo install -m 755 /tmp/cam-proxy /usr/local/bin/cam-proxy && rm /tmp/cam-proxy"
```

### 3. Docker Container
Official multi-architecture images (`linux/amd64`, `linux/arm64`) with `ffmpeg` and `ca-certificates` pre-installed:
```bash
docker run -d \
  --name cam-proxy \
  --restart unless-stopped \
  --network host \
  -v $(pwd)/cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro \
  ghcr.io/smford/cam-proxy:latest
```

### 4. Pre-Built Static Binaries
Download pre-compiled, zero-dependency static binaries (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`) with SHA256 checksums from the **[GitHub Releases](https://github.com/smford/cam-proxy/releases)** page.

### 5. Build from Source
Requires Go 1.22+:
```bash
git clone https://github.com/smford/cam-proxy.git
cd cam-proxy
make build
```

---

## Quick Start in 60 Seconds

1. **Discover Cameras on Local Network**:
   ```bash
   ./cam-proxy --scan --generate-config >> cam-proxy.yaml
   ```
2. **Configure Credentials & Settings**:
   Edit `cam-proxy.yaml` with your camera usernames/passwords, or launch the interactive terminal manager:
   ```bash
   ./cam-proxy --tui
   ```
3. **Start the Edge Daemon**:
   ```bash
   ./cam-proxy -config cam-proxy.yaml
   ```
   Live snapshots are immediately available at `http://localhost:8080/api/v1/cameras/<id>/snapshot`!

---

## Architecture

`cam-proxy` is engineered as a zero-CGO edge gateway running on minimal resources (~10–15 MB RAM, ~0% idle CPU) while protecting camera hardware from socket flooding via single-flight request coalescing and an in-memory frame cache.

For the complete architectural breakdown, data flow diagrams, hardware protection lifecycle, and protocol engine internals, see **[docs/architecture.md](docs/architecture.md)**.

---

## Network Camera Discovery (`--scan`)

`cam-proxy` can automatically scan your local network interfaces for ONVIF cameras (via WS-Discovery multicast `239.255.255.250:3702`) and active RTSP video streams (port 554/8554 sweep):

```bash
# Scan local network
./cam-proxy --scan

# Scan with custom timeout
./cam-proxy --scan --scan-timeout 2s

# Scan and generate ready-to-use YAML configuration block
./cam-proxy --scan --generate-config >> cam-proxy.yaml
```

**Example Output:**
```text
Scanning local network for ONVIF and RTSP cameras (timeout: 3s)...

IP ADDRESS       TYPE       MANUFACTURER   MODEL / NAME           RTSP / ONVIF ENDPOINT
------------------------------------------------------------------------------------------------
192.168.1.12     ONVIF+RTSP -              C216                   rtsp://192.168.1.12:554/live
192.168.1.113    ONVIF      -              C720                   http://192.168.1.113:2020/onvif...
------------------------------------------------------------------------------------------------
Discovered 2 camera(s) on local network.
```

---

## Interactive Camera Manager (TUI)

Launch a full terminal user interface to browse detected cameras, inspect live details, configure credentials, test connections, and save changes:

```bash
# Launch interactive TUI
./cam-proxy --tui

# Or via make
make tui
```

```text
 CAM-PROXY CAMERA MANAGER   Config: cam-proxy.yaml

  [1. Configured (2)]   2. Discovered (2)   [Tab] switch view

    CAMERA ID            NAME                     ONVIF ADDRESS          SNAPSHOT
    ────────────────────────────────────────────────────────────────────────────────
  ▶ front_door           Front Porch Tapo C216    192.168.1.12:2020      auto
    driveway             Driveway PTZ             192.168.1.113:2020     auto


  ● Press 's' to scan network, 'a' to add, 'e' to edit, 'w' to save, 'q' to quit
  [s] Scan LAN  [a] Add  [e/Enter] Edit  [d] Delete  [t] Test  [w] Write File  [q] Quit
```

### TUI Keybindings:
- **`[Tab]`**: Switch between **Configured** and **Discovered** cameras.
- **`[s]`**: Broadcast network WS-Discovery & RTSP sweep to find nearby cameras.
- **`[a]`**: Add camera (pre-populates with highlighted camera details when on Discovered tab).
- **`[e]` / `[Enter]`**: Edit highlighted camera credentials, endpoints, and event options.
- **`[d]`**: Delete selected camera from configuration.
- **`[t]`**: Test live camera connectivity and snapshot capture.
- **`[w]`**: Save/write updated configuration directly to `cam-proxy.yaml`.
- **`[q]` / `[Ctrl+C]`**: Quit TUI.

---

## Quick Start

### 1. Build
```bash
make build
```

Cross-compile for edge gateways (e.g., Raspberry Pi 4/5):
```bash
make build-linux-arm64
```

### 2. Auto-Discover or Configure
Auto-generate configuration from discovered cameras:
```bash
./cam-proxy --scan --generate-config > cam-proxy.yaml
```
Or copy [config.example.yaml](file:///Users/asc/git/cam-proxy/config.example.yaml):
```bash
cp config.example.yaml cam-proxy.yaml
```

### 3. Run
```bash
./cam-proxy -config cam-proxy.yaml -log-level debug
```

---

## API Reference & Examples (`curl` & `wget`)

All endpoints are available over HTTP on port `8080` (or your configured port).

### 1. Health & Per-Camera Operational Status (`GET /healthz` & `GET /api/v1/status`)
Inspect gateway health, process uptime, total camera count, and per-camera operational metrics (connection state `online`, `last_seen` timestamp, `last_snapshot_latency_ms`, and `last_error`).

- **curl**:
  ```bash
  curl -s http://localhost:8080/healthz | jq
  # Or via REST status endpoint:
  curl -s http://localhost:8080/api/v1/status | jq
  ```
- **wget**:
  ```bash
  wget -q -O- http://localhost:8080/healthz
  ```
- **Response**:
  ```json
  {
    "status": "ok",
    "uptime": "15m42s",
    "camera_count": 2,
    "cameras": {
      "front_door": {
        "id": "front_door",
        "online": true,
        "last_seen": "2026-10-03T17:15:30Z",
        "last_snapshot_latency_ms": 45,
        "last_error": ""
      },
      "backyard": {
        "id": "backyard",
        "online": false,
        "last_seen": null,
        "last_snapshot_latency_ms": 0,
        "last_error": "dial tcp 192.168.1.102:80: connect: connection refused"
      }
    }
  }
  ```

---

### 2. List Configured Cameras (`GET /api/v1/cameras`)
Retrieve metadata, ONVIF addresses, and configuration flags for all cameras.

- **curl**:
  ```bash
  curl -s http://localhost:8080/api/v1/cameras | jq
  ```
- **wget**:
  ```bash
  wget -q -O- http://localhost:8080/api/v1/cameras
  ```
- **Response**:
  ```json
  [
    {
      "id": "camera1",
      "name": "Front Porch C216",
      "address": "192.168.1.160:2020",
      "snapshot_method": "auto",
      "events_enabled": false
    },
    {
      "id": "tort1",
      "name": "Tortoise Terrarium",
      "address": "192.168.1.12:2020",
      "snapshot_method": "auto",
      "events_enabled": false
    }
  ]
  ```

---

### 3. Capture On-Demand Snapshot (`GET /api/v1/cameras/{id}/snapshot`)
Grabs a fresh keyframe on demand and serves it directly as a standard binary JPEG (`image/jpeg`).

- **curl**:
  ```bash
  # Download to snapshot.jpg
  curl -s -o snapshot.jpg http://localhost:8080/api/v1/cameras/camera1/snapshot

  # Save with timestamp in filename
  curl -s -o "camera1_$(date +%Y%m%d_%H%M%S).jpg" http://localhost:8080/api/v1/cameras/camera1/snapshot
  ```
- **wget**:
  ```bash
  # Download to snapshot.jpg
  wget -O snapshot.jpg http://localhost:8080/api/v1/cameras/camera1/snapshot

  # Quiet download with timestamp in filename
  wget -q -O "camera1_$(date +%Y%m%d_%H%M%S).jpg" http://localhost:8080/api/v1/cameras/camera1/snapshot
  ```

---

### 4. PTZ Control (`POST /api/v1/cameras/{id}/ptz`)
Send continuous velocity move vectors, halts, or preset recalls to motorized ONVIF Profile S cameras.

#### A. Continuous Move (Pan, Tilt, Zoom)
Velocity coordinates range from `-1.0` to `1.0`:
- `pan`: `-1.0` (max speed left) to `1.0` (max speed right)
- `tilt`: `-1.0` (max speed down) to `1.0` (max speed up)
- `zoom`: `-1.0` (zoom out) to `1.0` (zoom in)

**Move Left and Up:**
- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"move","pan":-0.5,"tilt":0.5}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"move","pan":-0.5,"tilt":0.5}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

**Pan Right:**
- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"move","pan":0.5,"tilt":0.0}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"move","pan":0.5,"tilt":0.0}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

**Tilt Down:**
- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"move","pan":0.0,"tilt":-0.5}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"move","pan":0.0,"tilt":-0.5}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

**Zoom In:**
- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"move","zoom":0.5}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"move","zoom":0.5}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

#### B. Stop Motion
Halts ongoing pan, tilt, or zoom motors immediately.

- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"stop"}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"stop"}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

#### C. Recall Preset Position
Moves the camera to a saved ONVIF viewpoint / preset token.

- **curl**:
  ```bash
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"preset","preset_token":"1"}'
  ```
- **wget**:
  ```bash
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"preset","preset_token":"1"}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

---

### 5. Prometheus Observability Metrics (`GET /metrics`)
Expose production metrics for Prometheus scraping (snapshot counts by source and status, latency histogram, camera online status gauge, and MQTT event counters):

- **curl**:
  ```bash
  curl -s http://localhost:8080/metrics
  ```
- **wget**:
  ```bash
  wget -q -O- http://localhost:8080/metrics
  ```
- **Key Metrics Exposed**:
  - `cam_proxy_snapshots_total{camera_id, status, source}`: Counters of snapshot requests served from `cache` vs fetched from `hardware`, with `success` or `error` status.
  - `cam_proxy_snapshot_duration_seconds`: Histogram measuring snapshot fetch latency from camera hardware.
  - `cam_proxy_camera_online{camera_id}`: Gauge tracking camera reachability (1 = online, 0 = offline).
  - `cam_proxy_mqtt_events_total{camera_id, status}`: Counters of ONVIF PullPoint events processed for MQTT (`published` or `error`).

---

### 6. OpenAPI 3.1.0 Specification (`GET /openapi.yaml`)
Retrieve the complete machine-readable OpenAPI specification for client generation, API testing, or importing into Swagger UI, Postman, or OWASP ZAP:

- **curl**:
  ```bash
  curl -s http://localhost:8080/openapi.yaml
  ```
- **wget**:
  ```bash
  wget -q -O- http://localhost:8080/openapi.yaml
  ```
- Spec file is also accessible directly in the repository at [`api/openapi.yaml`](api/openapi.yaml).

---

### 7. HTTP Security Headers & CORS Middleware

The HTTP API includes built-in security and CORS middleware out of the box:

- **Configurable CORS Support**: Enables web frontends, Home Assistant Lovelace cards, and browser extensions to fetch dynamic snapshots (`GET`) and issue PTZ control commands (`POST`) directly from browser contexts.
  - Automatically handles preflight `OPTIONS` requests with `204 No Content`.
  - Configurable allowed origins (`*` by default or custom origin list), methods, allowed headers, credentials, and preflight max age.
- **MIME Sniffing Protection**: Sets `X-Content-Type-Options: nosniff` across all API responses.
- **Dynamic Image Cache Prevention**: Enforces `Cache-Control: no-cache, no-store, must-revalidate`, `Pragma: no-cache`, and `Expires: 0` headers on all camera snapshot endpoints to prevent stale image caching in client browsers and intermediate forward proxies.

---

### 8. Shell Scripting & Automation Examples

#### Nudge Camera: Move for 1 Second, Then Stop
- **With curl**:
  ```bash
  # Start moving right
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"move","pan":0.5}'
  
  # Wait 1 second
  sleep 1
  
  # Stop moving
  curl -s -X POST http://localhost:8080/api/v1/cameras/camera1/ptz \
    -H "Content-Type: application/json" \
    -d '{"action":"stop"}'
  ```

- **With wget**:
  ```bash
  # Start moving right
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"move","pan":0.5}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  
  # Wait 1 second
  sleep 1
  
  # Stop moving
  wget --header="Content-Type: application/json" \
    --post-data='{"action":"stop"}' \
    -q -O- http://localhost:8080/api/v1/cameras/camera1/ptz
  ```

#### Periodic Snapshot Timelapse Loop (every 10 seconds)
```bash
while true; do
  curl -s -o "frame_$(date +%Y%m%d_%H%M%S).jpg" http://localhost:8080/api/v1/cameras/camera1/snapshot
  sleep 10
done
```

---

## MQTT Event Bridge

When `pull_events: true` is enabled on a camera, `cam-proxy` subscribes to the camera's ONVIF PullPoint notification queue. Detected alerts are normalized and published to:

`<topic_prefix>/<camera_id>/<event_type>` (e.g. `cam-proxy/events/front_door/motion`)

Payload format:
```json
{
  "camera_id": "front_door",
  "type": "motion",
  "state": true,
  "raw_topic": "tns1:RuleEngine/CellMotionDetector/Motion",
  "timestamp": "2026-10-01T22:45:00.123Z"
}
```

### Home Assistant MQTT Auto-Discovery

When MQTT and discovery are enabled (`discovery: true`, enabled by default), `cam-proxy` automatically publishes Home Assistant MQTT discovery payloads on startup for each camera with `pull_events: true`:

- **Discovery Topics**: `<discovery_prefix>/binary_sensor/cam_proxy_<camera_id>_<event_type>/config` (e.g., `homeassistant/binary_sensor/cam_proxy_front_door_motion/config`)
- **Automatic Binary Sensors**:
  - **Motion Detection** (`motion`): `device_class: "motion"`
  - **Tamper Alert** (`tamper`): `device_class: "tamper"`
  - **Line Crossing** (`line_cross`): `device_class: "motion"`, `icon: "mdi:vector-line"`
- **Device Linking & Topology**: Entities are grouped under their respective camera device cards with hardware metadata (`manufacturer`, `model`) and linked to `cam-proxy` via `via_device: "cam-proxy"`.
- **Zero Configuration**: Discovery messages are published with `retained: true` and QoS 1, ensuring Home Assistant automatically discovers or restores sensor states upon startup or broker reconnection.


---

## Production Deployment

### 1. Hardened Systemd Service (Linux Edge & Server)

For production Linux installations, `cam-proxy` provides a hardened Systemd unit template at [`systemd/cam-proxy.service`](systemd/cam-proxy.service) equipped with strict security sandboxing:

- **Security Isolation**: `DynamicUser=yes` dynamically provisions an unprivileged, ephemeral service user and group.
- **Read-Only Filesystem**: `ProtectSystem=strict` and `ProtectHome=yes` lock the filesystem down read-only, preventing unauthorized modifications.
- **Attack Surface Minimization**: `CapabilityBoundingSet=` drops all Linux root capabilities, while `MemoryDenyWriteExecute=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, and `ProtectKernelTunables=yes` block privilege escalation vectors.
- **Automatic Recovery**: `Restart=always` and `RestartSec=5s` guarantee immediate daemon recovery across camera or network faults.

**Installation Steps:**
```bash
# 1. Copy the compiled binary into standard path
sudo cp cam-proxy /usr/local/bin/

# 2. Set up the configuration directory
sudo mkdir -p /etc/cam-proxy
sudo cp config.example.yaml /etc/cam-proxy/cam-proxy.yaml

# 3. Install and activate the systemd unit
sudo cp systemd/cam-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cam-proxy

# 4. Check service status and logs
sudo systemctl status cam-proxy
journalctl -u cam-proxy -f
```

---

### 2. Docker Compose

A ready-to-use [`docker-compose.yml`](docker-compose.yml) is included in the repository:

```yaml
services:
  cam-proxy:
    image: ghcr.io/smford/cam-proxy:latest
    container_name: cam-proxy
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro
    environment:
      - TZ=UTC
```

Start the service:
```bash
docker compose up -d
```

> [!NOTE]
> **Why `network_mode: host`?**
> Local camera discovery (`--scan`) relies on ONVIF WS-Discovery (multicast UDP `239.255.255.250:3702`). Docker bridge networks do not forward UDP multicast broadcasts across the host's physical network adapter. If you do not need auto-discovery and configure cameras directly by IP, you can use standard port mapping instead (`-p 8080:8080`).

---

### 3. Build Docker Image Locally

```bash
# Using Make
make docker-build

# Or using Docker directly
docker build -t cam-proxy:latest .
```

---

## Releases & SemVer Versioning

Releases follow standard Semantic Versioning (`vMAJOR.MINOR.PATCH`).

### Creating a Release (Manual)
Use the built-in Makefile targets to automatically tag and initiate GitHub releases:
```bash
# Automatically bump patch and publish (e.g. v0.3.0 -> v0.3.1)
make release-patch

# Or bump minor and publish (e.g. v0.3.0 -> v0.4.0)
make release-minor

# Or release an explicit version tag
make release TAG=v0.4.0
```

---

## Static Application Security Testing (SAST)

`cam-proxy` incorporates a dual-layer Static Application Security Testing (SAST) strategy automated via [`.github/workflows/sast.yml`](.github/workflows/sast.yml) running on every push to `main`, every pull request, and a weekly scheduled scan (Mondays at 07:00 UTC):

| SAST Tool | Role | Strengths | Trigger / Output |
| :--- | :--- | :--- | :--- |
| **GitHub CodeQL** | Semantic Taint Analysis | Deep data-flow and inter-procedural taint analysis across Go packages (CWEs, untrusted inputs reaching network/exec sinks) | Analyzes Go source with `security-extended` query suite; outputs SARIF to GitHub Code Scanning |
| **Semgrep OSS** | Rapid Pattern Checks & Linting | Fast AST-based pattern matching and rule enforcement across Go source, Dockerfile, and GitHub Actions workflows | Runs `semgrep scan --config auto`; generates SARIF and uploads alerts to GitHub Code Scanning |

### Local Security Scanning
Run Semgrep OSS locally using the Makefile:
```bash
make sast
```
Or directly using `uvx`:
```bash
uvx semgrep scan --config auto
```

---

## Dynamic Application Security Testing (DAST)

`cam-proxy` runs automated Dynamic Application Security Testing (DAST) via [`.github/workflows/dast.yml`](.github/workflows/dast.yml) on push to `main`, pull requests, weekly scheduled runs (Mondays at 08:00 UTC), or manual `workflow_dispatch`:

1. **Service Container Spin-Up**:
   - Compiles and packages `cam-proxy` into a test container image (`cam-proxy:dast`).
   - Launches `cam-proxy` as a background service container mounted with a lightweight test configuration ([`.zap/dast-config.yaml`](.zap/dast-config.yaml)).
   - Polls `GET /healthz` until the HTTP daemon is fully initialized and reporting healthy status.

2. **OWASP ZAP Baseline Scan**:
   - Executes the official [`zaproxy/action-baseline`](https://github.com/zaproxy/action-baseline) container targeting the running `cam-proxy` daemon at `http://localhost:8080`.
   - Actively spiders API endpoints (`/healthz`, `/api/v1/cameras`, `/api/v1/cameras/{id}/snapshot`, `/api/v1/cameras/{id}/ptz`) and passively evaluates HTTP responses against OWASP Top 10 vulnerabilities.
   - Applies API-tailored rule overrides defined in [`.zap/rules.tsv`](.zap/rules.tsv).
   - Generates comprehensive HTML and Markdown security audit reports stored as GitHub Actions artifacts (`zap-baseline-report`).

### Local DAST Scanning
Spin up `cam-proxy` and run the OWASP ZAP Baseline scan locally:
```bash
make dast
```
