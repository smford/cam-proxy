package tui_test

import (
	"testing"

	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/tui"
)

func TestNewTUI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cameras["cam1"] = config.CameraConfig{
		ID:      "cam1",
		Name:    "Front Door",
		Address: "192.168.1.10:80",
	}

	model := tui.New("camstop.yaml", cfg)
	viewOutput := model.View()

	if viewOutput == "" {
		t.Fatalf("expected non-empty TUI view render")
	}
}
