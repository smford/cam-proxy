package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/smford/cam-proxy/internal/config"
	"github.com/smford/cam-proxy/internal/tui"
)

func TestNewTUI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cameras["cam1"] = config.CameraConfig{
		ID:         "cam1",
		Name:       "Front Door",
		Address:    "192.168.1.10:80",
		PullEvents: true,
	}

	model := tui.New("cam-proxy.yaml", cfg)
	viewOutput := model.View()

	if viewOutput == "" {
		t.Fatalf("expected non-empty TUI view render")
	}
}

func TestSelectableOptionsToggleAndSave(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cameras["cam1"] = config.CameraConfig{
		ID:             "cam1",
		Name:           "Front Door",
		Address:        "192.168.1.10:80",
		SnapshotMethod: "auto",
		PullEvents:     false,
	}

	m := tui.New("cam-proxy.yaml", cfg)

	// 1. Enter edit mode
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	// 2. Tab down to field 6 (Snapshot Method selector)
	for i := 0; i < 6; i++ {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	// Press space to cycle Snapshot Method from 'auto' to 'onvif'
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})

	// 3. Tab down to field 7 (Pull Events selector)
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})

	// Press space to toggle Pull Events from false (Disabled) to true (Enabled)
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})

	// Verify rendered view shows ● Enabled
	view := model.View()
	if !strings.Contains(view, "● Enabled") {
		t.Errorf("expected view to display '● Enabled', got:\n%s", view)
	}
	if !strings.Contains(view, "● onvif") {
		t.Errorf("expected view to display '● onvif', got:\n%s", view)
	}

	// 4. Save form using Ctrl+S
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	// Verify updated config
	updatedCam := cfg.Cameras["cam1"]
	if !updatedCam.PullEvents {
		t.Errorf("expected PullEvents to be true after toggle")
	}
	if updatedCam.SnapshotMethod != "onvif" {
		t.Errorf("expected SnapshotMethod to be 'onvif', got %q", updatedCam.SnapshotMethod)
	}
}

func TestReenteringEditDoesNotBleedIntoPullEvents(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cameras["cam1"] = config.CameraConfig{
		ID:         "cam1",
		Name:       "Front Door",
		Address:    "192.168.1.10:80",
		PullEvents: true,
	}

	m := tui.New("cam-proxy.yaml", cfg)

	// 1. Enter edit mode first time
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	// 2. Tab down through fields to focus "Pull Events" (field 7)
	for i := 0; i < 7; i++ {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	// 3. Press ESC to cancel and return to list view
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// 4. Re-enter edit mode for the second time
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	// 5. Type 'Z' into the first field ("Camera ID")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})

	// Render view and check the inputs
	rendered := model.View()

	lines := strings.Split(rendered, "\n")
	var pullEventsLine string
	for _, l := range lines {
		if strings.Contains(l, "Pull Events:") {
			pullEventsLine = l
			break
		}
	}

	if pullEventsLine == "" {
		t.Fatalf("expected to find 'Pull Events:' in rendered edit view")
	}

	if strings.Contains(pullEventsLine, "Z") {
		t.Errorf("focus leak detected! 'Pull Events' field received keystrokes intended for Camera ID: %s", pullEventsLine)
	}

	if !strings.Contains(pullEventsLine, "Enabled") {
		t.Errorf("expected 'Pull Events' to remain Enabled, got: %s", pullEventsLine)
	}
}
