package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/smford/cam-proxy/internal/config"
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

func TestSaveConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cam-proxy-save-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := config.DefaultConfig()
	cfg.Cameras["test_cam"] = config.CameraConfig{
		ID:      "test_cam",
		Name:    "Test Camera",
		Address: "192.168.1.99:80",
	}

	if err := config.Save(tmpFile.Name(), cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := config.Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to reload saved config: %v", err)
	}

	if loaded.Cameras["test_cam"].Name != "Test Camera" {
		t.Errorf("expected name 'Test Camera', got %q", loaded.Cameras["test_cam"].Name)
	}
}

func TestSnapshotCacheTTLConfig(t *testing.T) {
	// Verify default config has 1s cache TTL
	def := config.DefaultConfig()
	if def.Server.SnapshotCacheTTL != 1*time.Second {
		t.Errorf("expected default snapshot_cache_ttl 1s, got %v", def.Server.SnapshotCacheTTL)
	}

	yamlContent := `
server:
  snapshot_cache_ttl: 2s
cameras:
  cam_override:
    address: "192.168.1.10:80"
    snapshot_cache_ttl: 500ms
  cam_disabled:
    address: "192.168.1.11:80"
    snapshot_cache_ttl: 0s
  cam_default:
    address: "192.168.1.12:80"
`
	tmpFile, err := os.CreateTemp("", "cam-proxy-ttl-*.yaml")
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
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.SnapshotCacheTTL != 2*time.Second {
		t.Errorf("expected server TTL 2s, got %v", cfg.Server.SnapshotCacheTTL)
	}

	camOverride := cfg.Cameras["cam_override"]
	if camOverride.SnapshotCacheTTL == nil || *camOverride.SnapshotCacheTTL != 500*time.Millisecond {
		t.Errorf("expected cam_override TTL 500ms, got %v", camOverride.SnapshotCacheTTL)
	}

	camDisabled := cfg.Cameras["cam_disabled"]
	if camDisabled.SnapshotCacheTTL == nil || *camDisabled.SnapshotCacheTTL != 0 {
		t.Errorf("expected cam_disabled TTL 0s, got %v", camDisabled.SnapshotCacheTTL)
	}

	camDefault := cfg.Cameras["cam_default"]
	if camDefault.SnapshotCacheTTL != nil {
		t.Errorf("expected cam_default TTL nil, got %v", camDefault.SnapshotCacheTTL)
	}
}

func TestMQTTDiscoveryConfig(t *testing.T) {
	def := config.DefaultConfig()
	if !def.MQTT.Discovery {
		t.Errorf("expected default MQTT discovery true")
	}
	if def.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Errorf("expected default discovery_prefix 'homeassistant', got %q", def.MQTT.DiscoveryPrefix)
	}

	yamlContent := `
mqtt:
  enabled: true
  discovery: false
  discovery_prefix: "custom_ha"
`
	tmpFile, err := os.CreateTemp("", "cam-proxy-mqtt-*.yaml")
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
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MQTT.Discovery {
		t.Errorf("expected discovery to be overridden to false")
	}
	if cfg.MQTT.DiscoveryPrefix != "custom_ha" {
		t.Errorf("expected discovery_prefix 'custom_ha', got %q", cfg.MQTT.DiscoveryPrefix)
	}
}

func TestCORSConfig(t *testing.T) {
	// 1. Test defaults
	def := config.DefaultConfig()
	if !def.Server.CORS.Enabled {
		t.Errorf("expected default CORS enabled to be true")
	}
	if len(def.Server.CORS.AllowedOrigins) != 1 || def.Server.CORS.AllowedOrigins[0] != "*" {
		t.Errorf("expected default allowed_origins ['*'], got %v", def.Server.CORS.AllowedOrigins)
	}
	if len(def.Server.CORS.AllowedMethods) == 0 {
		t.Errorf("expected default allowed_methods non-empty")
	}
	if def.Server.CORS.MaxAge != 86400 {
		t.Errorf("expected default max_age 86400, got %d", def.Server.CORS.MaxAge)
	}

	// 2. Test YAML parsing with custom CORS
	yamlContent := `
server:
  cors:
    enabled: true
    allowed_origins:
      - "http://localhost:8123"
      - "https://dashboard.home"
    allowed_methods:
      - "GET"
      - "POST"
    max_age: 3600
`
	tmpFile, err := os.CreateTemp("", "cam-proxy-cors-*.yaml")
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

	if !cfg.Server.CORS.Enabled {
		t.Errorf("expected CORS enabled true")
	}
	if len(cfg.Server.CORS.AllowedOrigins) != 2 || cfg.Server.CORS.AllowedOrigins[0] != "http://localhost:8123" {
		t.Errorf("expected custom origins, got %v", cfg.Server.CORS.AllowedOrigins)
	}
	if cfg.Server.CORS.MaxAge != 3600 {
		t.Errorf("expected custom max_age 3600, got %d", cfg.Server.CORS.MaxAge)
	}
	// Headers was omitted in YAML, should receive normalized default
	if len(cfg.Server.CORS.AllowedHeaders) == 0 {
		t.Errorf("expected normalized default allowed_headers, got empty")
	}

	// 3. Test disabling CORS
	disabledYAML := `
server:
  cors:
    enabled: false
`
	tmpFile2, err := os.CreateTemp("", "cam-proxy-cors-disabled-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile2.Name())

	if _, err := tmpFile2.Write([]byte(disabledYAML)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile2.Close()

	cfg2, err := config.Load(tmpFile2.Name())
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if cfg2.Server.CORS.Enabled {
		t.Errorf("expected CORS enabled to be false")
	}
}
