#!/usr/bin/env bash
# Cross-compile static binaries into dist/. Usage: scripts/release.sh <version>
set -euo pipefail
VERSION="${1:-dev}"
cd "$(dirname "$0")/.."
mkdir -p dist
for target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""; [ "$os" = windows ] && ext=.exe
  out="dist/seedkit-${VERSION}-${os}-${arch}${ext}"
  echo "-> $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$out" ./cmd/seedkit
done
(cd dist && sha256sum seedkit-* > SHA256SUMS)
