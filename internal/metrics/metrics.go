package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics encapsulates Prometheus collectors for cam-proxy gateway observability.
type Metrics struct {
	Registry         *prometheus.Registry
	SnapshotsTotal   *prometheus.CounterVec
	SnapshotDuration *prometheus.HistogramVec
	CameraOnline     *prometheus.GaugeVec
	MQTTEventsTotal  *prometheus.CounterVec
}

// NewMetrics creates and initializes an isolated Prometheus metrics registry and collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Registry: reg,
		SnapshotsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cam_proxy",
				Subsystem: "snapshots",
				Name:      "total",
				Help:      "Total number of camera snapshot requests processed.",
			},
			[]string{"camera_id", "status", "source"},
		),
		SnapshotDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "cam_proxy",
				Subsystem: "snapshot",
				Name:      "duration_seconds",
				Help:      "Snapshot fetch latency in seconds.",
				Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"camera_id"},
		),
		CameraOnline: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cam_proxy",
				Subsystem: "camera",
				Name:      "online",
				Help:      "Camera operational status (1 = online, 0 = offline).",
			},
			[]string{"camera_id"},
		),
		MQTTEventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cam_proxy",
				Subsystem: "mqtt",
				Name:      "events_total",
				Help:      "Total number of camera events processed for MQTT.",
			},
			[]string{"camera_id", "status"},
		),
	}

	reg.MustRegister(m.SnapshotsTotal)
	reg.MustRegister(m.SnapshotDuration)
	reg.MustRegister(m.CameraOnline)
	reg.MustRegister(m.MQTTEventsTotal)

	return m
}

// RecordSnapshot increments snapshot counters and records latency and online status.
func (m *Metrics) RecordSnapshot(cameraID, status, source string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.SnapshotsTotal.WithLabelValues(cameraID, status, source).Inc()
	if status == "success" {
		if source != "cache" {
			m.SnapshotDuration.WithLabelValues(cameraID).Observe(durationSeconds)
		}
		m.CameraOnline.WithLabelValues(cameraID).Set(1)
	} else if status == "error" {
		m.CameraOnline.WithLabelValues(cameraID).Set(0)
	}
}

// SetCameraOnline updates the camera online gauge (1 = online, 0 = offline).
func (m *Metrics) SetCameraOnline(cameraID string, online bool) {
	if m == nil {
		return
	}
	val := 0.0
	if online {
		val = 1.0
	}
	m.CameraOnline.WithLabelValues(cameraID).Set(val)
}

// RecordMQTTEvent increments MQTT event publishing counter.
func (m *Metrics) RecordMQTTEvent(cameraID, status string) {
	if m == nil {
		return
	}
	m.MQTTEventsTotal.WithLabelValues(cameraID, status).Inc()
}

// Handler returns an http.Handler that renders metrics in Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
