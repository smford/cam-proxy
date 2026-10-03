package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the root configuration for cam-proxy.
type Config struct {
	Server    ServerConfig            `yaml:"server"`
	MQTT      MQTTConfig              `yaml:"mqtt"`
	Discovery DiscoveryConfig         `yaml:"discovery"`
	Cameras   map[string]CameraConfig `yaml:"cameras"`
}

// ServerConfig defines the HTTP server parameters.
type ServerConfig struct {
	Host             string        `yaml:"host"`
	Port             int           `yaml:"port"`
	ReadTimeout      time.Duration `yaml:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout"`
	SnapshotCacheTTL time.Duration `yaml:"snapshot_cache_ttl"`
	CORS             CORSConfig    `yaml:"cors"`
}

// CORSConfig defines Cross-Origin Resource Sharing parameters.
type CORSConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// MQTTConfig defines the MQTT broker parameters.
type MQTTConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Broker          string        `yaml:"broker"` // e.g. "tcp://192.168.1.50:1883"
	ClientID        string        `yaml:"client_id"`
	Username        string        `yaml:"username"`
	Password        string        `yaml:"password"`
	TopicPrefix     string        `yaml:"topic_prefix"` // e.g. "cam-proxy/events"
	KeepAlive       time.Duration `yaml:"keep_alive"`
	Discovery       bool          `yaml:"discovery"`                  // Publish Home Assistant auto-discovery configs
	DiscoveryPrefix string        `yaml:"discovery_prefix,omitempty"` // Home Assistant discovery prefix, default "homeassistant"
}

// DiscoveryConfig defines WS-Discovery behavior.
type DiscoveryConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// CameraConfig specifies camera endpoints, credentials, and features.
type CameraConfig struct {
	ID               string         `yaml:"id"`
	Name             string         `yaml:"name"`
	Address          string         `yaml:"address"` // e.g. "192.168.1.120:80"
	RTSPURL          string         `yaml:"rtsp_url"`
	ONVIFUsername    string         `yaml:"onvif_username"`
	ONVIFPassword    string         `yaml:"onvif_password"`
	SnapshotMethod   string         `yaml:"snapshot_method"` // "auto", "onvif", "rtsp"
	PullEvents       bool           `yaml:"pull_events"`
	ProfileToken     string         `yaml:"profile_token"`
	SnapshotCacheTTL *time.Duration `yaml:"snapshot_cache_ttl,omitempty"`
}

// DefaultConfig returns safe defaults suited for edge gateways.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:             "0.0.0.0",
			Port:             8080,
			ReadTimeout:      10 * time.Second,
			WriteTimeout:     15 * time.Second,
			SnapshotCacheTTL: 1 * time.Second,
			CORS: CORSConfig{
				Enabled:        true,
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET", "POST", "OPTIONS"},
				AllowedHeaders: []string{"Content-Type", "Authorization", "X-Requested-With"},
				MaxAge:         86400,
			},
		},
		MQTT: MQTTConfig{
			Enabled:         false,
			Broker:          "tcp://localhost:1883",
			ClientID:        "cam-proxy-daemon",
			TopicPrefix:     "cam-proxy/events",
			KeepAlive:       30 * time.Second,
			Discovery:       true,
			DiscoveryPrefix: "homeassistant",
		},
		Discovery: DiscoveryConfig{
			Enabled:  false,
			Interval: 5 * time.Minute,
		},
		Cameras: make(map[string]CameraConfig),
	}
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config YAML: %w", err)
	}

	if cfg.Server.CORS.Enabled {
		if len(cfg.Server.CORS.AllowedOrigins) == 0 {
			cfg.Server.CORS.AllowedOrigins = []string{"*"}
		}
		if len(cfg.Server.CORS.AllowedMethods) == 0 {
			cfg.Server.CORS.AllowedMethods = []string{"GET", "POST", "OPTIONS"}
		}
		if len(cfg.Server.CORS.AllowedHeaders) == 0 {
			cfg.Server.CORS.AllowedHeaders = []string{"Content-Type", "Authorization", "X-Requested-With"}
		}
		if cfg.Server.CORS.MaxAge <= 0 {
			cfg.Server.CORS.MaxAge = 86400
		}
	}

	if cfg.MQTT.DiscoveryPrefix == "" {
		cfg.MQTT.DiscoveryPrefix = "homeassistant"
	}

	// Normalize camera IDs
	for id, cam := range cfg.Cameras {
		if cam.ID == "" {
			cam.ID = id
		}
		if cam.SnapshotMethod == "" {
			cam.SnapshotMethod = "auto"
		}
		cfg.Cameras[id] = cam
	}

	return cfg, nil
}

// Save writes the configuration to a YAML file.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config YAML: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file %q: %w", path, err)
	}
	return nil
}
