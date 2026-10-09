#!/usr/bin/env bash
#
# RangerDanger lab installer — macOS / Linux
#
# Usage:
#   ./setup.sh                              # pull pre-built images from GHCR (default)
#   ./setup.sh --version v0.1.0             # pin to a specific release
#   ./setup.sh --from-tarballs <PATH>       # offline: docker load from images-<arch>.tar
#   ./setup.sh --from-source                # build the images on this host
#   ./setup.sh --package <id>               # first-start range package (default:
#                                           # us-dnp3-substation)
#   ./setup.sh --check-only                 # run pre-flight only and exit (no install)
#   ./setup.sh --skip-firewall-gate         # bring the lab up without the workshop
#                                           # firewall apply/reset gate (developer
#                                           # iteration on a known-broken stack)
#   ./setup.sh --help
#
# Runs from the repo root. Validates prerequisites, writes .env, gets the
# images, starts the platform (backend, frontend, proxy; Compose project
# rangerdanger-platform), then asks the backend to start the range
# (project rangerdanger) through POST /api/range, the same path the UI
# toggle uses. Re-running it is safe: ports held by the running install
# do not fail the pre-flight, and the range is restarted on the current
# package. With --check-only, runs the pre-flight checks and exits —
# useful for a pre-workshop "is my laptop ready?" check.

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

# True (0) if this host can execute linux/amd64 binaries: native on
# x86_64; via Docker Desktop/Rosetta on non-Linux arm64 (macOS); or via a
# registered qemu-x86_64 binfmt_misc handler on arm64 Linux. Used to
# decide whether OpenPLC (amd64-only upstream) needs an emulation shim.
amd64_emulation_present() {
    [ "$ARCH" = "amd64" ] && return 0
    [ "$OS" != "Linux" ] && return 0
    # qemu-x86_64 is the handler name registered by both tonistiigi/binfmt
    # and Debian's qemu-user-static; -static is a variant some setups use.
    # Explicit paths (no glob) so this is safe under any shell options.
    local d=/proc/sys/fs/binfmt_misc f
    [ -d "$d" ] || return 1
    for f in "$d"/qemu-x86_64 "$d"/qemu-x86_64-static; do
        [ -r "$f" ] && grep -q '^enabled' "$f" 2>/dev/null && return 0
    done
    return 1
}

# ─── arg parsing ────────────────────────────────────────────────────
VERSION="${VERSION:-latest}"
TARBALL_DIR=""
FROM_SOURCE=0
PACKAGE=""
SHOW_HELP=0
CHECK_ONLY=0
SKIP_FW_GATE=0

while [ $# -gt 0 ]; do
    case "$1" in
        --version)            VERSION="$2"; shift 2 ;;
        --version=*)          VERSION="${1#*=}"; shift ;;
        --from-tarballs)      TARBALL_DIR="$2"; shift 2 ;;
        --from-tarballs=*)    TARBALL_DIR="${1#*=}"; shift ;;
        --from-source)        FROM_SOURCE=1; shift ;;
        --package)            PACKAGE="$2"; shift 2 ;;
        --package=*)          PACKAGE="${1#*=}"; shift ;;
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

# Installation root H: Compose's project directory for the platform and
# every range, and the path the backend sees H at (it mounts H at H, so
# the daemon resolves the range's relative binds to real host paths).
# Physical path, so a symlinked checkout still names the directory the
# Docker daemon shares.
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PACKAGES_DIR="$ROOT_DIR/lab-definitions/packages"
ENV_FILE="$ROOT_DIR/.env"
API="http://localhost:8088"
PLATFORM_PROJECT="rangerdanger-platform"
RANGE_PROJECT="rangerdanger"
MGMT_NET="rangerdanger_mgmt_net"

# Mode: source builds on this host, release pulls from GHCR, offline loads
# the SSD tarball. Offline is release plus `--pull never`.
if [ "$FROM_SOURCE" -eq 1 ] && [ -n "$TARBALL_DIR" ]; then
    die "--from-source and --from-tarballs are exclusive."
fi
if [ "$FROM_SOURCE" -eq 1 ]; then
    MODE=source
    PLATFORM_FILE="$ROOT_DIR/docker-compose.yml"
    RANGE_FILE_NAME="compose.source.yml"
elif [ -n "$TARBALL_DIR" ]; then
    MODE=offline
    PLATFORM_FILE="$ROOT_DIR/docker-compose.release.yml"
    RANGE_FILE_NAME="compose.release.yml"
else
    MODE=release
    PLATFORM_FILE="$ROOT_DIR/docker-compose.release.yml"
    RANGE_FILE_NAME="compose.release.yml"
fi

# Every package directory: the range images of all of them are fetched,
# so a later switch in the UI finds its images on this host.
package_ids() {
    local d
    for d in "$PACKAGES_DIR"/*/; do
        if [ -f "$d/package.yml" ]; then basename "$d"; fi
    done
}

# Platform and range Compose calls. Both always run with H as the project
# directory and their explicit file, so relative paths resolve against H
# and Compose reads H/.env.
platform_compose() {
    docker compose -p "$PLATFORM_PROJECT" --project-directory "$ROOT_DIR" -f "$PLATFORM_FILE" "$@"
}
range_compose() { # <package id> <compose args...>
    local id="$1"; shift
    docker compose -p "$RANGE_PROJECT" --project-directory "$ROOT_DIR" \
        -f "$PACKAGES_DIR/$id/$RANGE_FILE_NAME" "$@"
}

# GHCR occasionally returns transient 5xx during layer fetches — retry
# each pull up to 3 times with backoff before giving up.
with_pull_retry() {
    local attempt wait_s
    for attempt in 1 2 3; do
        if "$@"; then return 0; fi
        if [ "$attempt" -lt 3 ]; then
            wait_s=$(( attempt * 15 ))
            warn "Pull attempt $attempt failed (likely a transient GHCR / network blip). Retrying in ${wait_s}s..."
            sleep "$wait_s"
        fi
    done
    die "Pulling images failed after 3 attempts. Common causes:
    - Network blocks ghcr.io (try the offline path: --from-tarballs <PATH>)
    - GHCR is genuinely down (rare; check https://www.githubstatus.com/)
    - Disk filled mid-pull (df -h to verify)"
}

# range_field and range_status: GET /api/range parsing shared with the
# host scripts.
# shellcheck source=scripts/lib/range.sh
. "$ROOT_DIR/scripts/lib/range.sh"

# ─── pre-flight checks ──────────────────────────────────────────────
banner "Pre-flight checks"

[ -f "$PLATFORM_FILE" ] || die "$PLATFORM_FILE not found — run from repo root or release tarball."
[ -n "$(package_ids)" ] || die "No range packages under $PACKAGES_DIR — run from repo root or release tarball."
for id in $(package_ids); do
    [ -f "$PACKAGES_DIR/$id/$RANGE_FILE_NAME" ] \
        || die "Package $id has no $RANGE_FILE_NAME — the checkout is incomplete."
done
case "$PACKAGE" in
    *[!a-z0-9-]*) die "Package ids are lowercase letters, digits and '-': '$PACKAGE'." ;;
esac
if [ -n "$PACKAGE" ] && [ ! -f "$PACKAGES_DIR/$PACKAGE/package.yml" ]; then
    die "Unknown package '$PACKAGE'. Available: $(package_ids | tr '\n' ' ')"
fi
# H goes into .env unquoted and into the backend's `H:H:ro` bind, so it
# must not hold characters either syntax treats specially.
case "$ROOT_DIR" in
    *[:\$\#\'\"\\]*|*$'\n'*)
        die "The install path '$ROOT_DIR' contains one of : \$ # ' \" \\ — move the checkout to a plain path." ;;
esac
say "Install root: $ROOT_DIR (mode: $MODE)"

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

OS=$(uname -s)

# OpenPLC is amd64-only upstream; on arm64 Linux it needs a qemu-x86_64
# binfmt handler to run (macOS arm64 gets this free via Docker Desktop /
# Rosetta). Surface the state here so --check-only reports it; the
# installer auto-registers it during the install phase below.
if [ "$OS" = "Linux" ] && [ "$ARCH" = "arm64" ]; then
    if amd64_emulation_present; then
        say "amd64 emulation: present (OpenPLC will run on this arm64 host)"
    else
        warn "amd64 emulation: not registered. OpenPLC (amd64-only) needs it on arm64 Linux."
        warn "  The installer auto-registers it below; --check-only does not change host state."
    fi
fi

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
# doesn't have to dig with lsof themselves. A port published by this
# install's own containers (a re-run, or the pre-split stack that the
# migration below replaces) is not a conflict.
port_held_by_rangerdanger() { # <port>
    local project
    for project in "$RANGE_PROJECT" "$PLATFORM_PROJECT"; do
        if [ -n "$(docker ps -q --filter "label=com.docker.compose.project=$project" --filter "publish=$1")" ]; then
            return 0
        fi
    done
    return 1
}
PORTS_REQUIRED="8088 9080 9443 2222"
PORTS_BUSY=""
PORTS_OURS=""
PORT_DETAILS=""
for port in $PORTS_REQUIRED; do
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
        if port_held_by_rangerdanger "$port"; then
            PORTS_OURS="$PORTS_OURS $port"
            continue
        fi
        PORTS_BUSY="$PORTS_BUSY $port"
        # Pull the first matching process line for the diagnostic message.
        proc=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR==2 {print $1, "pid="$2}')
        PORT_DETAILS="$PORT_DETAILS\n    $port: $proc"
    fi
done
if [ -n "$PORTS_BUSY" ]; then
    die "Required loopback ports already in use:$PORTS_BUSY$(printf '%b' "$PORT_DETAILS")
  Stop whatever is bound to them, then re-run. (kill the PID above, or
  bring down a competing dev stack.)"
fi
if [ -n "$PORTS_OURS" ]; then
    say "Loopback ports 8088, 9080, 9443, 2222 are free or held by this RangerDanger install:$PORTS_OURS"
else
    say "Loopback ports 8088, 9080, 9443, 2222 are free"
fi

# ─── check-only short-circuit ───────────────────────────────────────
if [ "$CHECK_ONLY" -eq 1 ]; then
    banner "Pre-flight passed — laptop is ready"
    cat <<EOF
  All checks above passed. To install:

    ./setup.sh                       # latest
    ./setup.sh --version v0.1.0      # pinned release

  For offline / SSD install, use --from-tarballs <PATH>.
  To build the images on this host, use --from-source.
EOF
    exit 0
fi

# ─── resolve the offline version ────────────────────────────────────
if [ "$MODE" = offline ]; then
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
fi

# ─── write .env ─────────────────────────────────────────────────────
# Compose reads H/.env for every platform and range call (H is always the
# project directory), and so does the backend's range Compose. Setup owns
# these keys; any other line in .env is kept. .env is gitignored.
#   VERSION                release image tag
#   RANGERDANGER_ROOT      H; the platform file refuses to start without it
#   RANGERDANGER_PACKAGE   only with --package: the first-start range
#   CONTAIND_JWT_SECRET    only when set in this environment
env_write() { # <key> [value]: drop the key, then append key=value if given
    local tmp="$ENV_FILE.tmp.$$"
    if [ -f "$ENV_FILE" ]; then
        grep -v "^$1=" "$ENV_FILE" > "$tmp" || true
    else
        : > "$tmp"
    fi
    if [ $# -eq 2 ]; then printf '%s=%s\n' "$1" "$2" >> "$tmp"; fi
    mv "$tmp" "$ENV_FILE"
}
env_write VERSION "$VERSION"
env_write RANGERDANGER_ROOT "$ROOT_DIR"
if [ -n "$PACKAGE" ]; then
    env_write RANGERDANGER_PACKAGE "$PACKAGE"
else
    env_write RANGERDANGER_PACKAGE
fi
if [ -n "${CONTAIND_JWT_SECRET:-}" ]; then
    env_write CONTAIND_JWT_SECRET "$CONTAIND_JWT_SECRET"
fi
# The shell environment overrides .env in Compose; make both agree.
export VERSION RANGERDANGER_ROOT="$ROOT_DIR"
if [ -n "$PACKAGE" ]; then export RANGERDANGER_PACKAGE="$PACKAGE"; else unset RANGERDANGER_PACKAGE; fi
say "Wrote VERSION=$VERSION, RANGERDANGER_ROOT=$ROOT_DIR${PACKAGE:+, RANGERDANGER_PACKAGE=$PACKAGE} to $ENV_FILE"

# The proxy includes the active range's routes from here, and the backend
# installs them; both bind it, so it must exist before the platform up.
mkdir -p "$ROOT_DIR/data/proxy-routes"

# ─── migrate the pre-split stack ────────────────────────────────────
# Before the platform/range split, one Compose project `rangerdanger` held
# everything and owned the mgmt network. Compose does not enforce network
# ownership: a platform up beside it would silently reuse that network
# under the old project's labels. Take the old project down first (by
# label; the range project has the same name and nothing else may run
# under it). Any owner other than these two is someone else's network.
MGMT_OWNER=""
if docker network inspect "$MGMT_NET" >/dev/null 2>&1; then
    MGMT_OWNER=$(docker network inspect -f '{{index .Labels "com.docker.compose.project"}}' "$MGMT_NET")
    case "$MGMT_OWNER" in
        "$PLATFORM_PROJECT"|"$RANGE_PROJECT") ;;
        *) die "Network $MGMT_NET exists but belongs to '${MGMT_OWNER:-no Compose project}', not RangerDanger.
  Remove it (docker network rm $MGMT_NET) or stop whatever created it, then re-run." ;;
    esac
fi
# Stopped pre-split containers can outlive their network; the platform's
# fixed container names would collide with them.
OLD_PLATFORM=$(docker ps -aq --filter "label=com.docker.compose.project=$RANGE_PROJECT" \
    --filter "label=com.docker.compose.service=backend")
if [ "$MGMT_OWNER" = "$RANGE_PROJECT" ] || [ -n "$OLD_PLATFORM" ]; then
    banner "Migrating the pre-split stack"
    say "Stopping Compose project $RANGE_PROJECT (the single-project stack) before the platform starts."
    "$ROOT_DIR/scripts/dev-down.sh" --range-only \
        || die "Could not remove the pre-split stack; nothing else was changed. Fix the error above, then re-run."
fi

# ─── image acquisition ──────────────────────────────────────────────
case "$MODE" in
    offline)
        banner "Loading images from tarballs"
        SIZE_HUMAN=$(du -h "$TARBALL" | cut -f1)
        say "Loading $TARBALL ($SIZE_HUMAN) - decompressing each image, ~5-15 min on a fast SSD."
        say "Watch the 'Loaded image:' lines below - one per image, 14-19 total."
        docker load -i "$TARBALL"
        say "Images loaded"
        ;;
    release)
        banner "Pulling images from GHCR"
        say "Version: $VERSION"
        say "(this can take a while on first run; subsequent pulls are layer-cached)"
        with_pull_retry platform_compose pull
        for id in $(package_ids); do
            say "Range package: $id"
            with_pull_retry range_compose "$id" pull
        done
        say "Images pulled"
        ;;
    source)
        banner "Building images from source"
        say "(the first build takes several minutes; later builds reuse the layer cache)"
        platform_compose build
        for id in $(package_ids); do
            say "Range package: $id"
            range_compose "$id" build
        done
        # Image-only services (containd, nginx, the HMI poller, ...).
        with_pull_retry platform_compose pull --ignore-buildable
        for id in $(package_ids); do
            with_pull_retry range_compose "$id" pull --ignore-buildable
        done
        say "Images built"
        ;;
esac

# ─── amd64 emulation for OpenPLC on arm64 Linux ─────────────────────
# OpenPLC is amd64-only (upstream tuttas/openplc_v3). macOS arm64 runs it
# under Rosetta automatically; arm64 Linux with Docker Engine has no
# Rosetta, so register a qemu-x86_64 binfmt_misc handler via
# tonistiigi/binfmt. Best-effort + idempotent: if it can't run (e.g.
# offline with the helper image uncached) we warn and continue — only
# OpenPLC is affected and nothing depends on it. The marker lets
# uninstall revert ONLY a handler we installed (never a pre-existing or
# Docker-Desktop one).
BINFMT_MARKER="$ROOT_DIR/.setup-binfmt-amd64"
# Pinned (not :latest) for reproducible installs. stage-ssd.sh stages this
# exact tag, so the offline path loads it and `docker run` finds it with no
# pull. Bump deliberately to pick up newer qemu. Keep in sync with
# stage-ssd.sh, stage-ssd-delta.sh, and uninstall-rangerdanger.sh.
BINFMT_REF="tonistiigi/binfmt:qemu-v10.2.1"
if [ "$OS" = "Linux" ] && [ "$ARCH" = "arm64" ] && ! amd64_emulation_present; then
    banner "Registering amd64 emulation (OpenPLC)"
    say "OpenPLC is amd64-only; registering a qemu-x86_64 handler via tonistiigi/binfmt..."
    if docker run --privileged --rm "$BINFMT_REF" --install amd64 >/dev/null 2>&1 \
        && amd64_emulation_present; then
        : > "$BINFMT_MARKER"
        say "amd64 emulation registered (./scripts/uninstall-rangerdanger.sh will revert it)"
        say "  (runtime only — for reboot persistence: sudo ./scripts/persist-emulation.sh)"
    else
        warn "Could not auto-register amd64 emulation."
        warn "Everything except the OpenPLC lab still works. To enable OpenPLC, run:"
        warn "    docker run --privileged --rm $BINFMT_REF --install amd64"
        warn "(needs network to pull $BINFMT_REF if not already cached)."
    fi
fi

# ─── start the platform ─────────────────────────────────────────────
banner "Starting the platform"
UP_FLAGS=(up -d --wait)
[ "$MODE" = offline ] && UP_FLAGS+=(--pull never)
platform_compose "${UP_FLAGS[@]}"
say "Platform started (backend, frontend, proxy)"

# ─── health smoke check ─────────────────────────────────────────────
banner "Health check"
say "Waiting for the backend to come up (up to 120 s)..."
HEALTH_URL="$API/api/health"
# ~2 s per iteration; 60 iterations = ~120 s. A cold start after a fresh
# image pull can take 60-90 s. The platform services have no
# healthchecks, so `up --wait` returns before the backend answers; the
# range request below needs it. Mirrors setup.ps1.
ATTEMPTS=60
for i in $(seq 1 $ATTEMPTS); do
    if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
        say "Backend reports healthy at $HEALTH_URL"
        break
    fi
    [ "$i" -eq $ATTEMPTS ] && die "Backend didn't report healthy in 120 s. Check 'docker logs rangerdanger-backend'."
    sleep 2
done

# ─── start the range ────────────────────────────────────────────────
# The backend owns the range lifecycle: the same request the UI toggle
# sends. With no package in the body the backend restarts its recorded
# package, or RANGERDANGER_PACKAGE on first start.
banner "Starting the range"
RANGE_BODY='{}'
[ -n "$PACKAGE" ] && RANGE_BODY="{\"package\":\"$PACKAGE\"}"
range_settled() { # <status json>: no transition in flight
    case "$(range_field "$1" phase)" in
        preflight|stopping|starting|configuring) return 1 ;;
    esac
}
RANGE_RESP=""
for attempt in 1 2 3; do
    POST_OUT=$(curl -sS -X POST -H 'Content-Type: application/json' -d "$RANGE_BODY" \
        -w '\n%{http_code}' "$API/api/range") || die "POST $API/api/range failed."
    POST_CODE=${POST_OUT##*$'\n'}
    RANGE_RESP=${POST_OUT%$'\n'*}
    case "$POST_CODE" in
        202) break ;;
        409)
            # Another transition holds the range (a UI switch). Let it
            # finish, then ask again.
            say "A range transition is already running; waiting for it to finish..."
            for _ in $(seq 1 200); do
                range_settled "$(curl -fsS "$API/api/range" 2>/dev/null)" && break
                sleep 3
            done
            RANGE_RESP=""
            ;;
        *) die "POST $API/api/range returned HTTP $POST_CODE: $RANGE_RESP" ;;
    esac
done
[ -n "$RANGE_RESP" ] || die "POST $API/api/range kept returning 409 (a transition is still running)."
# `target` names the package while the transition is in preflight; from
# stopping on, `package` is the new package.
RANGE_TARGET=$(range_field "$RANGE_RESP" target)
[ -n "$RANGE_TARGET" ] || RANGE_TARGET=$(range_field "$RANGE_RESP" package)
say "Range requested: ${RANGE_TARGET:-<backend default>}"

# up --wait has a 300 s budget in the backend, plus teardown, policy
# import and route install: allow 600 s.
say "Waiting for the range to report ready (up to 600 s)..."
LAST_PHASE=""
RANGE_OK=0
for i in $(seq 1 200); do
    RANGE_STATUS=$(curl -fsS "$API/api/range" 2>/dev/null) || RANGE_STATUS=""
    PHASE=$(range_field "$RANGE_STATUS" phase)
    RANGE_PACKAGE=$(range_field "$RANGE_STATUS" package)
    RANGE_ERROR=$(range_field "$RANGE_STATUS" error)
    if [ -n "$PHASE" ] && [ "$PHASE" != "$LAST_PHASE" ]; then
        say "  range phase: $PHASE"
        LAST_PHASE="$PHASE"
    fi
    # A preflight failure leaves the previous phase (ready, none or
    # failed) with `error` set; a failure after preflight is `failed`.
    if range_settled "$RANGE_STATUS" && [ -n "$RANGE_ERROR" ]; then
        die "The range did not start: $RANGE_ERROR
  Status: $RANGE_STATUS
  Logs:   docker logs rangerdanger-backend"
    fi
    if [ "$PHASE" = ready ] && { [ -z "$RANGE_TARGET" ] || [ "$RANGE_PACKAGE" = "$RANGE_TARGET" ]; }; then
        RANGE_OK=1
        break
    fi
    sleep 3
done
[ "$RANGE_OK" = 1 ] || die "The range did not report ready in 600 s (last phase: ${LAST_PHASE:-unknown}).
  Check: curl -s $API/api/range ; docker logs rangerdanger-backend"
say "Range $RANGE_PACKAGE is ready"

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
    dpi_degraded=0
    for cfg in weak improved; do
        # Capture the body, don't discard it. A 200 with warnings means containd
        # committed the policy but the kernel rejected part of the ruleset; nft
        # is atomic, so the WHOLE hardened ruleset rolls back while apply still
        # returns 200 — so a gate that pipes to /dev/null passes green while
        # segmentation silently does NOT enforce (the DPI "queue num" rules need
        # nfnetlink_queue / CONFIG_NFT_QUEUE in the host kernel). Mirrors setup.ps1.
        apply_resp=$(curl -fsS -X POST -H 'Content-Type: application/json' \
                -d "{\"config\":\"$cfg\"}" \
                http://localhost:8088/api/firewall/apply 2>/dev/null) || apply_resp=""
        if [ -z "$apply_resp" ]; then
            warn "  /api/firewall/apply ($cfg) failed — Lab 2.2/2.3/2.3-bonus/2.4 will not work"
            fw_fail=1
        elif [ "$cfg" = "improved" ] && echo "$apply_resp" | grep -qE 'nft apply failed|queue num|NFT_QUEUE'; then
            dpi_degraded=1
        fi
    done
    reset_resp=$(curl -fsS -X POST http://localhost:8088/api/workshop/reset 2>/dev/null) || reset_resp=""
    # Read the TOP-LEVEL "success", not a substring: the response embeds a
    # per-action array whose entries each carry their own "success":true, so a
    # naive grep false-passes even when the overall reset failed. Parse with
    # python3 (present on the workshop hosts); fall back to the grep otherwise.
    if command -v python3 >/dev/null 2>&1; then
        reset_ok=$(echo "$reset_resp" | python3 -c 'import json,sys
try: sys.stdout.write("1" if json.load(sys.stdin).get("success") is True else "0")
except Exception: sys.stdout.write("0")' 2>/dev/null)
    elif echo "$reset_resp" | grep -q '"success":true'; then reset_ok=1; else reset_ok=0; fi
    if [ "$reset_ok" != "1" ]; then
        warn "  /api/workshop/reset reported non-success — students hitting Reset Lab will see partial state"
        fw_fail=1
    fi
    if [ "$dpi_degraded" = "1" ]; then
        warn "------------------------------------------------------------"
        warn "Hardened policy applied WITH WARNINGS — ICS DPI / segmentation"
        warn "is NOT enforcing. Labs 2.3 / 2.3-bonus and the hardened L4 rules"
        warn "will silently fail to block the attacks they teach."
        warn "Cause: the host kernel rejected the DPI 'queue num' ruleset"
        warn "(nfnetlink_queue / CONFIG_NFT_QUEUE missing). On Linux native:"
        warn "    sudo modprobe nfnetlink_queue   # then re-run ./setup.sh"
        warn "Most distro kernels ship it; Docker Desktop VMs have it built in."
        warn "------------------------------------------------------------"
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

# ─── OpenPLC readiness (protection-logic lab) ───────────────────────
# OpenPLC has no healthcheck and no host port (it's an internal Modbus
# client), and it's the amd64-only image — so on arm64 it only runs once
# amd64 emulation is registered. Verify the container is actually running
# rather than crash-looping with "exec format error" (the silent failure
# mode when emulation is missing). Non-fatal: OpenPLC is isolated (nothing
# depends on it), so warn loudly with the fix instead of blocking install.
say "Workshop-readiness: OpenPLC (protection-logic lab)..."
openplc_ok=0
for i in $(seq 1 8); do
    if [ "$(docker inspect -f '{{.State.Status}}' rangerdanger-openplc 2>/dev/null)" = "running" ]; then
        openplc_ok=1; break
    fi
    sleep 2
done
if [ "$openplc_ok" = "1" ]; then
    say "OpenPLC container is running"
else
    # `|| true`: under `set -euo pipefail`, docker inspect failing on a
    # missing container would otherwise abort setup at this assignment.
    op_state=$(docker inspect -f '{{.State.Status}}' rangerdanger-openplc 2>/dev/null | tr -d '[:space:]' || true)
    [ -n "$op_state" ] || op_state=missing
    op_rc=$(docker inspect -f '{{.RestartCount}}' rangerdanger-openplc 2>/dev/null | tr -d '[:space:]' || true)
    [ -n "$op_rc" ] || op_rc="?"
    warn "OpenPLC is not running (state=$op_state, restarts=$op_rc) — the protection-logic lab won't work."
    if docker logs rangerdanger-openplc 2>&1 | grep -qi 'exec format error'; then
        warn "  Cause: 'exec format error' — amd64 emulation is missing on this arm64 host."
        warn "  Fix:   docker run --privileged --rm $BINFMT_REF --install amd64"
        warn "         then re-run ./setup.sh (it restarts the range)"
    else
        warn "  Check: docker logs rangerdanger-openplc"
    fi
fi

# ─── done ────────────────────────────────────────────────────────────
banner "RangerDanger is up"
cat <<EOF
  Web UI:        http://localhost:8088
  Exercises:     http://localhost:8088/exercises
  containd UI:   http://localhost:9080
  containd SSH:  ssh -p 2222 containd@localhost   (password: containd)
  Range:         $RANGE_PACKAGE (switch packages in the web UI)

To stop (the range, then the platform):
  ./scripts/dev-down.sh

To start again: re-run ./setup.sh with the same options.

To check status:
  curl -s $API/api/range
  docker compose ls

To view logs:
  docker logs -f rangerdanger-<service>     # e.g. rangerdanger-backend, rangerdanger-firewall

When you're done with the workshop (removes the stack and lab state):
  ./scripts/uninstall-rangerdanger.sh

For the lab security model and how to expose this to other machines
on purpose, see SECURITY.md.
EOF
