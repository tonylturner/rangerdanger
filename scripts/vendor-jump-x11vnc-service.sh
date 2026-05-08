#!/bin/bash
# /custom-services.d/x11vnc/run — s6 service for the lab VNC.
#
# webtop's Xvfb starts in parallel with our cont-init scripts, so
# x11vnc can't attach to display :1 until Xvfb is up. Running it as
# a long-lived s6 service means s6 will respawn us until the display
# is ready and x11vnc can stick.
#
# Exec's in foreground so s6 can supervise + reap properly.

set -e

VNC_PASSWD=/etc/x11vnc.pass

# If our cont-init script hasn't created the password yet, wait for
# it. (Cont-init runs before services, so this should be a no-op
# almost always — defensive only.)
for i in $(seq 1 30); do
    [ -s "$VNC_PASSWD" ] && break
    sleep 1
done

# Wait for the kasm Xvfb display to come up.
for i in $(seq 1 60); do
    [ -S /tmp/.X11-unix/X1 ] && break
    sleep 1
done

if [ ! -S /tmp/.X11-unix/X1 ]; then
    echo "[x11vnc-service] kasm display :1 never came up; sleeping 60s and exiting (s6 will restart)" >&2
    sleep 60
    exit 1
fi

echo "[x11vnc-service] starting x11vnc on :5900 attached to display :1" >&2
# -noshm: avoid MIT-SHM (Xvfb under selkies denies SHM attach, x11vnc
#   would crash on first connection without this).
# -listen 0.0.0.0: bind explicit IPv4 (selkies pulse-audio sometimes
#   holds the IPv6 5900 wildcard on its own port range and we don't
#   need IPv6 from the lab anyway).
exec x11vnc \
    -display :1 \
    -rfbport 5900 \
    -rfbauth "$VNC_PASSWD" \
    -forever \
    -shared \
    -noxdamage \
    -noshm \
    -listen 0.0.0.0
