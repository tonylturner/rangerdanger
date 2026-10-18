#!/usr/bin/env python3
"""Increment-4 attempt-window evidence through the real CC and Modbus fixtures."""

import concurrent.futures
import itertools
import json
import socket
import sys
import time

from ccprobe import get_json, good, pt, request

CC = "http://172.28.105.21:8080"
HIST = "http://172.28.105.30:8081"


def fixture(ip, query=""):
    return get_json(f"http://172.28.104.{ip}:8081", "/set?" + query)


def until(predicate, seconds=10):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.05)
    raise AssertionError("condition did not hold in attempt window")


def command(point, operation, status=200, result="completed"):
    code, text = request(
        CC,
        "POST",
        "/api/commands",
        json.dumps({"point": point, "operation": operation}),
    )
    body = json.loads(text)
    assert code == status, (code, body)
    if status == 200:
        assert set(body) == {
            "id",
            "point",
            "operation",
            "result",
            "reason",
            "selected_at",
            "executed_at",
            "con_at",
            "term_at",
            "feedback",
        }, body
        assert body["result"] == result, body
        assert body["point"] == point and body["operation"] == operation, body
        if result == "completed":
            assert (
                body["feedback"] is not None and body["con_at"] and body["term_at"]
            ), body
            assert (
                body["selected_at"]
                <= body["executed_at"]
                <= body["con_at"]
                <= body["feedback"]["at"]
                <= body["term_at"]
            )
    print(json.dumps(body), flush=True)
    return body


def command_write(point, operation, ip, address, value, **kw):
    before = len(fixture(ip)["writes"])
    start = time.monotonic()
    body = command(point, operation, **kw)
    writes = fixture(ip)["writes"]
    assert len(writes) == before + 1, (before, writes)
    write = writes[-1]
    assert (
        write["at"] >= start and write["address"] == address and write["value"] == value
    ), write
    return body


def raw_select(ioa, type_id, value, qualifier=0):
    """Minimal IEC104 peer for rejection tests; no alternate application API."""
    with socket.create_connection(("172.28.104.10", 2404), timeout=3) as sock:

        def exact(n):
            data = b""
            while len(data) < n:
                chunk = sock.recv(n - len(data))
                assert chunk
                data += chunk
            return data

        def frame():
            header = exact(2)
            assert header[0] == 104
            return exact(header[1])

        sock.sendall(bytes.fromhex("680407000000"))
        assert frame() == bytes.fromhex("0b000000")
        asdu = (
            bytes([type_id, 1, 6, 0, 1, 0])
            + ioa.to_bytes(3, "little")
            + bytes([128 | (qualifier << 2) | value])
        )
        sock.sendall(bytes([104, 4 + len(asdu), 0, 0, 0, 0]) + asdu)
        while True:
            data = frame()
            if data[0] & 1:
                continue
            received = (int.from_bytes(data[:2], "little") >> 1) + 1
            sock.sendall(bytes([104, 4, 1, 0]) + (received << 1).to_bytes(2, "little"))
            a = data[4:]
            if a[0] == type_id and int.from_bytes(a[6:9], "little") == ioa:
                assert a[2] & 64, a  # negative ACT-CON
                return


def main():
    action = sys.argv[1]
    if action == "drop":
        # Caller has just installed a real packet DROP on the RTU only.
        start = time.monotonic()
        until(lambda: pt(get_json(CC, "/api/state"), "rmu-1-position")["quality"]["nt"])
        time.sleep(2)
        s = get_json(CC, "/api/state")
        for ip, point in [
            (22, "rmu-2-position"),
            (23, "oltc-1-tap"),
            (24, "plc-1-running"),
        ]:
            polls = [t for t in fixture(ip)["polls"] if t >= start]
            assert (
                len(polls) >= 10
                and max(b - a for a, b in itertools.pairwise(polls)) <= 0.55
            ), polls
            assert good(pt(s, point)), pt(s, point)
        print("PASS DROP: every healthy peer's actual poll gaps <= poll_ms + 250 ms")
        return
    if action == "stale":
        # Fake Modbus device is paused; the live RTU reports source NT.
        until(lambda: pt(get_json(CC, "/api/state"), "oltc-1-tap")["quality"]["nt"])
        assert get_json(CC, "/api/state")["link"]["state"] == "started"
        command("oltc-1-step", "raise", status=409)
        print("PASS freshness refusal before select while IEC104 link still started")
        return

    # A quiet status well past its former 3 s age cutoff remains eligible.
    old = pt(get_json(CC, "/api/state"), "rmu-2-position")["received_at"]
    time.sleep(4)
    quiet = pt(get_json(CC, "/api/state"), "rmu-2-position")
    assert quiet["received_at"] == old and good(quiet), quiet
    command("rmu-2-command", "open")
    print("PASS quiet GOOD status remains command-eligible without a new GI")
    s = get_json(CC, "/api/state")
    assert s["schema"] == 3 and s["snapshot_seq"] > 0 and s["rendered_at"].endswith("Z")
    assert len(s["points"]) == 38 and all(good(p) for p in s["points"]), s
    assert pt(s, "oltc-1-tap")["value"] == {"value": 0, "transient": False}
    assert pt(s, "plc-1-setpoint-kv")["value"] == 20.4
    assert pt(s, "plc-1-deadband-pct")["value"] == 1
    assert pt(s, "plc-1-delay-s")["value"] == 30
    assert all(p["device"] and "limits" in p for p in s["points"])
    assert pt(s, "oltc-1-bus-kv")["limits"] == {"low": 19.8, "high": 21}
    fixture(23, "pf_invalid=true")
    until(lambda: pt(get_json(CC, "/api/state"), "oltc-1-pf")["quality"]["iv"])
    pf = pt(get_json(CC, "/api/state"), "oltc-1-pf")
    assert pf["value"] == 0 and not pf["quality"]["nt"]
    assert pf["stale_reason"] == "measurement_invalid", pf
    fixture(23, "pf_invalid=false")
    until(lambda: good(pt(get_json(CC, "/api/state"), "oltc-1-pf")))
    print("PASS schema-3 metadata, configured delay, NaN PF IV/no NT/zero and recovery")
    for point, op in [
        ("rmu-1-command", "raise"),
        ("oltc-1-step", "open"),
        ("oltc-1-mode", "lower"),
    ]:
        command(point, op, status=400)
    for ioa, ty, val in [(3101, 46, 1), (3301, 47, 2), (3302, 45, 1)]:
        raw_select(ioa, ty, val, qualifier=1)
    raw_select(3301, 46, 2)  # wrong type at a known command IOA
    print("PASS schema, FC3 scales, qualifier and command-type rejection")

    for point, ip in [("rmu-1-command", 21), ("rmu-2-command", 22)]:
        command_write(point, "close", ip, 0, 2)
        command_write(point, "open", ip, 0, 1)
    fixture(21, "lose_after_write=true")
    result = command_write("rmu-1-command", "open", 21, 0, 1, result="failed")
    assert result["con_at"] and result["term_at"] and result["feedback"] is None
    fixture(21, "lose_after_write=false&read_lost=false")
    until(lambda: good(pt(get_json(CC, "/api/state"), "rmu-1-position")))
    print("PASS cached matching DP without fresh readback cannot complete a no-op")
    command_write("oltc-1-mode", "manual", 23, 2, 1)
    with concurrent.futures.ThreadPoolExecutor() as pool:
        future = pool.submit(command_write, "oltc-1-step", "raise", 23, 0, 2)
        until(
            lambda: pt(get_json(CC, "/api/state"), "oltc-1-tap")["value"]["transient"]
        )
        assert not future.done()
        command("rmu-1-command", "open", status=409)
        raw_select(3201, 46, 2)  # RTU busy, not merely the CC HTTP mutex
        result = future.result()
    assert result["feedback"]["value"] == {"value": 1, "transient": False}, result
    result = command_write("oltc-1-step", "lower", 23, 0, 1)
    assert result["feedback"]["value"] == {"value": 0, "transient": False}
    print("PASS SBO RMU/SC/RC, moving VTI, typed feedback and global busy")

    fixture(23, "freeze=true")
    result = command_write("oltc-1-step", "raise", 23, 0, 2, result="failed")
    assert result["con_at"] and result["term_at"] and result["feedback"] is None, result
    fixture(23, "freeze=false")
    until(
        lambda: not pt(get_json(CC, "/api/state"), "oltc-1-tap")["value"]["transient"]
    )
    fixture(23, "write_mode=reject")
    result = command_write("oltc-1-step", "lower", 23, 0, 1, result="rejected")
    assert (
        result["con_at"] and result["term_at"] is None and result["feedback"] is None
    ), result
    fixture(23, "write_mode=lost_echo")
    result = command_write("oltc-1-step", "lower", 23, 0, 1, result="unknown")
    assert (
        result["con_at"] is None
        and result["term_at"] is None
        and result["feedback"] is None
    ), result
    fixture(23, "write_mode=ok")
    print(
        "PASS negative ACT-TERM, exception rejection and lost FC6 echo unknown/no retry"
    )

    fixture(23, "bad_vti=true")
    until(lambda: pt(get_json(CC, "/api/state"), "oltc-1-tap")["quality"]["iv"])
    before = len(fixture(23)["writes"])
    command("oltc-1-step", "raise", status=409)
    assert len(fixture(23)["writes"]) == before
    fixture(23, "bad_vti=false")
    until(lambda: good(pt(get_json(CC, "/api/state"), "oltc-1-tap")))
    for value in (-65, -64, 63, 64):
        fixture(23, f"tap={value}&target={value}")
        if -64 <= value <= 63:
            until(
                lambda value=value: (
                    good(pt(get_json(CC, "/api/state"), "oltc-1-tap"))
                    and pt(get_json(CC, "/api/state"), "oltc-1-tap")["value"]["value"]
                    == value
                )
            )
        else:
            until(lambda: pt(get_json(CC, "/api/state"), "oltc-1-tap")["quality"]["iv"])
    fixture(23, "tap=0&target=0")
    until(lambda: good(pt(get_json(CC, "/api/state"), "oltc-1-tap")))
    fixture(24, "heartbeat=false")
    until(
        lambda: all(
            p["stale_reason"] == "heartbeat" and p["quality"]["iv"]
            for p in get_json(CC, "/api/state")["points"]
            if p["id"].startswith("plc-")
        )
    )
    fixture(24, "heartbeat=true")
    until(lambda: good(pt(get_json(CC, "/api/state"), "plc-1-running")))
    print("PASS out-of-range VTI invalid/freshness refusal and frozen PLC heartbeat")

    until(lambda: get_json(CC, "/api/health")["historian"]["last_ok_at"] is not None)
    samples = get_json(HIST, "/samples")
    assert len(samples) >= 2
    assert all(x["state"]["schema"] == 3 for x in samples)
    [x["state"]["snapshot_seq"] for x in samples]
    # Prior regression checks deliberately restart CC, creating new epochs.
    current_epoch = get_json(CC, "/api/state")["cache_epoch"]
    current = [x for x in samples if x["state"]["cache_epoch"] == current_epoch]
    assert len(current) >= 2 and all(x["peer"] == "172.28.105.21" for x in current)
    assert all(
        b["state"]["snapshot_seq"] > a["state"]["snapshot_seq"]
        for a, b in itertools.pairwise(current)
    )
    before = get_json(CC, "/api/health")["historian"]["failures"]
    get_json(HIST, "/set?delay=2")
    until(lambda: get_json(CC, "/api/health")["historian"]["failures"] >= before + 2)
    # The slow historian cannot delay a real FC6/feedback command.
    command_write("oltc-1-mode", "auto", 23, 2, 2)
    get_json(HIST, "/set?delay=0")
    print(
        "PASS bound schema-3 historian POSTs, increasing snapshots and slow-ingest isolation"
    )


if __name__ == "__main__":
    main()
