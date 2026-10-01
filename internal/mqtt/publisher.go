package mqtt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/onvif"
)

// Publisher wraps the Paho MQTT client to publish normalized camera events.
type Publisher struct {
	client      paho.Client
	topicPrefix string
	enabled     bool
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
		prefix = "camtap/events"
	}

	return &Publisher{
		client:      client,
		topicPrefix: prefix,
		enabled:     true,
	}, nil
}

// PublishEvent sends a normalized ONVIF event to the MQTT broker.
// Topic structure: <prefix>/<camera_id>/<event_type> (e.g., camtap/events/front_porch/motion)
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
