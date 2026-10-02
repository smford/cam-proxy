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

// Profile represents an ONVIF media profile.
type Profile struct {
	Token    string
	Name     string
	Encoding string // e.g. "H264", "JPEG"
}

// Device represents an ONVIF camera connection.
type Device struct {
	Endpoint   string
	Username   string
	Password   string
	HTTPClient *http.Client
	TimeOffset time.Duration

	mu          sync.RWMutex
	snapshotURI string
	mediaURL    string
	eventsURL   string
	ptzURL      string
	profiles    []Profile
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

func detectSOAPAction(bodyXML string) string {
	switch {
	case strings.Contains(bodyXML, "GetSnapshotUri"):
		return "http://www.onvif.org/ver10/media/wsdl/GetSnapshotUri"
	case strings.Contains(bodyXML, "GetProfiles"):
		return "http://www.onvif.org/ver10/media/wsdl/GetProfiles"
	case strings.Contains(bodyXML, "GetStreamUri"):
		return "http://www.onvif.org/ver10/media/wsdl/GetStreamUri"
	case strings.Contains(bodyXML, "GetCapabilities"):
		return "http://www.onvif.org/ver10/device/wsdl/GetCapabilities"
	case strings.Contains(bodyXML, "GetServices"):
		return "http://www.onvif.org/ver10/device/wsdl/GetServices"
	case strings.Contains(bodyXML, "ContinuousMove"):
		return "http://www.onvif.org/ver20/ptz/wsdl/ContinuousMove"
	case strings.Contains(bodyXML, "Stop"):
		return "http://www.onvif.org/ver20/ptz/wsdl/Stop"
	case strings.Contains(bodyXML, "GotoPreset"):
		return "http://www.onvif.org/ver20/ptz/wsdl/GotoPreset"
	case strings.Contains(bodyXML, "CreatePullPointSubscription"):
		return "http://www.onvif.org/ver10/events/wsdl/CreatePullPointSubscription"
	case strings.Contains(bodyXML, "PullMessages"):
		return "http://www.onvif.org/ver10/events/wsdl/PullMessages"
	case strings.Contains(bodyXML, "Unsubscribe"):
		return "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"
	case strings.Contains(bodyXML, "Renew"):
		return "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest"
	}
	return ""
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

	action := detectSOAPAction(bodyXML)
	if action != "" {
		req.Header.Set("Content-Type", fmt.Sprintf("application/soap+xml; charset=utf-8; action=%q", action))
		req.Header.Set("SOAPAction", fmt.Sprintf("%q", action))
	} else {
		req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	}

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

// GetCapabilities discovers media, events, and PTZ service URLs.
func (d *Device) GetCapabilities(ctx context.Context) error {
	d.mu.RLock()
	if d.mediaURL != "" && d.eventsURL != "" && d.ptzURL != "" {
		d.mu.RUnlock()
		return nil
	}
	d.mu.RUnlock()

	body := `<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>`
	endpointsToTry := []string{
		d.Endpoint + "/onvif/device_service",
		d.Endpoint + "/onvif/service",
	}

	var respBytes []byte
	var err error
	for _, ep := range endpointsToTry {
		respBytes, err = d.SendSOAP(ctx, ep, body)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}

	type CapResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			GetCapabilitiesResponse struct {
				Capabilities struct {
					Media struct {
						XAddr string `xml:"XAddr"`
					} `xml:"Media"`
					Events struct {
						XAddr string `xml:"XAddr"`
					} `xml:"Events"`
					PTZ struct {
						XAddr string `xml:"XAddr"`
					} `xml:"PTZ"`
				} `xml:"Capabilities"`
			} `xml:"GetCapabilitiesResponse"`
		} `xml:"Body"`
	}

	var capResp CapResponse
	if err := xml.Unmarshal(respBytes, &capResp); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	caps := capResp.Body.GetCapabilitiesResponse.Capabilities
	if caps.Media.XAddr != "" {
		d.mediaURL, _ = d.ResolveSnapshotURI(caps.Media.XAddr)
	}
	if caps.Events.XAddr != "" {
		d.eventsURL, _ = d.ResolveSnapshotURI(caps.Events.XAddr)
	}
	if caps.PTZ.XAddr != "" {
		d.ptzURL, _ = d.ResolveSnapshotURI(caps.PTZ.XAddr)
	}
	return nil
}

// GetMediaURL returns the media service endpoint.
func (d *Device) GetMediaURL(ctx context.Context) string {
	d.mu.RLock()
	u := d.mediaURL
	d.mu.RUnlock()
	if u != "" {
		return u
	}
	_ = d.GetCapabilities(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.mediaURL != "" {
		return d.mediaURL
	}
	return d.Endpoint + "/onvif/media_service"
}

// GetEventsURL returns the events service endpoint.
func (d *Device) GetEventsURL(ctx context.Context) string {
	d.mu.RLock()
	u := d.eventsURL
	d.mu.RUnlock()
	if u != "" {
		return u
	}
	_ = d.GetCapabilities(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.eventsURL != "" {
		return d.eventsURL
	}
	return d.Endpoint + "/onvif/event_service"
}

// GetPTZURL returns the PTZ service endpoint.
func (d *Device) GetPTZURL(ctx context.Context) string {
	d.mu.RLock()
	u := d.ptzURL
	d.mu.RUnlock()
	if u != "" {
		return u
	}
	_ = d.GetCapabilities(ctx)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.ptzURL != "" {
		return d.ptzURL
	}
	return d.Endpoint + "/onvif/ptz_service"
}

// GetProfiles queries the ONVIF Media service for video profiles.
func (d *Device) GetProfiles(ctx context.Context) ([]Profile, error) {
	d.mu.RLock()
	if len(d.profiles) > 0 {
		profs := d.profiles
		d.mu.RUnlock()
		return profs, nil
	}
	d.mu.RUnlock()

	mediaURL := d.GetMediaURL(ctx)
	body := `<trt:GetProfiles/>`
	respBytes, err := d.SendSOAP(ctx, mediaURL, body)
	if err != nil {
		if !strings.HasSuffix(mediaURL, "/onvif/service") {
			mediaURL = d.Endpoint + "/onvif/service"
			respBytes, err = d.SendSOAP(ctx, mediaURL, body)
		}
		if err != nil {
			return nil, fmt.Errorf("GetProfiles failed: %w", err)
		}
	}

	type GetProfilesResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Profiles []struct {
					Token               string `xml:"token,attr"`
					Name                string `xml:"Name"`
					VideoEncoderConfiguration struct {
						Encoding string `xml:"Encoding"`
					} `xml:"VideoEncoderConfiguration"`
				} `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		} `xml:"Body"`
	}

	var parsed GetProfilesResponse
	if err := xml.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshaling profiles: %w", err)
	}

	var profiles []Profile
	for _, p := range parsed.Body.Response.Profiles {
		profiles = append(profiles, Profile{
			Token:    p.Token,
			Name:     p.Name,
			Encoding: p.VideoEncoderConfiguration.Encoding,
		})
	}

	d.mu.Lock()
	d.profiles = profiles
	d.mu.Unlock()

	return profiles, nil
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

	if profileToken == "" {
		profs, err := d.GetProfiles(ctx)
		if err == nil && len(profs) > 0 {
			profileToken = profs[0].Token
		}
	}

	body := fmt.Sprintf(`
    <trt:GetSnapshotUri>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetSnapshotUri>`, profileToken)

	mediaURL := d.GetMediaURL(ctx)
	respBytes, err := d.SendSOAP(ctx, mediaURL, body)
	if err != nil {
		if !strings.HasSuffix(mediaURL, "/onvif/service") {
			mediaURL = d.Endpoint + "/onvif/service"
			respBytes, err = d.SendSOAP(ctx, mediaURL, body)
		}
		if err != nil {
			return "", err
		}
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

// GetStreamURI returns the RTSP stream URL for the given profile token.
func (d *Device) GetStreamURI(ctx context.Context, profileToken string) (string, error) {
	if profileToken == "" {
		profs, err := d.GetProfiles(ctx)
		if err == nil && len(profs) > 0 {
			profileToken = profs[0].Token
		}
	}

	body := fmt.Sprintf(`
    <trt:GetStreamUri>
      <trt:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport>
          <tt:Protocol>RTSP</tt:Protocol>
        </tt:Transport>
      </trt:StreamSetup>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetStreamUri>`, profileToken)

	mediaURL := d.GetMediaURL(ctx)
	respBytes, err := d.SendSOAP(ctx, mediaURL, body)
	if err != nil {
		if !strings.HasSuffix(mediaURL, "/onvif/service") {
			mediaURL = d.Endpoint + "/onvif/service"
			respBytes, err = d.SendSOAP(ctx, mediaURL, body)
		}
		if err != nil {
			return "", err
		}
	}

	type GetStreamUriResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				MediaURI struct {
					URI string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetStreamUriResponse"`
		} `xml:"Body"`
	}

	var parsed GetStreamUriResponse
	if err := xml.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("unmarshaling stream URI response: %w", err)
	}

	uri := strings.TrimSpace(parsed.Body.Response.MediaURI.URI)
	if uri == "" {
		return "", fmt.Errorf("empty stream URI returned from camera")
	}

	return uri, nil
}

// GetMJPEGStreamURI returns an RTSP stream URI for an MJPEG video profile if available.
func (d *Device) GetMJPEGStreamURI(ctx context.Context) (string, error) {
	profs, err := d.GetProfiles(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range profs {
		if strings.EqualFold(p.Encoding, "JPEG") || strings.EqualFold(p.Encoding, "MJPEG") {
			return d.GetStreamURI(ctx, p.Token)
		}
	}
	return "", fmt.Errorf("no MJPEG/JPEG profile found in camera")
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
