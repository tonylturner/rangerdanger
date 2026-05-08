#!/bin/bash
# Vendor-jump remote-access services bootstrap.
#
# vendor-jump simulates a vendor's remote-access laptop / portal in
# the DMZ. The DefendICS workshop's segmentation lessons exercise
# the firewall rules that allow the enterprise zone to reach this
# host on SSH/HTTP/HTTPS/RDP/VNC under the weak baseline, and that
# narrow those rules in the improved policy. For the labs to feel
# real (curl, ssh, rdp clients all returning a believable response),
# the host needs actual listeners on those ports.
#
# This script runs once at container startup via the linuxserver.io
# webtop image's /custom-cont-init.d/ hook. It:
#   1. Creates the rangerdanger:rangerdanger user (sudo group).
#   2. Generates SSH host keys + writes a vendor-portal landing
#      page + writes a self-signed cert + sets the VNC password.
#   3. Starts sshd, nginx, xrdp, and tigervnc as background daemons
#      (this is a lab; the s6 zombie-reap warnings on nginx workers
#      are cosmetic — nginx respawns them and stays listening).
#
# Credentials are documented in docs/lab-credentials.md.

set -e

LAB_USER="rangerdanger"
LAB_PASS="rangerdanger"
LAB_HOME="/home/$LAB_USER"

log() { echo "[vendor-jump-services] $*"; }

# ── 1. User ─────────────────────────────────────────────────────────
if ! id "$LAB_USER" >/dev/null 2>&1; then
    log "creating user $LAB_USER"
    useradd -m -s /bin/bash "$LAB_USER"
    echo "$LAB_USER:$LAB_PASS" | chpasswd
    usermod -aG sudo "$LAB_USER"
fi

mkdir -p "$LAB_HOME/.vnc"
chown -R "$LAB_USER:$LAB_USER" "$LAB_HOME"

# ── 2. SSH ──────────────────────────────────────────────────────────
log "configuring sshd"
mkdir -p /run/sshd /var/run/sshd
ssh-keygen -A 2>/dev/null || true
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config
cat > /etc/ssh/banner <<'EOF'
+------------------------------------------------------------+
|  Vendor Remote Access Portal                              |
|  Authorized vendor support personnel only.                |
|  All sessions are logged and may be reviewed.             |
+------------------------------------------------------------+
EOF
grep -q '^Banner /etc/ssh/banner' /etc/ssh/sshd_config || \
    echo 'Banner /etc/ssh/banner' >> /etc/ssh/sshd_config

if ! pgrep -x sshd >/dev/null 2>&1; then
    /usr/sbin/sshd
    log "sshd started on :22"
fi

# ── 3. nginx HTTP/HTTPS ─────────────────────────────────────────────
log "configuring nginx"
if [ ! -f /etc/ssl/private/vendor.key ]; then
    mkdir -p /etc/ssl/private /etc/ssl/certs
    openssl req -x509 -newkey rsa:2048 -nodes -days 730 \
        -keyout /etc/ssl/private/vendor.key \
        -out /etc/ssl/certs/vendor.crt \
        -subj "/CN=vendor-portal.local/O=Vendor Inc/C=US" 2>/dev/null
    chmod 600 /etc/ssl/private/vendor.key
fi

mkdir -p /var/www/vendor-portal
cat > /var/www/vendor-portal/index.html <<'EOF'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Vendor Remote Access Portal</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 720px; margin: 4em auto; padding: 0 1em; color: #222; }
h1 { color: #c00; border-bottom: 2px solid #c00; padding-bottom: .3em; }
.banner { background: #fffbe6; border-left: 4px solid #f5a623; padding: 1em; margin: 1.5em 0; }
table { border-collapse: collapse; margin: 1em 0; }
th, td { padding: .5em 1em; border: 1px solid #ccc; text-align: left; }
th { background: #f5f5f5; }
code { background: #eee; padding: 2px 4px; border-radius: 3px; }
</style>
</head>
<body>
<h1>Vendor Remote Access Portal</h1>
<div class="banner">
<strong>Authorized vendor support personnel only.</strong>
All sessions are logged. Use of this system constitutes consent to monitoring.
</div>
<p>This portal provides remote support access for substation
field equipment maintained by Vendor Inc. Operators may use the
following channels:</p>
<table>
<tr><th>Service</th><th>Port</th><th>Protocol</th></tr>
<tr><td>SSH (shell)</td><td>22</td><td>tcp</td></tr>
<tr><td>HTTP (this portal)</td><td>80</td><td>tcp</td></tr>
<tr><td>HTTPS</td><td>443</td><td>tcp</td></tr>
<tr><td>RDP (remote desktop)</td><td>3389</td><td>tcp</td></tr>
<tr><td>VNC</td><td>5900</td><td>tcp</td></tr>
</table>
<p>For credentials, contact your designated vendor liaison.
Lab credentials are documented in <code>docs/lab-credentials.md</code>.</p>
</body>
</html>
EOF

cat > /etc/nginx/sites-available/vendor-portal <<'EOF'
server {
    listen 80 default_server;
    server_name _;
    root /var/www/vendor-portal;
    index index.html;
    server_tokens off;
    add_header X-Powered-By "Vendor Portal" always;
}
server {
    listen 443 ssl default_server;
    ssl_certificate /etc/ssl/certs/vendor.crt;
    ssl_certificate_key /etc/ssl/private/vendor.key;
    server_name _;
    root /var/www/vendor-portal;
    index index.html;
    server_tokens off;
    add_header X-Powered-By "Vendor Portal" always;
}
EOF
ln -sf /etc/nginx/sites-available/vendor-portal /etc/nginx/sites-enabled/default

if ! pgrep -x nginx >/dev/null 2>&1; then
    nginx -t 2>&1 | tail -2
    nginx
    log "nginx started on :80 and :443"
fi

# ── 4. xrdp ─────────────────────────────────────────────────────────
log "configuring xrdp"
if ! pgrep -x xrdp >/dev/null 2>&1; then
    /usr/sbin/xrdp-sesman 2>&1 | sed 's/^/[xrdp-sesman] /' &
    sleep 0.5
    /usr/sbin/xrdp 2>&1 | sed 's/^/[xrdp] /' &
    log "xrdp started on :3389"
fi

# ── 5. VNC ──────────────────────────────────────────────────────────
# x11vnc shares the existing X display (the same kasm desktop the
# web UI exposes on 8082). One desktop, three access channels:
# kasm web UI, RDP (xrdp), and VNC (x11vnc on 5900). x11vnc has its
# own `-storepasswd` so we don't need a separate vncpasswd tool.
log "configuring x11vnc"
VNC_PASSWD="/etc/x11vnc.pass"
if [ ! -s "$VNC_PASSWD" ]; then
    x11vnc -storepasswd "$LAB_PASS" "$VNC_PASSWD" >/dev/null
    chmod 600 "$VNC_PASSWD"
fi

# x11vnc runs as an s6 service from /custom-services.d/x11vnc/ so
# the supervisor can wait for the kasm Xvfb display to come up
# (which happens in parallel with our cont-init script) and respawn
# x11vnc cleanly. The service script itself is installed at image
# build time; nothing to do here.
log "x11vnc supervised by s6 (see /custom-services.d/x11vnc/run)"

log "all vendor-jump services started; user=$LAB_USER pass=$LAB_PASS"
