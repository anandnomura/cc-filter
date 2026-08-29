#!/usr/bin/env sh
set -eu
exec "$(dirname "$0")/scripts/start-bap-service.sh" "$@"
