package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smford/camstop/internal/api"
	"github.com/smford/camstop/internal/camera"
	"github.com/smford/camstop/internal/config"
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
