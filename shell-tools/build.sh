#!/usr/bin/env bash
set -Eeuo pipefail

TAG="${1:-current}"
[[ "$TAG" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || { echo "Invalid image tag." >&2; exit 2; }
ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
docker build --file "$ROOT/Dockerfile" --tag "trade-orbit/telegram-bot:$TAG" "$ROOT"
