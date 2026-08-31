#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENGINE=${1:-}
if [ -z "$ENGINE" ]; then
  if command -v podman >/dev/null 2>&1; then ENGINE=podman
  elif command -v docker >/dev/null 2>&1; then ENGINE=docker
  else echo "Podman or Docker is required." >&2; exit 1
  fi
fi
case "$ENGINE" in podman|docker) ;; *) echo "Usage: $0 [podman|docker]" >&2; exit 2 ;; esac

RUNTIME_DIR="$ROOT_DIR/.bap/runtime/$ENGINE"
mkdir -p "$RUNTIME_DIR"
if ! "$ENGINE" image inspect bap-service:local >/dev/null 2>&1; then
  "$ENGINE" build --file "$ROOT_DIR/Containerfile" --tag bap-service:local "$ROOT_DIR"
fi
"$ENGINE" run --rm --volume "$RUNTIME_DIR:/var/lib/bap:Z" bap-service:local initialize-certificates
for NAME in dev-ca.pem tls-cert.pem tls-key.pem bundle-public.pem bundle-private.pem; do
  if [ ! -s "$RUNTIME_DIR/$NAME" ]; then
    echo "Certificate initialization did not create $RUNTIME_DIR/$NAME" >&2
    exit 1
  fi
done
if [ ! -f "$RUNTIME_DIR/edge-api-key.txt" ]; then
  head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > "$RUNTIME_DIR/edge-api-key.txt"
  chmod 600 "$RUNTIME_DIR/edge-api-key.txt"
fi
echo "Certificates initialized under $RUNTIME_DIR"
echo "Policy-bundle verification key: $RUNTIME_DIR/bundle-public.pem"
