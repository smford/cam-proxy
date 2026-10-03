package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smford/cam-proxy/internal/api"
	"github.com/smford/cam-proxy/internal/camera"
	"github.com/smford/cam-proxy/internal/config"
)

func setupTestServer() (*http.ServeMux, *camera.Manager, *httptest.Server) {
	// Mock ONVIF server for driveway
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/onvif/media_service" {
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			resp := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <trt:Uri>%s/snap.jpg</trt:Uri>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`, mockServer.URL)
			_, _ = fmt.Fprint(w, resp)
			return
		}

		if r.URL.Path == "/snap.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("MOCK_JPEG_PAYLOAD"))
			return
		}

		if r.URL.Path == "/onvif/ptz_service" {
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`))
			return
		}

		http.NotFound(w, r)
	}))

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Cameras: map[string]config.CameraConfig{
			"driveway": {
				ID:             "driveway",
				Name:           "Driveway Cam",
				Address:        mockServer.URL,
				SnapshotMethod: "onvif",
			},
			"no_onvif": {
				ID:             "no_onvif",
				Name:           "Camera Without ONVIF",
				SnapshotMethod: "auto",
			},
		},
	}
	mgr := camera.NewManager(cfg, nil)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, mgr, time.Now())
	return mux, mgr, mockServer
}

func TestHealthz(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}

	cams, ok := body["cameras"].(map[string]any)
	if !ok {
		t.Fatalf("expected cameras map in healthz response")
	}
	if _, ok := cams["driveway"]; !ok {
		t.Errorf("expected driveway camera in healthz cameras")
	}
}

func TestStatusEndpoint(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}
	if _, ok := body["cameras"]; !ok {
		t.Errorf("expected cameras map in status response")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	mux, mgr, mockServer := setupTestServer()
	defer mockServer.Close()

	// Perform a snapshot to trigger snapshot metrics
	snapReq := httptest.NewRequest("GET", "/api/v1/cameras/driveway/snapshot", nil)
	snapRR := httptest.NewRecorder()
	mux.ServeHTTP(snapRR, snapReq)

	// Record an MQTT event
	mgr.Metrics().RecordMQTTEvent("driveway", "published")

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /metrics, got %d", rr.Code)
	}

	body := rr.Body.String()
	requiredMetrics := []string{
		"cam_proxy_snapshots_total",
		"cam_proxy_snapshot_duration_seconds",
		"cam_proxy_camera_online",
		"cam_proxy_mqtt_events_total",
	}

	for _, metricName := range requiredMetrics {
		if !strings.Contains(body, metricName) {
			t.Errorf("/metrics response missing expected metric %s\nBody:\n%s", metricName, body)
		}
	}
}

func TestOpenAPISpecEndpoint(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	for _, endpoint := range []string{"/openapi.yaml", "/api/v1/openapi.yaml"} {
		req := httptest.NewRequest("GET", endpoint, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK for %s, got %d", endpoint, rr.Code)
		}

		contentType := rr.Header().Get("Content-Type")
		if !strings.Contains(contentType, "application/yaml") {
			t.Errorf("expected application/yaml for %s, got %s", endpoint, contentType)
		}

		body := rr.Body.String()
		if !strings.Contains(body, "openapi: 3.1.0") {
			t.Errorf("expected openapi: 3.1.0 in spec from %s", endpoint)
		}
		if !strings.Contains(body, "/api/v1/cameras/{id}/snapshot:") {
			t.Errorf("expected snapshot endpoint in spec from %s", endpoint)
		}
	}
}

func TestListCameras(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	req := httptest.NewRequest("GET", "/api/v1/cameras", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var cams []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&cams); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(cams) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cams))
	}
}

func TestSnapshotSuccess(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	req := httptest.NewRequest("GET", "/api/v1/cameras/driveway/snapshot", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	if rr.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %s", rr.Header().Get("Content-Type"))
	}

	if rr.Body.String() != "MOCK_JPEG_PAYLOAD" {
		t.Errorf("expected MOCK_JPEG_PAYLOAD, got %q", rr.Body.String())
	}
}

func TestSnapshotAPICachingAndCoalescing(t *testing.T) {
	var snapCount atomic.Int32
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/onvif/media_service" {
			w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
			resp := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <trt:Uri>%s/snap.jpg</trt:Uri>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`, mockServer.URL)
			_, _ = fmt.Fprint(w, resp)
			return
		}

		if r.URL.Path == "/snap.jpg" {
			snapCount.Add(1)
			time.Sleep(40 * time.Millisecond)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("HTTP_TEST_IMAGE"))
			return
		}

		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ttl := 200 * time.Millisecond
	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8080, SnapshotCacheTTL: 1 * time.Second},
		Cameras: map[string]config.CameraConfig{
			"cam1": {
				ID:               "cam1",
				Address:          mockServer.URL,
				SnapshotMethod:   "onvif",
				SnapshotCacheTTL: &ttl,
			},
		},
	}
	mgr := camera.NewManager(cfg, nil)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, mgr, time.Now())

	// 5 concurrent HTTP snapshot requests through the router
	const concurrency = 5
	var wg sync.WaitGroup
	barrier := make(chan struct{})
	statusCodes := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			req := httptest.NewRequest("GET", "/api/v1/cameras/cam1/snapshot", nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			statusCodes[idx] = rr.Code
		}()
	}

	close(barrier)
	wg.Wait()

	for _, code := range statusCodes {
		if code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", code)
		}
	}

	// All 5 concurrent HTTP requests coalesced into 1
	if got := snapCount.Load(); got != 1 {
		t.Fatalf("expected 1 fetch for 5 concurrent requests, got %d", got)
	}

	// Immediate follow-up request hits TTL cache
	req := httptest.NewRequest("GET", "/api/v1/cameras/cam1/snapshot", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cached request, got %d", rr.Code)
	}
	if got := snapCount.Load(); got != 1 {
		t.Fatalf("expected fetch count to remain 1 due to TTL cache, got %d", got)
	}
}

func TestSnapshotNotFound(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	req := httptest.NewRequest("GET", "/api/v1/cameras/nonexistent/snapshot", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown camera, got %d", rr.Code)
	}
}

func TestPTZMoveSuccess(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	payload := `{"action": "move", "pan": 0.5, "tilt": 0.0, "zoom": 0.0}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/driveway/ptz", strings.NewReader(payload))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestPTZStopSuccess(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	payload := `{"action": "stop"}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/driveway/ptz", strings.NewReader(payload))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestPTZPresetMissingToken(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	payload := `{"action": "preset"}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/driveway/ptz", strings.NewReader(payload))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", rr.Code)
	}
}

func TestPTZNotConfigured(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	payload := `{"action": "stop"}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/no_onvif/ptz", strings.NewReader(payload))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 Not Implemented, got %d", rr.Code)
	}
}

func TestPTZInvalidAction(t *testing.T) {
	mux, _, mockServer := setupTestServer()
	defer mockServer.Close()

	payload := `{"action": "teleport"}`
	req := httptest.NewRequest("POST", "/api/v1/cameras/driveway/ptz", strings.NewReader(payload))
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid action, got %d", rr.Code)
	}
}
