package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	// Navigate up until we find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repository root containing go.mod")
		}
		dir = parent
	}
}

func TestSystemdUnitFile(t *testing.T) {
	root := findRepoRoot(t)
	servicePath := filepath.Join(root, "systemd", "cam-proxy.service")

	data, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("failed to read systemd service file at %s: %v", servicePath, err)
	}

	content := string(data)

	// Verify required sections
	requiredSections := []string{"[Unit]", "[Service]", "[Install]"}
	for _, sec := range requiredSections {
		if !strings.Contains(content, sec) {
			t.Errorf("systemd service file missing section %s", sec)
		}
	}

	// Verify key directives
	scanner := bufio.NewScanner(strings.NewReader(content))
	directives := make(map[string]string)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			directives[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	expectedDirectives := map[string]string{
		"DynamicUser":            "yes",
		"ProtectSystem":          "strict",
		"Restart":                "always",
		"NoNewPrivileges":        "yes",
		"ProtectHome":            "yes",
		"PrivateTmp":             "yes",
		"PrivateDevices":         "yes",
		"ProtectControlGroups":   "yes",
		"MemoryDenyWriteExecute": "yes",
		"ConfigurationDirectory": "cam-proxy",
		"WantedBy":               "multi-user.target",
	}

	for key, expectedVal := range expectedDirectives {
		actualVal, exists := directives[key]
		if !exists {
			t.Errorf("missing required systemd directive %q", key)
		} else if actualVal != expectedVal {
			t.Errorf("expected directive %s=%q, got %q", key, expectedVal, actualVal)
		}
	}

	// Verify ExecStart points to cam-proxy binary with config flag
	execStart, ok := directives["ExecStart"]
	if !ok {
		t.Fatalf("missing ExecStart directive")
	}
	if !strings.Contains(execStart, "cam-proxy") {
		t.Errorf("expected ExecStart to reference cam-proxy binary, got %q", execStart)
	}
	if !strings.Contains(execStart, "-config") {
		t.Errorf("expected ExecStart to pass -config parameter, got %q", execStart)
	}
}

func TestPackagingAndDistribution(t *testing.T) {
	root := findRepoRoot(t)
	configPath := filepath.Join(root, ".goreleaser.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .goreleaser.yaml: %v", err)
	}

	var goreleaserConfig struct {
		Version  int `yaml:"version"`
		Archives []struct {
			Files []string `yaml:"files"`
		} `yaml:"archives"`
	}

	if err := yaml.Unmarshal(data, &goreleaserConfig); err != nil {
		t.Fatalf("failed to parse .goreleaser.yaml: %v", err)
	}

	if goreleaserConfig.Version != 2 {
		t.Errorf("expected GoReleaser version 2, got %d", goreleaserConfig.Version)
	}

	// Verify systemd unit template is packaged in release archives
	var foundSystemdInArchive bool
	for _, archive := range goreleaserConfig.Archives {
		for _, f := range archive.Files {
			if strings.Contains(f, "systemd/cam-proxy.service") {
				foundSystemdInArchive = true
				break
			}
		}
	}
	if !foundSystemdInArchive {
		t.Errorf("expected systemd/cam-proxy.service to be included in archive files")
	}

	// Verify Homebrew formula generator script exists and is executable
	scriptPath := filepath.Join(root, "scripts", "generate_formula.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("failed to stat scripts/generate_formula.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("scripts/generate_formula.sh is not executable: mode %v", info.Mode())
	}

	// Verify workflow release file publishes to smford/homebrew-tap using HOMEBREW_TAP_GITHUB_TOKEN
	wfPath := filepath.Join(root, ".github", "workflows", "release.yml")
	wfData, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("failed to read .github/workflows/release.yml: %v", err)
	}
	wfContent := string(wfData)
	if !strings.Contains(wfContent, "smford/homebrew-tap") {
		t.Errorf("expected release workflow to target smford/homebrew-tap")
	}
	if !strings.Contains(wfContent, "HOMEBREW_TAP_GITHUB_TOKEN") {
		t.Errorf("expected release workflow to use secret HOMEBREW_TAP_GITHUB_TOKEN")
	}
	if !strings.Contains(wfContent, "Formula/cam-proxy.rb") {
		t.Errorf("expected release workflow to publish Formula/cam-proxy.rb")
	}
}

func TestGitHubPagesAndDocumentation(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Verify docs/index.html exists and is configured for light mode with release details
	docsHTMLPath := filepath.Join(root, "docs", "index.html")
	htmlData, err := os.ReadFile(docsHTMLPath)
	if err != nil {
		t.Fatalf("failed to read docs/index.html: %v", err)
	}
	htmlContent := string(htmlData)

	if !strings.Contains(htmlContent, "color-scheme: light") && !strings.Contains(htmlContent, "color-scheme\" content=\"light\"") {
		t.Errorf("expected docs/index.html to be explicitly configured for light mode")
	}
	if !strings.Contains(htmlContent, "release-version") {
		t.Errorf("expected docs/index.html to contain release-version element")
	}

	// 2. Verify scripts/update-docs-version.sh exists and is executable
	scriptPath := filepath.Join(root, "scripts", "update-docs-version.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("failed to stat scripts/update-docs-version.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("scripts/update-docs-version.sh is not executable: mode %v", info.Mode())
	}

	// 3. Verify .github/workflows/pages.yml triggers and permissions
	pagesWfPath := filepath.Join(root, ".github", "workflows", "pages.yml")
	pagesData, err := os.ReadFile(pagesWfPath)
	if err != nil {
		t.Fatalf("failed to read .github/workflows/pages.yml: %v", err)
	}
	pagesContent := string(pagesData)

	if !strings.Contains(pagesContent, "actions/deploy-pages") {
		t.Errorf("expected pages workflow to deploy pages")
	}
	if !strings.Contains(pagesContent, "docs/**") {
		t.Errorf("expected pages workflow to trigger on docs/** changes")
	}
	if !strings.Contains(pagesContent, "release:") {
		t.Errorf("expected pages workflow to trigger on release")
	}
}
