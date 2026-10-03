package camera_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/smford/cam-proxy/internal/camera"
	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/mqtt"
)

func TestManagerOperations(t *testing.T) {
	cfg := &config.Config{
		Cameras: map[string]config.CameraConfig{
			"cam1": {ID: "cam1", Name: "Front Door"},
			"cam2": {ID: "cam2", Name: "Backyard"},
		},
	}

	mgr := camera.NewManager(cfg, nil)

	// List cameras
	cams := mgr.ListCameras()
	if len(cams) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cams))
	}

	// Get existing camera
	cam1, err := mgr.GetCamera("cam1")
	if err != nil {
		t.Fatalf("failed to get cam1: %v", err)
	}
	if cam1.Config.Name != "Front Door" {
		t.Errorf("expected 'Front Door', got %q", cam1.Config.Name)
	}

	// Get non-existent camera
	_, err = mgr.GetCamera("unknown")
	if !errors.Is(err, camera.ErrCameraNotFound) {
		t.Errorf("expected ErrCameraNotFound, got %v", err)
	}

	// Start without errors
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("manager.Start returned error: %v", err)
	}
}

type dummyToken struct{}

func (d *dummyToken) Wait() bool                     { return true }
func (d *dummyToken) WaitTimeout(time.Duration) bool { return true }
func (d *dummyToken) Done() <-chan struct{}          { ch := make(chan struct{}); close(ch); return ch }
func (d *dummyToken) Error() error                   { return nil }

type mockClient struct {
	mu     sync.Mutex
	topics []string
}

func (m *mockClient) IsConnected() bool       { return true }
func (m *mockClient) IsConnectionOpen() bool  { return true }
func (m *mockClient) Connect() paho.Token     { return &dummyToken{} }
func (m *mockClient) Disconnect(quiesce uint) {}
func (m *mockClient) Subscribe(string, byte, paho.MessageHandler) paho.Token {
	return &dummyToken{}
}
func (m *mockClient) SubscribeMultiple(map[string]byte, paho.MessageHandler) paho.Token {
	return &dummyToken{}
}
func (m *mockClient) Unsubscribe(...string) paho.Token     { return &dummyToken{} }
func (m *mockClient) AddRoute(string, paho.MessageHandler) {}
func (m *mockClient) OptionsReader() paho.ClientOptionsReader {
	return paho.ClientOptionsReader{}
}
func (m *mockClient) Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topics = append(m.topics, topic)
	return &dummyToken{}
}

func TestManagerPublishDiscoveryOnStart(t *testing.T) {
	mock := &mockClient{}
	pub := mqtt.NewPublisherWithClient(mock, "cam-proxy/events", "homeassistant", true)

	cfg := &config.Config{
		Cameras: map[string]config.CameraConfig{
			"cam_with_events": {
				ID:         "cam_with_events",
				Name:       "Porch",
				PullEvents: true,
			},
			"cam_without_events": {
				ID:         "cam_without_events",
				Name:       "Static",
				PullEvents: false,
			},
		},
	}

	mgr := camera.NewManager(cfg, pub)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("unexpected error starting manager: %v", err)
	}

	mock.mu.Lock()
	topics := mock.topics
	mock.mu.Unlock()

	// Only cam_with_events should have discovery published (3 topics)
	if len(topics) != 3 {
		t.Fatalf("expected exactly 3 discovery topics for cam_with_events, got %d: %v", len(topics), topics)
	}

	expectedTopics := map[string]bool{
		"homeassistant/binary_sensor/cam_proxy_cam_with_events_motion/config":     true,
		"homeassistant/binary_sensor/cam_proxy_cam_with_events_tamper/config":     true,
		"homeassistant/binary_sensor/cam_proxy_cam_with_events_line_cross/config": true,
	}

	for _, topic := range topics {
		if !expectedTopics[topic] {
			t.Errorf("unexpected discovery topic: %s", topic)
		}
	}
}
