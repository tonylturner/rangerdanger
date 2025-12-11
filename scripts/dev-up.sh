#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

echo "[+] Building and starting lab trainer stack"
docker compose -f "$COMPOSE_FILE" up --build -d "$@"
