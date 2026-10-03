ARG BUILD_FROM
FROM ${BUILD_FROM}

ARG BUILD_ARCH
ARG CAM_PROXY_VERSION="0.4.0"

RUN apk add --no-cache ca-certificates tzdata ffmpeg curl jq py3-yaml

# Download cam-proxy release binary matching the target architecture, or build from source if not found
RUN set -xe; \
    case "${BUILD_ARCH}" in \
        "aarch64") ARCH="arm64" ;; \
        "amd64") ARCH="amd64" ;; \
        *) echo "Unsupported architecture: ${BUILD_ARCH}"; exit 1 ;; \
    esac; \
    DOWNLOAD_URL="https://github.com/smford/cam-proxy/releases/download/v${CAM_PROXY_VERSION}/cam-proxy_${CAM_PROXY_VERSION}_linux_${ARCH}.tar.gz"; \
    if curl -sSLf "${DOWNLOAD_URL}" -o /tmp/cam-proxy.tar.gz; then \
        tar -xzf /tmp/cam-proxy.tar.gz -C /tmp; \
        mv /tmp/cam-proxy /usr/bin/cam-proxy; \
    else \
        echo "Release tarball not found; building from git repository..."; \
        apk add --no-cache go git; \
        git clone --depth 1 https://github.com/smford/cam-proxy.git /tmp/src; \
        cd /tmp/src; \
        CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/bin/cam-proxy ./cmd/cam-proxy; \
        apk del go git; \
    fi; \
    chmod +x /usr/bin/cam-proxy; \
    rm -rf /tmp/* /root/go /root/.cache

COPY run.sh /run.sh
RUN chmod a+x /run.sh

CMD [ "/run.sh" ]
