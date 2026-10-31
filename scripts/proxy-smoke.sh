#!/usr/bin/env bash
# Embedded-service regression probes, on an already-running disposable range.
set -euo pipefail
API="${RANGERDANGER_API:-http://localhost:8088}"
fail=0
checks=0
logs="${PROXY_LOG_DIR:-build/proxy-smoke}"
mkdir -p "$logs"
if ! python3 scripts/lib/containd-ui-probe.py "$API" >"$logs/containd.log" 2>&1; then
  cat "$logs/containd.log"
  fail=1
else
  checks=$((checks + 1))
fi

# A portal 200 alone misses RD-09: it can replace webtop's nginx site.
# The Selkies shell is identified by its title and touch-gamepad mount point.
# Receipts contain the body followed by curl's HTTP status on its own line.
is_desktop_shell() {
  [ "$(tail -n 1 "$1")" = "200" ] &&
    grep -Fq '<title>Selkies</title>' "$1" &&
    grep -Fq 'touch-gamepad-host' "$1"
}

ready=0
for _ in $(seq 1 12); do
  if docker exec rangerdanger-vendor-jump curl -fsS --max-time 4 -w '\n%{http_code}\n' \
      http://127.0.0.1:3000/ >"$logs/vendor-3000.html" 2>"$logs/vendor-3000.stderr" &&
      is_desktop_shell "$logs/vendor-3000.html"; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "FAIL vendor webtop :3000"
  fail=1
else
  checks=$((checks + 1))
fi
if ! curl -fsS --max-time 10 -w '\n%{http_code}\n' \
    "$API/apps/vendor-jump/" >"$logs/desktop.html" ||
    ! is_desktop_shell "$logs/desktop.html"; then
  echo "FAIL proxied vendor desktop"
  fail=1
else
  checks=$((checks + 1))
fi
if ! docker exec rangerdanger-vendor-jump readlink /etc/nginx/sites-enabled/default \
    >"$logs/vendor-default.txt" ||
    ! grep -qx '/etc/nginx/sites-available/default' "$logs/vendor-default.txt"; then
  echo "FAIL vendor default nginx site replaced"
  fail=1
else
  checks=$((checks + 1))
fi
if ! docker exec rangerdanger-kali curl -fsS --max-time 10 http://10.20.20.10:80/ \
    >"$logs/portal.html" ||
    ! grep -q 'Vendor Remote Access Portal' "$logs/portal.html"; then
  echo "FAIL vendor portal :80"
  fail=1
else
  checks=$((checks + 1))
fi

# RFB auth-type only: no GUI and no VNC password implementation/dependency.
if ! docker exec -i rangerdanger-kali timeout 10 python3 - \
    >"$logs/rfb.log" 2>&1 <<'RFB_PY'
import socket


def receive(sock, count):
    result = b""
    while len(result) < count:
        chunk = sock.recv(count - len(result))
        if not chunk:
            raise ValueError("truncated RFB handshake")
        result += chunk
    return result


def negotiate(sock):
    banner = receive(sock, 12)
    if banner != b"RFB 003.008\n":
        raise ValueError(f"unexpected RFB version: {banner!r}")
    sock.sendall(banner)
    count = receive(sock, 1)[0]
    if not count:
        raise ValueError("RFB server rejected negotiation")
    types = list(receive(sock, count))
    if 2 not in types or 1 in types:
        raise ValueError(f"expected VNC auth (2), never None (1): {types}")
    return types


if __name__ == "__main__":
    with socket.create_connection(("10.20.20.10", 5900), timeout=5) as sock:
        sock.settimeout(5)
        print("RFB security types:", negotiate(sock))
RFB_PY
then
  cat "$logs/rfb.log"
  fail=1
else
  checks=$((checks + 1))
fi

# RD-22 is not reproducible; Programs remains explicitly non-blocking.
if ! python3 - "$API" >"$logs/openplc.log" 2>&1 <<'OPENPLC_PY'
import http.cookiejar
import sys
import urllib.parse
import urllib.request

base = sys.argv[1].rstrip("/") + "/apps/openplc/"
opener = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
body = urllib.parse.urlencode({"username": "openplc", "password": "openplc"}).encode()
with opener.open(base + "login", data=body, timeout=8) as response:
    response.read()
with opener.open(base + "programs", timeout=8) as response:
    text = response.read().decode(errors="replace")
    if (response.status != 200 or "login" in response.url
            or "program" not in text.lower() or 'name="username"' in text.lower()):
        raise ValueError("Programs did not return an authenticated program list")
    print("OpenPLC Programs: authenticated page returned")
OPENPLC_PY
then
  echo "WARN OpenPLC Programs unavailable (RD-22, non-blocking); see $logs/openplc.log"
else
  checks=$((checks + 1))
fi
if [ "$fail" -eq 0 ]; then
  echo "PASS proxy-smoke: $checks checks"
fi
exit "$fail"
