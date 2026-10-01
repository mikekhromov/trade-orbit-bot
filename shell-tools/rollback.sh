#!/usr/bin/env bash
set -Eeuo pipefail

TAG="${1:-}"
[[ "$TAG" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || { echo "Usage: rollback.sh RELEASE_TAG" >&2; exit 2; }
DEPLOY_TARGET="${DEPLOY_TARGET:-}"
DEPLOY_PATH="${DEPLOY_PATH:-/opt/trade-orbit-bot}"
[[ -n "$DEPLOY_TARGET" ]] || { echo "Set DEPLOY_TARGET, for example root@203.0.113.10." >&2; exit 2; }
ssh "$DEPLOY_TARGET" "cd '$DEPLOY_PATH' && docker image inspect 'trade-orbit/telegram-bot:$TAG' >/dev/null && IMAGE_TAG='$TAG' docker compose --env-file .env -f compose.yaml up -d --no-build --wait --wait-timeout 120 telegram-bot"
