# AGENTS.md

Instructions, conventions, and architectural context for AI agents working in the `cam-proxy` codebase.

---

## 1. Project Overview

`cam-proxy` is a lightweight, zero-CGO edge camera gateway written in Go. It proxies IP cameras (ONVIF and RTSP) to provide:
- **Fast HTTP Snapshots**: Native ONVIF HTTP extraction (<100ms) with fallback to RTSP keyframe capture.
- **Hardware Protection**: Single-flight request coalescing (`golang.org/x/sync/singleflight`) and configurable short-TTL in-memory frame caching to prevent hardware/socket flooding on low-cost IP cameras.
- **PTZ Proxying**: Continuous move, stop, and preset recall via ONVIF SOAP services with automatic snapshot cache invalidation.
- **Event Forwarding**: Subscribes to ONVIF PullPoint event streams and forwards motion/input events over MQTT.
- **Network Discovery & Interactive TUI**: WS-Discovery UDP probe to detect local cameras and an interactive Charmbracelet Bubble Tea terminal wizard.

---

## 2. Technical Stack & Constraints

- **Language**: Go 1.26+ (module: `github.com/smford/cam-proxy`)
- **Zero CGO**: All binaries must build with `CGO_ENABLED=0` for pure static edge deployment across `linux/amd64`, `linux/arm64`, and `darwin`. Do not introduce dependencies requiring CGO or native system headers.
- **Key Libraries**:
  - `github.com/bluenviron/gortsplib/v5` & `github.com/pion/rtp` (RTSP keyframe extraction)
  - `github.com/eclipse/paho.mqtt.golang` (MQTT client)
  - `github.com/charmbracelet/bubbletea` & `lipgloss` (Interactive terminal setup wizard)
  - `golang.org/x/sync/singleflight` (Snapshot deduplication)
  - `gopkg.in/yaml.v3` (Configuration parsing & generation)

---

## 3. Architecture & Directory Structure

```text
cam-proxy/
├── cmd/
│   └── cam-proxy/        # Main CLI entrypoint (flag parsing, daemon, --scan, --tui)
├── internal/
│   ├── api/             # HTTP REST endpoints (Go 1.22+ ServeMux routing)
│   ├── camera/          # Camera state, Manager, singleflight, TTL frame caching, health
│   ├── config/          # YAML configuration loading, defaults, normalization, saving
│   ├── discovery/       # WS-Discovery (UDP SOAP multicast) probe
│   ├── metrics/         # Prometheus observability metrics collector & registry
│   ├── mqtt/            # MQTT event publisher
│   ├── onvif/           # SOAP envelopes, WS-Security, Digest auth, PullPoint, PTZ
│   ├── rtsp/            # RTSP keyframe capture fallback
│   └── tui/             # Charmbracelet Bubble Tea interactive terminal setup
├── config.example.yaml  # Reference YAML configuration
├── Makefile             # Development automation targets
├── Dockerfile           # Minimal multi-stage scratch/alpine container build
└── .goreleaser.yaml     # Cross-platform release packaging
```

---

## 4. Development & Verification Workflow

Agents **must** verify all changes using the repository's standard quality gates before committing.

### Essential Commands

| Task | Command | Notes |
| :--- | :--- | :--- |
| **Run Tests** | `make test` or `go test -v -race ./...` | Must always pass with `-race` enabled. |
| **Formatting** | `gofmt -s -l .` / `gofmt -s -w <file>` | Zero output expected from `gofmt -s -l`. |
| **SAST Scan** | `make sast` | Runs Semgrep OSS (`uvx semgrep scan --config auto`). |
| **Build Binary** | `make build` | Produces static executable `cam-proxy`. |
| **Release Check** | `goreleaser check` | Validates `.goreleaser.yaml` schema. |
| **Run Daemon** | `make run` | Runs against `config.example.yaml` at debug log level. |

---

## 5. Coding & Concurrency Guidelines

### Camera Hardware Resilience & Concurrency
- **Single-Flight Coalescing**:
  Always route snapshot requests through `Camera.Snapshot(ctx)` which uses `singleflight.Group` (`sf.DoChan`). Never make direct uncoalesced camera connections on HTTP handler triggers.
- **Context Isolation in Single-Flight**:
  Use `sf.DoChan` and select on `ctx.Done()`. If an individual caller cancels their context or times out, they receive `ctx.Err()`, but the underlying in-flight singleflight fetch must continue bounded by its own safety timeout so other concurrent callers receive their data.
- **Defensive Copying**:
  Any byte slice returned from `c.cachedFrame` or shared via `singleflight` must be defensively copied (`copy(dest, src)`) before handing it to callers to prevent data races.
- **PTZ Invalidation**:
  Any operation that changes camera positioning (`PTZMove`, `PTZStop`, `PTZPreset`) must immediately invalidate cached frames (`c.InvalidateCache()`) and increment the cache generation counter.

### Error Handling & Logging
- Wrap errors using `fmt.Errorf("...: %w", err)`.
- Define sentinel errors in package scopes (e.g., `camera.ErrCameraNotFound`, `camera.ErrNoSnapshotSource`).
- Use structured logging via Go's standard `log/slog` (`slog.Info`, `slog.Warn`, `slog.Error`, `slog.Debug`) with key-value pairs (e.g., `"camera_id", id`, `"err", err`).
- Never log plain text passwords or authentication headers.

### HTTP Routing & API Conventions
- Use Go 1.22+ standard library method-prefixed path patterns on `http.ServeMux` (e.g. `GET /healthz`, `GET /api/v1/status`, `GET /metrics`, `GET /api/v1/cameras/{id}/snapshot`, `POST /api/v1/cameras/{id}/ptz`).
- Snapshot endpoints return `Content-Type: image/jpeg` and `Cache-Control: no-cache, no-store, must-revalidate`.
- Prometheus metrics are exposed at `GET /metrics`.

---

## 6. Security & CI/CD Standards

- **GitHub Actions Pinning**:
  All GitHub Actions in `.github/workflows/` must be pinned to **full 40-character commit SHAs** rather than mutable release tags (e.g., `actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2`).
- **Security Auditing**:
  Code must pass GitHub CodeQL and Semgrep OSS (`make sast`). Avoid shell command injection or arbitrary command evaluation.
- **Minimal Attack Surface**:
  The compiled daemon must run as an unprivileged user inside Docker containers with read-only root filesystems where applicable.
