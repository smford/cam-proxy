package rtsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmjpeg"
	"github.com/pion/rtp"
)

var (
	ErrNoVideoTrack    = errors.New("no supported video track found in RTSP stream")
	ErrCaptureTimeout  = errors.New("timeout waiting for keyframe in RTSP stream")
	ErrH264Unsupported = errors.New("H.264/H.265 video stream detected; snapshot requires ffmpeg or vlc to be installed, or camera with ONVIF HTTP snapshot / MJPEG support")
)

// SnapshotOptions configures RTSP snapshot extraction.
type SnapshotOptions struct {
	Timeout   time.Duration
	Transport *gortsplib.Protocol
}

// DefaultSnapshotOptions provides 6 second timeout with TCP transport.
func DefaultSnapshotOptions() SnapshotOptions {
	tcp := gortsplib.ProtocolTCP
	return SnapshotOptions{
		Timeout:   6 * time.Second,
		Transport: &tcp, // Prefer TCP to avoid packet drop on edge networks
	}
}

// CaptureSnapshot connects to an RTSP stream on demand, grabs the first complete keyframe,
// and returns the JPEG byte slice.
func CaptureSnapshot(ctx context.Context, rtspURL string, opts SnapshotOptions) ([]byte, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 6 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	u, err := base.ParseURL(rtspURL)
	if err != nil {
		return nil, fmt.Errorf("invalid RTSP URL: %w", err)
	}

	c := &gortsplib.Client{
		Scheme:       u.Scheme,
		Host:         u.Host,
		ReadTimeout:  opts.Timeout,
		WriteTimeout: opts.Timeout,
		Protocol:     opts.Transport,
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("RTSP connect to %s failed: %w", u.Host, err)
	}

	closed := false
	defer func() {
		if !closed {
			c.Close()
		}
	}()

	desc, _, err := c.Describe(u)
	if err != nil {
		return nil, fmt.Errorf("RTSP describe failed: %w", err)
	}

	// 1. Check for MJPEG format (direct JPEG frames over RTP)
	var formaMJPEG *format.MJPEG
	mediMJPEG := desc.FindFormat(&formaMJPEG)
	if mediMJPEG != nil {
		return captureMJPEG(ctx, c, desc, mediMJPEG, formaMJPEG)
	}

	// For H.264, H.265 or other codecs, close the gortsplib RTSP connection
	// before delegating to an external transcoder to prevent session conflict on the camera.
	closed = true
	c.Close()

	// 2. Decode using ffmpeg or vlc
	return captureExternal(ctx, rtspURL)
}

// captureMJPEG depacketizes MJPEG RTP packets directly into raw JPEG bytes.
func captureMJPEG(
	ctx context.Context,
	c *gortsplib.Client,
	desc *description.Session,
	medi *description.Media,
	forma *format.MJPEG,
) ([]byte, error) {
	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		return nil, fmt.Errorf("creating MJPEG decoder: %w", err)
	}

	_, err = c.Setup(desc.BaseURL, medi, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("setting up MJPEG track: %w", err)
	}

	resultChan := make(chan []byte, 1)

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		enc, err2 := rtpDec.Decode(pkt)
		if err2 != nil {
			if !errors.Is(err2, rtpmjpeg.ErrNonStartingPacketAndNoPrevious) && !errors.Is(err2, rtpmjpeg.ErrMorePacketsNeeded) {
				return
			}
			return
		}

		select {
		case resultChan <- enc:
		default:
		}
	})

	if _, err := c.Play(nil); err != nil {
		return nil, fmt.Errorf("starting RTSP play: %w", err)
	}

	select {
	case frame := <-resultChan:
		return frame, nil
	case <-ctx.Done():
		return nil, ErrCaptureTimeout
	}
}

func findExecutable(name string, commonPaths ...string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, p := range commonPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s executable not found", name)
}

func captureExternal(ctx context.Context, rtspURL string) ([]byte, error) {
	var errs []string

	if ffmpegPath, err := findExecutable("ffmpeg", "/usr/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/opt/homebrew/bin/ffmpeg"); err == nil {
		data, err := captureFFmpeg(ctx, ffmpegPath, rtspURL)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		errs = append(errs, fmt.Sprintf("ffmpeg failed: %v", err))
	} else {
		errs = append(errs, "ffmpeg not found")
	}

	if vlcPath, err := findExecutable("vlc", "/opt/homebrew/bin/vlc", "/Applications/VLC.app/Contents/MacOS/VLC", "/usr/local/bin/vlc", "/usr/bin/vlc"); err == nil {
		data, err := captureVLC(ctx, vlcPath, rtspURL)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		errs = append(errs, fmt.Sprintf("vlc failed: %v", err))
	} else {
		errs = append(errs, "vlc not found")
	}

	return nil, fmt.Errorf("%w (%s)", ErrH264Unsupported, strings.Join(errs, "; "))
}

func captureFFmpeg(ctx context.Context, ffmpegPath, rtspURL string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-frames:v", "1",
		"-f", "image2",
		"-",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	data := stdout.Bytes()
	if len(data) == 0 {
		return nil, errors.New("empty frame received")
	}
	return data, nil
}

func captureVLC(ctx context.Context, vlcPath, rtspURL string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "cam-proxy-snap-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	snapPrefix := "snapshot"
	expectedFile := filepath.Join(tmpDir, snapPrefix+".jpeg")

	cmd := exec.CommandContext(ctx, vlcPath,
		"-I", "dummy",
		rtspURL,
		"--vout=dummy",
		"--video-filter=scene",
		"--scene-format=jpeg",
		"--scene-ratio=1",
		"--scene-replace",
		"--scene-prefix="+snapPrefix,
		"--scene-path="+tmpDir,
		"--run-time=3",
		"vlc://quit",
	)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting vlc: %w", err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil, ctx.Err()
		case <-done:
			if data, err := os.ReadFile(expectedFile); err == nil && len(data) > 0 {
				return data, nil
			}
			if data, err := os.ReadFile(filepath.Join(tmpDir, snapPrefix+".jpg")); err == nil && len(data) > 0 {
				return data, nil
			}
			return nil, errors.New("vlc exited without capturing snapshot")
		case <-ticker.C:
			if fi, err := os.Stat(expectedFile); err == nil && fi.Size() > 1024 {
				data, readErr := os.ReadFile(expectedFile)
				if readErr == nil && len(data) > 1024 && bytes.HasPrefix(data, []byte("\xff\xd8")) && bytes.HasSuffix(data, []byte("\xff\xd9")) {
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					<-done
					return data, nil
				}
			}
		}
	}
}
