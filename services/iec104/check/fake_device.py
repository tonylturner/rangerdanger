#!/usr/bin/env python3
"""Wire-only fixtures for RMU, OLTC and PLC; never a production physics model."""

import http.server
import json
import os
import socketserver
import struct
import threading
import time
import urllib.parse

kind = os.environ["KIND"]
lock = threading.Lock()
state = {
    "tap": 0,
    "target": 0,
    "auto": True,
    "position": 1 if kind == "rmu-2" else 2,
    "moving_until": 0,
    "motion_ms": 2000,
    "freeze": False,
    "heartbeat": True,
    "bad_vti": False,
    "pf_invalid": False,
    "write_mode": "ok",
    "lose_after_write": False,
    "read_lost": False,
    "writes": [],
    "polls": [],
}
boot = time.monotonic()


def snapshot():
    now = time.monotonic()
    if state["moving_until"] and now >= state["moving_until"] and not state["freeze"]:
        state["tap"] = state["target"]
        state["moving_until"] = 0
    if kind == "plc":
        hb = (
            int((now - boot) * 2) & 65535
            if state["heartbeat"]
            else state["held_heartbeat"]
        )
        return [7, 20400, 10, 30, 0, hb]
    regs = [0] * (21 if kind == "oltc" else 17 if kind == "f2" else 13)
    regs[0] = (
        (100 if state["bad_vti"] else state["tap"]) & 65535
        if kind == "oltc"
        else state["position"]
    )
    regs[1] = 1
    if kind == "oltc":
        regs[1] |= (
            (2 if state["auto"] else 0) | (4 if state["moving_until"] else 0) | 64
        )
        regs[11] = state["target"] & 65535
        regs[12] = 20000
        regs[14] = int((now - boot) * 4) & 65535
    regs[2] = 1
    for addr, val in [
        (3, 20.0 if kind == "oltc" else 41.2),
        (5, 41.2 if kind == "oltc" else 19.9),
    ]:
        regs[addr : addr + 2] = struct.unpack(">HH", struct.pack(">f", val))
    if kind == "f2":
        for addr, val in [(11, 0.33), (13, 0.1), (15, 0.96)]:
            regs[addr:addr+2] = struct.unpack(">HH", struct.pack(">f", val))
    elif kind != "oltc":
        regs[11:13] = struct.unpack(">HH", struct.pack(">f", 20.0))
    if kind == "oltc":
        for addr, val in [(15, 2.0), (17, 0.6), (19, float("nan") if state["pf_invalid"] else 0.96)]:
            regs[addr:addr+2] = struct.unpack(">HH", struct.pack(">f", val))
    return regs


class Modbus(socketserver.BaseRequestHandler):
    def exact(self, n):
        data = b""
        while len(data) < n:
            chunk = self.request.recv(n - len(data))
            if not chunk:
                raise EOFError
            data += chunk
        return data

    def handle(self):
        try:
            while True:
                tx, proto, length, unit = struct.unpack(">HHHB", self.exact(7))
                pdu = self.exact(length - 1)
                fc, addr, value = struct.unpack(">BHH", pdu)
                with lock:
                    regs = snapshot()
                    if fc in (3, 4):
                        if state["read_lost"]:
                            return
                        start = 1024 if kind == "plc" else 0
                        assert fc == (3 if kind == "plc" else 4)
                        assert addr == start and value == len(regs), (
                            fc,
                            addr,
                            value,
                            kind,
                        )
                        state["polls"].append(time.monotonic())
                        state["polls"] = state["polls"][-200:]
                        response = bytes([fc, value * 2]) + struct.pack(
                            ">" + "H" * value, *regs
                        )
                    elif fc == 6:
                        state["writes"].append(
                            {"at": time.monotonic(), "address": addr, "value": value}
                        )
                        if state["write_mode"] == "reject":
                            response = bytes([134, 4])
                        else:
                            if kind == "oltc" and addr == 2:
                                state["auto"] = value == 2
                            elif kind == "oltc":
                                state["target"] = state["tap"] + (
                                    1 if value == 2 else -1
                                )
                                state["moving_until"] = (
                                    time.monotonic() + state["motion_ms"] / 1000
                                )
                            else:
                                state["position"] = value
                            if state["write_mode"] == "lost_echo":
                                return  # operation happened, but FC6 response was lost
                            if state["lose_after_write"]:
                                state["read_lost"] = True
                            response = pdu
                    else:
                        response = bytes([fc | 128, 1])
                self.request.sendall(
                    struct.pack(">HHHB", tx, proto, len(response) + 1, unit) + response
                )
        except (EOFError, ConnectionError):
            pass


class Control(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        args = urllib.parse.parse_qs(parsed.query)
        with lock:
            if parsed.path == "/set":
                for key, values in args.items():
                    assert key in state and key not in ("polls", "writes")
                    value = values[0]
                    if key == "heartbeat" and value == "false":
                        state["held_heartbeat"] = snapshot()[5]
                    state[key] = json.loads(value) if key != "write_mode" else value
            body = json.dumps(state).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


threading.Thread(
    target=Server(("0.0.0.0", 502), Modbus).serve_forever, daemon=True
).start()
http.server.ThreadingHTTPServer(("0.0.0.0", 8081), Control).serve_forever()
