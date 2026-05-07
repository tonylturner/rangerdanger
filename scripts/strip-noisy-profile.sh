#!/bin/sh
# Strip profile scripts that spam errors in non-systemd containers.
#
# linuxserver/webtop:ubuntu-mate ships /etc/profile.d/im-config_wayland.sh
# which sources /usr/share/im-config/initializer — that initializer
# logs three telemetry lines via `systemd-cat -p 6 -t im-config`.
# systemd-cat tries to write to journald's socket, which doesn't
# exist in a non-systemd container, so each call prints
# "Failed to create stream fd: No such file or directory" to stderr.
#
# The errors are cosmetic but visible every time a student opens an
# in-app terminal on a webtop-mate node (eng-ws, corp-ws). Removing
# the profile script costs nothing — input-method config is desktop
# shell init, not relevant in a CLI lab terminal.

rm -f /etc/profile.d/im-config_wayland.sh
echo "strip-noisy-profile: removed im-config_wayland.sh"
