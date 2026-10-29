#!/usr/bin/env bash
#
# CI only: move a GitHub-hosted ubuntu-26.04 runner to the Docker Engine
# the range needs, then assert setup.sh's version floor. The runner image
# ships Docker CE 29.4.2, not moby; pin Docker's packages explicitly so the
# Engine matches the backend's docker:29.8.0-cli. Package builds use the
# Ubuntu version and codename from /etc/os-release.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
. /etc/os-release
ubuntu_package_suffix="~ubuntu.${VERSION_ID}~${VERSION_CODENAME}"
ENGINE_PACKAGE_VERSION="5:29.8.0-1${ubuntu_package_suffix}"
CONTAINERD_PACKAGE_VERSION="2.4.2-3${ubuntu_package_suffix}"
BUILDX_PACKAGE_VERSION="0.38.1-1${ubuntu_package_suffix}"
COMPOSE_PACKAGE_VERSION="5.6.1-1${ubuntu_package_suffix}"

sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL --retry 3 --max-time 60 https://download.docker.com/linux/ubuntu/gpg \
    -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null

sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --allow-downgrades --no-install-recommends \
    "docker-ce=$ENGINE_PACKAGE_VERSION" "docker-ce-cli=$ENGINE_PACKAGE_VERSION" \
    "containerd.io=$CONTAINERD_PACKAGE_VERSION" \
    "docker-buildx-plugin=$BUILDX_PACKAGE_VERSION" \
    "docker-compose-plugin=$COMPOSE_PACKAGE_VERSION"

# The package starts dockerd; wait for it before reading versions.
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
