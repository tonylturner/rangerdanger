#!/usr/bin/env bash
#
# CI only: move a GitHub-hosted ubuntu-24.04 runner to the Docker Engine
# the range needs, then assert setup.sh's version floor. The runner image
# ships Engine 28.0.4 (API 1.48), but the range pins firewall NIC names
# with Compose interface_name, which needs Engine 28.1+ (API 1.49). The
# image installs Docker from Docker's apt repository, so the pinned
# packages come from there; 29.8.0 matches the backend's docker:29.8.0-cli.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ENGINE_PACKAGE_VERSION="5:29.8.0-1~ubuntu.24.04~noble"

sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    "docker-ce=$ENGINE_PACKAGE_VERSION" "docker-ce-cli=$ENGINE_PACKAGE_VERSION"

# The package restarts dockerd; wait for it before reading versions.
for _ in $(seq 1 30); do
    if docker info >/dev/null 2>&1; then break; fi
    sleep 1
done

# shellcheck source=scripts/lib/docker-floor.sh
. "$ROOT_DIR/scripts/lib/docker-floor.sh"
compose=$(docker compose version --short)
api=$(docker version --format '{{.Server.APIVersion}}')
docker_floor_check "$compose" "$api"
echo "Compose $compose on Engine API $api"
