package camera

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/mqtt"
	"github.com/smford/camstop/internal/onvif"
)

// Manager coordinates all configured cameras.
type Manager struct {
	mu        sync.RWMutex
	cameras   map[string]*Camera
	publisher *mqtt.Publisher
}

// NewManager creates a camera manager.
func NewManager(cfg *config.Config, publisher *mqtt.Publisher) *Manager {
	m := &Manager{
		cameras:   make(map[string]*Camera),
		publisher: publisher,
	}

	for id, camCfg := range cfg.Cameras {
		m.cameras[id] = NewCamera(camCfg)
	}

	return m
}

// Start begins background services like event subscriptions.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, cam := range m.cameras {
		if cam.Config.PullEvents {
			slog.Info("starting ONVIF event subscription", "camera_id", id)
			cam.StartEventListener(ctx, func(ev onvif.Event) {
				if m.publisher != nil {
					if err := m.publisher.PublishEvent(ev); err != nil {
						slog.Error("failed to publish MQTT event", "camera_id", ev.CameraID, "err", err)
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
