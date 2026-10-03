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

	"golang.org/x/sync/singleflight"

	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/metrics"
	"github.com/smford/cam-proxy/internal/onvif"
	"github.com/smford/cam-proxy/internal/rtsp"
)

var (
	ErrCameraNotFound     = errors.New("camera not found")
	ErrNoSnapshotSource   = errors.New("no snapshot source (ONVIF or RTSP) configured for camera")
	ErrONVIFNotConfigured = errors.New("ONVIF is not configured for this camera")
)

// CameraHealth holds per-camera operational health metrics and status.
type CameraHealth struct {
	ID                    string     `json:"id"`
	Online                bool       `json:"online"`
	LastSeen              *time.Time `json:"last_seen,omitempty"`
	LastSnapshotLatencyMS int64      `json:"last_snapshot_latency_ms"`
	LastError             string     `json:"last_error,omitempty"`
}

// Camera manages connections, state, and actions for an individual camera.
type Camera struct {
	Config      config.CameraConfig
	onvifDevice *onvif.Device
	mu          sync.RWMutex

	sf          singleflight.Group
	cacheMu     sync.RWMutex
	cachedFrame []byte
	cachedTime  time.Time
	cacheTTL    time.Duration
	cacheGen    uint64

	metrics *metrics.Metrics

	healthMu              sync.RWMutex
	online                bool
	lastSeen              time.Time
	lastSnapshotLatencyMS int64
	lastError             string
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
	ttl := 1 * time.Second
	if cfg.SnapshotCacheTTL != nil {
		ttl = *cfg.SnapshotCacheTTL
	}

	cam := &Camera{
		Config:   cfg,
		cacheTTL: ttl,
	}

	if cfg.Address != "" {
		normalizedAddr := normalizeONVIFAddress(cfg.Address)
		cam.onvifDevice = onvif.NewDevice(normalizedAddr, cfg.ONVIFUsername, cfg.ONVIFPassword)
	}

	return cam
}

// SetMetrics attaches a Prometheus metrics collector to this camera.
func (c *Camera) SetMetrics(m *metrics.Metrics) {
	c.metrics = m
}

// SetCacheTTL configures the snapshot in-memory cache TTL for this camera.
// Set to 0 to disable snapshot caching.
func (c *Camera) SetCacheTTL(ttl time.Duration) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cacheTTL = ttl
}

// CacheTTL returns the currently configured cache TTL for this camera.
func (c *Camera) CacheTTL() time.Duration {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.cacheTTL
}

// InvalidateCache invalidates any currently cached snapshot frame and forgets in-flight requests.
func (c *Camera) InvalidateCache() {
	c.cacheMu.Lock()
	c.cachedFrame = nil
	c.cachedTime = time.Time{}
	c.cacheGen++
	c.cacheMu.Unlock()
	c.sf.Forget("snapshot")
}

func (c *Camera) recordSuccess(d time.Duration) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.online = true
	c.lastSeen = time.Now()
	c.lastSnapshotLatencyMS = d.Milliseconds()
	c.lastError = ""
}

func (c *Camera) recordFailure(err error) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.online = false
	if err != nil {
		c.lastError = err.Error()
	}
}

// Health returns the operational health status of this camera.
func (c *Camera) Health() CameraHealth {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	h := CameraHealth{
		ID:                    c.Config.ID,
		Online:                c.online,
		LastSnapshotLatencyMS: c.lastSnapshotLatencyMS,
		LastError:             c.lastError,
	}
	if !c.lastSeen.IsZero() {
		t := c.lastSeen
		h.LastSeen = &t
	}
	return h
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
// Concurrent snapshot requests share a single in-flight camera connection via singleflight,
// and recent frames are returned immediately from memory if within cache TTL.
func (c *Camera) Snapshot(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Fast path: check cached frame under read lock
	c.cacheMu.RLock()
	ttl := c.cacheTTL
	if ttl > 0 && len(c.cachedFrame) > 0 && time.Since(c.cachedTime) < ttl {
		frame := make([]byte, len(c.cachedFrame))
		copy(frame, c.cachedFrame)
		c.cacheMu.RUnlock()
		if c.metrics != nil {
			c.metrics.RecordSnapshot(c.Config.ID, "success", "cache", 0)
		}
		return frame, nil
	}
	gen := c.cacheGen
	c.cacheMu.RUnlock()

	// Coalesce concurrent fetches using singleflight
	ch := c.sf.DoChan("snapshot", func() (any, error) {
		// Hardware fetch with bounded safety timeout
		captureCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		start := time.Now()
		data, err := c.capture(captureCtx)
		duration := time.Since(start)

		if err != nil {
			c.recordFailure(err)
			if c.metrics != nil {
				c.metrics.RecordSnapshot(c.Config.ID, "error", "hardware", duration.Seconds())
			}
			return nil, err
		}

		c.recordSuccess(duration)
		if c.metrics != nil {
			c.metrics.RecordSnapshot(c.Config.ID, "success", "hardware", duration.Seconds())
		}

		c.cacheMu.Lock()
		if c.cacheGen == gen && c.cacheTTL > 0 {
			c.cachedFrame = data
			c.cachedTime = time.Now()
		}
		c.cacheMu.Unlock()

		return data, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-ch:
		if !ok {
			return nil, errors.New("snapshot channel closed")
		}
		if res.Err != nil {
			return nil, res.Err
		}
		data, ok := res.Val.([]byte)
		if !ok {
			return nil, errors.New("unexpected snapshot data type")
		}
		frame := make([]byte, len(data))
		copy(frame, data)
		return frame, nil
	}
}

// capture fetches a fresh snapshot directly from configured camera sources (ONVIF or RTSP).
func (c *Camera) capture(ctx context.Context) ([]byte, error) {
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
	c.InvalidateCache()
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
	c.InvalidateCache()
	return c.onvifDevice.Stop(ctx, c.resolveProfileToken(ctx))
}

// PTZPreset recalls a preset position.
func (c *Camera) PTZPreset(ctx context.Context, presetToken string) error {
	if c.onvifDevice == nil {
		return ErrONVIFNotConfigured
	}
	c.InvalidateCache()
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
	c.onvifDevice.StartEventLoop(ctx, c.Config.ID, func(ev onvif.Event) {
		c.healthMu.Lock()
		c.online = true
		c.lastSeen = time.Now()
		c.healthMu.Unlock()
		if c.metrics != nil {
			c.metrics.SetCameraOnline(c.Config.ID, true)
		}
		handler(ev)
	})
}
