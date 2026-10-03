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

func TestGitHubStatsWorkflow(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Verify .gh-stats.yml exists and has valid YAML syntax
	configPath := filepath.Join(root, ".gh-stats.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .gh-stats.yml: %v", err)
	}
	var ghStatsConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &ghStatsConfig); err != nil {
		t.Fatalf("failed to parse .gh-stats.yml: %v", err)
	}

	// 2. Verify .github/workflows/gh-stats.yml exists and uses smford/gh-stats
	wfPath := filepath.Join(root, ".github", "workflows", "gh-stats.yml")
	wfData, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("failed to read .github/workflows/gh-stats.yml: %v", err)
	}
	wfContent := string(wfData)

	if !strings.Contains(wfContent, "smford/gh-stats@") {
		t.Errorf("expected gh-stats workflow to use smford/gh-stats")
	}
	if !strings.Contains(wfContent, "security-events: write") {
		t.Errorf("expected gh-stats workflow to have security-events: write permission")
	}
	if !strings.Contains(wfContent, "pull_request:") || !strings.Contains(wfContent, "push:") {
		t.Errorf("expected gh-stats workflow to trigger on pull_request and push")
	}
}

func TestHomeAssistantAddonAndDocs(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Verify repository.yaml
	repoYAMLPath := filepath.Join(root, "repository.yaml")
	data, err := os.ReadFile(repoYAMLPath)
	if err != nil {
		t.Fatalf("failed to read repository.yaml: %v", err)
	}
	var repoMeta struct {
		Name       string `yaml:"name"`
		URL        string `yaml:"url"`
		Maintainer string `yaml:"maintainer"`
	}
	if err := yaml.Unmarshal(data, &repoMeta); err != nil {
		t.Fatalf("failed to parse repository.yaml: %v", err)
	}
	if repoMeta.Name == "" || repoMeta.URL == "" {
		t.Errorf("repository.yaml missing name or url: %+v", repoMeta)
	}

	// 2. Verify ha-addon/cam-proxy/config.yaml
	configYAMLPath := filepath.Join(root, "ha-addon", "cam-proxy", "config.yaml")
	cfgData, err := os.ReadFile(configYAMLPath)
	if err != nil {
		t.Fatalf("failed to read ha-addon/cam-proxy/config.yaml: %v", err)
	}
	var addonConfig struct {
		Name string   `yaml:"name"`
		Slug string   `yaml:"slug"`
		Arch []string `yaml:"arch"`
	}
	if err := yaml.Unmarshal(cfgData, &addonConfig); err != nil {
		t.Fatalf("failed to parse ha-addon/cam-proxy/config.yaml: %v", err)
	}
	if addonConfig.Slug != "cam-proxy" {
		t.Errorf("expected slug 'cam-proxy', got %q", addonConfig.Slug)
	}
	hasAmd64, hasAarch64 := false, false
	for _, a := range addonConfig.Arch {
		if a == "amd64" {
			hasAmd64 = true
		}
		if a == "aarch64" {
			hasAarch64 = true
		}
	}
	if !hasAmd64 || !hasAarch64 {
		t.Errorf("expected addon config to support amd64 and aarch64, got %v", addonConfig.Arch)
	}

	// 3. Verify ha-addon/cam-proxy/build.yaml
	buildYAMLPath := filepath.Join(root, "ha-addon", "cam-proxy", "build.yaml")
	buildData, err := os.ReadFile(buildYAMLPath)
	if err != nil {
		t.Fatalf("failed to read ha-addon/cam-proxy/build.yaml: %v", err)
	}
	var buildConfig struct {
		BuildFrom map[string]string `yaml:"build_from"`
	}
	if err := yaml.Unmarshal(buildData, &buildConfig); err != nil {
		t.Fatalf("failed to parse ha-addon/cam-proxy/build.yaml: %v", err)
	}
	if buildConfig.BuildFrom["amd64"] == "" || buildConfig.BuildFrom["aarch64"] == "" {
		t.Errorf("expected build.yaml to define build_from for amd64 and aarch64: %+v", buildConfig)
	}

	// 4. Verify ha-addon/cam-proxy/run.sh
	runScriptPath := filepath.Join(root, "ha-addon", "cam-proxy", "run.sh")
	info, err := os.Stat(runScriptPath)
	if err != nil {
		t.Fatalf("failed to stat ha-addon/cam-proxy/run.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("ha-addon/cam-proxy/run.sh is not executable: mode %v", info.Mode())
	}

	// 5. Verify ha-addon/cam-proxy/Dockerfile & DOCS.md
	dockerfilePath := filepath.Join(root, "ha-addon", "cam-proxy", "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		t.Errorf("expected ha-addon/cam-proxy/Dockerfile to exist: %v", err)
	}
	docsPath := filepath.Join(root, "ha-addon", "cam-proxy", "DOCS.md")
	if _, err := os.Stat(docsPath); err != nil {
		t.Errorf("expected ha-addon/cam-proxy/DOCS.md to exist: %v", err)
	}

	// 6. Verify docs/home-assistant.md
	guidePath := filepath.Join(root, "docs", "home-assistant.md")
	guideData, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("failed to read docs/home-assistant.md: %v", err)
	}
	guideContent := string(guideData)
	if !strings.Contains(guideContent, "Generic Camera") {
		t.Errorf("expected docs/home-assistant.md to cover Generic Camera")
	}
	if !strings.Contains(guideContent, "rest_command") {
		t.Errorf("expected docs/home-assistant.md to cover rest_command for PTZ")
	}
	if !strings.Contains(guideContent, "MQTT") {
		t.Errorf("expected docs/home-assistant.md to cover MQTT")
	}

	// 7. Verify docs/index.html includes Home Assistant section and nav link
	indexHTMLPath := filepath.Join(root, "docs", "index.html")
	indexData, err := os.ReadFile(indexHTMLPath)
	if err != nil {
		t.Fatalf("failed to read docs/index.html: %v", err)
	}
	indexContent := string(indexData)
	if !strings.Contains(indexContent, "id=\"home-assistant\"") {
		t.Errorf("expected docs/index.html to contain section id='home-assistant'")
	}
	if !strings.Contains(indexContent, "href=\"#home-assistant\"") {
		t.Errorf("expected docs/index.html to contain nav link href='#home-assistant'")
	}
}

func TestShellCheckWorkflow(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Verify .github/workflows/shellcheck.yml exists
	wfPath := filepath.Join(root, ".github", "workflows", "shellcheck.yml")
	wfData, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("failed to read .github/workflows/shellcheck.yml: %v", err)
	}

	// 2. Verify valid YAML
	var wfConfig map[string]interface{}
	if err := yaml.Unmarshal(wfData, &wfConfig); err != nil {
		t.Fatalf("failed to parse .github/workflows/shellcheck.yml: %v", err)
	}

	wfContent := string(wfData)

	// 3. Verify triggers on push and pull_request
	if !strings.Contains(wfContent, "push:") || !strings.Contains(wfContent, "pull_request:") {
		t.Errorf("expected shellcheck workflow to trigger on push and pull_request")
	}

	// 4. Verify uses ludeeus/action-shellcheck
	if !strings.Contains(wfContent, "ludeeus/action-shellcheck@") {
		t.Errorf("expected shellcheck workflow to use ludeeus/action-shellcheck")
	}

	// 5. Verify action is pinned to release commit SHA (00cae500b08a931fb5698e11e79bfbd38e612a38)
	expectedSHA := "00cae500b08a931fb5698e11e79bfbd38e612a38"
	if !strings.Contains(wfContent, "ludeeus/action-shellcheck@"+expectedSHA) {
		t.Errorf("expected ludeeus/action-shellcheck to be pinned to release SHA %s", expectedSHA)
	}

	// 6. Verify all 'uses:' references in the workflow are pinned to 40-character commit SHAs
	scanner := bufio.NewScanner(strings.NewReader(wfContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "uses:") {
			parts := strings.Split(line, "@")
			if len(parts) != 2 {
				t.Errorf("malformed uses directive: %s", line)
				continue
			}
			shaAndComment := strings.Fields(parts[1])
			if len(shaAndComment) == 0 {
				t.Errorf("missing version or SHA: %s", line)
				continue
			}
			sha := shaAndComment[0]
			if len(sha) != 40 {
				t.Errorf("action %s is not pinned to 40-character SHA (got %q)", line, sha)
			}
		}
	}
}

func TestPreCommitConfig(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Verify .pre-commit-config.yaml exists
	configPath := filepath.Join(root, ".pre-commit-config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .pre-commit-config.yaml: %v", err)
	}

	// 2. Parse YAML
	var config struct {
		Repos []struct {
			Repo  string `yaml:"repo"`
			Rev   string `yaml:"rev"`
			Hooks []struct {
				ID string `yaml:"id"`
			} `yaml:"hooks"`
		} `yaml:"repos"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse .pre-commit-config.yaml: %v", err)
	}

	if len(config.Repos) == 0 {
		t.Fatalf("expected repos to be defined in .pre-commit-config.yaml")
	}

	// 3. Verify key hooks are configured
	hookIDs := make(map[string]bool)
	for _, r := range config.Repos {
		for _, h := range r.Hooks {
			hookIDs[h.ID] = true
		}
	}

	requiredHooks := []string{
		"trailing-whitespace",
		"end-of-file-fixer",
		"check-yaml",
		"shellcheck",
		"go-fmt",
		"go-vet-mod",
		"go-mod-tidy",
		"go-test-mod",
	}

	for _, hook := range requiredHooks {
		if !hookIDs[hook] {
			t.Errorf("missing expected hook %q in .pre-commit-config.yaml", hook)
		}
	}
}
