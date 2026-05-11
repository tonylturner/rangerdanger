#!/usr/bin/env bash
#
# RangerDanger lab installer — macOS / Linux
#
# Usage:
#   ./setup.sh                              # pull pre-built images from GHCR (default)
#   ./setup.sh --version v0.1.0             # pin to a specific release
#   ./setup.sh --from-tarballs <PATH>       # offline: docker load from images-<arch>.tar
#   ./setup.sh --check-only                 # run pre-flight only and exit (no install)
#   ./setup.sh --skip-firewall-gate         # bring stack up without the workshop
#                                           # firewall apply/reset gate (developer
#                                           # iteration on a known-broken stack)
#   ./setup.sh --help
#
# Runs from the repo root. Validates prerequisites, brings the lab up
# via docker-compose.release.yml, and prints next steps. With
# --check-only, runs the pre-flight checks and exits — useful for a
# pre-workshop "is my laptop ready?" check.

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
CHECK_ONLY=0
SKIP_FW_GATE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --version)            VERSION="$2"; shift 2 ;;
        --version=*)          VERSION="${1#*=}"; shift ;;
        --from-tarballs)      TARBALL_DIR="$2"; shift 2 ;;
        --from-tarballs=*)    TARBALL_DIR="${1#*=}"; shift ;;
        --check-only)         CHECK_ONLY=1; shift ;;
        --skip-firewall-gate) SKIP_FW_GATE=1; shift ;;
        -h|--help)            SHOW_HELP=1; shift ;;
        *) die "Unknown argument: $1 (use --help)" ;;
    esac
done

if [ "$SHOW_HELP" -eq 1 ]; then
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"
OFFLINE_OVERLAY="$ROOT_DIR/docker-compose.offline.yml"

# Compose flag set used by every `docker compose ...` invocation below.
# Offline (--from-tarballs) installs add the offline overlay so
# `pull_policy: never` overrides the release file's `pull_policy: always`,
# preventing GHCR fetches in network-blocked classroom environments.
COMPOSE_FLAGS=(-f "$COMPOSE_FILE")
if [ -n "$TARBALL_DIR" ]; then
    [ -f "$OFFLINE_OVERLAY" ] || die "$OFFLINE_OVERLAY not found — required for --from-tarballs."
    COMPOSE_FLAGS+=(-f "$OFFLINE_OVERLAY")
fi

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

# Free disk — recommend 30 GB.
#
# df flags differ per platform:
#   GNU (Linux):  df --output=avail -BG <path>  → numeric "30G" rows
#   BSD (mac):    df -g <path>                  → blocks col 4 in GB
# We try GNU first (covers Linux students), fall back to BSD (mac
# students). Strip any trailing 'G' / whitespace so the integer
# comparison below doesn't choke on '30G' or '30.0'. Round
# fractional values down to the int floor.
DISK_AVAIL_GB=""
if df --output=avail -BG "$ROOT_DIR" >/dev/null 2>&1; then
    DISK_AVAIL_GB=$(df --output=avail -BG "$ROOT_DIR" 2>/dev/null \
        | tail -n1 | tr -d 'G ' | awk '{print int($1)}')
fi
if [ -z "$DISK_AVAIL_GB" ] || [ "$DISK_AVAIL_GB" = "0" ]; then
    DISK_AVAIL_GB=$(df -g "$ROOT_DIR" 2>/dev/null \
        | awk 'NR==2 {print int($4)}' | head -1)
fi
if [ -n "$DISK_AVAIL_GB" ] && [ "$DISK_AVAIL_GB" -gt 0 ] 2>/dev/null; then
    if [ "$DISK_AVAIL_GB" -lt 30 ]; then
        warn "Only ${DISK_AVAIL_GB} GB free on this volume — recommend ≥ 30 GB. Build/pull may fail mid-flight."
    else
        say "Free disk: ${DISK_AVAIL_GB} GB"
    fi
else
    warn "Could not determine free disk space — recommend ≥ 30 GB; verify manually with 'df -h .'."
fi

# Docker memory.
#
# Docker Desktop reports a VM-allocated memory limit via
# `docker info -f '{{.MemTotal}}'`. Linux native Docker Engine
# returns 0 because there is no VM — the daemon uses host RAM
# directly, so the "configured" memory is whatever the host has.
# Read /proc/meminfo as the fallback so Linux-native students still
# get a warning if the host itself is too small.
MEM_GB=0
if docker info --format '{{.MemTotal}}' >/dev/null 2>&1; then
    MEM_BYTES=$(docker info --format '{{.MemTotal}}' 2>/dev/null || echo 0)
    MEM_GB=$(( MEM_BYTES / 1073741824 ))
fi
MEM_SOURCE="Docker"
if [ "$MEM_GB" -le 0 ] && [ -r /proc/meminfo ]; then
    # MemTotal is reported in kB; convert to GB (rounded down).
    MEM_KB=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
    if [ -n "$MEM_KB" ] && [ "$MEM_KB" -gt 0 ]; then
        MEM_GB=$(( MEM_KB / 1024 / 1024 ))
        MEM_SOURCE="host (Linux native Docker)"
    fi
fi
if [ "$MEM_GB" -gt 0 ]; then
    if [ "$MEM_GB" -lt 7 ]; then
        warn "${MEM_SOURCE} memory is ${MEM_GB} GB — recommend ≥ 8 GB. On Docker Desktop, raise Settings → Resources. On Linux native, this is host RAM and the lab will OOM-kill containers."
    else
        say "Memory: ${MEM_GB} GB (${MEM_SOURCE})"
    fi
else
    warn "Could not determine memory — verify manually with 'free -g' or Docker Desktop → Resources."
fi

# Required host ports — show what's holding any conflict so the user
# doesn't have to dig with lsof themselves.
PORTS_REQUIRED="8088 9080 9443 2222"
PORTS_BUSY=""
PORT_DETAILS=""
for port in $PORTS_REQUIRED; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
        PORTS_BUSY="$PORTS_BUSY $port"
        # Pull the first matching process line for the diagnostic message.
        proc=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR==2 {print $1, "pid="$2}')
        PORT_DETAILS="$PORT_DETAILS\n    $port: $proc"
    fi
done
if [ -n "$PORTS_BUSY" ]; then
    die "Required loopback ports already in use:$PORTS_BUSY$(printf "$PORT_DETAILS")
  Stop whatever is bound to them, then re-run. (kill the PID above, or
  bring down a competing dev stack.)"
fi
say "Loopback ports 8088, 9080, 9443, 2222 are free"

# ─── check-only short-circuit ───────────────────────────────────────
if [ "$CHECK_ONLY" -eq 1 ]; then
    banner "Pre-flight passed — laptop is ready"
    cat <<EOF
  All checks above passed. To install:

    ./setup.sh                       # latest
    ./setup.sh --version v0.1.0      # pinned release

  For offline / SSD install, use --from-tarballs <PATH>.
EOF
    exit 0
fi

# ─── image acquisition ──────────────────────────────────────────────
if [ -n "$TARBALL_DIR" ]; then
    banner "Loading images from tarballs"
    [ -d "$TARBALL_DIR" ] || die "Tarball directory not found: $TARBALL_DIR"

    # Auto-detect the staged version from .version file written by
    # stage-ssd.sh. Without this, VERSION defaults to "latest" and
    # compose looks for `:latest` tags that don't exist on the SSD's
    # `:vX.Y.Z`-tagged images, failing with "No such image: ...:latest".
    # The student passing --version is fragile — the SSD should be
    # self-describing.
    if [ -f "$TARBALL_DIR/.version" ] && [ "$VERSION" = "latest" ]; then
        VERSION=$(tr -d '\n[:space:]' < "$TARBALL_DIR/.version")
        say "Auto-detected version from SSD: $VERSION"
    fi

    TARBALL="$TARBALL_DIR/images-$ARCH.tar"
    [ -f "$TARBALL" ] || die "Expected $TARBALL (this host is $ARCH). Have you staged the right architecture?"
    SIZE_HUMAN=$(ls -lh "$TARBALL" | awk '{print $5}')
    say "Loading $TARBALL ($SIZE_HUMAN) - decompressing each image, ~5-15 min on a fast SSD."
    say "Watch the 'Loaded image:' lines below - one per image, 14-19 total."
    docker load -i "$TARBALL"
    say "Images loaded"
else
    banner "Pulling images from GHCR"
    say "Version: $VERSION"
    say "(this can take a while on first run; subsequent pulls are layer-cached)"
    # GHCR occasionally returns transient 5xx during layer fetches —
    # retry up to 3 times with exponential backoff before giving up.
    PULL_OK=0
    for attempt in 1 2 3; do
        if VERSION="$VERSION" docker compose "${COMPOSE_FLAGS[@]}" pull; then
            PULL_OK=1
            break
        fi
        if [ "$attempt" -lt 3 ]; then
            wait_s=$(( attempt * 15 ))
            warn "Pull attempt $attempt failed (likely a transient GHCR / network blip). Retrying in ${wait_s}s..."
            sleep "$wait_s"
        fi
    done
    if [ "$PULL_OK" -ne 1 ]; then
        die "Pulling images failed after 3 attempts. Common causes:
    - Network blocks ghcr.io (try the offline path: --from-tarballs <PATH>)
    - GHCR is genuinely down (rare; check https://www.githubstatus.com/)
    - Disk filled mid-pull (df -h to verify)"
    fi
fi

# ─── pin VERSION in .env so bare `docker compose` works after install ──
# Without this, a student who runs `./setup.sh --from-tarballs <SSD>` and
# later wants to run `docker compose -f docker-compose.release.yml
# -f docker-compose.offline.yml up -d` directly will hit
# `No such image: ...:latest` because compose interpolates
# `${VERSION:-latest}` and the SSD tarball is tagged :vX.Y.Z.
# Writing the resolved VERSION to .env (compose auto-loads .env from
# cwd) makes the bare compose invocation work the same as it did via
# setup.sh. .env is gitignored, so this is safe to write into the repo.
# Idempotent: replaces an existing VERSION= line, appends if absent.
ENV_FILE="$ROOT_DIR/.env"
if [ -f "$ENV_FILE" ] && grep -q "^VERSION=" "$ENV_FILE"; then
    # BSD sed (macOS) needs the -i.bak / rm dance; GNU sed accepts -i ''
    # but the -i.bak form is portable across both.
    sed -i.bak -e "s|^VERSION=.*|VERSION=$VERSION|" "$ENV_FILE" \
        && rm -f "${ENV_FILE}.bak"
else
    echo "VERSION=$VERSION" >> "$ENV_FILE"
fi
say "Pinned VERSION=$VERSION in $ENV_FILE"

# ─── start the stack ────────────────────────────────────────────────
banner "Starting RangerDanger"
VERSION="$VERSION" docker compose "${COMPOSE_FLAGS[@]}" up -d
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

# Workshop-critical surfaces. /api/health alone returned green in past
# audits while firewall apply/reset were broken — students hit the
# regression at lab-time, not at setup. Gate explicitly here so a
# half-working stack never makes it past the installer banner.
# Pass --skip-firewall-gate for developer iteration on a known-
# broken stack.
if [ "$SKIP_FW_GATE" -eq 1 ]; then
    say "Skipping firewall workshop-readiness gate (--skip-firewall-gate)"
else
    say "Workshop-readiness gate: firewall health + apply + reset..."
    fw_fail=0
    if ! curl -fsS http://localhost:8088/api/firewall/health >/dev/null 2>&1; then
        warn "  /api/firewall/health failed — containd management interface is down"
        fw_fail=1
    fi
    for cfg in weak improved; do
        if ! curl -fsS -X POST -H 'Content-Type: application/json' \
                -d "{\"config\":\"$cfg\"}" \
                http://localhost:8088/api/firewall/apply >/dev/null 2>&1; then
            warn "  /api/firewall/apply ($cfg) failed — Lab 2.2/2.3/2.3-bonus/2.4 will not work"
            fw_fail=1
        fi
    done
    reset_resp=$(curl -fsS -X POST http://localhost:8088/api/workshop/reset 2>/dev/null) || reset_resp=""
    if ! echo "$reset_resp" | grep -q '"success":true'; then
        warn "  /api/workshop/reset reported non-success — students hitting Reset Lab will see partial state"
        fw_fail=1
    fi
    if [ "$fw_fail" -eq 1 ]; then
        die "Workshop-readiness gate failed. Common causes:
    - containd image drift (bump containd or pin a known-good tag)
    - mgmt subnet not in firewall input chain (CONTAIND_AUTO_LAN3_SUBNET)
    - sims still warming up (re-run setup, or wait 30s and re-probe)
  Re-run with --skip-firewall-gate to bring the stack up anyway for diagnosis."
    fi
    say "Firewall apply/reset workshop gate passed"
fi

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
