# Home Assistant Integration Guide

`cam-proxy` is designed as a companion gateway for **Home Assistant**. By offloading snapshot polling, request coalescing, and ONVIF event stream parsing, `cam-proxy` ensures that Home Assistant dashboards remain responsive without overwhelming fragile camera hardware.

---

## 1. Architectural Overview

```text
┌─────────────────┐       HTTP Snapshots (sub-100ms)       ┌────────────────────────┐
│  Home Assistant │ ◄───────────────────────────────────── │                        │
│    Lovelace     │        MJPEG Streams / PTZ REST        │       cam-proxy        │
│   Dashboards    │ ─────────────────────────────────────► │  (Zero-CGO Gateway)    │
└─────────────────┘                                        └───────────┬────────────┘
        ▲                                                              │
        │ MQTT Discovery                                               │ ONVIF SOAP /
        │ & Event Topics                                               │ RTSP Stream
        │                                                              ▼
┌───────┴─────────┐                                        ┌────────────────────────┐
│ Mosquitto Broker│ ◄─── State: "ON"/"OFF" ─────────────── │   IP Security Cameras  │
│ (Auto-Config)   │      Motion / Tamper / Line-Cross      │  (Dahua, Hikvision,    │
└─────────────────┘                                        │   Reolink, Amcrest)    │
                                                           └────────────────────────┘
```

### Key Benefits
- **Zero Camera Lockups**: Home Assistant dashboard cards refreshing snapshots every 1–2 seconds can crash low-cost camera SOCs. `cam-proxy` combines concurrent requests into a single hardware call and serves cached frames.
- **Instant Dashboards**: Native HTTP snapshots are returned in <100ms compared to multi-second delays with direct RTSP extraction.
- **Automatic Event Entities**: ONVIF PullPoint events (motion, line crossing, tamper) automatically appear as Home Assistant `binary_sensor` entities via MQTT Discovery.
- **RESTful PTZ**: Pan, tilt, zoom, and preset triggers via clean HTTP POST calls.

---

## 2. Installation Options

### Option A: Home Assistant Add-on (Recommended for HA OS / Supervised)

1. In Home Assistant, navigate to **Settings** ➔ **Add-ons** ➔ **Add-on Store**.
2. Click the three-dot menu in the upper-right corner and select **Repositories**.
3. Enter the repository URL:
   ```text
   https://github.com/smford/cam-proxy
   ```
4. Click **Add**, close the dialog, and refresh the store.
5. Select **cam-proxy** and click **Install**.
6. Switch to the **Configuration** tab, define your cameras, and click **Start**.

> **Note**: If you have the official **Mosquitto broker** add-on installed, `cam-proxy` will automatically discover the broker credentials and enable Home Assistant MQTT auto-discovery.

### Option B: Standalone Docker Container

If you run Home Assistant Container or Core, run `cam-proxy` as a lightweight container alongside:

```yaml
services:
  cam-proxy:
    image: ghcr.io/smford/cam-proxy:latest
    container_name: cam-proxy
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro
```

---

## 3. Adding Cameras to Home Assistant

Add each camera using the native **Generic Camera** integration in Home Assistant:

1. Go to **Settings** ➔ **Devices & Services** ➔ **Add Integration**.
2. Search for **Generic Camera**.
3. Configure the fields:
   - **Still Image URL**: `http://<cam-proxy-host>:8080/api/v1/cameras/<camera-id>/snapshot`
   - **Stream Source**: `rtsp://<camera-ip>:554/live/ch0` *(or `http://<cam-proxy-host>:8080/api/v1/cameras/<camera-id>/stream.mjpg`)*
   - **Framerate**: `2` (recommended for preview cards)
   - **Verify SSL Certificate**: Unchecked (unless using HTTPS reverse proxy)
4. Submit and assign to an Area.

---

## 4. MQTT Auto-Discovery & Sensors

When `cam-proxy` connects to your MQTT broker with discovery enabled, it automatically announces ONVIF event sensors to Home Assistant:

```yaml
mqtt:
  enabled: true
  broker: "tcp://192.168.1.10:1883"
  username: "mqtt_user"
  password: "mqtt_password"
  discovery_enabled: true
  discovery_prefix: "homeassistant"
  topic_prefix: "cam-proxy"
```

The following entities are automatically created in Home Assistant:
- `binary_sensor.<camera_id>_motion`: ONVIF motion detection (`device_class: motion`).
- `binary_sensor.<camera_id>_tamper`: Camera tamper / occlusion alert (`device_class: problem`).
- `binary_sensor.<camera_id>_cell_motion_detector`: Grid cell motion detection.
- `binary_sensor.<camera_id>_line_crossing`: ONVIF line crossing alert.

---

## 5. PTZ Control via REST Commands

You can control PTZ cameras directly from Home Assistant using REST commands. Add the following to your Home Assistant `configuration.yaml`:

```yaml
rest_command:
  cam_ptz_move:
    url: "http://localhost:8080/api/v1/cameras/{{ camera_id }}/ptz"
    method: POST
    headers:
      Content-Type: "application/json"
    payload: >-
      {
        "action": "move",
        "pan": {{ pan | default(0.0) }},
        "tilt": {{ tilt | default(0.0) }},
        "zoom": {{ zoom | default(0.0) }}
      }

  cam_ptz_stop:
    url: "http://localhost:8080/api/v1/cameras/{{ camera_id }}/ptz"
    method: POST
    headers:
      Content-Type: "application/json"
    payload: '{"action": "stop"}'

  cam_ptz_preset:
    url: "http://localhost:8080/api/v1/cameras/{{ camera_id }}/ptz"
    method: POST
    headers:
      Content-Type: "application/json"
    payload: >-
      {
        "action": "preset",
        "preset_token": "{{ preset_token }}"
      }
```

---

## 6. Lovelace Dashboard Cards

### Picture Glance Card
Use a standard Lovelace `picture-glance` card to view the camera snapshot and its auto-discovered motion sensor:

```yaml
type: picture-glance
title: Front Door
camera_image: camera.front_door
entities:
  - binary_sensor.front_door_motion
  - binary_sensor.front_door_tamper
```

### PTZ Control Card with D-Pad
Combine camera viewing with PTZ buttons using a Lovelace grid card:

```yaml
type: vertical-stack
cards:
  - type: picture-entity
    entity: camera.front_door
    show_state: false
    show_name: true

  - type: grid
    columns: 3
    square: true
    cards:
      - type: button
        icon: mdi:arrow-top-left
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            pan: -0.5
            tilt: 0.5

      - type: button
        icon: mdi:arrow-up-bold
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            tilt: 0.5

      - type: button
        icon: mdi:arrow-top-right
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            pan: 0.5
            tilt: 0.5

      - type: button
        icon: mdi:arrow-left-bold
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            pan: -0.5

      - type: button
        icon: mdi:stop-circle-outline
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_stop
          service_data:
            camera_id: "front-door"

      - type: button
        icon: mdi:arrow-right-bold
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            pan: 0.5

      - type: button
        icon: mdi:magnify-plus-outline
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            zoom: 0.5

      - type: button
        icon: mdi:arrow-down-bold
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            tilt: -0.5

      - type: button
        icon: mdi:magnify-minus-outline
        tap_action:
          action: call-service
          service: rest_command.cam_ptz_move
          service_data:
            camera_id: "front-door"
            zoom: -0.5
```

---

## 7. Actionable Notifications with Fast Snapshots

Because `cam-proxy` serves snapshots in sub-100ms, push notifications to iOS and Android arrive instantly with fresh imagery:

```yaml
automation:
  - alias: "Front Door Motion Notification"
    trigger:
      - platform: state
        entity_id: binary_sensor.front_door_motion
        to: "on"
    action:
      - service: notify.notify
        data:
          title: "Motion Detected at Front Door"
          message: "Camera detected movement."
          data:
            image: "http://<cam-proxy-host>:8080/api/v1/cameras/front-door/snapshot"
            clickAction: "/lovelace/security"
```
