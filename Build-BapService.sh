#!/usr/bin/env sh
set -eu
exec "$(dirname "$0")/scripts/build-bap-service.sh" "$@"
