package discovery

import (
	"strings"
	"testing"
)

func TestBuildProbeXML(t *testing.T) {
	xml := buildProbeXML("dn:NetworkVideoTransmitter")
	if !strings.Contains(xml, "<d:Types>dn:NetworkVideoTransmitter</d:Types>") {
		t.Errorf("expected probe to contain Types element, got: %s", xml)
	}
	if !strings.Contains(xml, "urn:schemas-xmlsoap-org:ws:2005:04:discovery") {
		t.Errorf("expected discovery namespace")
	}

	genericXML := buildProbeXML("")
	if strings.Contains(genericXML, "<d:Types>") {
		t.Errorf("generic probe should omit Types element")
	}
}

func TestParseProbeMatches(t *testing.T) {
	sampleResp := []byte(`<?xml version="1.0" encoding="utf-8"?>
<Envelope xmlns="http://www.w3.org/2003/05/soap-envelope"
          xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing"
          xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <Body>
    <ProbeMatches xmlns="http://schemas.xmlsoap.org/ws/2005/04/discovery">
      <ProbeMatch>
        <EndpointReference>
          <Address>urn:uuid:12345678-1234-1234-1234-123456789abc</Address>
        </EndpointReference>
        <Types>dn:NetworkVideoTransmitter</Types>
        <Scopes>
          onvif://www.onvif.org/type/video_encoder
          onvif://www.onvif.org/name/Front%20Door
          onvif://www.onvif.org/hardware/DS-2CD2042WD-I
          onvif://www.onvif.org/location/Porch
        </Scopes>
        <XAddrs>http://192.168.1.105:80/onvif/device_service</XAddrs>
      </ProbeMatch>
    </ProbeMatches>
  </Body>
</Envelope>`)

	devs := parseProbeMatches(sampleResp)
	if len(devs) != 1 {
		t.Fatalf("expected 1 discovered device, got %d", len(devs))
	}

	d := devs[0]
	if d.IP != "192.168.1.105" {
		t.Errorf("expected IP 192.168.1.105, got %q", d.IP)
	}
	if d.Port != 80 {
		t.Errorf("expected port 80, got %d", d.Port)
	}
	if d.Name != "Front Door" {
		t.Errorf("expected name 'Front Door', got %q", d.Name)
	}
	if d.Model != "DS-2CD2042WD-I" {
		t.Errorf("expected model 'DS-2CD2042WD-I', got %q", d.Model)
	}
	if d.Manufacturer != "Hikvision" {
		t.Errorf("expected inferred manufacturer 'Hikvision', got %q", d.Manufacturer)
	}
	if d.Location != "Porch" {
		t.Errorf("expected location 'Porch', got %q", d.Location)
	}
}

func TestGenerateSampleConfig(t *testing.T) {
	devices := []DiscoveredDevice{
		{
			IP:           "192.168.1.50",
			Port:         80,
			Name:         "Backyard",
			Manufacturer: "Amcrest",
			RTSPURLs:     []string{"rtsp://192.168.1.50:554/live"},
		},
	}

	yamlOut := GenerateSampleConfig(devices)
	if !strings.Contains(yamlOut, "cam_192_168_1_50:") {
		t.Errorf("expected camera id 'cam_192_168_1_50' in config")
	}
	if !strings.Contains(yamlOut, "name: \"Backyard\"") {
		t.Errorf("expected camera name in config")
	}
	if !strings.Contains(yamlOut, "rtsp_url: \"rtsp://192.168.1.50:554/live\"") {
		t.Errorf("expected rtsp_url in config")
	}
}
