package mqtt_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/mqtt"
	"github.com/smford/cam-proxy/internal/onvif"
)

type dummyToken struct {
	err error
}

func (d *dummyToken) Wait() bool                     { return true }
func (d *dummyToken) WaitTimeout(time.Duration) bool { return true }
func (d *dummyToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (d *dummyToken) Error() error                   { return d.err }

type publishedMessage struct {
	Topic    string
	QoS      byte
	Retained bool
	Payload  []byte
}

type mockMQTTClient struct {
	mu       sync.Mutex
	messages []publishedMessage
}

func (m *mockMQTTClient) IsConnected() bool       { return true }
func (m *mockMQTTClient) IsConnectionOpen() bool  { return true }
func (m *mockMQTTClient) Connect() paho.Token     { return &dummyToken{} }
func (m *mockMQTTClient) Disconnect(quiesce uint) {}
func (m *mockMQTTClient) Subscribe(string, byte, paho.MessageHandler) paho.Token {
	return &dummyToken{}
}
func (m *mockMQTTClient) SubscribeMultiple(map[string]byte, paho.MessageHandler) paho.Token {
	return &dummyToken{}
}
func (m *mockMQTTClient) Unsubscribe(...string) paho.Token        { return &dummyToken{} }
func (m *mockMQTTClient) AddRoute(string, paho.MessageHandler)    {}
func (m *mockMQTTClient) OptionsReader() paho.ClientOptionsReader { return paho.ClientOptionsReader{} }

func (m *mockMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token {
	m.mu.Lock()
	defer m.mu.Unlock()

	var data []byte
	switch p := payload.(type) {
	case []byte:
		data = p
	case string:
		data = []byte(p)
	}

	m.messages = append(m.messages, publishedMessage{
		Topic:    topic,
		QoS:      qos,
		Retained: retained,
		Payload:  data,
	})
	return &dummyToken{}
}

func (m *mockMQTTClient) getMessages() []publishedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]publishedMessage, len(m.messages))
	copy(out, m.messages)
	return out
}

func TestDisabledPublisher(t *testing.T) {
	cfg := config.MQTTConfig{
		Enabled: false,
	}

	pub, err := mqtt.NewPublisher(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating disabled publisher: %v", err)
	}

	ev := onvif.Event{
		CameraID:  "front_door",
		Type:      "motion",
		State:     true,
		Timestamp: time.Now().UTC(),
	}

	// Should not error when disabled
	if err := pub.PublishEvent(ev); err != nil {
		t.Errorf("expected nil error on disabled publisher, got: %v", err)
	}

	if err := pub.PublishDiscovery(config.CameraConfig{ID: "cam1"}); err != nil {
		t.Errorf("expected nil error on disabled discovery, got: %v", err)
	}

	pub.Close()
}

func TestBuildDiscoveryPayloads(t *testing.T) {
	client := &mockMQTTClient{}
	pub := mqtt.NewPublisherWithClient(client, "cam-proxy/events", "homeassistant", true)

	cam := config.CameraConfig{
		ID:   "driveway",
		Name: "Driveway Main",
	}

	payloads := pub.BuildDiscoveryPayloads(cam)
	if len(payloads) != 3 {
		t.Fatalf("expected 3 discovery payloads (motion, tamper, line_cross), got %d", len(payloads))
	}

	// 1. Motion Sensor
	motionTopic := "homeassistant/binary_sensor/cam_proxy_driveway_motion/config"
	motionData, ok := payloads[motionTopic]
	if !ok {
		t.Fatalf("missing motion discovery topic %s", motionTopic)
	}

	var motionCfg mqtt.HABinarySensorConfig
	if err := json.Unmarshal(motionData, &motionCfg); err != nil {
		t.Fatalf("failed to unmarshal motion config: %v", err)
	}

	if motionCfg.Name != "Driveway Main Motion" {
		t.Errorf("expected name 'Driveway Main Motion', got %q", motionCfg.Name)
	}
	if motionCfg.UniqueID != "cam_proxy_driveway_motion" {
		t.Errorf("expected unique_id 'cam_proxy_driveway_motion', got %q", motionCfg.UniqueID)
	}
	if motionCfg.StateTopic != "cam-proxy/events/driveway/motion" {
		t.Errorf("expected state_topic 'cam-proxy/events/driveway/motion', got %q", motionCfg.StateTopic)
	}
	if motionCfg.ValueTemplate != "{{ value_json.state }}" {
		t.Errorf("expected value_template '{{ value_json.state }}', got %q", motionCfg.ValueTemplate)
	}
	if motionCfg.PayloadOn != "true" || motionCfg.PayloadOff != "false" {
		t.Errorf("expected payload_on/off true/false, got %s/%s", motionCfg.PayloadOn, motionCfg.PayloadOff)
	}
	if motionCfg.DeviceClass != "motion" {
		t.Errorf("expected device_class motion, got %q", motionCfg.DeviceClass)
	}
	if len(motionCfg.Device.Identifiers) != 1 || motionCfg.Device.Identifiers[0] != "cam_proxy_driveway" {
		t.Errorf("expected identifier cam_proxy_driveway, got %v", motionCfg.Device.Identifiers)
	}
	if motionCfg.Device.Manufacturer != "cam-proxy" {
		t.Errorf("expected manufacturer cam-proxy, got %q", motionCfg.Device.Manufacturer)
	}
	if motionCfg.Device.ViaDevice != "cam-proxy" {
		t.Errorf("expected via_device cam-proxy, got %q", motionCfg.Device.ViaDevice)
	}

	// 2. Tamper Sensor
	tamperTopic := "homeassistant/binary_sensor/cam_proxy_driveway_tamper/config"
	tamperData, ok := payloads[tamperTopic]
	if !ok {
		t.Fatalf("missing tamper discovery topic %s", tamperTopic)
	}
	var tamperCfg mqtt.HABinarySensorConfig
	if err := json.Unmarshal(tamperData, &tamperCfg); err != nil {
		t.Fatalf("failed to unmarshal tamper config: %v", err)
	}
	if tamperCfg.DeviceClass != "tamper" {
		t.Errorf("expected device_class tamper, got %q", tamperCfg.DeviceClass)
	}
	if tamperCfg.Name != "Driveway Main Tamper" {
		t.Errorf("expected name 'Driveway Main Tamper', got %q", tamperCfg.Name)
	}

	// 3. Line-Crossing Sensor
	lineTopic := "homeassistant/binary_sensor/cam_proxy_driveway_line_cross/config"
	lineData, ok := payloads[lineTopic]
	if !ok {
		t.Fatalf("missing line crossing discovery topic %s", lineTopic)
	}
	var lineCfg mqtt.HABinarySensorConfig
	if err := json.Unmarshal(lineData, &lineCfg); err != nil {
		t.Fatalf("failed to unmarshal line crossing config: %v", err)
	}
	if lineCfg.Name != "Driveway Main Line Crossing" {
		t.Errorf("expected name 'Driveway Main Line Crossing', got %q", lineCfg.Name)
	}
	if lineCfg.Icon != "mdi:vector-line" {
		t.Errorf("expected icon mdi:vector-line, got %q", lineCfg.Icon)
	}
}

func TestPublishDiscovery(t *testing.T) {
	client := &mockMQTTClient{}
	pub := mqtt.NewPublisherWithClient(client, "cam-proxy/events", "homeassistant", true)

	cam := config.CameraConfig{
		ID:   "porch",
		Name: "Porch Camera",
	}

	err := pub.PublishDiscovery(cam)
	if err != nil {
		t.Fatalf("unexpected error publishing discovery: %v", err)
	}

	msgs := client.getMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 retained messages published, got %d", len(msgs))
	}

	for _, msg := range msgs {
		if !msg.Retained {
			t.Errorf("expected discovery message on %s to be RETAINED", msg.Topic)
		}
		if msg.QoS != 1 {
			t.Errorf("expected discovery message on %s to have QoS 1, got %d", msg.Topic, msg.QoS)
		}
	}
}

func TestPublishDiscoveryDisabledOption(t *testing.T) {
	client := &mockMQTTClient{}
	// discovery flag set to false
	pub := mqtt.NewPublisherWithClient(client, "cam-proxy/events", "homeassistant", false)

	cam := config.CameraConfig{
		ID:   "porch",
		Name: "Porch Camera",
	}

	err := pub.PublishDiscovery(cam)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := client.getMessages()
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages when discovery is disabled, got %d", len(msgs))
	}
}
