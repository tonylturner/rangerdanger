#!/usr/bin/env bash
#
# RangerDanger lab installer — macOS / Linux
#
# Usage:
#   ./setup.sh                              # pull pre-built images from GHCR (default)
#   ./setup.sh --version v0.1.0             # pin to a specific release
#   ./setup.sh --from-tarballs <PATH>       # offline: docker load from images-<arch>.tar
#   ./setup.sh --help
#
# Runs from the repo root. Validates prerequisites, brings the lab up
# via docker-compose.release.yml, and prints next steps.

set -euo pipefail

# ─── color helpers ──────────────────────────────────────────────────
if [ -t 1 ]; then
    RED=$'\e[31m'; GREEN=$'\e[32m'; YELLOW=$'\e[33m'; BOLD=$'\e[1m'; RESET=$'\e[0m'
else
    RED=""; GREEN=""; YELLOW=""; BOLD=""; RESET=""
fi
say()    { printf "%s[+]%s %s\n" "$GREEN" "$RESET" "$*"; }
warn()   { printf "%s[!]%s %s\n" "$YELLOW" "$RESET" "$*" >&2; }
die()    { printf "%s[✗]%s %s\n" "$RED" "$RESET" "$*" >&2; exit 1; }
banner() { printf "\n%s%s%s\n%s\n\n" "$BOLD" "$1" "$RESET" "$(printf '%.0s─' $(seq 1 ${#1}))"; }

# ─── arg parsing ────────────────────────────────────────────────────
VERSION="${VERSION:-latest}"
TARBALL_DIR=""
SHOW_HELP=0

while [ $# -gt 0 ]; do
    case "$1" in
        --version)         VERSION="$2"; shift 2 ;;
        --version=*)       VERSION="${1#*=}"; shift ;;
        --from-tarballs)   TARBALL_DIR="$2"; shift 2 ;;
        --from-tarballs=*) TARBALL_DIR="${1#*=}"; shift ;;
        -h|--help)         SHOW_HELP=1; shift ;;
        *) die "Unknown argument: $1 (use --help)" ;;
    esac
done

if [ "$SHOW_HELP" -eq 1 ]; then
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"

# ─── pre-flight checks ──────────────────────────────────────────────
banner "Pre-flight checks"

[ -f "$COMPOSE_FILE" ] || die "$COMPOSE_FILE not found — run from repo root or release tarball."

# Docker engine reachable
if ! docker info >/dev/null 2>&1; then
    die "Docker is not running or not installed. Start Docker Desktop / dockerd, then re-run."
fi
say "Docker reachable"

# Compose v2
if ! docker compose version >/dev/null 2>&1; then
    die "Docker Compose v2 not found (this script requires the 'docker compose' subcommand, not 'docker-compose')."
fi
say "Compose v2 present ($(docker compose version --short 2>/dev/null || echo 'unknown'))"

# Architecture
ARCH_RAW=$(uname -m)
case "$ARCH_RAW" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64)  ARCH=amd64 ;;
    *)             die "Unsupported architecture: $ARCH_RAW (need arm64 or amd64)" ;;
esac
say "Architecture: linux/$ARCH"

# Free disk — recommend 30 GB
DISK_AVAIL_GB=$(df -g "$ROOT_DIR" 2>/dev/null | awk 'NR==2 {print $4}' | head -1)
if [ -z "$DISK_AVAIL_GB" ]; then
    DISK_AVAIL_GB=$(df -BG "$ROOT_DIR" 2>/dev/null | awk 'NR==2 {gsub("G",""); print $4}' | head -1)
fi
if [ -n "$DISK_AVAIL_GB" ]; then
    if [ "$DISK_AVAIL_GB" -lt 30 ]; then
        warn "Only ${DISK_AVAIL_GB} GB free on this volume — recommend ≥ 30 GB. Build/pull may fail mid-flight."
    else
        say "Free disk: ${DISK_AVAIL_GB} GB"
    fi
fi

# Docker memory — best-effort, only available on Docker Desktop
if docker info --format '{{.MemTotal}}' >/dev/null 2>&1; then
    MEM_BYTES=$(docker info --format '{{.MemTotal}}' 2>/dev/null || echo 0)
    MEM_GB=$(( MEM_BYTES / 1073741824 ))
    if [ "$MEM_GB" -gt 0 ]; then
        if [ "$MEM_GB" -lt 7 ]; then
            warn "Docker is configured with ${MEM_GB} GB RAM — recommend ≥ 8 GB. Bump in Docker Desktop → Settings → Resources."
        else
            say "Docker memory: ${MEM_GB} GB"
        fi
    fi
fi

# Required host ports
PORTS_REQUIRED="8088 9080 9443 2222"
PORTS_BUSY=""
for port in $PORTS_REQUIRED; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
        PORTS_BUSY="$PORTS_BUSY $port"
    fi
done
if [ -n "$PORTS_BUSY" ]; then
    die "Required loopback ports already in use:$PORTS_BUSY. Stop whatever is bound to them, then re-run."
fi
say "Loopback ports 8088, 9080, 9443, 2222 are free"

# ─── image acquisition ──────────────────────────────────────────────
if [ -n "$TARBALL_DIR" ]; then
    banner "Loading images from tarballs"
    [ -d "$TARBALL_DIR" ] || die "Tarball directory not found: $TARBALL_DIR"
    TARBALL="$TARBALL_DIR/images-$ARCH.tar"
    [ -f "$TARBALL" ] || die "Expected $TARBALL (this host is $ARCH). Have you staged the right architecture?"
    SIZE_HUMAN=$(ls -lh "$TARBALL" | awk '{print $5}')
    say "Loading $TARBALL ($SIZE_HUMAN) — this can take a few minutes"
    docker load -i "$TARBALL"
    say "Images loaded"
else
    banner "Pulling images from GHCR"
    say "Version: $VERSION"
    say "(this can take a while on first run; subsequent pulls are layer-cached)"
    VERSION="$VERSION" docker compose -f "$COMPOSE_FILE" pull
fi

# ─── start the stack ────────────────────────────────────────────────
banner "Starting RangerDanger"
VERSION="$VERSION" docker compose -f "$COMPOSE_FILE" up -d
say "Containers started"

# ─── health smoke check ─────────────────────────────────────────────
banner "Health check"
say "Waiting for the backend to come up (up to 60 s)..."
HEALTH_URL="http://localhost:8088/api/health"
ATTEMPTS=30
for i in $(seq 1 $ATTEMPTS); do
    if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
        say "Backend reports healthy at $HEALTH_URL"
        break
    fi
    sleep 2
    [ $i -eq $ATTEMPTS ] && warn "Backend didn't report healthy in 60 s. Check 'docker compose -f docker-compose.release.yml logs backend'."
done

# ─── done ────────────────────────────────────────────────────────────
banner "RangerDanger is up"
cat <<EOF
  Web UI:        http://localhost:8088
  Exercises:     http://localhost:8088/exercises
  containd UI:   http://localhost:9080
  containd SSH:  ssh -p 2222 containd@localhost   (password: containd)

To stop:
  docker compose -f docker-compose.release.yml down

To check status:
  docker compose -f docker-compose.release.yml ps

To view logs:
  docker compose -f docker-compose.release.yml logs -f <service>

For the lab security model and how to expose this to other machines
on purpose, see SECURITY.md.
EOF
