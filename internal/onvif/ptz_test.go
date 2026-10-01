package onvif_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smford/camstop/internal/onvif"
)

func TestPTZCommands(t *testing.T) {
	var lastRequestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastRequestBody = string(body)

		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`))
	}))
	defer server.Close()

	dev := onvif.NewDevice(server.URL, "admin", "pass")
	ctx := context.Background()

	// 1. Test Continuous Move
	err := dev.ContinuousMove(ctx, onvif.PTZMoveCommand{
		ProfileToken: "prof1",
		Pan:          0.75,
		Tilt:         -0.5,
		Zoom:         0.0,
	})
	if err != nil {
		t.Fatalf("unexpected error in ContinuousMove: %v", err)
	}
	if !strings.Contains(lastRequestBody, "ContinuousMove") || !strings.Contains(lastRequestBody, "0.75") {
		t.Errorf("ContinuousMove payload missing expected elements: %s", lastRequestBody)
	}

	// 2. Test Stop
	err = dev.Stop(ctx, "prof1")
	if err != nil {
		t.Fatalf("unexpected error in Stop: %v", err)
	}
	if !strings.Contains(lastRequestBody, "Stop") || !strings.Contains(lastRequestBody, "prof1") {
		t.Errorf("Stop payload missing expected elements: %s", lastRequestBody)
	}

	// 3. Test GotoPreset
	err = dev.GotoPreset(ctx, onvif.PTZPresetCommand{
		ProfileToken: "prof1",
		PresetToken:  "preset_home",
	})
	if err != nil {
		t.Fatalf("unexpected error in GotoPreset: %v", err)
	}
	if !strings.Contains(lastRequestBody, "GotoPreset") || !strings.Contains(lastRequestBody, "preset_home") {
		t.Errorf("GotoPreset payload missing expected elements: %s", lastRequestBody)
	}
}
