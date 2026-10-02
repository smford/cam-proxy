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

		// Strategy 1b: If ONVIF HTTP snapshot is not supported by camera firmware (e.g. Tapo),
		// check if ONVIF exposes an MJPEG video stream (e.g. Tapo stream8)
		if method == "auto" {
			if mjpegStreamURI, err := c.onvifDevice.GetMJPEGStreamURI(ctx); err == nil && mjpegStreamURI != "" {
				authMJPEG := authenticatedRTSPURL(mjpegStreamURI, c.Config.ONVIFUsername, c.Config.ONVIFPassword)
				data, err := rtsp.CaptureSnapshot(ctx, authMJPEG, rtsp.DefaultSnapshotOptions())
				if err == nil && len(data) > 0 {
					return data, nil
				}
				slog.Debug("ONVIF MJPEG stream capture failed", "camera_id", c.Config.ID, "err", err)
			}
		}
	}

	// Strategy 2: RTSP on-demand connection
	if (method == "auto" || method == "rtsp") && c.Config.RTSPURL != "" {
		authURL := authenticatedRTSPURL(c.Config.RTSPURL, c.Config.ONVIFUsername, c.Config.ONVIFPassword)
		data, err := rtsp.CaptureSnapshot(ctx, authURL, rtsp.DefaultSnapshotOptions())
		if err == nil && len(data) > 0 {
			return data, nil
		}

		// Fallback for cameras (like TP-Link Tapo) where stream1 / stream2 are H.264,
		// or where /live was configured instead of /stream1 or /stream8
		if (errors.Is(err, rtsp.ErrH264Unsupported) || strings.Contains(err.Error(), "404")) &&
			(strings.Contains(authURL, "/stream1") || strings.Contains(authURL, "/stream2") || strings.Contains(authURL, "/live")) {
			for _, altPath := range []string{"/stream8", "/stream1"} {
				altURL := authURL
				for _, old := range []string{"/stream1", "/stream2", "/live"} {
					if strings.Contains(altURL, old) {
						altURL = strings.Replace(altURL, old, altPath, 1)
						break
					}
				}
				if altURL != authURL {
					dataAlt, errAlt := rtsp.CaptureSnapshot(ctx, altURL, rtsp.DefaultSnapshotOptions())
					if errAlt == nil && len(dataAlt) > 0 {
						return dataAlt, nil
					}
				}
			}
		}

		return nil, err
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
