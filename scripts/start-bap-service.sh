#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENGINE=${1:-}
if [ -z "$ENGINE" ]; then
  if command -v podman >/dev/null 2>&1; then ENGINE=podman; else ENGINE=docker; fi
fi
case "$ENGINE" in podman|docker) ;; *) echo "Usage: $0 [podman|docker]" >&2; exit 2 ;; esac

RUNTIME_DIR="$ROOT_DIR/.bap/runtime/$ENGINE"
mkdir -p "$RUNTIME_DIR"
if ! "$ENGINE" image inspect bap-service:local >/dev/null 2>&1; then
  "$ROOT_DIR/scripts/build-bap-service.sh" "$ENGINE"
fi
"$ROOT_DIR/scripts/initialize-certificates.sh" "$ENGINE"
if [ ! -s "$RUNTIME_DIR/edge-api-key.txt" ]; then
  echo "Missing $RUNTIME_DIR/edge-api-key.txt" >&2
  exit 1
fi
API_KEY=$(tr -d '\r\n' < "$RUNTIME_DIR/edge-api-key.txt")
"$ENGINE" rm --force bap-service-local >/dev/null 2>&1 || true
"$ENGINE" run -d --name bap-service-local \
  -p 127.0.0.1:8443:8443 \
  -v "$RUNTIME_DIR:/var/lib/bap:Z" \
  -e BAP_DEVELOPMENT_TLS=true \
  -e "BAP_EDGE_API_KEY=$API_KEY" \
  -e BAP_EDGE_PRINCIPAL=local-developer \
  bap-service:local
echo "BAP Service started at https://127.0.0.1:8443"
