package rtsp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmjpeg"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"
)

var (
	ErrNoVideoTrack    = errors.New("no supported video track found in RTSP stream")
	ErrCaptureTimeout  = errors.New("timeout waiting for keyframe in RTSP stream")
	ErrH264Unsupported = errors.New("H.264 stream detected; pure Go decoding to JPEG requires ONVIF HTTP snapshot or MJPEG stream")
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
	defer c.Close()

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

	// 2. Check for H264 format
	var formaH264 *format.H264
	mediH264 := desc.FindFormat(&formaH264)
	if mediH264 != nil {
		return captureH264(ctx, c, desc, mediH264, formaH264)
	}

	return nil, ErrNoVideoTrack
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

// captureH264 waits for an I-frame (IDR random access unit).
func captureH264(
	ctx context.Context,
	c *gortsplib.Client,
	desc *description.Session,
	medi *description.Media,
	forma *format.H264,
) ([]byte, error) {
	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		return nil, fmt.Errorf("creating H264 decoder: %w", err)
	}

	_, err = c.Setup(desc.BaseURL, medi, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("setting up H264 track: %w", err)
	}

	keyframeChan := make(chan [][]byte, 1)

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		au, err2 := rtpDec.Decode(pkt)
		if err2 != nil {
			if !errors.Is(err2, rtph264.ErrNonStartingPacketAndNoPrevious) && !errors.Is(err2, rtph264.ErrMorePacketsNeeded) {
				return
			}
			return
		}

		if h264.IsRandomAccess(au) {
			select {
			case keyframeChan <- au:
			default:
			}
		}
	})

	if _, err := c.Play(nil); err != nil {
		return nil, fmt.Errorf("starting RTSP play: %w", err)
	}

	select {
	case <-keyframeChan:
		// H.264 IDR frame successfully captured from RTSP stream.
		// As pure Go cannot decode H.264 macroblocks without CGO/FFmpeg,
		// cameras should be configured with ONVIF HTTP snapshots (the edge standard),
		// or configured for MJPEG RTSP sub-streams.
		return nil, ErrH264Unsupported
	case <-ctx.Done():
		return nil, ErrCaptureTimeout
	}
}
