package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/tui"
)

func TestNewTUI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Cameras["cam1"] = config.CameraConfig{
		ID:         "cam1",
		Name:       "Front Door",
		Address:    "192.168.1.10:80",
		PullEvents: true,
	}

	model := tui.New("camstop.yaml", cfg)
	viewOutput := model.View()

	if viewOutput == "" {
		t.Fatalf("expected non-empty TUI view render")
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

	m := tui.New("camstop.yaml", cfg)

	// 1. Enter edit mode first time
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	// 2. Tab down through all fields to focus "Pull Events" (field 7)
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

	// Verify that Pull Events still renders "true" and does NOT contain 'Z'
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

	if !strings.Contains(pullEventsLine, "true") {
		t.Errorf("expected 'Pull Events' to remain 'true', got: %s", pullEventsLine)
	}
}
