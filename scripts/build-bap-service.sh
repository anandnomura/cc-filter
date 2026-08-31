#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENGINE=${1:-}
if [ -z "$ENGINE" ]; then
  if command -v podman >/dev/null 2>&1; then ENGINE=podman; else ENGINE=docker; fi
fi
case "$ENGINE" in podman|docker) ;; *) echo "Usage: $0 [podman|docker]" >&2; exit 2 ;; esac

echo "Building Linux BAP Service image with $ENGINE..."
TAG=${BAP_IMAGE_TAG:-bap-service:local}
VERSION=${BAP_BUILD_VERSION:-dev}
GO_BUILD_IMAGE=${BAP_GO_BUILD_IMAGE:-docker.io/library/golang:1.23-bookworm}
RUNTIME_IMAGE=${BAP_RUNTIME_IMAGE:-docker.io/library/debian:bookworm-slim}
"$ENGINE" build --file "$ROOT_DIR/Containerfile" --tag "$TAG" \
  --build-arg "BAP_VERSION=$VERSION" \
  --build-arg "GO_BUILD_IMAGE=$GO_BUILD_IMAGE" \
  --build-arg "RUNTIME_IMAGE=$RUNTIME_IMAGE" "$ROOT_DIR"
echo "Built $TAG"
