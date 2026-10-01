# camtap

`camtap` is a lightweight (< 20MB RSS), single-binary edge daemon written in pure Go. It bridges IP cameras to modern edge computing environments by providing:

1. **On-Demand Snapshots**: Connects to the camera on-demand (via ONVIF HTTP or RTSP), grabs a frame, converts it to JPEG in memory, and serves it over HTTP. Zero 24/7 decoding overhead.
2. **PTZ Control**: Exposes simple JSON REST endpoints for continuous velocity moves, absolute stops, and preset recalls via ONVIF SOAP.
3. **ONVIF Event-to-MQTT Bridge**: Subscribes to camera PullPoint notification queues (motion, tamper, line crossing), normalizes states, and publishes clean JSON events to MQTT.
4. **Zero Heavy Dependencies**: Pure Go compilation (`CGO_ENABLED=0`) with no FFmpeg runtime dependency required.

---

## Architecture Overview

```
                      +---------------------------------------+
                      |         HTTP Clients (REST)           |
                      +---------------------------------------+
                                          |
                                          v [Go 1.22+ net/http]
+-----------------------------------------------------------------------------+
|                                camtap daemon                                |
|                                                                             |
|   +-----------------------+   +--------------------+   +----------------+   |
|   |  GET .../snapshot     |   |   POST .../ptz     |   |   GET /healthz |   |
|   +-----------------------+   +--------------------+   +----------------+   |
|               |                         |                                   |
|               v                         v                                   |
|   +---------------------------------------------------------------------+   |
|   |                         Camera Manager                              |   |
|   +---------------------------------------------------------------------+   |
|               |                                       |                     |
|               v                                       v                     |
|   +-----------------------+               +-----------------------+         |
|   |  ONVIF Client (SOAP)  |               |      RTSP Client      |         |
|   |  - WS-Security Auth   |               |  (bluenviron/gortsplib)|        |
|   |  - HTTP Digest Auth   |               |  - MJPEG / RTP depack |         |
|   |  - PullPoint Poller   |               |  - Keyframe detector  |         |
|   +-----------------------+               +-----------------------+         |
|               |                                       |                     |
+---------------|---------------------------------------|---------------------+
                |                                       |
                | PullPoint Events                      |
                v                                       |
     +--------------------+                             |
     |   MQTT Publisher   |                             |
     +--------------------+                             |
                |                                       |
                v                                       v
        [ MQTT Broker ]                        [ IP Camera (RTSP/ONVIF) ]
```

### Memory Footprint & Resource Strategy
- **Idle State**: ~10–15MB RSS. Only small background goroutines for MQTT keep-alives and ONVIF PullPoint long-polling.
- **Snapshot Request**: Ephemeral connection opened, JPEG downloaded or RTP depacketized, memory garbage-collected immediately. No continuous video decoders or frame ring buffers kept in RAM.

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

### 2. Configure
Copy [config.example.yaml](file:///Users/asc/git/camstop/config.example.yaml) to `camtap.yaml`:
```bash
cp config.example.yaml camtap.yaml
```

Edit your camera IP addresses, ONVIF credentials, and RTSP stream URLs.

### 3. Run
```bash
./camtap -config camtap.yaml -log-level debug
```

---

## API Reference

### 1. Health & Status
```http
GET /healthz
```
Response:
```json
{
  "camera_count": 2,
  "status": "ok",
  "uptime": "12m34s"
}
```

### 2. List Configured Cameras
```http
GET /api/v1/cameras
```

### 3. On-Demand Snapshot
```http
GET /api/v1/cameras/{id}/snapshot
```
Returns: Binary JPEG image (`Content-Type: image/jpeg`) with cache-busting headers.

### 4. PTZ Control
```http
POST /api/v1/cameras/{id}/ptz
Content-Type: application/json
```

**Move (Continuous velocity):**
```json
{
  "action": "move",
  "pan": 0.5,
  "tilt": -0.2,
  "zoom": 0.0
}
```

**Stop:**
```json
{
  "action": "stop"
}
```

**Recall Preset:**
```json
{
  "action": "preset",
  "preset_token": "1"
}
```

---

## MQTT Event Bridge

When `pull_events: true` is enabled on a camera, `camtap` subscribes to the camera's ONVIF PullPoint notification queue. Detected alerts are normalized and published to:

`<topic_prefix>/<camera_id>/<event_type>` (e.g. `camtap/events/front_door/motion`)

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

---

## Releases & SemVer Versioning

Releases follow standard Semantic Versioning (`vMAJOR.MINOR.PATCH`).

### Automated CI/CD
Whenever a Git tag matching `v*.*.*` is pushed to GitHub, the `.github/workflows/release.yml` workflow triggers [GoReleaser](https://goreleaser.com/) to build static binaries for:
- `linux/amd64`
- `linux/arm64` (Raspberry Pi 3/4/5, Armbian, edge appliances)
- `darwin/amd64`
- `darwin/arm64` (Apple Silicon)

It automatically bundles archives, computes SHA256 checksums, and attaches them to the new GitHub Release.

### Creating a Release
Use the built-in Makefile targets:
```bash
# Bump patch (v0.1.0 -> v0.1.1)
make tag-patch
git push origin v0.1.1

# Or bump minor (v0.1.0 -> v0.2.0)
make tag-minor
git push origin v0.2.0
```