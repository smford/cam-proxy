package mqtt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/onvif"
)

// HADevice represents device registry metadata linking entities back to cam-proxy in Home Assistant.
type HADevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

// HABinarySensorConfig represents the payload for Home Assistant MQTT binary sensor discovery.
type HABinarySensorConfig struct {
	Name          string   `json:"name"`
	UniqueID      string   `json:"unique_id"`
	StateTopic    string   `json:"state_topic"`
	ValueTemplate string   `json:"value_template"`
	PayloadOn     string   `json:"payload_on"`
	PayloadOff    string   `json:"payload_off"`
	DeviceClass   string   `json:"device_class,omitempty"`
	Icon          string   `json:"icon,omitempty"`
	Device        HADevice `json:"device"`
}

// DiscoverySensorDef describes a binary sensor type registered with Home Assistant.
type DiscoverySensorDef struct {
	EventSuffix string // e.g. "motion", "tamper", "line_cross"
	NameSuffix  string // e.g. "Motion", "Tamper", "Line Crossing"
	DeviceClass string // e.g. "motion", "tamper"
	Icon        string // e.g. "mdi:vector-line"
}

// DefaultDiscoverySensors defines the standard binary sensors registered for cameras.
var DefaultDiscoverySensors = []DiscoverySensorDef{
	{
		EventSuffix: "motion",
		NameSuffix:  "Motion",
		DeviceClass: "motion",
	},
	{
		EventSuffix: "tamper",
		NameSuffix:  "Tamper",
		DeviceClass: "tamper",
	},
	{
		EventSuffix: "line_cross",
		NameSuffix:  "Line Crossing",
		DeviceClass: "motion",
		Icon:        "mdi:vector-line",
	},
}

// Publisher wraps the Paho MQTT client to publish normalized camera events and HA discovery.
type Publisher struct {
	client          paho.Client
	topicPrefix     string
	discoveryPrefix string
	discovery       bool
	enabled         bool
}

// NewPublisher initializes and connects an MQTT client based on config.
func NewPublisher(cfg config.MQTTConfig) (*Publisher, error) {
	if !cfg.Enabled || cfg.Broker == "" {
		return &Publisher{enabled: false}, nil
	}

	opts := paho.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(cfg.KeepAlive)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	opts.SetOnConnectHandler(func(c paho.Client) {
		slog.Info("connected to MQTT broker", "broker", cfg.Broker)
	})

	opts.SetConnectionLostHandler(func(c paho.Client, err error) {
		slog.Warn("connection to MQTT broker lost", "err", err)
	})

	client := paho.NewClient(opts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("MQTT connect error: %w", token.Error())
	}

	prefix := cfg.TopicPrefix
	if prefix == "" {
		prefix = "cam-proxy/events"
	}

	discPrefix := cfg.DiscoveryPrefix
	if discPrefix == "" {
		discPrefix = "homeassistant"
	}

	return &Publisher{
		client:          client,
		topicPrefix:     prefix,
		discoveryPrefix: discPrefix,
		discovery:       cfg.Discovery,
		enabled:         true,
	}, nil
}

// NewPublisherWithClient creates a Publisher instance using an injected or mock client for tests.
func NewPublisherWithClient(client paho.Client, topicPrefix, discoveryPrefix string, discovery bool) *Publisher {
	if topicPrefix == "" {
		topicPrefix = "cam-proxy/events"
	}
	if discoveryPrefix == "" {
		discoveryPrefix = "homeassistant"
	}
	return &Publisher{
		client:          client,
		topicPrefix:     topicPrefix,
		discoveryPrefix: discoveryPrefix,
		discovery:       discovery,
		enabled:         true,
	}
}

// BuildDiscoveryPayloads constructs the Home Assistant discovery topic -> JSON payload map for a camera.
func (p *Publisher) BuildDiscoveryPayloads(cam config.CameraConfig) map[string][]byte {
	displayName := cam.Name
	if displayName == "" {
		displayName = cam.ID
	}

	out := make(map[string][]byte, len(DefaultDiscoverySensors))

	for _, sensor := range DefaultDiscoverySensors {
		topic := fmt.Sprintf("%s/binary_sensor/cam_proxy_%s_%s/config", p.discoveryPrefix, cam.ID, sensor.EventSuffix)
		cfg := HABinarySensorConfig{
			Name:          fmt.Sprintf("%s %s", displayName, sensor.NameSuffix),
			UniqueID:      fmt.Sprintf("cam_proxy_%s_%s", cam.ID, sensor.EventSuffix),
			StateTopic:    fmt.Sprintf("%s/%s/%s", p.topicPrefix, cam.ID, sensor.EventSuffix),
			ValueTemplate: "{{ value_json.state }}",
			PayloadOn:     "true",
			PayloadOff:    "false",
			DeviceClass:   sensor.DeviceClass,
			Icon:          sensor.Icon,
			Device: HADevice{
				Identifiers:  []string{fmt.Sprintf("cam_proxy_%s", cam.ID)},
				Name:         displayName,
				Manufacturer: "cam-proxy",
				Model:        "ONVIF IP Camera",
				ViaDevice:    "cam-proxy",
			},
		}

		data, err := json.Marshal(cfg)
		if err == nil {
			out[topic] = data
		}
	}

	return out
}

// PublishDiscovery publishes Home Assistant MQTT discovery configuration for all supported sensors of a camera.
func (p *Publisher) PublishDiscovery(cam config.CameraConfig) error {
	if !p.enabled || !p.discovery || p.client == nil {
		return nil
	}

	payloads := p.BuildDiscoveryPayloads(cam)
	for topic, data := range payloads {
		token := p.client.Publish(topic, 1, true, data)
		if token.Wait() && token.Error() != nil {
			return fmt.Errorf("publishing HA discovery payload to %s: %w", topic, token.Error())
		}
		slog.Debug("published Home Assistant MQTT discovery", "topic", topic)
	}

	return nil
}

// PublishEvent sends a normalized ONVIF event to the MQTT broker.
// Topic structure: <prefix>/<camera_id>/<event_type> (e.g., cam-proxy/events/front_porch/motion)
func (p *Publisher) PublishEvent(event onvif.Event) error {
	if !p.enabled || p.client == nil {
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshaling event JSON: %w", err)
	}

	topic := fmt.Sprintf("%s/%s/%s", p.topicPrefix, event.CameraID, event.Type)
	token := p.client.Publish(topic, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("publishing MQTT message to %s: %w", topic, token.Error())
	}

	slog.Debug("published camera event to MQTT", "topic", topic, "state", event.State)
	return nil
}

// Close gracefully disconnects from the MQTT broker.
func (p *Publisher) Close() {
	if p.enabled && p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(250)
	}
}
