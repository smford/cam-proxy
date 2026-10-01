package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/smford/camstop/internal/camera"
	"github.com/smford/camstop/internal/config"
	"github.com/smford/camstop/internal/discovery"
)

type viewMode int

const (
	modeList viewMode = iota
	modeEdit
	modeScanning
)

type tabIndex int

const (
	tabConfigured tabIndex = iota
	tabDiscovered
)

// Model represents the bubbletea application state.
type Model struct {
	configPath string
	cfg        *config.Config
	mode       viewMode
	activeTab  tabIndex

	// Configured cameras
	configuredKeys []string
	cfgCursor      int

	// Discovered cameras
	discovered []discovery.DiscoveredDevice
	discCursor int

	// Form inputs for add/edit
	editingID   string
	isNewCam    bool
	inputs      []textinput.Model
	focusIndex  int

	// Status & messaging
	statusMsg  string
	statusErr  bool
	statusTime time.Time
	width      int
	height     int
	scanning   bool
}

// Messages
type scanDoneMsg struct {
	devices []discovery.DiscoveredDevice
	err     error
}

type testDoneMsg struct {
	cameraID string
	err      error
}

// New creates and initializes the TUI model.
func New(configPath string, cfg *config.Config) Model {
	m := Model{
		configPath: configPath,
		cfg:        cfg,
		mode:       modeList,
		activeTab:  tabConfigured,
		statusMsg:  "Press 's' to scan network, 'a' to add, 'e' to edit, 'w' to save, 'q' to quit",
	}
	m.refreshConfiguredKeys()
	m.initInputs()
	return m
}

func (m *Model) refreshConfiguredKeys() {
	m.configuredKeys = make([]string, 0, len(m.cfg.Cameras))
	for id := range m.cfg.Cameras {
		m.configuredKeys = append(m.configuredKeys, id)
	}
	if m.cfgCursor >= len(m.configuredKeys) && len(m.configuredKeys) > 0 {
		m.cfgCursor = len(m.configuredKeys) - 1
	}
}

func (m *Model) initInputs() {
	labels := []string{
		"Camera ID (unique)",
		"Display Name",
		"ONVIF Address (IP:port)",
		"RTSP URL",
		"ONVIF Username",
		"ONVIF Password",
		"Snapshot Method (auto/onvif/rtsp)",
		"Pull Events (true/false)",
	}

	m.inputs = make([]textinput.Model, len(labels))
	for i, lbl := range labels {
		ti := textinput.New()
		ti.Placeholder = lbl
		ti.CharLimit = 128
		ti.Width = 48
		if i == 5 {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		m.inputs[i] = ti
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case scanDoneMsg:
		m.scanning = false
		m.mode = modeList
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Scan failed: %v", msg.err), true)
		} else {
			m.discovered = msg.devices
			m.discCursor = 0
			m.activeTab = tabDiscovered
			m.setStatus(fmt.Sprintf("Discovered %d camera(s) on local network", len(msg.devices)), false)
		}
		return m, nil

	case testDoneMsg:
		if msg.err != nil {
			m.setStatus(fmt.Sprintf("Camera '%s' test failed: %v", msg.cameraID, msg.err), true)
		} else {
			m.setStatus(fmt.Sprintf("Camera '%s' snapshot test SUCCEEDED!", msg.cameraID), false)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeEdit:
			return m.updateEdit(msg)
		case modeScanning:
			if msg.String() == "ctrl+c" || msg.String() == "esc" {
				m.mode = modeList
				m.scanning = false
				return m, nil
			}
		}
	}

	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if m.activeTab == tabConfigured {
			m.activeTab = tabDiscovered
		} else {
			m.activeTab = tabConfigured
		}
		return m, nil

	case "up", "k":
		if m.activeTab == tabConfigured && m.cfgCursor > 0 {
			m.cfgCursor--
		} else if m.activeTab == tabDiscovered && m.discCursor > 0 {
			m.discCursor--
		}

	case "down", "j":
		if m.activeTab == tabConfigured && m.cfgCursor < len(m.configuredKeys)-1 {
			m.cfgCursor++
		} else if m.activeTab == tabDiscovered && m.discCursor < len(m.discovered)-1 {
			m.discCursor++
		}

	case "s":
		m.scanning = true
		m.mode = modeScanning
		m.setStatus("Scanning local network for ONVIF & RTSP cameras...", false)
		return m, triggerScanCmd()

	case "a":
		m.startAddForm()
		m.mode = modeEdit
		return m, m.inputs[0].Focus()

	case "e", "enter":
		if m.activeTab == tabConfigured && len(m.configuredKeys) > 0 {
			camID := m.configuredKeys[m.cfgCursor]
			m.startEditForm(camID)
			m.mode = modeEdit
			return m, m.inputs[0].Focus()
		} else if m.activeTab == tabDiscovered && len(m.discovered) > 0 {
			m.startAddFromDiscovered(m.discovered[m.discCursor])
			m.mode = modeEdit
			return m, m.inputs[0].Focus()
		}

	case "d":
		if m.activeTab == tabConfigured && len(m.configuredKeys) > 0 {
			camID := m.configuredKeys[m.cfgCursor]
			delete(m.cfg.Cameras, camID)
			m.refreshConfiguredKeys()
			m.setStatus(fmt.Sprintf("Removed camera '%s' (unsaved, press 'w' to save to disk)", camID), false)
		}

	case "w":
		if err := config.Save(m.configPath, m.cfg); err != nil {
			m.setStatus(fmt.Sprintf("Save failed: %v", err), true)
		} else {
			m.setStatus(fmt.Sprintf("Configuration successfully saved to %s", m.configPath), false)
		}

	case "t":
		if m.activeTab == tabConfigured && len(m.configuredKeys) > 0 {
			camID := m.configuredKeys[m.cfgCursor]
			camCfg := m.cfg.Cameras[camID]
			m.setStatus(fmt.Sprintf("Testing connection to %s (%s)...", camID, camCfg.Address), false)
			return m, testCameraCmd(camCfg)
		}
	}

	return m, nil
}

func (m Model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.setStatus("Canceled camera edit", false)
		return m, nil

	case "tab", "down":
		m.inputs[m.focusIndex].Blur()
		m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
		return m, m.inputs[m.focusIndex].Focus()

	case "shift+tab", "up":
		m.inputs[m.focusIndex].Blur()
		m.focusIndex = (m.focusIndex - 1 + len(m.inputs)) % len(m.inputs)
		return m, m.inputs[m.focusIndex].Focus()

	case "enter":
		if m.focusIndex == len(m.inputs)-1 {
			// Save form
			if err := m.saveForm(); err != nil {
				m.setStatus(err.Error(), true)
				return m, nil
			}
			m.mode = modeList
			m.activeTab = tabConfigured
			m.refreshConfiguredKeys()
			m.setStatus(fmt.Sprintf("Saved camera '%s' in memory (press 'w' to save file)", m.editingID), false)
			return m, nil
		}
		m.inputs[m.focusIndex].Blur()
		m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
		return m, m.inputs[m.focusIndex].Focus()
	}

	// Update text inputs
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *Model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	return tea.Batch(cmds...)
}

func (m *Model) startAddForm() {
	m.isNewCam = true
	m.editingID = ""
	m.focusIndex = 0
	for i := range m.inputs {
		m.inputs[i].Reset()
	}
	m.inputs[6].SetValue("auto")
	m.inputs[7].SetValue("true")
}

func (m *Model) startAddFromDiscovered(dev discovery.DiscoveredDevice) {
	m.isNewCam = true
	m.focusIndex = 0
	cleanIP := strings.ReplaceAll(dev.IP, ".", "_")
	id := fmt.Sprintf("cam_%s", cleanIP)
	m.editingID = id

	name := dev.Name
	if name == "" {
		name = dev.Model
	}
	if name == "" {
		name = fmt.Sprintf("Camera %s", dev.IP)
	}

	onvifAddr := fmt.Sprintf("%s:%d", dev.IP, dev.Port)
	rtspURL := fmt.Sprintf("rtsp://admin:password@%s:554/live", dev.IP)
	if len(dev.RTSPURLs) > 0 {
		rtspURL = dev.RTSPURLs[0]
	}

	m.inputs[0].SetValue(id)
	m.inputs[1].SetValue(name)
	m.inputs[2].SetValue(onvifAddr)
	m.inputs[3].SetValue(rtspURL)
	m.inputs[4].SetValue("admin")
	m.inputs[5].SetValue("")
	m.inputs[6].SetValue("auto")
	m.inputs[7].SetValue("true")
}

func (m *Model) startEditForm(camID string) {
	m.isNewCam = false
	m.editingID = camID
	m.focusIndex = 0
	cam := m.cfg.Cameras[camID]

	m.inputs[0].SetValue(cam.ID)
	m.inputs[1].SetValue(cam.Name)
	m.inputs[2].SetValue(cam.Address)
	m.inputs[3].SetValue(cam.RTSPURL)
	m.inputs[4].SetValue(cam.ONVIFUsername)
	m.inputs[5].SetValue(cam.ONVIFPassword)
	m.inputs[6].SetValue(cam.SnapshotMethod)
	pullEv := "false"
	if cam.PullEvents {
		pullEv = "true"
	}
	m.inputs[7].SetValue(pullEv)
}

func (m *Model) saveForm() error {
	id := strings.TrimSpace(m.inputs[0].Value())
	if id == "" {
		return fmt.Errorf("camera ID cannot be empty")
	}

	pullEv := strings.ToLower(strings.TrimSpace(m.inputs[7].Value())) == "true"

	camCfg := config.CameraConfig{
		ID:             id,
		Name:           strings.TrimSpace(m.inputs[1].Value()),
		Address:        strings.TrimSpace(m.inputs[2].Value()),
		RTSPURL:        strings.TrimSpace(m.inputs[3].Value()),
		ONVIFUsername:  strings.TrimSpace(m.inputs[4].Value()),
		ONVIFPassword:  strings.TrimSpace(m.inputs[5].Value()),
		SnapshotMethod: strings.TrimSpace(m.inputs[6].Value()),
		PullEvents:     pullEv,
	}

	if camCfg.SnapshotMethod == "" {
		camCfg.SnapshotMethod = "auto"
	}

	// If renamed, remove old ID
	if !m.isNewCam && m.editingID != id {
		delete(m.cfg.Cameras, m.editingID)
	}

	m.cfg.Cameras[id] = camCfg
	return nil
}

func (m *Model) setStatus(msg string, isErr bool) {
	m.statusMsg = msg
	m.statusErr = isErr
	m.statusTime = time.Now()
}

// Commands
func triggerScanCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		opts := discovery.DefaultScanOptions()
		opts.Timeout = 2500 * time.Millisecond
		devs, err := discovery.Scan(ctx, opts)
		return scanDoneMsg{devices: devs, err: err}
	}
}

func testCameraCmd(cfg config.CameraConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		cam := camera.NewCamera(cfg)
		_, err := cam.Snapshot(ctx)
		return testDoneMsg{cameraID: cfg.ID, err: err}
	}
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#008080")).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Background(lipgloss.Color("#222222")).
			Padding(0, 2)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFAA")).
			Background(lipgloss.Color("#2A2A3A"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DDDDDD"))

	statusOkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF88"))

	statusErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))
)

// View
func (m Model) View() string {
	var b strings.Builder

	// Header
	b.WriteString(titleStyle.Render(" CAMSTOP CAMERA MANAGER "))
	b.WriteString(fmt.Sprintf("  Config: %s\n\n", m.configPath))

	switch m.mode {
	case modeScanning:
		b.WriteString("\n  🔍 Scanning local network for ONVIF and RTSP cameras...\n\n")
		b.WriteString(helpStyle.Render("  Please wait (press ESC to cancel)..."))
		return b.String()

	case modeEdit:
		b.WriteString(m.renderEditForm())
		return b.String()

	case modeList:
		b.WriteString(m.renderTabs())
		b.WriteString("\n\n")

		if m.activeTab == tabConfigured {
			b.WriteString(m.renderConfiguredList())
		} else {
			b.WriteString(m.renderDiscoveredList())
		}

		b.WriteString("\n\n")
		b.WriteString(m.renderStatusBar())
		b.WriteString("\n")
		b.WriteString(m.renderKeybindings())
	}

	return b.String()
}

func (m Model) renderTabs() string {
	cfgTab := tabInactiveStyle.Render(fmt.Sprintf("1. Configured (%d)", len(m.configuredKeys)))
	discTab := tabInactiveStyle.Render(fmt.Sprintf("2. Discovered (%d)", len(m.discovered)))

	if m.activeTab == tabConfigured {
		cfgTab = tabActiveStyle.Render(fmt.Sprintf("1. Configured (%d)", len(m.configuredKeys)))
	} else {
		discTab = tabActiveStyle.Render(fmt.Sprintf("2. Discovered (%d)", len(m.discovered)))
	}

	return fmt.Sprintf("  %s %s  %s", cfgTab, discTab, helpStyle.Render("[Tab] switch view"))
}

func (m Model) renderConfiguredList() string {
	if len(m.configuredKeys) == 0 {
		return "    (No configured cameras found in configuration file. Press 's' to scan or 'a' to add.)"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("    %-20s %-24s %-22s %s\n", "CAMERA ID", "NAME", "ONVIF ADDRESS", "SNAPSHOT"))
	b.WriteString("    " + strings.Repeat("─", 80) + "\n")

	for i, id := range m.configuredKeys {
		cam := m.cfg.Cameras[id]
		cursor := "  "
		line := fmt.Sprintf("%-20s %-24s %-22s %s",
			truncate(cam.ID, 19),
			truncate(cam.Name, 23),
			truncate(cam.Address, 21),
			cam.SnapshotMethod,
		)

		if i == m.cfgCursor {
			cursor = "▶ "
			b.WriteString("  " + cursor + selectedStyle.Render(line) + "\n")
		} else {
			b.WriteString("  " + cursor + normalStyle.Render(line) + "\n")
		}
	}

	return b.String()
}

func (m Model) renderDiscoveredList() string {
	if len(m.discovered) == 0 {
		return "    (No cameras discovered yet. Press 's' to broadcast WS-Discovery & RTSP scan.)"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("    %-16s %-12s %-16s %-20s %s\n", "IP ADDRESS", "TYPE", "MANUFACTURER", "MODEL / NAME", "ENDPOINT"))
	b.WriteString("    " + strings.Repeat("─", 84) + "\n")

	for i, d := range m.discovered {
		name := d.Name
		if name == "" {
			name = d.Model
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

		cursor := "  "
		line := fmt.Sprintf("%-16s %-12s %-16s %-20s %s",
			d.IP,
			d.Type,
			truncate(mfr, 15),
			truncate(name, 19),
			truncate(endpoint, 30),
		)

		if i == m.discCursor {
			cursor = "▶ "
			b.WriteString("  " + cursor + selectedStyle.Render(line) + "\n")
		} else {
			b.WriteString("  " + cursor + normalStyle.Render(line) + "\n")
		}
	}

	return b.String()
}

func (m Model) renderEditForm() string {
	var b strings.Builder
	actionTitle := "Edit Camera"
	if m.isNewCam {
		actionTitle = "Add New Camera"
	}

	b.WriteString(fmt.Sprintf("  %s\n", tabActiveStyle.Render(actionTitle)))
	b.WriteString("  " + strings.Repeat("─", 60) + "\n\n")

	labels := []string{
		"Camera ID:",
		"Display Name:",
		"ONVIF Address:",
		"RTSP Stream URL:",
		"ONVIF Username:",
		"ONVIF Password:",
		"Snapshot Method:",
		"Pull Events:",
	}

	for i := range m.inputs {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", labels[i], m.inputs[i].View()))
	}

	b.WriteString("\n  " + helpStyle.Render("[Tab/Down] Next Field   [Enter] Save   [Esc] Cancel"))
	return b.String()
}

func (m Model) renderStatusBar() string {
	if m.statusErr {
		return "  " + statusErrStyle.Render("● "+m.statusMsg)
	}
	return "  " + statusOkStyle.Render("● "+m.statusMsg)
}

func (m Model) renderKeybindings() string {
	if m.activeTab == tabConfigured {
		return helpStyle.Render("  [s] Scan LAN  [a] Add  [e/Enter] Edit  [d] Delete  [t] Test  [w] Write File  [q] Quit")
	}
	return helpStyle.Render("  [s] Rescan  [Enter/a] Add to Config  [Tab] Switch to Configured  [q] Quit")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
