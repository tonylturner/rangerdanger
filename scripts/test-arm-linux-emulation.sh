#!/usr/bin/env bash
#
# Test the arm64-Linux amd64-emulation path that setup.sh auto-installs
# for OpenPLC (and that uninstall reverts). OpenPLC is amd64-only
# upstream; on arm64 Linux it needs a qemu-x86_64 binfmt handler, which
# setup.sh registers via tonistiigi/binfmt. This validates that
# mechanism end to end and prints a single PASS/FAIL.
#
# RUN THIS IN A THROWAWAY arm64 LINUX VM (e.g. Multipass on Apple
# Silicon) -- it changes the host's binfmt_misc registration.
#
# Usage:
#   scripts/test-arm-linux-emulation.sh            # fast: mechanism + detector
#   scripts/test-arm-linux-emulation.sh --yes      # skip the confirmation
#   scripts/test-arm-linux-emulation.sh --full     # also run setup.sh + uninstall
#
# Exit codes: 0 = all asserts passed, 1 = a failure, 2 = wrong environment.

set -uo pipefail

YES=0
FULL=0
for a in "$@"; do
    case "$a" in
        -y|--yes)  YES=1 ;;
        --full)    FULL=1 ;;
        -h|--help) sed -n '2,/^set /p' "$0" | sed 's/^# \?//;/^set /d'; exit 0 ;;
        *) echo "unknown arg: $a (see --help)" >&2; exit 2 ;;
    esac
done

if [ -t 1 ]; then G=$'\e[32m'; R=$'\e[31m'; Y=$'\e[33m'; B=$'\e[1m'; X=$'\e[0m'
else G=""; R=""; Y=""; B=""; X=""; fi

PASS=0; FAIL=0
ok()   { printf "%s  PASS%s %s\n" "$G" "$X" "$*"; PASS=$((PASS+1)); }
no()   { printf "%s  FAIL%s %s\n" "$R" "$X" "$*"; FAIL=$((FAIL+1)); }
info() { printf "%s[*]%s %s\n" "$Y" "$X" "$*"; }
sec()  { printf "\n%s%s%s\n" "$B" "$*" "$X"; }

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SETUP="$REPO/setup.sh"
UNINSTALL="$REPO/scripts/uninstall-rangerdanger.sh"
MARKER="$REPO/.setup-binfmt-amd64"
HANDLER=/proc/sys/fs/binfmt_misc/qemu-x86_64
BINFMT_REF="tonistiigi/binfmt:qemu-v10.2.1"  # pinned; matches setup.sh

sec "RangerDanger -- arm64 Linux amd64-emulation test"

# ── environment gate ────────────────────────────────────────────────
OS=$(uname -s); MACH=$(uname -m)
info "Host: $OS / $MACH"
if [ "$OS" != "Linux" ]; then
    no "must run on Linux (this is $OS). Use a throwaway arm64 Linux VM."
    printf "\n%s  ✗ wrong environment%s\n" "$R" "$X"; exit 2
fi
case "$MACH" in
    arm64|aarch64) ARCH=arm64 ;;
    *) no "must run on arm64/aarch64 (this is $MACH). The emulation path is arm64-only."
       printf "\n%s  ✗ wrong environment%s\n" "$R" "$X"; exit 2 ;;
esac
command -v docker >/dev/null 2>&1   || { no "docker not found in the VM"; exit 2; }
docker info >/dev/null 2>&1         || { no "docker daemon not reachable in the VM"; exit 2; }
docker compose version >/dev/null 2>&1 || { no "docker compose v2 not found in the VM"; exit 2; }
[ -f "$SETUP" ]                     || { no "setup.sh not found at $SETUP -- run from a checkout"; exit 2; }
ok "arm64 Linux + Docker Engine + Compose v2 present"

# ── pull the REAL detector out of setup.sh so we test shipped logic ──
fn=$(awk '/^amd64_emulation_present\(\) \{/,/^\}/' "$SETUP")
if ! printf '%s' "$fn" | grep -q 'binfmt_misc'; then
    no "could not extract amd64_emulation_present() from setup.sh"
    printf "\n%s  ✗ test harness error%s\n" "$R" "$X"; exit 1
fi
eval "$fn"
ok "extracted amd64_emulation_present() from setup.sh (testing shipped logic)"

# ── consent (mutates host binfmt state) ─────────────────────────────
if [ "$YES" = "0" ]; then
    info "This registers/removes a qemu-x86_64 binfmt handler on THIS host."
    printf "Proceed? (intended for a throwaway VM) [y/N] "
    read -r ans; case "$ans" in y|Y|yes|YES) ;; *) info "aborted, no changes made."; exit 0 ;; esac
fi

started_present=0
[ -e "$HANDLER" ] && started_present=1

if [ "$started_present" = "1" ]; then
    sec "amd64 emulation already present -- testing the present-path only"
    info "A qemu-x86_64 handler is already registered; skipping the install/"
    info "uninstall round-trip so a pre-existing handler isn't removed."
    info "For the full round-trip, use a clean VM with no amd64 emulation."
    if amd64_emulation_present; then ok "detector reports present (matches host)"
    else no "detector says absent but a handler exists"; fi
    if docker run --rm --platform linux/amd64 alpine uname -m 2>/dev/null | grep -qx x86_64; then
        ok "amd64 container executes (uname -m = x86_64)"
    else
        no "amd64 container did not run under the existing handler"
    fi
else
    sec "Clean host -- testing detect → install → run → revert"

    # 1. detector agrees nothing is registered
    if amd64_emulation_present; then
        no "detector reports present, but no handler exists yet"
    else
        ok "detector reports absent on clean host (setup would auto-install)"
    fi

    # 2. install (exactly what setup.sh runs)
    info "installing amd64 emulation via tonistiigi/binfmt ..."
    if docker run --privileged --rm "$BINFMT_REF" --install amd64 >/dev/null 2>&1; then
        ok "tonistiigi/binfmt --install amd64 succeeded"
    else
        no "tonistiigi/binfmt --install amd64 failed (network? offline VM?)"
    fi

    # 3. handler now registered + enabled
    if [ -e "$HANDLER" ] && grep -q '^enabled' "$HANDLER" 2>/dev/null; then
        ok "qemu-x86_64 handler registered and enabled"
    else
        no "qemu-x86_64 handler missing/disabled after install"
    fi

    # 4. detector now agrees
    if amd64_emulation_present; then
        ok "detector reports present after install"
    else
        no "detector still reports absent after install"
    fi

    # 5. emulation actually executes amd64 code
    if docker run --rm --platform linux/amd64 alpine uname -m 2>/dev/null | grep -qx x86_64; then
        ok "amd64 container executes under emulation (uname -m = x86_64)"
    else
        no "amd64 container failed to run -- emulation not working"
    fi

    # 6. revert (exactly what uninstall runs)
    info "reverting via tonistiigi/binfmt --uninstall qemu-x86_64 ..."
    docker run --privileged --rm "$BINFMT_REF" --uninstall qemu-x86_64 >/dev/null 2>&1 || true
    if [ ! -e "$HANDLER" ]; then
        ok "qemu-x86_64 handler removed (host back to pre-setup state)"
    else
        no "qemu-x86_64 handler still present after uninstall"
    fi

    # 7. detector agrees again
    if amd64_emulation_present; then
        no "detector reports present after revert"
    else
        ok "detector reports absent after revert"
    fi
fi

# ── optional: full setup.sh + uninstall integration ─────────────────
if [ "$FULL" = "1" ]; then
    sec "Full integration: ./setup.sh → marker + OpenPLC → uninstall revert"
    info "this brings up the whole stack (~15 min, pulls images)..."
    if "$SETUP"; then ok "setup.sh completed"; else no "setup.sh exited non-zero"; fi
    if [ -f "$MARKER" ]; then ok "setup wrote the binfmt marker ($MARKER)"; else no "marker not written by setup"; fi
    if docker ps --format '{{.Names}} {{.Status}}' | grep '^rangerdanger-openplc ' | grep -qi 'up'; then
        ok "rangerdanger-openplc is Up (running under emulation)"
    else
        no "rangerdanger-openplc is not Up -- check 'docker ps -a'"
    fi
    info "running uninstall to revert..."
    if "$UNINSTALL" --yes; then ok "uninstall.sh completed"; else no "uninstall.sh exited non-zero"; fi
    if [ ! -f "$MARKER" ]; then ok "marker removed by uninstall"; else no "marker still present after uninstall"; fi
    if [ ! -e "$HANDLER" ]; then ok "binfmt handler reverted by uninstall"; else no "handler still present after uninstall"; fi
fi

# ── tally ───────────────────────────────────────────────────────────
sec "Result"
if [ "$FAIL" -eq 0 ]; then
    printf "%s  ✓ ALL %d CHECKS PASSED%s\n" "$G" "$PASS" "$X"; exit 0
else
    printf "%s  ✗ %d passed, %d FAILED%s\n" "$R" "$PASS" "$FAIL" "$X"; exit 1
fi
