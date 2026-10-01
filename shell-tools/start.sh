#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
ENV_FILE="${ENV_FILE:-$ROOT/.env}"
[[ -f "$ENV_FILE" ]] || { echo "Missing $ENV_FILE; copy .env.example and set the required values." >&2; exit 1; }
for key in TOKEN_TG_BOT CORE_API_URL INTERNAL_SERVICE_TOKEN; do
  grep -Eq "^[[:space:]]*$key[[:space:]]*=[[:space:]]*[^[:space:]#]+" "$ENV_FILE" || {
    echo "$key must be set to a non-empty value in $ENV_FILE." >&2
    exit 1
  }
done
IMAGE_TAG="${IMAGE_TAG:-current}"
IMAGE_TAG="$IMAGE_TAG" ENV_FILE="$ENV_FILE" bash "$SCRIPT_DIR/compose.sh" config --quiet
bash "$SCRIPT_DIR/build.sh" "$IMAGE_TAG"
IMAGE_TAG="$IMAGE_TAG" ENV_FILE="$ENV_FILE" bash "$SCRIPT_DIR/compose.sh" up -d --wait --wait-timeout 120 telegram-bot
IMAGE_TAG="$IMAGE_TAG" ENV_FILE="$ENV_FILE" bash "$SCRIPT_DIR/compose.sh" ps telegram-bot
