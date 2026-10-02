package camera

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

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

// normalizeONVIFAddress ensures that the ONVIF HTTP endpoint does not accidentally connect
// to the RTSP port (554) or standard port 80 if the camera runs ONVIF on port 2020 (like Tapo).
func normalizeONVIFAddress(address string) string {
	clean := strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://")
	host, port, err := net.SplitHostPort(clean)
	if err == nil {
		if port == "554" {
			// Port 554 is RTSP. If port 2020 is open, use 2020 for ONVIF.
			conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(host, "2020"), 250*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close()
				return net.JoinHostPort(host, "2020")
			}
			return net.JoinHostPort(host, "80")
		}
		return clean
	}
	// No port specified (e.g. "192.168.1.12")
	conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(clean, "2020"), 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return net.JoinHostPort(clean, "2020")
	}
	return clean
}

// NewCamera initializes a Camera instance from configuration.
func NewCamera(cfg config.CameraConfig) *Camera {
	cam := &Camera{
		Config: cfg,
	}

	if cfg.Address != "" {
		normalizedAddr := normalizeONVIFAddress(cfg.Address)
		cam.onvifDevice = onvif.NewDevice(normalizedAddr, cfg.ONVIFUsername, cfg.ONVIFPassword)
	}

	return cam
}

// authenticatedRTSPURL injects camera ONVIF username/password into the RTSP URL if credentials are not already embedded.
func authenticatedRTSPURL(rawURL, username, password string) string {
	if username == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.User == nil || u.User.Username() == "" {
		u.User = url.UserPassword(username, password)
		return u.String()
	}
	return rawURL
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
	if method == "auto" || method == "rtsp" {
		rtspURL := c.Config.RTSPURL
		if rtspURL == "" && c.onvifDevice != nil {
			if uri, err := c.onvifDevice.GetStreamURI(ctx, c.Config.ProfileToken); err == nil && uri != "" {
				rtspURL = uri
			}
		}

		if rtspURL != "" {
			authURL := authenticatedRTSPURL(rtspURL, c.Config.ONVIFUsername, c.Config.ONVIFPassword)
			data, err := rtsp.CaptureSnapshot(ctx, authURL, rtsp.DefaultSnapshotOptions())
			if err == nil && len(data) > 0 {
				return data, nil
			}
			return nil, err
		}
	}

	return nil, ErrNoSnapshotSource
}

func (c *Camera) resolveProfileToken(ctx context.Context) string {
	if c.Config.ProfileToken != "" {
		return c.Config.ProfileToken
	}
	if c.onvifDevice != nil {
		if profiles, err := c.onvifDevice.GetProfiles(ctx); err == nil && len(profiles) > 0 {
			return profiles[0].Token
		}
	}
	return "profile_1"
}

// PTZMove sends continuous move velocity command.
func (c *Camera) PTZMove(ctx context.Context, pan, tilt, zoom float64) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	return c.onvifDevice.ContinuousMove(ctx, onvif.PTZMoveCommand{
		ProfileToken: c.resolveProfileToken(ctx),
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
	return c.onvifDevice.Stop(ctx, c.resolveProfileToken(ctx))
}

// PTZPreset recalls a preset position.
func (c *Camera) PTZPreset(ctx context.Context, presetToken string) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	return c.onvifDevice.GotoPreset(ctx, onvif.PTZPresetCommand{
		ProfileToken: c.resolveProfileToken(ctx),
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
