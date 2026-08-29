#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENGINE=${1:-}
if [ -z "$ENGINE" ]; then
  if command -v podman >/dev/null 2>&1; then ENGINE=podman; else ENGINE=docker; fi
fi
case "$ENGINE" in podman|docker) ;; *) echo "Usage: $0 [podman|docker]" >&2; exit 2 ;; esac

echo "Building Linux BAP Service image with $ENGINE..."
"$ENGINE" build --file "$ROOT_DIR/Containerfile" --tag bap-service:local "$ROOT_DIR"
echo "Built bap-service:local"
