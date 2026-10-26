#!/usr/bin/env bash
#
# CI only: move a GitHub-hosted ubuntu-24.04 runner to the Docker Engine
# the range needs, then assert setup.sh's version floor. The runner image
# ships Microsoft's moby-* packages at Engine 28.0.4 (API 1.48), but the
# range pins firewall NIC names with Compose interface_name, which needs
# Engine 28.1+ (API 1.49). Docker's own apt repository carries the pinned
# Engine; 29.8.0 matches the backend's docker:29.8.0-cli.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ENGINE_PACKAGE_VERSION="5:29.8.0-1~ubuntu.24.04~noble"
CONTAINERD_PACKAGE_VERSION="2.4.2-3~ubuntu.24.04~noble"
BUILDX_PACKAGE_VERSION="0.38.1-1~ubuntu.24.04~noble"
COMPOSE_PACKAGE_VERSION="5.6.1-1~ubuntu.24.04~noble"

# Docker's packages conflict with the moby-* ones; remove whatever is installed.
# dpkg-query exits 1 when no package matches the pattern.
moby=$({ dpkg-query -W -f='${binary:Package} ${db:Status-Status}\n' 'moby-*' 2>/dev/null || true; } \
    | awk '$2 == "installed" { print $1 }')
if [ -n "$moby" ]; then
    # shellcheck disable=SC2086 # one package name per word
    sudo DEBIAN_FRONTEND=noninteractive apt-get remove -y $moby
fi

sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL --retry 3 --max-time 60 https://download.docker.com/linux/ubuntu/gpg \
    -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $codename stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null

sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
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
