package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smford/cam-proxy/internal/api"
	"github.com/smford/cam-proxy/internal/config"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /api/v1/cameras/{id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("IMAGE_BYTES"))
	})

	handler := api.SecurityHeadersMiddleware(mux)

	// 1. Regular status endpoint: check nosniff is set, but image cache-control is not forced
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rrStatus := httptest.NewRecorder()
	handler.ServeHTTP(rrStatus, reqStatus)

	if rrStatus.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rrStatus.Code)
	}
	if nosniff := rrStatus.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", nosniff)
	}
	if pragma := rrStatus.Header().Get("Pragma"); pragma != "" {
		t.Errorf("expected empty Pragma on non-snapshot endpoint, got %q", pragma)
	}

	// 2. Snapshot endpoint: check nosniff and dynamic image cache-control
	reqSnap := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/driveway/snapshot", nil)
	rrSnap := httptest.NewRecorder()
	handler.ServeHTTP(rrSnap, reqSnap)

	if rrSnap.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rrSnap.Code)
	}
	if nosniff := rrSnap.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", nosniff)
	}
	if cacheCtrl := rrSnap.Header().Get("Cache-Control"); !strings.Contains(cacheCtrl, "no-cache") || !strings.Contains(cacheCtrl, "no-store") {
		t.Errorf("expected Cache-Control no-cache, no-store, got %q", cacheCtrl)
	}
	if pragma := rrSnap.Header().Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma: no-cache, got %q", pragma)
	}
	if expires := rrSnap.Header().Get("Expires"); expires != "0" {
		t.Errorf("expected Expires: 0, got %q", expires)
	}
}

func TestCORSMiddleware_DefaultWildcard(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/cameras", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("POST /api/v1/cameras/{id}/ptz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	corsCfg := config.DefaultConfig().Server.CORS
	handler := api.CORSMiddleware(corsCfg)(mux)

	// 1. Regular GET request with Origin
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras", nil)
	req.Header.Set("Origin", "http://localhost:8123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", origin)
	}

	// 2. Preflight OPTIONS request for snapshot
	reqPreflight := httptest.NewRequest(http.MethodOptions, "/api/v1/cameras/front_door/snapshot", nil)
	reqPreflight.Header.Set("Origin", "http://localhost:8123")
	reqPreflight.Header.Set("Access-Control-Request-Method", "GET")
	rrPreflight := httptest.NewRecorder()
	handler.ServeHTTP(rrPreflight, reqPreflight)

	if rrPreflight.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for preflight, got %d", rrPreflight.Code)
	}
	if origin := rrPreflight.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", origin)
	}
	methods := rrPreflight.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") || !strings.Contains(methods, "OPTIONS") {
		t.Errorf("expected methods to include GET, POST, OPTIONS, got %q", methods)
	}
	headers := rrPreflight.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Content-Type") || !strings.Contains(headers, "Authorization") {
		t.Errorf("expected headers to include Content-Type, Authorization, got %q", headers)
	}
	if maxAge := rrPreflight.Header().Get("Access-Control-Max-Age"); maxAge != "86400" {
		t.Errorf("expected max-age 86400, got %q", maxAge)
	}
}

func TestCORSMiddleware_AllowedOrigins(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/cameras/{id}/ptz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	corsCfg := config.CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"http://homeassistant.local:8123", "https://dashboard.lan"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	}
	handler := api.CORSMiddleware(corsCfg)(mux)

	// 1. Allowed origin request
	reqAllowed := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam1/ptz", strings.NewReader(`{}`))
	reqAllowed.Header.Set("Origin", "http://homeassistant.local:8123")
	rrAllowed := httptest.NewRecorder()
	handler.ServeHTTP(rrAllowed, reqAllowed)

	if origin := rrAllowed.Header().Get("Access-Control-Allow-Origin"); origin != "http://homeassistant.local:8123" {
		t.Errorf("expected allowed origin to be reflected, got %q", origin)
	}
	if vary := rrAllowed.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary: Origin header, got %q", vary)
	}

	// 2. Disallowed origin request
	reqDisallowed := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam1/ptz", strings.NewReader(`{}`))
	reqDisallowed.Header.Set("Origin", "http://evil.com")
	rrDisallowed := httptest.NewRecorder()
	handler.ServeHTTP(rrDisallowed, reqDisallowed)

	if origin := rrDisallowed.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for disallowed origin, got %q", origin)
	}

	// 3. Preflight with allowed origin
	reqPreflight := httptest.NewRequest(http.MethodOptions, "/api/v1/cameras/cam1/ptz", nil)
	reqPreflight.Header.Set("Origin", "https://dashboard.lan")
	reqPreflight.Header.Set("Access-Control-Request-Method", "POST")
	rrPreflight := httptest.NewRecorder()
	handler.ServeHTTP(rrPreflight, reqPreflight)

	if rrPreflight.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rrPreflight.Code)
	}
	if origin := rrPreflight.Header().Get("Access-Control-Allow-Origin"); origin != "https://dashboard.lan" {
		t.Errorf("expected https://dashboard.lan, got %q", origin)
	}
	if maxAge := rrPreflight.Header().Get("Access-Control-Max-Age"); maxAge != "3600" {
		t.Errorf("expected max-age 3600, got %q", maxAge)
	}
}

func TestCORSMiddleware_AllowCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/cameras", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	corsCfg := config.CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}
	handler := api.CORSMiddleware(corsCfg)(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras", nil)
	req.Header.Set("Origin", "http://my-dashboard.local")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// When credentials are true, wildcard origin must reflect the request origin
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "http://my-dashboard.local" {
		t.Errorf("expected reflected origin when credentials enabled, got %q", origin)
	}
	if creds := rr.Header().Get("Access-Control-Allow-Credentials"); creds != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", creds)
	}
	if vary := rr.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary: Origin when reflecting origin, got %q", vary)
	}
}

func TestCORSMiddleware_Disabled(t *testing.T) {
	mux := http.NewServeMux()
	var reachedInner bool
	mux.HandleFunc("OPTIONS /api/v1/cameras", func(w http.ResponseWriter, r *http.Request) {
		reachedInner = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("CUSTOM_OPTIONS"))
	})

	corsCfg := config.CORSConfig{
		Enabled: false,
	}
	handler := api.CORSMiddleware(corsCfg)(mux)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/cameras", nil)
	req.Header.Set("Origin", "http://localhost:8123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !reachedInner {
		t.Errorf("expected inner handler to be called when CORS is disabled")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected inner status 200, got %d", rr.Code)
	}
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("expected no Access-Control-Allow-Origin when CORS disabled, got %q", origin)
	}
}

func TestServer_EndToEndSecurityAndCORS(t *testing.T) {
	_, mgr, mockServer := setupTestServer()
	defer mockServer.Close()

	cfg := config.DefaultConfig().Server
	server := api.NewServer(cfg, mgr)
	handler := server.Handler()

	// 1. Snapshot request end-to-end
	snapReq := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/driveway/snapshot", nil)
	snapReq.Header.Set("Origin", "http://homeassistant.local:8123")
	snapRR := httptest.NewRecorder()
	handler.ServeHTTP(snapRR, snapReq)

	if snapRR.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for snapshot, got %d (body: %s)", snapRR.Code, snapRR.Body.String())
	}
	if snapRR.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", snapRR.Header().Get("X-Content-Type-Options"))
	}
	if snapRR.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", snapRR.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(snapRR.Header().Get("Cache-Control"), "no-cache") {
		t.Errorf("expected Cache-Control no-cache, got %q", snapRR.Header().Get("Cache-Control"))
	}

	// 2. Preflight PTZ OPTIONS request end-to-end
	ptzOptReq := httptest.NewRequest(http.MethodOptions, "/api/v1/cameras/driveway/ptz", nil)
	ptzOptReq.Header.Set("Origin", "http://homeassistant.local:8123")
	ptzOptReq.Header.Set("Access-Control-Request-Method", "POST")
	ptzOptRR := httptest.NewRecorder()
	handler.ServeHTTP(ptzOptRR, ptzOptReq)

	if ptzOptRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for PTZ preflight, got %d", ptzOptRR.Code)
	}
	if ptzOptRR.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff on preflight, got %q", ptzOptRR.Header().Get("X-Content-Type-Options"))
	}
	if ptzOptRR.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", ptzOptRR.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(ptzOptRR.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("expected POST in allowed methods, got %q", ptzOptRR.Header().Get("Access-Control-Allow-Methods"))
	}
}
