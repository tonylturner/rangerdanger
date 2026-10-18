#!/bin/bash
# Seed errors must never fall through to the base image's blank program.
set -euo pipefail

/usr/local/bin/set-gateway.sh
cd /opt/openplc-eu
sha256sum --check seed.sha256
seed_hash=$(sha256sum seed.sha256 | cut -d ' ' -f 1)

cd /root/OpenPLC_v3/webserver
cp /opt/openplc-eu/tap-control.st st_files/tap-control.st
# The compiler optimizes its input in place; hash the immutable seed, not that copy.
rm -f core/openplc
./scripts/compile_program.sh tap-control.st
test -x core/openplc
test "$(cat active_program)" = tap-control.st
python3 /opt/openplc-eu/install.py
printf '%s\n' "$seed_hash" > /var/lib/openplc-eu-seed.sha256
echo "EU OpenPLC seed sha256: $seed_hash"

# The base web server starts the already-compiled runtime using Start_run_mode=true.
python2 webserver.py &
server_pid=$!
trap 'exit 143' TERM
trap 'exit 130' INT
trap 'kill "$server_pid" 2>/dev/null || : ' EXIT

# Prove the seeded program is executing, rather than trusting a listening socket.
python3 - <<'PY'
import socket
import struct
import time


def receive(sock, size):
    data = b""
    while len(data) < size:
        chunk = sock.recv(size - len(data))
        if not chunk:
            raise RuntimeError("short Modbus response")
        data += chunk
    return data


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


previous = None
for attempt in range(30):
    try:
        with socket.create_connection(("127.0.0.1", 502), 1) as sock:
            sock.settimeout(1)
            sock.sendall(struct.pack(">HHHBBHH", 1, 0, 6, 1, 3, 1024, 6))
            header = receive(sock, 7)
            require(header == struct.pack(">HHHB", 1, 0, 15, 1), "invalid Modbus header")
            body = receive(sock, 14)
            require(body[:2] == b"\x03\x0c", "invalid Modbus response")
            words = struct.unpack(">6H", body[2:])
            require(words[0] & 1 and words[1:4] == (20400, 10, 30), "invalid seeded status")
            if previous is not None and words[5] != previous:
                with socket.create_connection(("127.0.0.1", 8080), 1):
                    pass
                break
            previous = words[5]
    except OSError:
        pass  # Bounded startup wait only; malformed protocol/status fails immediately.
    time.sleep(1)
else:
    raise RuntimeError("seeded runtime did not become ready")
PY
wait "$server_pid"
