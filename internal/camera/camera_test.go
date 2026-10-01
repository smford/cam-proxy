package camera_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smford/camstop/internal/camera"
	"github.com/smford/camstop/internal/config"
)

func TestCameraNoSource(t *testing.T) {
	cam := camera.NewCamera(config.CameraConfig{
		ID: "cam_empty",
	})

	_, err := cam.Snapshot(context.Background())
	if !errors.Is(err, camera.ErrNoSnapshotSource) {
		t.Errorf("expected ErrNoSnapshotSource, got %v", err)
	}

	err = cam.PTZMove(context.Background(), 0.1, 0.1, 0.0)
	if !errors.Is(err, camera.ErrONVIFNotConfigured) {
		t.Errorf("expected ErrONVIFNotConfigured, got %v", err)
	}

	err = cam.PTZStop(context.Background())
	if !errors.Is(err, camera.ErrONVIFNotConfigured) {
		t.Errorf("expected ErrONVIFNotConfigured, got %v", err)
	}

	err = cam.PTZPreset(context.Background(), "1")
	if !errors.Is(err, camera.ErrONVIFNotConfigured) {
		t.Errorf("expected ErrONVIFNotConfigured, got %v", err)
	}
}

func TestCameraONVIFSnapshotSuccess(t *testing.T) {
	// Mock ONVIF server providing snapshot URI and serving the JPEG
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/onvif/media_service" {
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			resp := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <trt:Uri>%s/snapshot.jpg</trt:Uri>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`, serverURL)
			_, _ = fmt.Fprint(w, resp)
			return
		}

		if r.URL.Path == "/snapshot.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("FAKE_JPEG_IMAGE_BYTES"))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	cam := camera.NewCamera(config.CameraConfig{
		ID:             "cam_onvif",
		Address:        server.URL,
		SnapshotMethod: "onvif",
	})

	data, err := cam.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("unexpected snapshot error: %v", err)
	}

	if string(data) != "FAKE_JPEG_IMAGE_BYTES" {
		t.Errorf("expected FAKE_JPEG_IMAGE_BYTES, got %q", string(data))
	}
}
