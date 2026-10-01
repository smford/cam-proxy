package onvif

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// DigestTransport implements http.RoundTripper and handles HTTP Digest Authentication challenges.
type DigestTransport struct {
	Username string
	Password string
	Base     http.RoundTripper

	mu    sync.Mutex
	nonce string
	realm string
	qop   string
	nc    int
}

// NewDigestTransport creates a new DigestTransport.
func NewDigestTransport(username, password string) *DigestTransport {
	return &DigestTransport{
		Username: username,
		Password: password,
		Base:     http.DefaultTransport,
	}
}

// RoundTrip executes the HTTP request, attempting an initial unauthenticated request,
// and handling 401 Digest authentication challenges if required.
func (t *DigestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Username == "" {
		return t.Base.RoundTrip(req)
	}

	t.mu.Lock()
	cachedNonce := t.nonce
	cachedRealm := t.realm
	t.mu.Unlock()

	// If we already have credentials cached from earlier, send them preemptively
	if cachedNonce != "" && cachedRealm != "" {
		req.Header.Set("Authorization", t.buildAuthHeader(req.Method, req.URL.RequestURI()))
	}

	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// Read WWW-Authenticate header
	authHeader := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(authHeader), "digest ") {
		return resp, nil // Basic auth or unsupported
	}

	// Parse challenge params
	t.parseChallenge(authHeader)

	// Clone request for retry
	retryReq := req.Clone(req.Context())
	retryReq.Header.Set("Authorization", t.buildAuthHeader(retryReq.Method, retryReq.URL.RequestURI()))

	// Close initial response body before retrying
	_ = resp.Body.Close()

	return t.Base.RoundTrip(retryReq)
}

func (t *DigestTransport) parseChallenge(header string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	header = strings.TrimPrefix(header, "Digest ")
	parts := strings.Split(header, ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)

		switch key {
		case "realm":
			t.realm = val
		case "nonce":
			t.nonce = val
		case "qop":
			t.qop = val
		}
	}
	t.nc = 0
}

func (t *DigestTransport) buildAuthHeader(method, uri string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nc++
	ncStr := fmt.Sprintf("%08x", t.nc)

	cnonceRaw := make([]byte, 8)
	_, _ = rand.Read(cnonceRaw)
	cnonce := hex.EncodeToString(cnonceRaw)

	// HA1 = MD5(username:realm:password)
	ha1Input := fmt.Sprintf("%s:%s:%s", t.Username, t.realm, t.Password)
	ha1 := md5Hash(ha1Input)

	// HA2 = MD5(method:digestURI)
	ha2Input := fmt.Sprintf("%s:%s", method, uri)
	ha2 := md5Hash(ha2Input)

	var response string
	if strings.Contains(t.qop, "auth") {
		// response = MD5(HA1:nonce:nc:cnonce:qop:HA2)
		response = md5Hash(fmt.Sprintf("%s:%s:%s:%s:auth:%s", ha1, t.nonce, ncStr, cnonce, ha2))
		return fmt.Sprintf(
			`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=auth, nc=%s, cnonce="%s", response="%s"`,
			t.Username, t.realm, t.nonce, uri, ncStr, cnonce, response,
		)
	}

	// Legacy RFC 2069 without qop
	response = md5Hash(fmt.Sprintf("%s:%s:%s", ha1, t.nonce, ha2))
	return fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		t.Username, t.realm, t.nonce, uri, response,
	)
}

func md5Hash(in string) string {
	h := md5.New()
	h.Write([]byte(in))
	return hex.EncodeToString(h.Sum(nil))
}
