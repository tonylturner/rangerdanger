#!/usr/bin/env python3
"""Real IEC104 timed reports immediately overwritten by GI; captured HTTP proof."""

import socket
import struct
import time

from ccprobe import get_json, pt

CC = "http://172.28.105.22:8080"
HIST = "http://172.28.105.30:8081"


def until(predicate, seconds=10):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.02)
    raise AssertionError("condition not met in attempt window")


def samples(epoch):
    return [
        x["state"]
        for x in get_json(HIST, "/samples")
        if x["state"]["cache_epoch"] == epoch
    ]


def main():
    get_json(HIST, "/set?delay=0.4")
    try:
        with socket.socket() as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("172.28.104.40", 2404))
            listener.listen(1)
            listener.settimeout(15)
            sock, _ = listener.accept()
        with sock:
            sock.settimeout(5)

            def exact(n):
                data = b""
                while len(data) < n:
                    chunk = sock.recv(n - len(data))
                    assert chunk
                    data += chunk
                return data

            def frame():
                assert exact(1) == b"\x68"
                return exact(exact(1)[0])

            assert frame() == bytes.fromhex("07000000")
            sock.sendall(bytes.fromhex("68040b000000"))
            while True:
                gi = frame()
                if not gi[0] & 1 and gi[4] == 100:
                    break
            rx = (int.from_bytes(gi[:2], "little") >> 1) + 1
            tx = 0

            def report(type_id, ioa, payload, cot):
                nonlocal tx
                asdu = bytes([type_id, 1, cot, 0, 1, 0])
                asdu += ioa.to_bytes(3, "little") + payload
                wire = bytes([104, 4 + len(asdu)]) + struct.pack("<HH", tx * 2, rx * 2) + asdu
                tx += 1
                return wire

            sock.sendall(report(100, 0, b"\x14", 7) + report(100, 0, b"\x14", 10))
            epoch = get_json(CC, "/api/state")["cache_epoch"]
            until(lambda: len(samples(epoch)) >= 1)
            # The historian has the cadence body but is still holding its
            # response. Send paired ASDUs in ONE write, never relying on sleeps
            # between the timed report and the GI that clears its cached tag.
            expected = []
            wire = b""
            for invalid in (False, True):
                tag = struct.pack("<H", 56789) + bytes([34 | (128 if invalid else 0), 12, 15, 10, 26])
                for timed, untimed, ioa, point, value in [
                    (31, 3, 1001, "feeder-breaker-position", b"\x02"),
                    (30, 1, 1303, "oltc-1-auto", b"\x01"),
                    (32, 5, 1301, "oltc-1-tap", b"\x03\x00"),
                ]:
                    wire += report(timed, ioa, value + tag, 3)
                    wire += report(untimed, ioa, value, 20)
                    expected.append((point, invalid))
            sock.sendall(wire)

            def archived():
                states = samples(epoch)
                return all(
                    any(
                        pt(s, point)["source_time"] == "2026-10-15T12:34:56.789Z"
                        and pt(s, point)["source_time_invalid"] is invalid
                        for s in states
                    )
                    for point, invalid in expected
                )

            until(archived)
            state = get_json(CC, "/api/state")
            for point, _ in expected:
                assert pt(state, point)["source_time"] is None
                assert pt(state, point)["source_time_invalid"] is None
            print("PASS six timed DP/SP/step snapshots retain tag and CP56 IV despite immediate GI", flush=True)

            # Drain the six-event batch before a deliberate overload while
            # each HTTP response still costs 400 ms (< the 500 ms timeout).
            time.sleep(3)
            before = get_json(CC, "/api/health")["historian"]["dropped_snapshots"]
            wire = b""
            for i in range(80):
                tag = struct.pack("<H", 10000 + i) + bytes([35, 12, 15, 10, 26])
                wire += report(30, 1303, b"\x01" + tag, 3)
                wire += report(1, 1303, b"\x01", 20)
            sock.sendall(wire)
            until(lambda: get_json(CC, "/api/health")["historian"]["dropped_snapshots"] > before)
            get_json(HIST, "/set?delay=0")
            until(
                lambda: any(
                    pt(s, "oltc-1-auto")["source_time"] == "2026-10-15T12:35:10.079Z"
                    for s in samples(epoch)
                )
            )
            state = get_json(CC, "/api/state")
            assert state["link"]["state"] == "started"
            assert pt(state, "oltc-1-auto")["source_time"] is None
            states = samples(epoch)
            assert all(b["snapshot_seq"] > a["snapshot_seq"] for a, b in zip(states, states[1:]))
            failures = get_json(CC, "/api/health")["historian"]["failures"]
            assert failures == 0, failures
            print("PASS bounded oldest-drop queue counted overflow, kept newest tag, ordered pushes and live link", flush=True)
    finally:
        get_json(HIST, "/set?delay=0")


if __name__ == "__main__":
    main()
