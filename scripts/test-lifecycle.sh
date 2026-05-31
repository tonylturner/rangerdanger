#!/usr/bin/env bash
#
# test-lifecycle.sh — end-to-end setup -> execute -> teardown validator.
#
# Runs the REAL setup.sh and uninstaller against the full stack, then
# asserts each phase independently and prints a single PASS/FAIL. This is
# the heavy, exhaustive lifecycle check — use it on a TEST machine or a
# throwaway VM (e.g. Multipass on Apple Silicon for the arm64 Linux path).
#
# It catches things a bare `./setup.sh` does not: OpenPLC's readiness probe
# is non-fatal, so setup can exit 0 with OpenPLC crash-looping — this
# asserts it explicitly. It also verifies teardown leaves the host clean.
#
# Usage:
#   ./scripts/test-lifecycle.sh                       # online, full lifecycle
#   ./scripts/test-lifecycle.sh --from-tarballs <DIR> # offline / SSD path
#   ./scripts/test-lifecycle.sh --reinstall           # also re-install after teardown
#   ./scripts/test-lifecycle.sh --no-teardown         # leave stack up after asserts
#   ./scripts/test-lifecycle.sh --yes                 # skip the confirmation
#
# Exit: 0 all asserts passed, 1 a failure, 2 wrong environment.

set -uo pipefail

YES=0; REINSTALL=0; NO_TEARDOWN=0; TARBALL_DIR=""
while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes)          YES=1; shift ;;
        --reinstall)       REINSTALL=1; shift ;;
        --no-teardown)     NO_TEARDOWN=1; shift ;;
        --from-tarballs)   TARBALL_DIR="$2"; shift 2 ;;
        --from-tarballs=*) TARBALL_DIR="${1#*=}"; shift ;;
        -h|--help)         sed -n '2,/^set /p' "$0" | sed 's/^# \?//;/^set /d'; exit 0 ;;
        *) echo "unknown arg: $1 (see --help)" >&2; exit 2 ;;
    esac
done

if [ -t 1 ]; then G=$'\e[32m'; R=$'\e[31m'; Y=$'\e[33m'; B=$'\e[1m'; X=$'\e[0m'
else G=""; R=""; Y=""; B=""; X=""; fi
PASS=0; FAIL=0
ok()   { printf "%s  PASS%s %s\n" "$G" "$X" "$*"; PASS=$((PASS+1)); }
no()   { printf "%s  FAIL%s %s\n" "$R" "$X" "$*"; FAIL=$((FAIL+1)); }
info() { printf "%s[*]%s %s\n" "$Y" "$X" "$*"; }
phase(){ printf "\n%s== %s ==%s\n" "$B" "$*" "$X"; }

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPO" || { echo "cannot cd to repo root"; exit 2; }
COMPOSE_FILE="$REPO/docker-compose.release.yml"
MARKER="$REPO/.setup-binfmt-amd64"
CF=(-f "$COMPOSE_FILE")
[ -n "$TARBALL_DIR" ] && CF+=(-f "$REPO/docker-compose.offline.yml")

phase "RangerDanger lifecycle test"
OS=$(uname -s); MACH=$(uname -m)
case "$MACH" in arm64|aarch64) ARCH=arm64 ;; x86_64|amd64) ARCH=amd64 ;; *) ARCH="$MACH" ;; esac
info "Host: $OS / $ARCH   install: $([ -n "$TARBALL_DIR" ] && echo "offline ($TARBALL_DIR)" || echo online)"
[ -f "$COMPOSE_FILE" ] || { no "run from a checkout — $COMPOSE_FILE missing"; exit 2; }
command -v docker >/dev/null 2>&1 || { no "docker not found"; exit 2; }
docker compose version >/dev/null 2>&1 || { no "docker compose v2 not found"; exit 2; }

# This installs the full stack and then removes it. Confirm.
if [ "$YES" = "0" ]; then
    info "This runs ./setup.sh (full stack) then uninstalls it. Intended for a test host/VM."
    printf "Proceed? [y/N] "; read -r a; case "$a" in y|Y|yes|YES) ;; *) info "aborted."; exit 0 ;; esac
fi

# Helper: HTTP 200 check
http_ok() { curl -fsS -o /dev/null --max-time 10 "$1" 2>/dev/null; }

run_setup() {
    local args=()
    [ -n "$TARBALL_DIR" ] && args+=(--from-tarballs "$TARBALL_DIR")
    ./setup.sh "${args[@]}"
}

assert_up() {
    phase "Assert stack is up + healthy"
    if http_ok http://localhost:8088/api/health; then ok "backend /api/health"; else no "backend /api/health"; fi
    if http_ok http://localhost:8088/;            then ok "web UI (8088)";       else no "web UI (8088)"; fi
    if http_ok http://localhost:9080/;            then ok "containd UI (9080)";   else no "containd UI (9080)"; fi
    if http_ok http://localhost:8088/api/firewall/health; then ok "containd firewall health"; else no "containd firewall health"; fi

    # Every compose service should be in state 'running' (not restarting/exited).
    local bad
    bad=$(docker compose "${CF[@]}" ps -a --format '{{.Service}} {{.State}}' 2>/dev/null | awk '$2!="running"{printf "%s(%s) ",$1,$2}')
    if [ -z "$bad" ]; then ok "all compose services running"; else no "services not running: $bad"; fi

    # OpenPLC explicitly — setup's probe is non-fatal, so verify here.
    if [ "$(docker inspect -f '{{.State.Status}}' rangerdanger-openplc 2>/dev/null | tr -d '[:space:]')" = "running" ]; then
        ok "OpenPLC running$([ "$ARCH" = arm64 ] && echo ' (amd64 under emulation)')"
    else
        no "OpenPLC NOT running — protection-logic lab broken (emulation? check 'docker logs rangerdanger-openplc')"
    fi

    # arm64 Linux: emulation must be registered.
    if [ "$OS" = Linux ] && [ "$ARCH" = arm64 ]; then
        if [ -e /proc/sys/fs/binfmt_misc/qemu-x86_64 ]; then ok "qemu-x86_64 emulation registered"; else no "qemu-x86_64 NOT registered"; fi
        if [ -f "$MARKER" ]; then ok "setup wrote the binfmt marker"; else info "no binfmt marker (emulation was pre-existing — fine)"; fi
    fi
}

assert_execute() {
    phase "Assert workshop-critical execution"
    local cfg ok_all=1
    local apply_resp
    for cfg in weak improved; do
        # Capture the body: a 200 with warnings on 'improved' means the kernel
        # rejected the DPI ruleset and the hardened policy silently rolled back.
        apply_resp=$(curl -fsS --max-time 15 -X POST -H 'Content-Type: application/json' \
            -d "{\"config\":\"$cfg\"}" http://localhost:8088/api/firewall/apply 2>/dev/null) || apply_resp=""
        if [ -z "$apply_resp" ]; then
            no "firewall apply ($cfg) — Lab 2.2/2.3/2.4 would not work"; ok_all=0
        elif [ "$cfg" = "improved" ] && echo "$apply_resp" | grep -qE 'nft apply failed|queue num|NFT_QUEUE'; then
            no "firewall apply (improved) returned warnings — hardened policy NOT enforcing (host kernel missing nfnetlink_queue/CONFIG_NFT_QUEUE)"; ok_all=0
        else
            ok "firewall apply ($cfg)"
        fi
    done
    # Top-level success, not a substring (per-action entries each carry their own).
    local resp reset_ok=0
    resp=$(curl -fsS --max-time 15 -X POST http://localhost:8088/api/workshop/reset 2>/dev/null || true)
    if command -v python3 >/dev/null 2>&1; then
        reset_ok=$(echo "$resp" | python3 -c 'import json,sys
try: sys.stdout.write("1" if json.load(sys.stdin).get("success") is True else "0")
except Exception: sys.stdout.write("0")' 2>/dev/null)
    elif echo "$resp" | grep -q '"success":true'; then reset_ok=1; fi
    if [ "$reset_ok" = "1" ]; then ok "workshop reset"; else no "workshop reset top-level success != true (got: ${resp:-<none>})"; fi
}

assert_teardown() {
    phase "Teardown — uninstall + assert clean"
    local had_marker=0; [ -f "$MARKER" ] && had_marker=1
    if ./scripts/uninstall-rangerdanger.sh --yes; then ok "uninstall exited 0"; else no "uninstall exited non-zero"; fi

    local left
    left=$(docker ps -a --format '{{.Names}}' --filter "name=rangerdanger-" 2>/dev/null | grep -c . || true)
    if [ "$left" = "0" ]; then ok "no rangerdanger containers remain"; else no "$left rangerdanger container(s) still present"; fi

    local vols
    vols=$(docker volume ls --format '{{.Name}}' 2>/dev/null | grep -cE '^rangerdanger' || true)
    if [ "$vols" = "0" ]; then ok "no rangerdanger volumes remain"; else no "$vols rangerdanger volume(s) remain"; fi

    if [ ! -f "$REPO/.env" ]; then ok ".env removed"; else no ".env still present"; fi

    if [ "$OS" = Linux ] && [ "$ARCH" = arm64 ] && [ "$had_marker" = "1" ]; then
        if [ ! -e /proc/sys/fs/binfmt_misc/qemu-x86_64 ]; then ok "binfmt emulation reverted"; else no "binfmt handler still registered after uninstall"; fi
        if [ ! -f "$MARKER" ]; then ok "binfmt marker removed"; else no "binfmt marker still present"; fi
    fi
}

# ── run the lifecycle ───────────────────────────────────────────────
phase "Setup (./setup.sh)"
if run_setup; then ok "setup.sh exited 0"; else no "setup.sh exited non-zero"; fi
assert_up
assert_execute

if [ "$NO_TEARDOWN" = "1" ]; then
    info "Leaving stack up (--no-teardown). Tear down later with ./scripts/uninstall-rangerdanger.sh --yes"
else
    assert_teardown
    if [ "$REINSTALL" = "1" ]; then
        phase "Re-install (idempotency)"
        if run_setup; then ok "re-install setup.sh exited 0"; else no "re-install setup.sh exited non-zero"; fi
        assert_up
        [ "$NO_TEARDOWN" = "1" ] || assert_teardown
    fi
fi

phase "Result"
if [ "$FAIL" -eq 0 ]; then printf "%s  ✓ ALL %d CHECKS PASSED%s\n" "$G" "$PASS" "$X"; exit 0
else printf "%s  ✗ %d passed, %d FAILED%s\n" "$R" "$PASS" "$FAIL" "$X"; exit 1; fi
