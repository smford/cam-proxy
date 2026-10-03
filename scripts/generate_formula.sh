#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
TAG_NAME="${2:-}"
DIST_DIR="${3:-dist}"
OUT_FILE="${4:-${DIST_DIR}/cam-proxy.rb}"

if [ -z "$VERSION" ]; then
  if [ -n "${GITHUB_REF_NAME:-}" ]; then
    VERSION="${GITHUB_REF_NAME#v}"
    TAG_NAME="${GITHUB_REF_NAME}"
  else
    LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.3.0")
    VERSION="${LATEST_TAG#v}"
    TAG_NAME="$LATEST_TAG"
  fi
fi

# Normalize version and tag
VERSION="${VERSION#v}"
if [ -z "$TAG_NAME" ]; then
  TAG_NAME="v${VERSION}"
fi

calc_hash() {
  local target="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$target" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$target" | cut -d' ' -f1
  else
    echo "REPLACE_WITH_SHA256"
  fi
}

get_sha() {
  local os="$1"
  local arch="$2"
  local file="${DIST_DIR}/cam-proxy_${VERSION}_${os}_${arch}.tar.gz"

  if [ -f "$file" ]; then
    calc_hash "$file"
  elif [ -f "${DIST_DIR}/checksums.txt" ]; then
    local line
    line=$(grep -E "cam-proxy.*_${os}_${arch}\.tar\.gz" "${DIST_DIR}/checksums.txt" | head -n 1 || true)
    if [ -n "$line" ]; then
      echo "$line" | cut -d' ' -f1
    else
      echo "REPLACE_WITH_SHA256"
    fi
  else
    local found
    found=$(find "${DIST_DIR}" -name "cam-proxy*_${os}_${arch}.tar.gz" 2>/dev/null | head -n 1 || true)
    if [ -n "$found" ] && [ -f "$found" ]; then
      calc_hash "$found"
    else
      echo "REPLACE_WITH_SHA256"
    fi
  fi
}

SHA_DARWIN_ARM64=$(get_sha "darwin" "arm64")
SHA_DARWIN_AMD64=$(get_sha "darwin" "amd64")
SHA_LINUX_ARM64=$(get_sha "linux" "arm64")
SHA_LINUX_AMD64=$(get_sha "linux" "amd64")

mkdir -p "$(dirname "$OUT_FILE")"

cat <<EOF > "$OUT_FILE"
# typed: false
# frozen_string_literal: true

# This formula was auto-generated for cam-proxy (https://github.com/smford/cam-proxy).
class CamProxy < Formula
  desc "Lightweight edge camera gateway — snapshots, PTZ control, and ONVIF-to-MQTT events"
  homepage "https://github.com/smford/cam-proxy"
  version "${VERSION}"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/smford/cam-proxy/releases/download/v#{version}/cam-proxy_#{version}_darwin_arm64.tar.gz"
      sha256 "${SHA_DARWIN_ARM64}"
    end
    on_intel do
      url "https://github.com/smford/cam-proxy/releases/download/v#{version}/cam-proxy_#{version}_darwin_amd64.tar.gz"
      sha256 "${SHA_DARWIN_AMD64}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/smford/cam-proxy/releases/download/v#{version}/cam-proxy_#{version}_linux_arm64.tar.gz"
      sha256 "${SHA_LINUX_ARM64}"
    end
    on_intel do
      url "https://github.com/smford/cam-proxy/releases/download/v#{version}/cam-proxy_#{version}_linux_amd64.tar.gz"
      sha256 "${SHA_LINUX_AMD64}"
    end
  end

  def install
    bin.install "cam-proxy"
  end

  test do
    assert_match "cam-proxy version", shell_output("#{bin}/cam-proxy --version")
  end
end
EOF

echo "Generated Homebrew formula at ${OUT_FILE}"
