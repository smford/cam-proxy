package onvif_test

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smford/cam-proxy/internal/onvif"
)

func TestDigestTransportRoundTrip(t *testing.T) {
	const (
		expectedUser = "admin"
		expectedPass = "password123"
		realm        = "CameraRealm"
		nonce        = "dcd98b7102dd2f0e8b11d0f600bfb0c093"
	)

	// Server that challenges with Digest Auth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}

		if !strings.HasPrefix(auth, "Digest ") {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Verify digest components
		if !strings.Contains(auth, fmt.Sprintf(`username="%s"`, expectedUser)) {
			t.Errorf("expected username in header: %s", auth)
		}
		if !strings.Contains(auth, fmt.Sprintf(`realm="%s"`, realm)) {
			t.Errorf("expected realm in header: %s", auth)
		}
		if !strings.Contains(auth, fmt.Sprintf(`nonce="%s"`, nonce)) {
			t.Errorf("expected nonce in header: %s", auth)
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("JPEG_MOCK_DATA"))
	}))
	defer server.Close()

	client := &http.Client{
		Transport: onvif.NewDigestTransport(expectedUser, expectedPass),
	}

	resp, err := client.Get(server.URL + "/snapshot.jpg")
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if string(body) != "JPEG_MOCK_DATA" {
		t.Errorf("expected JPEG_MOCK_DATA, got %q", string(body))
	}
}

func md5Str(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}
