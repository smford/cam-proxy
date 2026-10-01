package camera

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/onvif"
	"github.com/smford/camstop/internal/rtsp"
)

var (
	ErrCameraNotFound    = errors.New("camera not found")
	ErrNoSnapshotSource  = errors.New("no snapshot source (ONVIF or RTSP) configured for camera")
	ErrONVIFNotConfigured = errors.New("ONVIF is not configured for this camera")
)

// Camera manages connections, state, and actions for an individual camera.
type Camera struct {
	Config      config.CameraConfig
	onvifDevice *onvif.Device
	mu          sync.RWMutex
}

// NewCamera initializes a Camera instance from configuration.
func NewCamera(cfg config.CameraConfig) *Camera {
	cam := &Camera{
		Config: cfg,
	}

	if cfg.Address != "" {
		cam.onvifDevice = onvif.NewDevice(cfg.Address, cfg.ONVIFUsername, cfg.ONVIFPassword)
	}

	return cam
}

// Snapshot captures a still image using either ONVIF native snapshot or RTSP keyframe extraction.
func (c *Camera) Snapshot(ctx context.Context) ([]byte, error) {
	method := c.Config.SnapshotMethod

	// Strategy 1: ONVIF HTTP Snapshot (Fastest, zero CGO, <100ms)
	if (method == "auto" || method == "onvif") && c.onvifDevice != nil {
		uri, err := c.onvifDevice.GetSnapshotURI(ctx, c.Config.ProfileToken)
		if err == nil {
			resolvedURI, err := c.onvifDevice.ResolveSnapshotURI(uri)
			if err == nil {
				data, err := c.onvifDevice.DownloadSnapshot(ctx, resolvedURI)
				if err == nil && len(data) > 0 {
					return data, nil
				}
				slog.Warn("ONVIF snapshot download failed, falling back to RTSP if available", "camera_id", c.Config.ID, "err", err)
			}
		} else {
			slog.Debug("could not retrieve ONVIF snapshot URI", "camera_id", c.Config.ID, "err", err)
		}
	}

	// Strategy 2: RTSP on-demand connection
	if (method == "auto" || method == "rtsp") && c.Config.RTSPURL != "" {
		return rtsp.CaptureSnapshot(ctx, c.Config.RTSPURL, rtsp.DefaultSnapshotOptions())
	}

	return nil, ErrNoSnapshotSource
}

// PTZMove sends continuous move velocity command.
func (c *Camera) PTZMove(ctx context.Context, pan, tilt, zoom float64) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	return c.onvifDevice.ContinuousMove(ctx, onvif.PTZMoveCommand{
		ProfileToken: c.Config.ProfileToken,
		Pan:          pan,
		Tilt:         tilt,
		Zoom:         zoom,
	})
}

// PTZStop stops continuous PTZ move.
func (c *Camera) PTZStop(ctx context.Context) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	return c.onvifDevice.Stop(ctx, c.Config.ProfileToken)
}

// PTZPreset recalls a preset position.
func (c *Camera) PTZPreset(ctx context.Context, presetToken string) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	return c.onvifDevice.GotoPreset(ctx, onvif.PTZPresetCommand{
		ProfileToken: c.Config.ProfileToken,
		PresetToken:  presetToken,
	})
}

// StartEventListener starts the background PullPoint event loop if enabled.
func (c *Camera) StartEventListener(ctx context.Context, handler onvif.EventHandler) {
	if !c.Config.PullEvents || c.onvifDevice == nil {
		return
	}
	c.onvifDevice.StartEventLoop(ctx, c.Config.ID, handler)
}
