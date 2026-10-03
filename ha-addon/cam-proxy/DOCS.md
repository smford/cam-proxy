# Home Assistant Add-on: cam-proxy

`cam-proxy` is an ultra-lightweight, zero-CGO edge gateway for IP security cameras (ONVIF and RTSP). It shields sensitive or low-cost camera hardware from socket exhaustion while delivering sub-100ms HTTP snapshot caching, single-flight request coalescing, continuous PTZ control, and ONVIF PullPoint event-to-MQTT forwarding.

---

## Features

- **Hardware Protection**: Protects fragile camera SOCs with `singleflight` request coalescing and short-TTL in-memory caching.
- **Fast HTTP Snapshots**: Sub-100ms snapshots for Home Assistant Lovelace dashboards.
- **PTZ Proxying**: Continuous move, stop, and preset recall via standard REST endpoints.
- **MQTT Event Forwarding**: Subscribes to ONVIF PullPoint events and forwards motion, tamper, and line-crossing events to Home Assistant with automatic MQTT discovery.
- **Zero Overhead**: Written in pure Go with zero CGO dependencies. Runs at negligible CPU and ~15MB RAM.

---

## Installation

1. In Home Assistant, navigate to **Settings** -> **Add-ons** -> **Add-on Store**.
2. Click the three dots (top right) -> **Repositories**.
3. Add this repository URL: `https://github.com/smford/cam-proxy`
4. Find **cam-proxy** in the list and click **Install**.

---

## Configuration

Configure your cameras directly in the Add-on **Configuration** tab:

```yaml
log_level: "info"
snapshot_cache_ttl: "1s"
server_port: 8080
cameras:
  - id: "front-door"
    name: "Front Door Camera"
    onvif_url: "http://192.168.1.50:8899/onvif/device_service"
    snapshot_url: "http://192.168.1.50/onvif/snapshot"
    rtsp_url: "rtsp://192.168.1.50:554/live/ch0"
    username: "admin"
    password: "YourSecurePassword"
    pull_events: true
    ptz:
      enabled: true
      profile_token: "Profile_1"
```

### Auto-Configured MQTT

If you run the Home Assistant **Mosquitto broker** add-on, `cam-proxy` automatically detects it and provisions Home Assistant MQTT Auto-Discovery entities (`binary_sensor.<id>_motion`) with zero manual broker configuration.

---

## Adding Cameras to Home Assistant

Add each camera as a **Generic Camera** integration in Home Assistant:

- **Still Image URL**: `http://localhost:8080/api/v1/cameras/front-door/snapshot`
- **Stream Source**: `rtsp://192.168.1.50:554/live/ch0` (or `http://localhost:8080/api/v1/cameras/front-door/stream.mjpg`)
- **Framerate**: 2-5 fps for dashboard cards
