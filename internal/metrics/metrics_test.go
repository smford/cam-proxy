package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smford/cam-proxy/internal/metrics"
)

func TestMetricsRecordAndExposition(t *testing.T) {
	m := metrics.NewMetrics()

	// Record initial offline status
	m.SetCameraOnline("front_door", false)

	// Record successful hardware snapshot
	m.RecordSnapshot("front_door", "success", "hardware", 0.085)

	// Record cached snapshot
	m.RecordSnapshot("front_door", "success", "cache", 0)

	// Record failed snapshot on second camera
	m.RecordSnapshot("backyard", "error", "hardware", 0.200)

	// Record MQTT events
	m.RecordMQTTEvent("front_door", "published")
	m.RecordMQTTEvent("front_door", "error")

	// Query Prometheus HTTP handler
	handler := m.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	bodyBytes, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("failed reading metrics body: %v", err)
	}
	body := string(bodyBytes)

	// Verify required Prometheus metrics are exposed
	requiredSubstrings := []string{
		"# TYPE cam_proxy_snapshots_total counter",
		`cam_proxy_snapshots_total{camera_id="front_door",source="hardware",status="success"} 1`,
		`cam_proxy_snapshots_total{camera_id="front_door",source="cache",status="success"} 1`,
		`cam_proxy_snapshots_total{camera_id="backyard",source="hardware",status="error"} 1`,
		"# TYPE cam_proxy_snapshot_duration_seconds histogram",
		`cam_proxy_snapshot_duration_seconds_count{camera_id="front_door"} 1`,
		"# TYPE cam_proxy_camera_online gauge",
		`cam_proxy_camera_online{camera_id="front_door"} 1`,
		`cam_proxy_camera_online{camera_id="backyard"} 0`,
		"# TYPE cam_proxy_mqtt_events_total counter",
		`cam_proxy_mqtt_events_total{camera_id="front_door",status="published"} 1`,
		`cam_proxy_mqtt_events_total{camera_id="front_door",status="error"} 1`,
	}

	for _, expected := range requiredSubstrings {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics exposition missing expected string: %s\nActual output:\n%s", expected, body)
		}
	}
}

func TestNilMetricsSafety(t *testing.T) {
	var m *metrics.Metrics
	// Ensure nil metrics never panics
	m.RecordSnapshot("cam", "success", "hardware", 0.1)
	m.SetCameraOnline("cam", true)
	m.RecordMQTTEvent("cam", "published")

	handler := m.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nil metrics handler, got %d", rr.Code)
	}
}
