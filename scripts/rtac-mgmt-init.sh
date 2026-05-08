#!/bin/sh
# rtac-mgmt-init.sh — start the RTAC management surface (sshd + nginx).
#
# The improved firewall policy allows vendor (DMZ) → OT (lan1) on
# tcp/22 and tcp/443 ("encrypted management only"). For Lab 2.4
# students to actually verify that allowance, the RTAC needs real
# listeners on those ports. Without them, every probe gets a no-
# listener RST that Docker Desktop's bridge sometimes drops as a
# runt frame — indistinguishable from a firewall drop.
#
# This script runs as part of the rtac-sim CMD before the Go binary
# exec's. It:
#   1. Creates rangerdanger:rangerdanger (matches vendor-jump creds
#      documented in docs/lab-credentials.md).
#   2. Starts sshd on :22 with a banner.
#   3. Generates a self-signed cert and starts nginx on :443 serving
#      a minimal RTAC management portal page.
#
# All listeners bind 0.0.0.0; rtac-harden.sh installs an iptables
# INPUT DROP on the field_net interface so sshd/nginx are not
# reachable from field devices, only from OT Ops + traffic that
# transits the firewall.

set -e

LAB_USER="rangerdanger"
LAB_PASS="rangerdanger"
LAB_HOME="/home/$LAB_USER"

log() { echo "[rtac-mgmt-init] $*"; }

if ! id "$LAB_USER" >/dev/null 2>&1; then
    log "creating user $LAB_USER"
    adduser -D -s /bin/sh "$LAB_USER"
    echo "$LAB_USER:$LAB_PASS" | chpasswd
fi

mkdir -p "$LAB_HOME"
chown -R "$LAB_USER:$LAB_USER" "$LAB_HOME"

# ── sshd ────────────────────────────────────────────────────────────
log "configuring sshd"
mkdir -p /etc/ssh /run/sshd
ssh-keygen -A 2>/dev/null || true
sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config
cat > /etc/ssh/banner <<'EOF'
+------------------------------------------------------------+
|  RTAC Management Console                                  |
|  Authorized OT operators only.                            |
|  All sessions are logged.                                 |
+------------------------------------------------------------+
EOF
grep -q '^Banner /etc/ssh/banner' /etc/ssh/sshd_config || \
    echo 'Banner /etc/ssh/banner' >> /etc/ssh/sshd_config

if ! pgrep -x sshd >/dev/null 2>&1; then
    /usr/sbin/sshd
    log "sshd started on :22"
fi

# ── nginx HTTPS ─────────────────────────────────────────────────────
log "configuring nginx"
if [ ! -f /etc/ssl/private/rtac.key ]; then
    mkdir -p /etc/ssl/private /etc/ssl/certs
    openssl req -x509 -newkey rsa:2048 -nodes -days 730 \
        -keyout /etc/ssl/private/rtac.key \
        -out /etc/ssl/certs/rtac.crt \
        -subj "/CN=rtac.local/O=Substation/C=US" 2>/dev/null
    chmod 600 /etc/ssl/private/rtac.key
fi

mkdir -p /var/www/rtac /run/nginx
cat > /var/www/rtac/index.html <<'EOF'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>RTAC Management Console</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 720px; margin: 4em auto; padding: 0 1em; color: #222; }
h1 { color: #036; border-bottom: 2px solid #036; padding-bottom: .3em; }
.banner { background: #fffbe6; border-left: 4px solid #f5a623; padding: 1em; margin: 1.5em 0; }
code { background: #eee; padding: 2px 4px; border-radius: 3px; }
</style>
</head>
<body>
<h1>RTAC Management Console</h1>
<div class="banner">
<strong>Authorized OT operators only.</strong>
This is a lab simulation of an RTAC management web console — in
production this would expose vendor-supplied health / firmware /
diagnostics endpoints over TLS.
</div>
<p>The live RTAC API is on port 8080 (<code>/api/state</code>,
<code>/api/health</code>). This TLS endpoint exists so the firewall's
"vendor → OT for monitoring" allowance has a real listener to probe
against in Lab 2.4.</p>
</body>
</html>
EOF

# Replace alpine's stock default conf with our RTAC portal config.
cat > /etc/nginx/http.d/default.conf <<'EOF'
server {
    listen 443 ssl default_server;
    ssl_certificate /etc/ssl/certs/rtac.crt;
    ssl_certificate_key /etc/ssl/private/rtac.key;
    server_name _;
    root /var/www/rtac;
    index index.html;
    server_tokens off;
}
EOF

if ! pgrep -x nginx >/dev/null 2>&1; then
    nginx -t 2>&1 | sed 's/^/[nginx] /' | tail -2
    nginx
    log "nginx started on :443"
fi

log "RTAC management surface up — user=$LAB_USER on :22, TLS portal on :443"
