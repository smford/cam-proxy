package camera_test

import (
	"context"
	"errors"
	"testing"

	"github.com/smford/camstop/internal/camera"
	"github.com/smford/camstop/internal/config"
)

func TestManagerOperations(t *testing.T) {
	cfg := &config.Config{
		Cameras: map[string]config.CameraConfig{
			"cam1": {ID: "cam1", Name: "Front Door"},
			"cam2": {ID: "cam2", Name: "Backyard"},
		},
	}

	mgr := camera.NewManager(cfg, nil)

	// List cameras
	cams := mgr.ListCameras()
	if len(cams) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cams))
	}

	// Get existing camera
	cam1, err := mgr.GetCamera("cam1")
	if err != nil {
		t.Fatalf("failed to get cam1: %v", err)
	}
	if cam1.Config.Name != "Front Door" {
		t.Errorf("expected 'Front Door', got %q", cam1.Config.Name)
	}

	// Get non-existent camera
	_, err = mgr.GetCamera("unknown")
	if !errors.Is(err, camera.ErrCameraNotFound) {
		t.Errorf("expected ErrCameraNotFound, got %v", err)
	}

	// Start without errors
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("manager.Start returned error: %v", err)
	}
}
