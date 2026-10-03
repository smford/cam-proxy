BINARY_NAME := camstop
MODULE      := github.com/smford/camstop
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE        ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS     := -s -w \
               -X main.version=$(VERSION) \
               -X main.commit=$(COMMIT) \
               -X main.date=$(DATE)

.PHONY: all build clean test lint run scan sast release-snapshot tag-patch tag-minor tag-major docker-build docker-run

all: build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/camstop

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME)-linux-arm64 ./cmd/camstop

build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME)-linux-amd64 ./cmd/camstop

test:
	go test -v -race ./...

sast:
	uvx semgrep scan --config auto

scan: build
	./$(BINARY_NAME) --scan

tui: build
	./$(BINARY_NAME) --tui

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* camtap camtap-* coverage.out

run: build
	./$(BINARY_NAME) -config config.example.yaml -log-level debug

docker-build:
	docker build -t $(BINARY_NAME):latest .

docker-run:
	docker run --rm -it --network host \
		-v $$(pwd)/camstop.yaml:/etc/camstop/camstop.yaml:ro \
		$(BINARY_NAME):latest

release-snapshot:
	goreleaser release --snapshot --clean

# SemVer Helper targets
tag-patch:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	patch=$$(echo $$v | cut -d. -f3); \
	next="v$$major.$$minor.$$((patch + 1))"; \
	echo "Tagging $$next"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run: git push origin $$next"

tag-minor:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	minor=$$(echo $$v | cut -d. -f2); \
	next="v$$major.$$((minor + 1)).0"; \
	echo "Tagging $$next"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run: git push origin $$next"

tag-major:
	@latest=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	v=$${latest#v}; \
	major=$$(echo $$v | cut -d. -f1); \
	next="v$$((major + 1)).0.0"; \
	echo "Tagging $$next"; \
	git tag -a $$next -m "Release $$next" && echo "Created tag $$next. Run: git push origin $$next"
