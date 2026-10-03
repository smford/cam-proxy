package mqtt_test

import (
	"testing"
	"time"

	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/mqtt"
	"github.com/smford/cam-proxy/internal/onvif"
)

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

	// Should not panic on close
	pub.Close()
}
