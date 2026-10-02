package discovery

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiscoveredDevice holds details about a detected network camera.
type DiscoveredDevice struct {
	IP           string   `json:"ip"`
	Port         int      `json:"port"`
	Type         string   `json:"type"` // "ONVIF", "RTSP", "ONVIF+RTSP"
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	Name         string   `json:"name,omitempty"`
	Location     string   `json:"location,omitempty"`
	Hardware     string   `json:"hardware,omitempty"`
	XAddrs       []string `json:"xaddrs,omitempty"`
	RTSPURLs     []string `json:"rtsp_urls,omitempty"`
	ServerHeader string   `json:"server_header,omitempty"`
	URN          string   `json:"urn,omitempty"`
}

// ScanOptions configures network camera discovery.
type ScanOptions struct {
	Timeout   time.Duration
	ScanRTSP  bool
	RTSPPorts []int
}

// DefaultScanOptions returns balanced settings for quick LAN scanning.
func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		Timeout:   3 * time.Second,
		ScanRTSP:  true,
		RTSPPorts: []int{554, 8554},
	}
}

// Scan broadcasts ONVIF WS-Discovery probes and performs concurrent RTSP port detection.
func Scan(ctx context.Context, opts ScanOptions) ([]DiscoveredDevice, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	if len(opts.RTSPPorts) == 0 {
		opts.RTSPPorts = []int{554, 8554}
	}

	results := make(map[string]*DiscoveredDevice)
	var mu sync.Mutex

	var wg sync.WaitGroup

	// 1. Run ONVIF WS-Discovery
	wg.Add(1)
	go func() {
		defer wg.Done()
		onvifDevs, err := scanONVIF(ctx, opts.Timeout)
		if err == nil {
			mu.Lock()
			for _, d := range onvifDevs {
				existing, exists := results[d.IP]
				if exists {
					existing.Type = "ONVIF"
					if len(d.XAddrs) > 0 {
						existing.XAddrs = d.XAddrs
					}
					if d.Manufacturer != "" {
						existing.Manufacturer = d.Manufacturer
					}
					if d.Model != "" {
						existing.Model = d.Model
					}
					if d.Name != "" {
						existing.Name = d.Name
					}
				} else {
					copyDev := d
					results[d.IP] = &copyDev
				}
			}
			mu.Unlock()
		}
	}()

	// 2. Run RTSP Subnet Sweep if requested
	if opts.ScanRTSP {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rtspDevs, err := scanRTSP(ctx, opts.Timeout, opts.RTSPPorts)
			if err == nil {
				mu.Lock()
				for _, d := range rtspDevs {
					existing, exists := results[d.IP]
					if exists {
						if !strings.Contains(existing.Type, d.Type) {
							existing.Type = fmt.Sprintf("%s, %s", existing.Type, d.Type)
						}
						existing.RTSPURLs = append(existing.RTSPURLs, d.RTSPURLs...)
						if existing.ServerHeader == "" {
							existing.ServerHeader = d.ServerHeader
						}
						if strings.Contains(d.Type, "ONVIF") && d.Port != 554 && d.Port != 0 {
							existing.Port = d.Port
						}
					} else {
						copyDev := d
						results[d.IP] = &copyDev
					}
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Consolidate list and check RTSP for any ONVIF devices missing explicit RTSP
	list := make([]DiscoveredDevice, 0, len(results))
	for _, dev := range results {
		isTapo := strings.Contains(strings.ToLower(dev.Manufacturer), "tp-link") ||
			strings.Contains(strings.ToLower(dev.Manufacturer), "tapo") ||
			strings.HasPrefix(strings.ToUpper(dev.Model), "C") ||
			strings.HasPrefix(strings.ToUpper(dev.Model), "TC") ||
			dev.Port == 2020

		if strings.Contains(dev.Type, "ONVIF") && len(dev.RTSPURLs) == 0 {
			if isTapo {
				dev.RTSPURLs = append(dev.RTSPURLs, fmt.Sprintf("rtsp://<user>:<password>@%s:554/stream1", dev.IP))
			} else {
				dev.RTSPURLs = append(dev.RTSPURLs, fmt.Sprintf("rtsp://<user>:<password>@%s:554/live", dev.IP))
			}
		}
		list = append(list, *dev)
	}

	// Sort by IP address
	sort.Slice(list, func(i, j int) bool {
		return ipToUint32(list[i].IP) < ipToUint32(list[j].IP)
	})

	return list, nil
}

// scanONVIF sends WS-Discovery probes and collects responses.
func scanONVIF(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	multicastAddr, err := net.ResolveUDPAddr("udp4", "239.255.255.250:3702")
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	// Send both typed and generic WS-Discovery probes
	probes := []string{
		buildProbeXML("dn:NetworkVideoTransmitter"),
		buildProbeXML("tds:Device"),
		buildProbeXML(""),
	}

	for _, p := range probes {
		_, _ = conn.WriteToUDP([]byte(p), multicastAddr)
	}

	var devices []DiscoveredDevice
	seen := make(map[string]bool)
	buf := make([]byte, 8192)

	for {
		select {
		case <-ctx.Done():
			return devices, nil
		default:
		}

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break // Timeout reached
		}

		devs := parseProbeMatches(buf[:n])
		for _, d := range devs {
			if !seen[d.IP] && d.IP != "" {
				seen[d.IP] = true
				devices = append(devices, d)
			}
		}
	}

	return devices, nil
}

func buildProbeXML(types string) string {
	uuidBytes := make([]byte, 16)
	_, _ = rand.Read(uuidBytes)
	msgID := fmt.Sprintf("%x-%x-%x-%x-%x", uuidBytes[0:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:])

	typeElem := ""
	if types != "" {
		typeElem = fmt.Sprintf("<d:Types>%s</d:Types>", types)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<Envelope xmlns="http://www.w3.org/2003/05/soap-envelope"
          xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing"
          xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
          xmlns:dn="http://www.onvif.org/ver10/network/wsdl"
          xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <Header>
    <wsa:MessageID>urn:uuid:%s</wsa:MessageID>
    <wsa:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</wsa:To>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>
  </Header>
  <Body>
    <d:Probe>
      %s
    </d:Probe>
  </Body>
</Envelope>`, msgID, typeElem)
}

type probeMatchesEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		ProbeMatches struct {
			ProbeMatch []struct {
				EndpointReference struct {
					Address string `xml:"Address"`
				} `xml:"EndpointReference"`
				Types  string `xml:"Types"`
				Scopes string `xml:"Scopes"`
				XAddrs string `xml:"XAddrs"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

func parseProbeMatches(data []byte) []DiscoveredDevice {
	var env probeMatchesEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil
	}

	var out []DiscoveredDevice
	for _, pm := range env.Body.ProbeMatches.ProbeMatch {
		xaddrs := strings.Fields(pm.XAddrs)
		if len(xaddrs) == 0 {
			continue
		}

		firstURL, err := url.Parse(xaddrs[0])
		if err != nil {
			continue
		}

		host, portStr, _ := net.SplitHostPort(firstURL.Host)
		if host == "" {
			host = firstURL.Host
		}
		port := 80
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			port = p
		}

		dev := DiscoveredDevice{
			IP:     host,
			Port:   port,
			Type:   "ONVIF",
			XAddrs: xaddrs,
			URN:    pm.EndpointReference.Address,
		}

		parseScopes(pm.Scopes, &dev)
		out = append(out, dev)
	}

	return out
}

func parseScopes(scopesStr string, dev *DiscoveredDevice) {
	tokens := strings.Fields(scopesStr)
	for _, tok := range tokens {
		u, err := url.Parse(tok)
		if err != nil {
			continue
		}

		path := strings.TrimPrefix(u.Path, "/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(parts[0])
		val, _ := url.PathUnescape(parts[1])

		switch key {
		case "name":
			if dev.Name == "" {
				dev.Name = val
			}
		case "hardware":
			if dev.Model == "" {
				dev.Model = val
			}
		case "location":
			if dev.Location == "" {
				dev.Location = val
			}
		case "mfr", "manufacturer":
			if dev.Manufacturer == "" {
				dev.Manufacturer = val
			}
		}
	}

	// Heuristic fallback for manufacturer if model has known prefixes
	if dev.Manufacturer == "" && dev.Model != "" {
		m := strings.ToUpper(dev.Model)
		if strings.HasPrefix(m, "DS-") {
			dev.Manufacturer = "Hikvision"
		} else if strings.HasPrefix(m, "IPC-") || strings.HasPrefix(m, "DH-") {
			dev.Manufacturer = "Dahua"
		} else if strings.HasPrefix(m, "IP4M") || strings.HasPrefix(m, "IP8M") {
			dev.Manufacturer = "Amcrest"
		} else if strings.HasPrefix(m, "RLC-") {
			dev.Manufacturer = "Reolink"
		}
	}
}

// scanRTSP sweeps local LAN subnets for listening RTSP ports (554, 8554).
func scanRTSP(ctx context.Context, timeout time.Duration, ports []int) ([]DiscoveredDevice, error) {
	subnets := getLocalSubnets()
	if len(subnets) == 0 {
		return nil, nil
	}

	var targets []string
	for _, sub := range subnets {
		targets = append(targets, hostsInSubnet(sub)...)
	}

	if len(targets) > 512 {
		targets = targets[:512] // Limit sweep for responsiveness
	}

	resultsChan := make(chan DiscoveredDevice, len(targets))
	semaphore := make(chan struct{}, 64) // Concurrent dial workers
	var wg sync.WaitGroup

	dialer := &net.Dialer{Timeout: 350 * time.Millisecond}

	for _, ip := range targets {
		for _, port := range ports {
			wg.Add(1)
			go func(targetIP string, targetPort int) {
				defer wg.Done()
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-semaphore }()

				addr := fmt.Sprintf("%s:%d", targetIP, targetPort)
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err != nil {
					return
				}
				defer conn.Close()

				// RTSP ping
				_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
				req := fmt.Sprintf("OPTIONS rtsp://%s/ RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: camstop\r\n\r\n", addr)
				if _, err := conn.Write([]byte(req)); err != nil {
					return
				}

				reader := bufio.NewReader(conn)
				statusLine, err := reader.ReadString('\n')
				if err != nil || !strings.HasPrefix(statusLine, "RTSP/1.0") {
					return
				}

				serverHeader := ""
				for {
					line, err := reader.ReadString('\n')
					if err != nil || line == "\r\n" || line == "\n" {
						break
					}
					if strings.HasPrefix(strings.ToLower(line), "server:") {
						serverHeader = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
						serverHeader = strings.TrimSpace(strings.TrimPrefix(serverHeader, "server:"))
					}
				}

				// Check if ONVIF port 2020 is also open on this camera (e.g. TP-Link Tapo)
				devType := "RTSP"
				devPort := targetPort
				rtspURL := fmt.Sprintf("rtsp://%s/live", addr)
				if onvifConn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:2020", targetIP)); err == nil {
					_ = onvifConn.Close()
					devType = "ONVIF+RTSP"
					devPort = 2020
					rtspURL = fmt.Sprintf("rtsp://%s:554/stream1", targetIP)
				}

				resultsChan <- DiscoveredDevice{
					IP:           targetIP,
					Port:         devPort,
					Type:         devType,
					RTSPURLs:     []string{rtspURL},
					ServerHeader: serverHeader,
				}
			}(ip, port)
		}
	}

	wg.Wait()
	close(resultsChan)

	var found []DiscoveredDevice
	for dev := range resultsChan {
		found = append(found, dev)
	}

	return found, nil
}

func getLocalSubnets() []*net.IPNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var subnets []*net.IPNet
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				// Only scan /24 or smaller
				ones, bits := ipnet.Mask.Size()
				if bits == 32 && ones >= 24 {
					subnets = append(subnets, ipnet)
				}
			}
		}
	}
	return subnets
}

func hostsInSubnet(ipnet *net.IPNet) []string {
	var ips []string
	ip := ipnet.IP.To4()
	if ip == nil {
		return nil
	}

	mask := ipnet.Mask
	network := ip.Mask(mask)

	// Calculate broadcast
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = network[i] | ^mask[i]
	}

	curr := make(net.IP, 4)
	copy(curr, network)

	for {
		incIP(curr)
		if curr.Equal(broadcast) {
			break
		}
		ips = append(ips, curr.String())
	}

	return ips
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func ipToUint32(ipStr string) uint32 {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// PrintTable formats the discovered cameras in a clean ASCII terminal table.
func PrintTable(devices []DiscoveredDevice) {
	if len(devices) == 0 {
		fmt.Println("No ONVIF or RTSP cameras discovered on local network.")
		return
	}

	fmt.Printf("\n%-16s %-10s %-14s %-22s %s\n", "IP ADDRESS", "TYPE", "MANUFACTURER", "MODEL / NAME", "RTSP / ONVIF ENDPOINT")
	fmt.Println(strings.Repeat("-", 96))

	for _, d := range devices {
		name := d.Name
		if name == "" {
			name = d.Model
		}
		if name == "" && d.ServerHeader != "" {
			name = d.ServerHeader
		}
		if name == "" {
			name = "-"
		}

		mfr := d.Manufacturer
		if mfr == "" {
			mfr = "-"
		}

		endpoint := "-"
		if len(d.RTSPURLs) > 0 {
			endpoint = d.RTSPURLs[0]
		} else if len(d.XAddrs) > 0 {
			endpoint = d.XAddrs[0]
		}

		fmt.Printf("%-16s %-10s %-14s %-22s %s\n", d.IP, d.Type, truncate(mfr, 14), truncate(name, 22), endpoint)
	}

	fmt.Println(strings.Repeat("-", 96))
	fmt.Printf("Discovered %d camera(s) on local network.\n\n", len(devices))
}

// GenerateSampleConfig prints a ready-to-use YAML configuration block for the detected cameras.
func GenerateSampleConfig(devices []DiscoveredDevice) string {
	var b strings.Builder
	b.WriteString("# Generated camstop camera definitions\ncameras:\n")

	for i, d := range devices {
		id := fmt.Sprintf("cam_%s", strings.ReplaceAll(d.IP, ".", "_"))
		name := d.Name
		if name == "" {
			name = fmt.Sprintf("Camera %d (%s)", i+1, d.IP)
		}

		rtspURL := fmt.Sprintf("rtsp://admin:password@%s:554/live", d.IP)
		if len(d.RTSPURLs) > 0 {
			rtspURL = d.RTSPURLs[0]
		}

		onvifAddr := fmt.Sprintf("%s:%d", d.IP, d.Port)

		b.WriteString(fmt.Sprintf("  %s:\n", id))
		b.WriteString(fmt.Sprintf("    name: %q\n", name))
		b.WriteString(fmt.Sprintf("    address: %q\n", onvifAddr))
		b.WriteString(fmt.Sprintf("    rtsp_url: %q\n", rtspURL))
		b.WriteString("    onvif_username: \"admin\"\n")
		b.WriteString("    onvif_password: \"password\"\n")
		b.WriteString("    snapshot_method: \"auto\"\n")
		b.WriteString("    pull_events: true\n\n")
	}

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
