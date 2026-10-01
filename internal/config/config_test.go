package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/smford/camstop/internal/config"
)

func TestLoadConfig(t *testing.T) {
	yamlContent := `
server:
  host: "127.0.0.1"
  port: 9090
  read_timeout: 5s
  write_timeout: 10s

mqtt:
  enabled: true
  broker: "tcp://192.168.1.50:1883"
  client_id: "test-client"
  topic_prefix: "test/events"
  keep_alive: 15s

cameras:
  front_door:
    name: "Front Door"
    address: "192.168.1.101:80"
    rtsp_url: "rtsp://admin:secret@192.168.1.101:554/live"
    onvif_username: "admin"
    onvif_password: "secret"
    snapshot_method: "auto"
    pull_events: true
`
	tmpFile, err := os.CreateTemp("", "camtap-test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := config.Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 5*time.Second {
		t.Errorf("expected read_timeout 5s, got %v", cfg.Server.ReadTimeout)
	}
	if !cfg.MQTT.Enabled {
		t.Errorf("expected MQTT enabled")
	}

	cam, ok := cfg.Cameras["front_door"]
	if !ok {
		t.Fatalf("camera front_door not found")
	}
	if cam.ID != "front_door" {
		t.Errorf("expected ID 'front_door', got %q", cam.ID)
	}
	if cam.SnapshotMethod != "auto" {
		t.Errorf("expected snapshot_method 'auto', got %q", cam.SnapshotMethod)
	}
	if !cam.PullEvents {
		t.Errorf("expected pull_events true")
	}
}
