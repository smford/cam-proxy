package rtsp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smford/camstop/internal/rtsp"
)

func TestCaptureSnapshotInvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := rtsp.CaptureSnapshot(ctx, "invalid-url", rtsp.SnapshotOptions{
		Timeout: 1 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected error for invalid RTSP URL, got nil")
	}
}

func TestCaptureSnapshotUnreachable(t *testing.T) {
	ctx := context.Background()
	// Connect to non-routable / non-listening port with short timeout
	_, err := rtsp.CaptureSnapshot(ctx, "rtsp://127.0.0.1:54321/live", rtsp.SnapshotOptions{
		Timeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected connection error for unreachable RTSP server, got nil")
	}

	if !strings.Contains(err.Error(), "RTSP connect") && !strings.Contains(err.Error(), "failed") {
		t.Logf("connection error received as expected: %v", err)
	}
}
