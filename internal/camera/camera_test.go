package camera_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smford/cam-proxy/internal/camera"
	"github.com/smford/cam-proxy/internal/config"
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

func TestCameraRTSPCredentialInjection(t *testing.T) {
	cam := camera.NewCamera(config.CameraConfig{
		ID:             "cam_rtsp_auth",
		RTSPURL:        "rtsp://192.168.1.50:554/stream1",
		ONVIFUsername:  "myuser",
		ONVIFPassword:  "mypass",
		SnapshotMethod: "rtsp",
	})

	// When snapshot is called with unreachable IP, it attempts connection with injected credentials
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := cam.Snapshot(ctx)
	// Should fail with network connect/timeout rather than ErrNoSnapshotSource
	if errors.Is(err, camera.ErrNoSnapshotSource) {
		t.Errorf("expected network attempt, got %v", err)
	}
}

func TestSnapshotSingleFlightCoalescing(t *testing.T) {
	var fetchCount atomic.Int32
	var serverURL string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			fetchCount.Add(1)
			// Simulate network/hardware exposure delay
			time.Sleep(60 * time.Millisecond)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("COALESCED_JPEG_DATA"))
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()
	serverURL = mockServer.URL

	cam := camera.NewCamera(config.CameraConfig{
		ID:             "test_sf_cam",
		Address:        mockServer.URL,
		SnapshotMethod: "onvif",
	})
	// Explicitly disable cache so we isolate and verify singleflight coalescing alone
	cam.SetCacheTTL(0)

	const numCallers = 10
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errorsCh := make(chan error, numCallers)
	resultsCh := make(chan string, numCallers)

	for i := 0; i < numCallers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			data, err := cam.Snapshot(context.Background())
			if err != nil {
				errorsCh <- err
				return
			}
			resultsCh <- string(data)
		}()
	}

	// Release all goroutines simultaneously
	close(startBarrier)
	wg.Wait()
	close(errorsCh)
	close(resultsCh)

	for err := range errorsCh {
		t.Fatalf("unexpected snapshot error in caller: %v", err)
	}

	for res := range resultsCh {
		if res != "COALESCED_JPEG_DATA" {
			t.Errorf("expected COALESCED_JPEG_DATA, got %q", res)
		}
	}

	// Despite 10 concurrent callers, the camera hardware/server should only have been hit once
	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 hardware fetch due to singleflight coalescing, got %d", got)
	}
}

func TestSnapshotTTLFrameCache(t *testing.T) {
	var fetchCount atomic.Int32
	var serverURL string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			count := fetchCount.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = fmt.Fprintf(w, "FRAME_VERSION_%d", count)
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()
	serverURL = mockServer.URL

	cam := camera.NewCamera(config.CameraConfig{
		ID:             "test_cache_cam",
		Address:        mockServer.URL,
		SnapshotMethod: "onvif",
	})
	// Configure short 100ms TTL
	cam.SetCacheTTL(100 * time.Millisecond)

	// First call should miss cache and fetch from server
	f1, err := cam.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot 1 failed: %v", err)
	}
	if string(f1) != "FRAME_VERSION_1" {
		t.Errorf("expected FRAME_VERSION_1, got %q", string(f1))
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected fetchCount 1, got %d", fetchCount.Load())
	}

	// Rapid calls within TTL (100ms) should return cached frame without hitting backend
	for i := 0; i < 5; i++ {
		fCached, err := cam.Snapshot(context.Background())
		if err != nil {
			t.Fatalf("cached snapshot failed: %v", err)
		}
		if string(fCached) != "FRAME_VERSION_1" {
			t.Errorf("expected cached FRAME_VERSION_1, got %q", string(fCached))
		}
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected fetchCount to stay 1 during cache TTL, got %d", fetchCount.Load())
	}

	// Sleep past the TTL
	time.Sleep(120 * time.Millisecond)

	// Now cache should be expired and hit backend again
	f2, err := cam.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot 2 failed: %v", err)
	}
	if string(f2) != "FRAME_VERSION_2" {
		t.Errorf("expected fresh FRAME_VERSION_2, got %q", string(f2))
	}
	if fetchCount.Load() != 2 {
		t.Fatalf("expected fetchCount 2 after expiration, got %d", fetchCount.Load())
	}
}

func TestSnapshotCacheInvalidationOnPTZ(t *testing.T) {
	var fetchCount atomic.Int32
	var serverURL string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			count := fetchCount.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = fmt.Fprintf(w, "PTZ_FRAME_%d", count)
			return
		}

		if r.URL.Path == "/onvif/ptz_service" {
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`))
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()
	serverURL = mockServer.URL

	cam := camera.NewCamera(config.CameraConfig{
		ID:             "test_ptz_cache_cam",
		Address:        mockServer.URL,
		SnapshotMethod: "onvif",
	})
	// Long TTL (1 minute) so it won't expire on its own during test
	cam.SetCacheTTL(1 * time.Minute)

	// Fetch 1: misses cache, gets frame 1
	f1, err := cam.Snapshot(context.Background())
	if err != nil || string(f1) != "PTZ_FRAME_1" {
		t.Fatalf("expected PTZ_FRAME_1, got %q (err: %v)", string(f1), err)
	}

	// Verify cached
	fCached, err := cam.Snapshot(context.Background())
	if err != nil || string(fCached) != "PTZ_FRAME_1" {
		t.Fatalf("expected cached PTZ_FRAME_1, got %q (err: %v)", string(fCached), err)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected fetchCount 1, got %d", fetchCount.Load())
	}

	// Issue PTZ Move: must invalidate cache
	if err := cam.PTZMove(context.Background(), 0.5, 0.0, 0.0); err != nil {
		t.Fatalf("PTZMove failed: %v", err)
	}

	// Next snapshot must fetch fresh frame
	f2, err := cam.Snapshot(context.Background())
	if err != nil || string(f2) != "PTZ_FRAME_2" {
		t.Fatalf("expected fresh PTZ_FRAME_2 after move, got %q (err: %v)", string(f2), err)
	}
	if fetchCount.Load() != 2 {
		t.Fatalf("expected fetchCount 2 after move invalidation, got %d", fetchCount.Load())
	}

	// Issue PTZ Stop: must invalidate cache
	if err := cam.PTZStop(context.Background()); err != nil {
		t.Fatalf("PTZStop failed: %v", err)
	}

	// Next snapshot must fetch fresh frame
	f3, err := cam.Snapshot(context.Background())
	if err != nil || string(f3) != "PTZ_FRAME_3" {
		t.Fatalf("expected fresh PTZ_FRAME_3 after stop, got %q (err: %v)", string(f3), err)
	}
	if fetchCount.Load() != 3 {
		t.Fatalf("expected fetchCount 3 after stop invalidation, got %d", fetchCount.Load())
	}

	// Issue PTZ Preset: must invalidate cache
	if err := cam.PTZPreset(context.Background(), "preset_1"); err != nil {
		t.Fatalf("PTZPreset failed: %v", err)
	}

	// Next snapshot must fetch fresh frame
	f4, err := cam.Snapshot(context.Background())
	if err != nil || string(f4) != "PTZ_FRAME_4" {
		t.Fatalf("expected fresh PTZ_FRAME_4 after preset, got %q (err: %v)", string(f4), err)
	}
	if fetchCount.Load() != 4 {
		t.Fatalf("expected fetchCount 4 after preset invalidation, got %d", fetchCount.Load())
	}
}

func TestSnapshotSingleFlightContextCancellation(t *testing.T) {
	var serverURL string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			time.Sleep(100 * time.Millisecond)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("ASYNC_JPEG_DATA"))
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()
	serverURL = mockServer.URL

	cam := camera.NewCamera(config.CameraConfig{
		ID:             "test_cancel_cam",
		Address:        mockServer.URL,
		SnapshotMethod: "onvif",
	})
	cam.SetCacheTTL(0)

	var wg sync.WaitGroup
	wg.Add(2)

	// Caller 1: short timeout that will expire while the 100ms fetch is in progress
	var errCaller1 error
	go func() {
		defer wg.Done()
		ctx1, cancel1 := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel1()
		_, errCaller1 = cam.Snapshot(ctx1)
	}()

	// Caller 2: long timeout that will wait for the 100ms fetch to complete
	var dataCaller2 []byte
	var errCaller2 error
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // Ensure caller 1 starts the flight first
		ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel2()
		dataCaller2, errCaller2 = cam.Snapshot(ctx2)
	}()

	wg.Wait()

	// Caller 1 must receive context deadline exceeded
	if !errors.Is(errCaller1, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded for caller 1, got %v", errCaller1)
	}

	// Caller 2 must succeed despite caller 1's cancellation
	if errCaller2 != nil {
		t.Fatalf("expected caller 2 to succeed, got error: %v", errCaller2)
	}
	if string(dataCaller2) != "ASYNC_JPEG_DATA" {
		t.Errorf("expected ASYNC_JPEG_DATA for caller 2, got %q", string(dataCaller2))
	}
}

func TestManagerCacheTTLConfiguration(t *testing.T) {
	customTTL := 250 * time.Millisecond
	zeroTTL := time.Duration(0)

	cfg := &config.Config{
		Server: config.ServerConfig{
			SnapshotCacheTTL: 2 * time.Second,
		},
		Cameras: map[string]config.CameraConfig{
			"cam_inherited": {
				ID: "cam_inherited",
			},
			"cam_custom": {
				ID:               "cam_custom",
				SnapshotCacheTTL: &customTTL,
			},
			"cam_disabled": {
				ID:               "cam_disabled",
				SnapshotCacheTTL: &zeroTTL,
			},
		},
	}

	mgr := camera.NewManager(cfg, nil)

	camInherited, err := mgr.GetCamera("cam_inherited")
	if err != nil {
		t.Fatalf("failed to get cam_inherited: %v", err)
	}
	if camInherited.CacheTTL() != 2*time.Second {
		t.Errorf("expected 2s inherited TTL, got %v", camInherited.CacheTTL())
	}

	camCustom, err := mgr.GetCamera("cam_custom")
	if err != nil {
		t.Fatalf("failed to get cam_custom: %v", err)
	}
	if camCustom.CacheTTL() != 250*time.Millisecond {
		t.Errorf("expected 250ms custom TTL, got %v", camCustom.CacheTTL())
	}

	camDisabled, err := mgr.GetCamera("cam_disabled")
	if err != nil {
		t.Fatalf("failed to get cam_disabled: %v", err)
	}
	if camDisabled.CacheTTL() != 0 {
		t.Errorf("expected 0s disabled TTL, got %v", camDisabled.CacheTTL())
	}
}
