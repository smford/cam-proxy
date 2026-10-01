package onvif_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smford/camstop/internal/onvif"
)

func TestBuildWSSecurityHeader(t *testing.T) {
	dev := onvif.NewDevice("http://192.168.1.100", "admin", "secret123")
	dev.TimeOffset = 10 * time.Minute

	header := dev.BuildWSSecurityHeader()
	if !strings.Contains(header, "<wsse:Username>admin</wsse:Username>") {
		t.Errorf("expected username in header, got: %s", header)
	}
	if !strings.Contains(header, "PasswordDigest") {
		t.Errorf("expected PasswordDigest in header, got: %s", header)
	}
	if !strings.Contains(header, "<wsse:Nonce") {
		t.Errorf("expected Nonce in header, got: %s", header)
	}
	if !strings.Contains(header, "<wsu:Created>") {
		t.Errorf("expected Created timestamp in header, got: %s", header)
	}

	// Empty username should return empty header
	anonDev := onvif.NewDevice("http://192.168.1.100", "", "")
	if anonDev.BuildWSSecurityHeader() != "" {
		t.Errorf("expected empty header for anonymous device")
	}
}

func TestResolveSnapshotURI(t *testing.T) {
	dev := onvif.NewDevice("http://192.168.1.50:8080/onvif/device_service", "admin", "pass")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already absolute",
			input:    "http://192.168.1.50:8080/cgi-bin/snapshot.jpg",
			expected: "http://192.168.1.50:8080/cgi-bin/snapshot.jpg",
		},
		{
			name:     "relative path with leading slash",
			input:    "/onvif/snapshot.jpg",
			expected: "http://192.168.1.50:8080/onvif/snapshot.jpg",
		},
		{
			name:     "relative path without leading slash",
			input:    "snapshot.jpg",
			expected: "http://192.168.1.50:8080/onvif/snapshot.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := dev.ResolveSnapshotURI(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, res)
			}
		})
	}
}

func TestGetSnapshotURI(t *testing.T) {
	// Mock ONVIF media service response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if !strings.Contains(string(body), "GetSnapshotUri") {
			http.Error(w, "unexpected body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		respXML := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <trt:Uri>http://192.168.1.100/onvif/snapshot.jpg</trt:Uri>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`
		_, _ = fmt.Fprint(w, respXML)
	}))
	defer server.Close()

	dev := onvif.NewDevice(server.URL, "admin", "pass")
	uri, err := dev.GetSnapshotURI(context.Background(), "profile_1")
	if err != nil {
		t.Fatalf("unexpected error getting snapshot URI: %v", err)
	}

	if uri != "http://192.168.1.100/onvif/snapshot.jpg" {
		t.Errorf("expected http://192.168.1.100/onvif/snapshot.jpg, got %s", uri)
	}
}
