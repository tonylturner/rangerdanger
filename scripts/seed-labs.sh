#!/usr/bin/env bash
set -euo pipefail

API_URL=${API_URL:-"http://localhost:8080/api/admin/seed"}

echo "[+] Seeding lab definitions via $API_URL"
if command -v jq >/dev/null 2>&1; then
  curl -sf -X POST "$API_URL" | jq .
else
  curl -sf -X POST "$API_URL"
fi
