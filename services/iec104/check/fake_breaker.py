#!/usr/bin/env python3
# RangerDanger IEC 60870-5-104 checks: a minimal fake feeder-breaker.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# A stand-in for the real services/devices feeder breaker, implementing just
# the Modbus TCP register map the RTU reads (contract §4) plus a tiny HTTP
# control plane so the check harness can inject a fault, clear it,
# invalidate measurements, delay FC6 answers and reset. stdlib only; run
# with `python3 -I`.
#
# Modbus (unit 1, big-endian, FC4/FC3/FC6):
#   input 0      position: 1 open, 2 closed (double-point code)
#   input 1      bits: 0 remote_enabled, 1 lockout, 2 fault_active, 3 tripped
#   input 2      bit 0 measurements_valid
#   input 3-4    current_a   float32, high word first
#   input 5-6    voltage_kv  float32, high word first
#   holding 0    FC6: 1 open, 2 close (echo only after the model accepts;
#                close under lockout/fault -> exception 04 interlock)
import http.server
import socket
import socketserver
import struct
import sys
import threading
import time
import urllib.parse

CLOSED_CURRENT_A = 28.9
CLOSED_VOLTAGE_KV = 20.0

lock = threading.Lock()
state = {
    "position": 2,  # closed
    "remote_enabled": True,
    "lockout": False,
    "fault_active": False,
    "tripped": False,
    "measurements_valid": True,
}
# FC6 answer delay in ms (GET /delay?ms=N), so the CC harness can hold a
# command in flight; reset() clears it.
write_delay = {"ms": 0}
# Delay the first input read after an accepted FC6
# (GET /delay-next-read-after-write?ms=N), letting the CLI check stop the RTU
# after ACT-CON but before its ACT-TERM.
read_delay = {"ms": 0, "pending": False}
# Lifetime transport/poll evidence; device resets must not erase reconnects.
stats = {"connections": 0, "read_replies": 0, "read_exceptions": 0}
read_exception = {"pending": False}


def input_registers():
    with lock:
        pos = state["position"]
        bits = (
            (1 if state["remote_enabled"] else 0)
            | (2 if state["lockout"] else 0)
            | (4 if state["fault_active"] else 0)
            | (8 if state["tripped"] else 0)
        )
        mv = 1 if state["measurements_valid"] else 0
        closed = pos == 2
        cur = CLOSED_CURRENT_A if closed else 0.0
        volt = CLOSED_VOLTAGE_KV if closed else 0.0
    regs = [0] * 11
    regs[0] = pos
    regs[1] = bits
    regs[2] = mv
    c = struct.unpack(">HH", struct.pack(">f", cur))
    v = struct.unpack(">HH", struct.pack(">f", volt))
    regs[3], regs[4] = c
    regs[5], regs[6] = v
    return regs


def write_holding(value):
    """Returns None on accept (echo), or a Modbus exception code on reject."""
    with lock:
        if value not in (1, 2):
            return 3  # illegal data value
        if not state["remote_enabled"]:
            return 4
        want_closed = value == 2
        if want_closed and (state["lockout"] or state["fault_active"]):
            return 4  # interlock
        state["position"] = 2 if want_closed else 1
        return None


def apply_fault(active):
    with lock:
        state["fault_active"] = active
        if active:
            state["position"] = 1  # open immediately
            state["lockout"] = True
            state["tripped"] = True


def reset():
    with lock:
        state.update(
            position=2,
            remote_enabled=True,
            lockout=False,
            fault_active=False,
            tripped=False,
            measurements_valid=True,
        )
        write_delay["ms"] = 0
        read_delay.update(ms=0, pending=False)
        read_exception["pending"] = False


def handle_pdu(pdu):
    if len(pdu) < 1:
        return None
    fc = pdu[0]
    if fc == 0x04 or fc == 0x03:  # read input / holding
        if len(pdu) != 5:
            return bytes([fc | 0x80, 3])
        addr, count = struct.unpack(">HH", pdu[1:5])
        if fc == 0x04:
            with lock:
                if read_exception["pending"]:
                    read_exception["pending"] = False
                    stats["read_exceptions"] += 1
                    return bytes([fc | 0x80, 4])
                delay = read_delay["ms"] if read_delay["pending"] else 0
                if delay:
                    read_delay["pending"] = False
            if delay:
                print("delayed FC4 read started", flush=True)
                time.sleep(delay / 1000.0)
            regs = input_registers()
        else:
            regs = [state["position"]]
        if addr + count > len(regs) or count < 1:
            return bytes([fc | 0x80, 2])
        body = b"".join(struct.pack(">H", regs[addr + i]) for i in range(count))
        with lock:
            stats["read_replies"] += 1
        return bytes([fc, len(body)]) + body
    if fc == 0x06:  # write single holding
        if len(pdu) != 5:
            return bytes([fc | 0x80, 3])
        addr, value = struct.unpack(">HH", pdu[1:5])
        if addr != 0:
            return bytes([fc | 0x80, 2])
        with lock:
            delay = write_delay["ms"]
        if delay:
            time.sleep(delay / 1000.0)
        exc = write_holding(value)
        if exc is not None:
            return bytes([fc | 0x80, exc])
        with lock:
            if read_delay["ms"] > 0:
                read_delay["pending"] = True
        return pdu  # echo
    return bytes([fc | 0x80, 1])  # illegal function


def serve_modbus(host, port):
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((host, port))
    srv.listen(8)
    while True:
        conn, _ = srv.accept()
        with lock:
            stats["connections"] += 1
        threading.Thread(target=modbus_conn, args=(conn,), daemon=True).start()


def modbus_conn(conn):
    with conn:
        while True:
            hdr = recv_exact(conn, 7)
            if not hdr:
                return
            tx, proto, length = struct.unpack(">HHH", hdr[:6])
            unit = hdr[6]
            pdu = recv_exact(conn, length - 1)
            if pdu is None:
                return
            resp = handle_pdu(pdu)
            if resp is None:
                return
            out = struct.pack(">HHH", tx, 0, len(resp) + 1) + bytes([unit]) + resp
            conn.sendall(out)


def recv_exact(conn, n):
    buf = b""
    while len(buf) < n:
        chunk = conn.recv(n - len(buf))
        if not chunk:
            return None
        buf += chunk
    return buf


class Control(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        if u.path == "/fault":
            apply_fault(q.get("active", ["1"])[0] == "1")
        elif u.path == "/reset":
            reset()
        elif u.path == "/mvalid":
            with lock:
                state["measurements_valid"] = q.get("valid", ["1"])[0] == "1"
        elif u.path == "/delay":
            with lock:
                write_delay["ms"] = int(q.get("ms", ["0"])[0])
        elif u.path == "/delay-next-read-after-write":
            with lock:
                read_delay.update(
                    ms=int(q.get("ms", ["0"])[0]),
                    pending=False,
                )
        elif u.path == "/exception-next-read":
            with lock:
                read_exception["pending"] = True
        with lock:
            snap = dict(state, **stats)
        body = (str(snap) + "\n").encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    host = sys.argv[1] if len(sys.argv) > 1 else "0.0.0.0"
    threading.Thread(target=serve_modbus, args=(host, 502), daemon=True).start()
    httpd = socketserver.ThreadingTCPServer((host, 8081), Control)
    httpd.allow_reuse_address = True
    httpd.serve_forever()


if __name__ == "__main__":
    main()
