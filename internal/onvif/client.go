package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Device represents an ONVIF camera connection.
type Device struct {
	Endpoint   string
	Username   string
	Password   string
	HTTPClient *http.Client
	TimeOffset time.Duration

	mu          sync.RWMutex
	snapshotURI string
	profiles    []string
}

// NewDevice initializes an ONVIF device client.
func NewDevice(endpoint, username, password string) *Device {
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	return &Device{
		Endpoint: endpoint,
		Username: username,
		Password: password,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// BuildWSSecurityHeader builds the standard ONVIF WS-Security UsernameToken header
// with SHA-1 password digest and Base64 nonce.
func (d *Device) BuildWSSecurityHeader() string {
	if d.Username == "" {
		return ""
	}

	// 16 bytes cryptographic nonce
	nonceRaw := make([]byte, 16)
	_, _ = rand.Read(nonceRaw)
	nonceBase64 := base64.StdEncoding.EncodeToString(nonceRaw)

	// Apply clock offset to counter camera clock drift
	created := time.Now().UTC().Add(d.TimeOffset).Format("2006-01-02T15:04:05.000Z")

	// Password_Digest = Base64( SHA-1( raw_nonce + created_string + password_string ) )
	h := sha1.New()
	h.Write(nonceRaw)
	h.Write([]byte(created))
	h.Write([]byte(d.Password))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return fmt.Sprintf(`
  <s:Header>
    <wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>%s</wsse:Username>
        <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>
        <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce>
        <wsu:Created>%s</wsu:Created>
      </wsse:UsernameToken>
    </wsse:Security>
  </s:Header>`, d.Username, digest, nonceBase64, created)
}

// SendSOAP sends a raw SOAP 1.2 request to the camera service endpoint.
func (d *Device) SendSOAP(ctx context.Context, serviceURL, bodyXML string) ([]byte, error) {
	if serviceURL == "" {
		serviceURL = d.Endpoint + "/onvif/device_service"
	}

	envelope := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tt="http://www.onvif.org/ver10/schema"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
            xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
            xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
%s
  <s:Body>
%s
  </s:Body>
</s:Envelope>`, d.BuildWSSecurityHeader(), bodyXML)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceURL, bytes.NewBufferString(envelope))
	if err != nil {
		return nil, fmt.Errorf("creating soap request: %w", err)
	}

	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing soap request to %s: %w", serviceURL, err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading soap response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return respData, fmt.Errorf("soap error (status %d): %s", resp.StatusCode, string(respData))
	}

	return respData, nil
}

// GetSnapshotURI queries the ONVIF Media service for the HTTP JPEG snapshot endpoint.
func (d *Device) GetSnapshotURI(ctx context.Context, profileToken string) (string, error) {
	d.mu.RLock()
	if d.snapshotURI != "" && profileToken == "" {
		uri := d.snapshotURI
		d.mu.RUnlock()
		return uri, nil
	}
	d.mu.RUnlock()

	body := fmt.Sprintf(`
    <trt:GetSnapshotUri>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetSnapshotUri>`, profileToken)

	mediaURL := d.Endpoint + "/onvif/media_service"
	respBytes, err := d.SendSOAP(ctx, mediaURL, body)
	if err != nil {
		return "", err
	}

	type GetSnapshotUriResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				MediaURI struct {
					URI string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetSnapshotUriResponse"`
		} `xml:"Body"`
	}

	var parsed GetSnapshotUriResponse
	if err := xml.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("unmarshaling snapshot URI response: %w", err)
	}

	uri := strings.TrimSpace(parsed.Body.Response.MediaURI.URI)
	if uri == "" {
		return "", fmt.Errorf("empty snapshot URI returned from camera")
	}

	d.mu.Lock()
	if profileToken == "" {
		d.snapshotURI = uri
	}
	d.mu.Unlock()

	return uri, nil
}

// DownloadSnapshot fetches a JPEG snapshot from the camera via HTTP with Digest/Basic auth.
func (d *Device) DownloadSnapshot(ctx context.Context, snapshotURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		return nil, err
	}

	// Use digest/basic transport for snapshot download
	client := &http.Client{
		Timeout:   6 * time.Second,
		Transport: NewDigestTransport(d.Username, d.Password),
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching snapshot from %s: %w", snapshotURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("snapshot HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// ResolveSnapshotURI resolves relative URLs returned by some non-compliant ONVIF cameras.
func (d *Device) ResolveSnapshotURI(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return rawURI, nil
	}
	base, err := url.Parse(d.Endpoint)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(u).String(), nil
}
