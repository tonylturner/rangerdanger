#!/bin/sh
# Start xrdp, tigervnc, and sshd for the vendor-rdp-compromise lab.
#
# Under the weak baseline the enterprise zone can reach the vendor
# DMZ on RDP/3389, VNC/5900, and SSH/22 — all three are the lateral
# path described in the workshop deck (Slides 55, 88). Under the
# hardened policy RDP/VNC are blocked at the perimeter; SSH is left
# open as a legitimate management path, but the second hop
# (vendor → field) is blocked instead.
#
# Mounted at /custom-cont-init.d/20-start-rdp-vnc.sh by docker-compose.

# --- Create vendor-user (password: vendor) on first boot. ----------
if ! id vendor-user >/dev/null 2>&1; then
    useradd -m -s /bin/bash vendor-user
    echo 'vendor-user:vendor' | chpasswd
    # xrdp picks up the user's startup file when it spawns a session.
    echo "xfce4-session" > /home/vendor-user/.xsession
    chown vendor-user:vendor-user /home/vendor-user/.xsession
fi

# xrdp needs to be in the ssl-cert group to read the snakeoil cert.
adduser xrdp ssl-cert 2>/dev/null || true

# --- xrdp on TCP/3389. -------------------------------------------
# linuxserver/webtop is s6-based; service/init.d both work for
# scripts dropped into /custom-cont-init.d. Try service first, fall
# back to invoking the daemons directly if it isn't available.
if command -v service >/dev/null 2>&1; then
    service xrdp start || true
    service xrdp-sesman start || true
else
    /usr/sbin/xrdp-sesman --nodaemon &
    /usr/sbin/xrdp --nodaemon &
fi

# --- sshd on TCP/22. ---------------------------------------------
# Generate host keys on first boot if they aren't there.
ssh-keygen -A 2>/dev/null || true
mkdir -p /run/sshd
if command -v service >/dev/null 2>&1; then
    service ssh start || /usr/sbin/sshd
else
    /usr/sbin/sshd
fi

# --- VNC on TCP/5900 (no auth — lab convenience only). -----------
# tigervncserver opens a new X display for vendor-user with no
# security. Real-world VNC would require a password and TLS; this
# is deliberately open so the lab demonstrates the "anonymous VNC
# was left running" failure mode that's common in the wild.
sudo -u vendor-user mkdir -p /home/vendor-user/.vnc
sudo -u vendor-user tigervncserver :0 \
    -geometry 1024x768 \
    -depth 24 \
    -localhost no \
    -SecurityTypes None \
    -xstartup /usr/bin/startxfce4 \
    >/tmp/vnc.log 2>&1 || true

echo "vendor-jump: xrdp/3389, sshd/22, tigervnc/5900 started"
