package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/smford/cam-proxy/internal/camera"
)

//go:embed openapi.yaml
var openAPISpec []byte

type PTZRequest struct {
	Action      string  `json:"action"` // "move", "stop", "preset"
	Pan         float64 `json:"pan,omitempty"`
	Tilt        float64 `json:"tilt,omitempty"`
	Zoom        float64 `json:"zoom,omitempty"`
	PresetToken string  `json:"preset_token,omitempty"`
}

// RegisterRoutes attaches REST endpoints using Go 1.22+ method + path routing.
func RegisterRoutes(mux *http.ServeMux, mgr *camera.Manager, startTime time.Time) {
	mux.HandleFunc("GET /healthz", handleHealthz(mgr, startTime))
	mux.HandleFunc("GET /api/v1/status", handleStatus(mgr, startTime))
	mux.HandleFunc("GET /api/v1/cameras", handleListCameras(mgr))
	mux.HandleFunc("GET /api/v1/cameras/{id}/snapshot", handleSnapshot(mgr))
	mux.HandleFunc("POST /api/v1/cameras/{id}/ptz", handlePTZ(mgr))
	mux.HandleFunc("GET /openapi.yaml", handleOpenAPISpec)
	mux.HandleFunc("GET /api/v1/openapi.yaml", handleOpenAPISpec)
	if mgr != nil && mgr.Metrics() != nil {
		mux.Handle("GET /metrics", mgr.Metrics().Handler())
	}
}

// handleOpenAPISpec serves the embedded OpenAPI 3.1.0 specification.
func handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpec)
}

// handleHealthz reports system health, camera count, process uptime, and per-camera operational health.
func handleHealthz(mgr *camera.Manager, startTime time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cams := mgr.ListCameras()
		healthMap := mgr.ListCameraHealth()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "ok",
			"uptime":       time.Since(startTime).String(),
			"camera_count": len(cams),
			"cameras":      healthMap,
		})
	}
}

// handleStatus reports detailed operational health at /api/v1/status.
func handleStatus(mgr *camera.Manager, startTime time.Time) http.HandlerFunc {
	return handleHealthz(mgr, startTime)
}

// handleListCameras returns basic status and metadata for all configured cameras.
func handleListCameras(mgr *camera.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cams := mgr.ListCameras()
		type CamSummary struct {
			ID             string              `json:"id"`
			Name           string              `json:"name"`
			Address        string              `json:"address"`
			SnapshotMethod string              `json:"snapshot_method"`
			EventsEnabled  bool                `json:"events_enabled"`
			Health         camera.CameraHealth `json:"health"`
		}

		out := make([]CamSummary, 0, len(cams))
		for _, c := range cams {
			out = append(out, CamSummary{
				ID:             c.Config.ID,
				Name:           c.Config.Name,
				Address:        c.Config.Address,
				SnapshotMethod: c.Config.SnapshotMethod,
				EventsEnabled:  c.Config.PullEvents,
				Health:         c.Health(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// handleSnapshot extracts an on-demand snapshot and serves it as image/jpeg.
func handleSnapshot(mgr *camera.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameraID := r.PathValue("id")
		if cameraID == "" {
			http.Error(w, "missing camera id", http.StatusBadRequest)
			return
		}

		cam, err := mgr.GetCamera(cameraID)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// Use request context with timeout
		imgBytes, err := cam.Snapshot(r.Context())
		if err != nil {
			slog.Error("snapshot capture failed", "camera_id", cameraID, "err", err)
			http.Error(w, fmt.Sprintf("failed to capture snapshot: %v", err), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(imgBytes)))
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(imgBytes)
	}
}

// handlePTZ dispatches PTZ commands (move, stop, preset) to the target camera.
func handlePTZ(mgr *camera.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameraID := r.PathValue("id")
		cam, err := mgr.GetCamera(cameraID)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		var req PTZRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var ptzErr error

		switch req.Action {
		case "move":
			ptzErr = cam.PTZMove(ctx, req.Pan, req.Tilt, req.Zoom)
		case "stop":
			ptzErr = cam.PTZStop(ctx)
		case "preset":
			if req.PresetToken == "" {
				http.Error(w, "preset_token is required for preset action", http.StatusBadRequest)
				return
			}
			ptzErr = cam.PTZPreset(ctx, req.PresetToken)
		default:
			http.Error(w, "unsupported PTZ action; use 'move', 'stop', or 'preset'", http.StatusBadRequest)
			return
		}

		if ptzErr != nil {
			if errors.Is(ptzErr, camera.ErrONVIFNotConfigured) {
				http.Error(w, "PTZ unavailable: ONVIF not configured", http.StatusNotImplemented)
				return
			}
			slog.Error("PTZ execution failed", "camera_id", cameraID, "action", req.Action, "err", ptzErr)
			http.Error(w, fmt.Sprintf("PTZ error: %v", ptzErr), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "success",
			"action": req.Action,
		})
	}
}
