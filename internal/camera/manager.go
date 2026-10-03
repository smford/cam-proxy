package camera

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/metrics"
	"github.com/smford/cam-proxy/internal/mqtt"
	"github.com/smford/cam-proxy/internal/onvif"
)

// Manager coordinates all configured cameras.
type Manager struct {
	mu        sync.RWMutex
	cameras   map[string]*Camera
	publisher *mqtt.Publisher
	metrics   *metrics.Metrics
}

// NewManager creates a camera manager with default metrics.
func NewManager(cfg *config.Config, publisher *mqtt.Publisher) *Manager {
	return NewManagerWithMetrics(cfg, publisher, nil)
}

// NewManagerWithMetrics creates a camera manager with custom metrics.
func NewManagerWithMetrics(cfg *config.Config, publisher *mqtt.Publisher, met *metrics.Metrics) *Manager {
	if met == nil {
		met = metrics.NewMetrics()
	}

	m := &Manager{
		cameras:   make(map[string]*Camera),
		publisher: publisher,
		metrics:   met,
	}

	for id, camCfg := range cfg.Cameras {
		cam := NewCamera(camCfg)
		cam.SetMetrics(met)
		met.SetCameraOnline(id, false)
		if camCfg.SnapshotCacheTTL != nil {
			cam.SetCacheTTL(*camCfg.SnapshotCacheTTL)
		} else {
			cam.SetCacheTTL(cfg.Server.SnapshotCacheTTL)
		}
		m.cameras[id] = cam
	}

	return m
}

// Metrics returns the Prometheus metrics collector used by the manager.
func (m *Manager) Metrics() *metrics.Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics
}

// Start begins background services like event subscriptions and MQTT discovery.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Publish Home Assistant MQTT discovery on startup for cameras with pull_events enabled
	if m.publisher != nil {
		for _, cam := range m.cameras {
			if cam.Config.PullEvents {
				if err := m.publisher.PublishDiscovery(cam.Config); err != nil {
					slog.Warn("failed to publish Home Assistant MQTT discovery", "camera_id", cam.Config.ID, "err", err)
				}
			}
		}
	}

	for id, cam := range m.cameras {
		if cam.Config.PullEvents {
			slog.Info("starting ONVIF event subscription", "camera_id", id)
			cam.StartEventListener(ctx, func(ev onvif.Event) {
				if m.publisher != nil {
					if err := m.publisher.PublishEvent(ev); err != nil {
						slog.Error("failed to publish MQTT event", "camera_id", ev.CameraID, "err", err)
						if m.metrics != nil {
							m.metrics.RecordMQTTEvent(ev.CameraID, "error")
						}
					} else {
						if m.metrics != nil {
							m.metrics.RecordMQTTEvent(ev.CameraID, "published")
						}
					}
				}
			})
		}
	}
	return nil
}

// GetCamera retrieves a camera by ID.
func (m *Manager) GetCamera(id string) (*Camera, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cam, ok := m.cameras[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCameraNotFound, id)
	}
	return cam, nil
}

// ListCameras returns all registered cameras.
func (m *Manager) ListCameras() []*Camera {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Camera, 0, len(m.cameras))
	for _, cam := range m.cameras {
		list = append(list, cam)
	}
	return list
}

// GetCameraHealth retrieves operational health for a specific camera ID.
func (m *Manager) GetCameraHealth(id string) (CameraHealth, error) {
	cam, err := m.GetCamera(id)
	if err != nil {
		return CameraHealth{}, err
	}
	return cam.Health(), nil
}

// ListCameraHealth returns a map of camera ID to CameraHealth for all cameras.
func (m *Manager) ListCameraHealth() map[string]CameraHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string]CameraHealth, len(m.cameras))
	for id, cam := range m.cameras {
		res[id] = cam.Health()
	}
	return res
}
