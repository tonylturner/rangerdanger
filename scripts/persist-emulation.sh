#!/usr/bin/env bash
#
# Persist amd64 emulation across reboots (arm64 Linux only).
#
# setup.sh registers a qemu-x86_64 binfmt handler via tonistiigi/binfmt so
# the amd64-only OpenPLC image runs on arm64 Linux -- but that registration
# is runtime-only and is lost on reboot. This installs a systemd oneshot
# unit that re-registers it on every boot, so OpenPLC survives reboots.
#
# Opt-in and separate from setup.sh because it needs root (setup.sh does
# not). No-op on macOS (Rosetta) and on amd64 (OpenPLC is native there).
#
# Usage (needs root for systemd):
#   sudo ./scripts/persist-emulation.sh              # install the boot unit
#   sudo ./scripts/persist-emulation.sh --uninstall  # remove it
#
# Requires: systemd + Docker.

set -euo pipefail

# Keep in sync with setup.sh / stage-ssd*.sh / uninstall-rangerdanger.sh.
BINFMT_REF="tonistiigi/binfmt:qemu-v10.2.1"
UNIT_NAME="rangerdanger-binfmt.service"
UNIT="/etc/systemd/system/${UNIT_NAME}"

if [ -t 1 ]; then
    GREEN=$'\e[32m'; YELLOW=$'\e[33m'; RED=$'\e[31m'; BOLD=$'\e[1m'; RESET=$'\e[0m'
else
    GREEN=""; YELLOW=""; RED=""; BOLD=""; RESET=""
fi
say()  { printf "%s[+]%s %s\n" "$GREEN" "$RESET" "$*"; }
warn() { printf "%s[!]%s %s\n" "$YELLOW" "$RESET" "$*" >&2; }
die()  { printf "%s[x]%s %s\n" "$RED" "$RESET" "$*" >&2; exit 1; }

ACTION=install
case "${1:-}" in
    ""|install)            ACTION=install ;;
    --uninstall|uninstall) ACTION=uninstall ;;
    -h|--help)             sed -n '2,/^set /p' "$0" | sed 's/^# \?//;/^set /d'; exit 0 ;;
    *)                     die "unknown arg: $1 (see --help)" ;;
esac

# ─── gates ──────────────────────────────────────────────────────────
OS=$(uname -s); MACH=$(uname -m)
[ "$OS" = "Linux" ] || die "Linux only (this host is $OS). macOS uses Rosetta; nothing to persist."
case "$MACH" in
    arm64|aarch64) ;;
    *) die "arm64 only (this host is $MACH). amd64 hosts run OpenPLC natively -- no emulation needed." ;;
esac
command -v systemctl >/dev/null 2>&1 || die "systemd not found (no systemctl) -- can't install a boot unit here."
[ "$(id -u)" = "0" ] || die "Run with sudo: sudo $0 $([ "$ACTION" = uninstall ] && echo --uninstall)"

# ─── uninstall ──────────────────────────────────────────────────────
if [ "$ACTION" = "uninstall" ]; then
    if [ -f "$UNIT" ]; then
        systemctl disable --now "$UNIT_NAME" >/dev/null 2>&1 || true
        rm -f "$UNIT"
        systemctl daemon-reload
        say "Removed $UNIT. amd64 emulation will no longer re-register on boot."
        say "(The current session keeps it until you reboot or run uninstall-rangerdanger.sh.)"
    else
        say "No boot unit installed ($UNIT absent) -- nothing to do."
    fi
    exit 0
fi

# ─── install ────────────────────────────────────────────────────────
DOCKER=$(command -v docker) || die "docker not found in PATH."

cat > "$UNIT" <<EOF
[Unit]
Description=RangerDanger: register amd64 (qemu-x86_64) emulation for OpenPLC
Documentation=https://github.com/tonylturner/rangerdanger
Wants=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=${DOCKER} run --privileged --rm ${BINFMT_REF} --install amd64

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "$UNIT_NAME"

if [ -e /proc/sys/fs/binfmt_misc/qemu-x86_64 ]; then
    say "Installed + enabled $UNIT_NAME."
    say "amd64 emulation is registered now and will re-register on every boot."
else
    warn "Unit installed, but qemu-x86_64 isn't registered yet."
    warn "Check: systemctl status $UNIT_NAME ; journalctl -u $UNIT_NAME"
fi
echo
echo "To undo: sudo $0 --uninstall"
