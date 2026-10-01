#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
ENV_FILE="${ENV_FILE:-$ROOT/.env}"
if [[ -f "$ENV_FILE" ]]; then
  exec docker compose --env-file "$ENV_FILE" -f "$ROOT/compose.yaml" "$@"
fi
exec docker compose -f "$ROOT/compose.yaml" "$@"
