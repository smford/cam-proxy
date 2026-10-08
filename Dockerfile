# Build stage
FROM golang:1.27-alpine@sha256:738d1cf061836894ff6bb8c33881080ac66de8cf0586615012a0c8f592649cfa AS builder

WORKDIR /src

# Download dependencies first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build statically linked binary
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /bin/cam-proxy ./cmd/cam-proxy

# Runtime stage
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# Install ca-certificates (HTTPS/ONVIF), tzdata (timezones), and ffmpeg (H.264 snapshot decoding)
RUN apk add --no-cache ca-certificates tzdata ffmpeg

# Create non-root user and configuration directory
RUN addgroup -S cam-proxy && adduser -S cam-proxy -G cam-proxy && \
    mkdir -p /etc/cam-proxy && \
    chown -R cam-proxy:cam-proxy /etc/cam-proxy

COPY --from=builder /bin/cam-proxy /usr/local/bin/cam-proxy

USER cam-proxy
WORKDIR /etc/cam-proxy

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q -O- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/cam-proxy"]
CMD ["-config", "/etc/cam-proxy/cam-proxy.yaml"]
