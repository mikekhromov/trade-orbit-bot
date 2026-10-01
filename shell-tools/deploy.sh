#!/usr/bin/env bash
set -Eeuo pipefail

DEPLOY_TARGET="${DEPLOY_TARGET:-}"
DEPLOY_PATH="${DEPLOY_PATH:-/opt/trade-orbit}"
DEPLOY_PLATFORM="${DEPLOY_PLATFORM:-linux/amd64}"
RELEASE_TAG="${RELEASE_TAG:-$(date -u +%Y%m%d%H%M%S)}"
[[ -n "$DEPLOY_TARGET" ]] || { echo "Set DEPLOY_TARGET, for example root@203.0.113.10." >&2; exit 2; }
[[ "$RELEASE_TAG" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || { echo "Invalid RELEASE_TAG." >&2; exit 2; }
ROOT=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
IMAGE="trade-orbit/telegram-bot"

ssh "$DEPLOY_TARGET" "cd '$DEPLOY_PATH' && test -f .env && docker compose --env-file .env -f compose.yaml config --services | grep -qx telegram-bot"
docker buildx build --platform "$DEPLOY_PLATFORM" --load -f "$ROOT/Dockerfile" \
  -t "$IMAGE:$RELEASE_TAG" -t "$IMAGE:current" "$ROOT"
docker save "$IMAGE:$RELEASE_TAG" "$IMAGE:current" | gzip | ssh "$DEPLOY_TARGET" 'gunzip | docker load'
ssh "$DEPLOY_TARGET" "cd '$DEPLOY_PATH' && docker compose --env-file .env -f compose.yaml up -d --no-deps --wait --wait-timeout 120 telegram-bot && docker compose --env-file .env -f compose.yaml ps telegram-bot"

echo "Deployed bot image $IMAGE:$RELEASE_TAG"
