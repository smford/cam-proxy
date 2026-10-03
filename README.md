# cam-proxy

`cam-proxy` is a lightweight (< 20MB RSS), single-binary edge daemon written in pure Go. It bridges IP cameras to modern edge computing environments by providing:

1. **On-Demand Snapshots**: Connects to the camera on-demand (via ONVIF HTTP or RTSP), grabs a frame, converts it to JPEG in memory, and serves it over HTTP. Zero 24/7 decoding overhead.
2. **PTZ Control**: Exposes simple JSON REST endpoints for continuous velocity moves, absolute stops, and preset recalls via ONVIF SOAP.
3. **ONVIF Event-to-MQTT Bridge**: Subscribes to camera PullPoint notification queues (motion, tamper, line crossing), normalizes states, and publishes clean JSON events to MQTT.
4. **Zero Heavy Dependencies**: Pure Go compilation (`CGO_ENABLED=0`) with built-in H.264 snapshot extraction via external decoder or pure Go MJPEG.

---

## Architecture Overview

```mermaid
flowchart TD
    subgraph Clients["Clients"]
        HTTPClients["HTTP Clients / Frontends"]
    end

    subgraph Daemon["cam-proxy daemon"]
        subgraph API["Go 1.22+ net/http API"]
            SnapshotEP["GET /api/v1/cameras/{id}/snapshot"]
            PTZEP["POST /api/v1/cameras/{id}/ptz"]
            HealthzEP["GET /healthz & /api/v1/status"]
            MetricsEP["GET /metrics"]
            OpenAPIEP["GET /openapi.yaml"]
        end

        Manager["Camera Manager"]

        subgraph Protocols["Protocol Engines"]
            ONVIF["ONVIF Client (SOAP)
            • WS-Security Auth
            • HTTP Digest Auth
            • PullPoint Poller"]

            RTSP["RTSP Client (gortsplib)
            • MJPEG / RTP Depacketizer
            • Keyframe Detector"]
        end

        MQTTPub["MQTT Publisher"]
    end

    subgraph External["External Infrastructure"]
        Broker["MQTT Broker"]
        Camera["IP Camera (ONVIF / RTSP)"]
    end

    HTTPClients -->|REST / HTTP| API
    SnapshotEP --> Manager
    PTZEP --> Manager
    HealthzEP -.-> Manager

    Manager --> ONVIF
    Manager --> RTSP

    ONVIF -->|SOAP PTZ / HTTP Digest Snapshot| Camera
    RTSP -->|RTSP / RTP Video Stream| Camera

    ONVIF -->|Normalized Events| MQTTPub
    MQTTPub -->|Publish JSON Alerts| Broker
```

### Memory Footprint & Resource Strategy
- **Idle State**: ~10–15MB RSS. Only small background goroutines for MQTT keep-alives and ONVIF PullPoint long-polling.
- **Snapshot Request**: Ephemeral connection opened, JPEG downloaded or RTP depacketized, memory garbage-collected immediately. No continuous video decoders or frame ring buffers kept in RAM.

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

## Installation & Distribution

### 1. Homebrew (macOS & Linux)

Install `cam-proxy` via the official [smford/homebrew-tap](https://github.com/smford/homebrew-tap):

```bash
# Add the tap repository
brew tap smford/tap

# Install cam-proxy
brew install cam-proxy
```

---

### 2. Hardened Systemd Service (Linux Edge & Server)

For production Linux deployments, `cam-proxy` provides a hardened Systemd unit template at [`systemd/cam-proxy.service`](systemd/cam-proxy.service) equipped with strict security sandboxing:

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

## Docker Deployment

`cam-proxy` provides official multi-architecture Docker images (`linux/amd64` and `linux/arm64`) with `ffmpeg` and `ca-certificates` pre-installed for seamless H.264/H.265 RTSP snapshot extraction.

### 1. Run with Docker CLI
```bash
docker run -d \
  --name cam-proxy \
  --restart unless-stopped \
  --network host \
  -v $(pwd)/cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro \
  ghcr.io/smford/cam-proxy:latest
```

> [!NOTE]
> **Why `--network host`?**
> Local camera discovery (`--scan`) relies on ONVIF WS-Discovery (multicast UDP `239.255.255.250:3702`). Docker bridge networks do not forward UDP multicast broadcasts across the host's physical network adapter. If you do not need auto-discovery and configure cameras directly by IP, you can use standard port mapping instead (`-p 8080:8080`).

### 2. Run with Docker Compose
A [`docker-compose.yml`](docker-compose.yml) is included in the repository:

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

Start the container:
```bash
docker compose up -d
```

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

### Automated CI/CD & Multi-Arch Docker Publishing
Whenever a Git tag matching `v*.*.*` is pushed to GitHub, the `.github/workflows/release.yml` workflow triggers [GoReleaser](https://goreleaser.com/) to automatically build and publish:

1. **Static Binary Archives**:
   - `linux/amd64`
   - `linux/arm64` (Raspberry Pi 3/4/5, Armbian, edge appliances)
   - `darwin/amd64` (Intel Mac)
   - `darwin/arm64` (Apple Silicon)
   - Accompanying SHA256 checksums attached to the GitHub Release.

2. **Multi-Architecture Container Images**:
   - Pushed directly to GitHub Container Registry (`ghcr.io`):
     - `ghcr.io/smford/cam-proxy:latest`
     - `ghcr.io/smford/cam-proxy:vX.Y.Z`
     - `ghcr.io/smford/cam-proxy:vX.Y`
     - `ghcr.io/smford/cam-proxy:vX`
   - Supporting both `linux/amd64` and `linux/arm64`.

3. **Homebrew Tap Distribution**:
   - Automatically publishes and synchronizes package recipes to [`smford/homebrew-tap`](https://github.com/smford/homebrew-tap) via GoReleaser.

### Creating a Release (Manual)
Use the built-in Makefile targets:
```bash
# Bump patch (v0.2.0 -> v0.2.1)
make tag-patch
git push origin v0.2.1

# Or bump minor (v0.2.0 -> v0.3.0)
make tag-minor
git push origin v0.3.0
```

---

## Dependabot & Automated Release Pipeline

`cam-proxy` is configured with an automated, weekly dependency maintenance pipeline:

1. **Weekly Scheduled Updates**:
   - Dependabot checks for dependency updates **once a week** (Mondays at 06:00 UTC).
   - Updates are grouped into consolidated pull requests:
     - `go-dependencies`: Grouped `go.mod` / `go.sum` updates.
     - `actions-dependencies`: Grouped GitHub Actions workflow updates.
     - `docker`: Docker base image updates.

2. **Robust Multi-Stage CI Verification**:
   Before any Dependabot PR can merge to `main`, it must pass comprehensive CI checks:
   - **Lint & Code Standards**: `gofmt` style validation, `go vet`, `go mod verify`, and `go mod tidy` cleanliness check.
   - **Race-Detection Tests**: Full test suite with `-race` and code coverage profiling.
   - **Cross-Compilation Matrix**: Verified compilation across `linux/amd64`, `linux/arm64`, and `darwin/arm64`.
   - **Container Build Test**: Multi-stage Docker image build verification.
   - **Release Config Validation**: `goreleaser check` to ensure release configs remain valid.

3. **Automated Merging**:
   - Dependabot pull requests are automatically approved and queued for auto-merge.
   - GitHub auto-merge safely waits until all CI status checks succeed before merging onto `main`.

4. **Automated Release on Merge**:
   - When a Dependabot PR merges onto `main`, the `.github/workflows/dependabot-release.yml` workflow triggers automatically.
   - It calculates the next patch version (e.g. `v0.2.0` -> `v0.2.1`), creates and pushes the Git tag, and invokes GoReleaser.
   - The new release binaries and multi-architecture Docker containers (`ghcr.io/smford/cam-proxy:vX.Y.Z` and `:latest`) are published immediately without manual intervention.

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