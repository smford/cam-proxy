# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Download dependencies first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build statically linked binary
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o /bin/camstop ./cmd/camstop

# Runtime stage
FROM alpine:3.21

# Install ca-certificates (HTTPS/ONVIF), tzdata (timezones), and ffmpeg (H.264 snapshot decoding)
RUN apk add --no-cache ca-certificates tzdata ffmpeg

# Create non-root user and configuration directory
RUN addgroup -S camstop && adduser -S camstop -G camstop && \
    mkdir -p /etc/camstop && \
    chown -R camstop:camstop /etc/camstop

COPY --from=builder /bin/camstop /usr/local/bin/camstop

USER camstop
WORKDIR /etc/camstop

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/camstop"]
CMD ["-config", "/etc/camstop/camstop.yaml"]
