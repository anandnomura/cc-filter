#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ARCH=${1:-amd64}
case "$ARCH" in amd64|arm64) ;; *) echo "Usage: $0 [amd64|arm64]" >&2; exit 2 ;; esac
if ! command -v go >/dev/null 2>&1; then
  echo "An approved Go 1.23.12+ compiler is required for this native Linux Edge build." >&2
  exit 1
fi
mkdir -p "$ROOT_DIR/dist"
cd "$ROOT_DIR"
go test -mod=vendor ./cmd/bap-edge ./configs ./internal/...
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -mod=vendor -trimpath -o "dist/bap-edge-linux-$ARCH" ./cmd/bap-edge
sha256sum "dist/bap-edge-linux-$ARCH"
