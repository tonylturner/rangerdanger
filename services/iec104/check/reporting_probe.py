#!/usr/bin/env python3
"""Inspect real RTU ASDUs, including object packing and analog event cadence."""
import socket
import struct
import threading
import time
import urllib.request

from ccprobe import get_json, pt, request

CC = "http://172.28.105.21:8080"


def control(query):
    with urllib.request.urlopen("http://172.28.104.20:8081/" + query, timeout=3) as r:
        r.read()


def until(predicate, seconds=6):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(0.01)
    raise AssertionError("reporting condition timed out")


class Peer:
    def __init__(self):
        self.sock = socket.create_connection(("172.28.104.10", 2404), timeout=4)
        self.sock.settimeout(10)
        self.frames = []
        self.error = None
        self.lock = threading.Lock()
        self.tx = self.rx = 0
        self.stop = False
        self.thread = threading.Thread(target=self.read)
        self.thread.start()
        self.send(bytes.fromhex("680407000000"))
        time.sleep(0.2)

    def send(self, data):
        with self.lock:
            self.sock.sendall(data)

    def gi(self):
        with self.lock:
            a = bytes.fromhex("64010600010000000014")
            self.sock.sendall(bytes([104, len(a) + 4]) + struct.pack("<HH", self.tx * 2, self.rx * 2) + a)
            self.tx += 1

    def exact(self, n):
        data = b""
        while len(data) < n:
            b = self.sock.recv(n - len(data))
            if not b:
                raise EOFError
            data += b
        return data

    def read(self):
        try:
            while not self.stop:
                assert self.exact(1) == b"\x68"
                data = self.exact(self.exact(1)[0])
                if not data[0] & 1:
                    self.rx = (int.from_bytes(data[:2], "little") >> 1) + 1
                    self.frames.append((time.monotonic(), data[4:]))
                    self.send(bytes([104, 4, 1, 0]) + (self.rx * 2).to_bytes(2, "little"))
                elif data[0] == 0x43:
                    self.send(bytes.fromhex("680483000000"))
        except (OSError, EOFError, AssertionError) as e:
            if not self.stop:
                self.error = e

    def analog(self, cot, since=0):
        out = []
        for at, a in list(self.frames):
            if at < since or a[0] != 13 or a[2] & 63 != cot:
                continue
            assert a[1] & 128 == 0, a  # SQ=0: each object has its own IOA
            n = a[1] & 127
            assert len(a) == 6 + 8 * n, a
            for i in range(n):
                obj = a[6 + 8 * i:14 + 8 * i]
                if int.from_bytes(obj[:3], "little") == 2001:
                    out.append((at, struct.unpack("<f", obj[3:7])[0], obj[7], n))
        assert self.error is None, self.error
        return out

    def close(self):
        self.stop = True
        self.sock.shutdown(socket.SHUT_RDWR)
        self.sock.close()
        self.thread.join()


def main():
    control("reset")
    peer = Peer()
    try:
        peer.gi()
        until(lambda: any(a[0] == 100 and a[2] & 63 == 10 for _, a in peer.frames))
        gi = [(a[0], a[1] & 127) for _, a in peer.frames if a[2] & 63 == 20]
        assert sorted(gi) == [(1, 15), (3, 3), (5, 1), (13, 19)], gi
        cycles = until(lambda: peer.analog(1) if len(peer.analog(1)) >= 3 else None)
        assert all(x[3] == 19 for x in cycles), cycles
        assert all(1.9 <= b[0] - a[0] <= 2.5 for a, b in zip(cycles, cycles[1:])), cycles
        print("PASS GI and COT 1 pack by type with SQ=0; cyclic floats arrive every 2000 ms")

        # Start just after a cycle, so the limiter expiry precedes the next one.
        n = len(peer.analog(1))
        until(lambda: len(peer.analog(1)) > n)
        start = time.monotonic()
        control("analog?current=29.0")  # +0.1 < test deadband 0.5
        time.sleep(0.4)
        assert not peer.analog(3, start), peer.analog(3, start)
        control("analog?current=30.0")
        first = until(lambda: peer.analog(3, start))[0]
        control("analog?current=31.0")
        time.sleep(0.4)
        assert len(peer.analog(3, start)) == 1, peer.analog(3, start)
        second = until(lambda: peer.analog(3, start)[1:] or None)[0]
        assert 0.99 <= second[0] - first[0] <= 1.5, (first, second)
        control("mvalid?valid=0")
        bad = until(lambda: [x for x in peer.analog(3, second[0] + 0.001) if x[2] & 128])[0]
        assert bad[0] - second[0] < 0.9, (second, bad)
        control("mvalid?valid=1")
        until(lambda: any(x[0] > bad[0] and x[2] == 0 for x in peer.analog(3)))
        print("PASS deadband suppression, deferred value event at 1000 ms, immediate quality bypass")

        before = get_json(CC, "/api/state")["link"]["gi"]["started_at"]
        code, text = request(CC, "POST", "/api/gi")
        assert code == 202, (code, text)
        until(lambda: (lambda g: g["state"] == "complete" and g["trigger"] == "operator" and g["started_at"] > before)(
            get_json(CC, "/api/state")["link"]["gi"]))
        p = pt(get_json(CC, "/api/state"), "feeder-breaker-position")
        assert p["asdu"] == "M_DP_NA_1" and p["kind"] == "double-point" and p["reporting"] is None, p
        print("PASS operator POST reaches RTU and completes; API exposes received GI type")
    finally:
        peer.close()
        control("reset")


if __name__ == "__main__":
    main()
