#!/usr/bin/with-contenv bashio
# shellcheck shell=bash

bashio::log.info "Starting cam-proxy edge camera gateway..."

CONFIG_PATH="/etc/cam-proxy/cam-proxy.yaml"
mkdir -p /etc/cam-proxy

LOG_LEVEL=$(bashio::config 'log_level' 'info')
CACHE_TTL=$(bashio::config 'snapshot_cache_ttl' '1s')
PORT=$(bashio::config 'server_port' 8080)

# Build YAML configuration
cat <<EOF > "$CONFIG_PATH"
server:
  port: ${PORT}
  log_level: "${LOG_LEVEL}"
  snapshot_cache_ttl: "${CACHE_TTL}"
  cors:
    enabled: true
    allowed_origins:
      - "*"
    allowed_methods:
      - "GET"
      - "POST"
      - "OPTIONS"
    allowed_headers:
      - "Content-Type"
      - "Authorization"
EOF

# Auto-configure MQTT if available through Home Assistant Supervisor
if bashio::services.available "mqtt"; then
  bashio::log.info "Home Assistant MQTT service detected, configuring automatic event forwarding..."
  MQTT_HOST=$(bashio::services mqtt "host")
  MQTT_PORT=$(bashio::services mqtt "port")
  MQTT_USER=$(bashio::services mqtt "username")
  MQTT_PASS=$(bashio::services mqtt "password")

  cat <<EOF >> "$CONFIG_PATH"
mqtt:
  enabled: true
  broker: "tcp://${MQTT_HOST}:${MQTT_PORT}"
  username: "${MQTT_USER}"
  password: "${MQTT_PASS}"
  topic_prefix: "cam-proxy"
  discovery_enabled: true
  discovery_prefix: "homeassistant"
EOF
fi

# Append cameras from add-on options
CAMERAS_JSON=$(bashio::config 'cameras')
if [ -n "$CAMERAS_JSON" ] && [ "$CAMERAS_JSON" != "[]" ]; then
  echo "cameras:" >> "$CONFIG_PATH"
  python3 -c '
import json, yaml, sys
try:
    cams = json.loads(sys.argv[1])
    for cam in cams:
        lines = yaml.dump([cam], default_flow_style=False).splitlines()
        for line in lines:
            print("  " + line)
except Exception as e:
    pass
' "$CAMERAS_JSON" >> "$CONFIG_PATH"
fi

bashio::log.info "Generated cam-proxy configuration successfully."
exec /usr/bin/cam-proxy -config "$CONFIG_PATH"
