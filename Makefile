BINARY_NAME := cam-proxy
MODULE      := github.com/smford/cam-proxy
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE        ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS     := -s -w \
               -X main.version=$(VERSION) \
               -X main.commit=$(COMMIT) \
               -X main.date=$(DATE)

.PHONY: all help build build-linux-arm64 build-linux-amd64 clean test lint run scan sast dast release release-patch release-minor release-major release-tag release-snapshot tag-patch tag-minor tag-major docker-build docker-run

all: build

help:
	@echo "cam-proxy development targets:"
	@echo "  build              Build static binary for local architecture"
	@echo "  build-linux-arm64  Cross-compile static binary for linux/arm64 (Raspberry Pi)"
	@echo "  build-linux-amd64  Cross-compile static binary for linux/amd64"
	@echo "  test               Run test suite with race detector (-race)"
	@echo "  lint               Verify code formatting (gofmt), go vet, and modules"
	@echo "  sast               Run Static Application Security Testing (Semgrep OSS)"
	@echo "  dast               Run Dynamic Application Security Testing (OWASP ZAP)"
	@echo "  run                Build and run local daemon with config.example.yaml"
	@echo "  scan               Scan local network for ONVIF and RTSP cameras"
	@echo "  tui                Launch interactive terminal setup wizard"
	@echo "  release            Initiate GitHub release (default: next patch, or TAG=...)"
	@echo "  release-patch      Calculate next patch version, tag, and push to origin"
	@echo "  release-minor      Calculate next minor version, tag, and push to origin"
	@echo "  release-major      Calculate next major version, tag, and push to origin"
	@echo "  release-snapshot   Build local snapshot release archives via GoReleaser"
	@echo "  docker-build       Build local Docker container image"
	@echo "  docker-run         Run local Docker container with host networking"
	@echo "  clean              Remove build artifacts and temporary binaries"

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/cam-proxy

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME)-linux-arm64 ./cmd/cam-proxy

build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME)-linux-amd64 ./cmd/cam-proxy

test:
	go test -v -race ./...

lint:
	@echo "Checking formatting (gofmt -s -l)..."
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files found:\n$$unformatted" >&2; \
		exit 1; \
	fi
	@echo "Running go vet..."
	go vet ./...
	@echo "Verifying modules..."
	go mod verify

sast:
	uvx semgrep scan --config auto

dast: docker-build
	docker run -d --name cam-proxy-dast-local --rm -p 8080:8080 -v $$(pwd)/.zap/dast-config.yaml:/etc/cam-proxy/cam-proxy.yaml:ro $(BINARY_NAME):latest
	@echo "Waiting for cam-proxy to start..."
	@sleep 2
	@curl -sf http://127.0.0.1:8080/healthz || (docker stop cam-proxy-dast-local && exit 1)
	@echo "Running OWASP ZAP Baseline Scan..."
	docker run --rm --network host -v $$(pwd)/.zap:/zap/wrk/:rw ghcr.io/zaproxy/zaproxy:stable zap-baseline.py -t http://localhost:8080 -c rules.tsv -m 3 || true
	docker stop cam-proxy-dast-local

scan: build
	./$(BINARY_NAME) --scan

tui: build
	./$(BINARY_NAME) --tui

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* camstop camstop-* camtap camtap-* coverage.out

run: build
	./$(BINARY_NAME) -config config.example.yaml -log-level debug

docker-build:
	docker build -t $(BINARY_NAME):latest .

docker-run:
	docker run --rm -it --network host \
		-v $$(pwd)/cam-proxy.yaml:/etc/cam-proxy/cam-proxy.yaml:ro \
		$(BINARY_NAME):latest

release-snapshot:
	goreleaser release --snapshot --clean

# GitHub Release targets (tags and pushes to origin to trigger GitHub Actions release workflow)
release:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory has uncommitted changes. Commit or stash them before releasing." >&2; \
		exit 1; \
	fi
	@if [ -n "$(TAG)" ]; then \
		$(MAKE) release-tag TAG="$(TAG)"; \
	elif [ "$(origin VERSION)" = "command line" ]; then \
		$(MAKE) release-tag TAG="$(VERSION)"; \
	else \
		$(MAKE) release-patch; \
	fi

release-patch:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory has uncommitted changes. Commit or stash them before releasing." >&2; \
		exit 1; \
	fi
	@git fetch --tags origin
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	patch=$$(echo $$v | cut -d. -f3); \
	next="v$$major.$$minor.$$((patch + 1))"; \
	$(MAKE) release-tag TAG=$$next

release-minor:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory has uncommitted changes. Commit or stash them before releasing." >&2; \
		exit 1; \
	fi
	@git fetch --tags origin
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	next="v$$major.$$((minor + 1)).0"; \
	$(MAKE) release-tag TAG=$$next

release-major:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory has uncommitted changes. Commit or stash them before releasing." >&2; \
		exit 1; \
	fi
	@git fetch --tags origin
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	next="v$$((major + 1)).0.0"; \
	$(MAKE) release-tag TAG=$$next

release-tag:
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG is required (e.g. make release TAG=v1.0.0)" >&2; \
		exit 1; \
	fi
	@tag="$(TAG)"; \
	case "$$tag" in \
		v*) ;; \
		*) tag="v$$tag" ;; \
	esac; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: Working directory has uncommitted changes. Commit or stash them before releasing." >&2; \
		exit 1; \
	fi; \
	git fetch --tags origin; \
	if git ls-remote --tags --exit-code origin "refs/tags/$$tag" >/dev/null 2>&1; then \
		echo "Error: Tag $$tag already exists on origin." >&2; \
		exit 1; \
	fi; \
	branch=$$(git rev-parse --abbrev-ref HEAD); \
	if [ "$$branch" != "HEAD" ]; then \
		echo "Pushing branch '$$branch' to origin..."; \
		git push origin "$$branch" || exit 1; \
	fi; \
	echo "Initiating release for $$tag..."; \
	if ! git rev-parse -q --verify "refs/tags/$$tag" >/dev/null; then \
		git tag -a "$$tag" -m "Release $$tag" || exit 1; \
	fi; \
	git push origin "$$tag" && \
	echo "Successfully pushed tag $$tag. GitHub Actions release workflow initiated."

# SemVer Local Tagging Helper targets (tags locally only)
tag-patch:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	patch=$$(echo $$v | cut -d. -f3); \
	next="v$$major.$$minor.$$((patch + 1))"; \
	echo "Tagging $$next locally"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run 'git push origin $$next' or 'make release-patch' to publish."

tag-minor:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	next="v$$major.$$((minor + 1)).0"; \
	echo "Tagging $$next locally"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run 'git push origin $$next' or 'make release-minor' to publish."

tag-major:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	next="v$$((major + 1)).0.0"; \
	echo "Tagging $$next locally"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run 'git push origin $$next' or 'make release-major' to publish."
