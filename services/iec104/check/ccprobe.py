#!/usr/bin/env python3
# RangerDanger IEC 60870-5-104 checks: control-centre HTTP API probe.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Runs inside a throwaway python container on the CC's management network
# (the CC API is not reachable from the host). stdlib only; `python3 -I`.
#
#   ccprobe.py BASE get PATH                 print "<status> <body>"
#   ccprobe.py BASE cmd open|close           POST /api/commands, print "<status> <body>"
#   ccprobe.py BASE wait SECONDS EXPR        poll until EXPR is true; print the last state
#   ccprobe.py BASE overlap DELAY_S          open, then close DELAY_S later; print both
#   ccprobe.py BASE badinput                 every 4xx case of the API; exit 1 on a mismatch
#
# EXPR is a Python expression over `s` (GET /api/state), `j` (GET
# /api/journal) and the helpers below, e.g. "all_good(s) and link(s)=='started'".
import http.client
import json
import socket
import sys
import threading
import time
import urllib.parse

POINTS = ["feeder-breaker-position", "feeder-protection-trip", "feeder-current", "feeder-voltage"]
FLOATS = ["feeder-current", "feeder-voltage"]


def request(base, method, path, body=None, headers=None):
    u = urllib.parse.urlparse(base)
    conn = http.client.HTTPConnection(u.hostname, u.port, timeout=20)
    hdrs = dict(headers or {})
    data = None
    if body is not None:
        data = body if isinstance(body, bytes) else body.encode()
        hdrs.setdefault("Content-Type", "application/json")
    conn.request(method, path, body=data, headers=hdrs)
    resp = conn.getresponse()
    text = resp.read().decode()
    conn.close()
    return resp.status, text


def raw(base, payload):
    """Sends raw bytes and returns the status code of the reply."""
    u = urllib.parse.urlparse(base)
    with socket.create_connection((u.hostname, u.port), timeout=10) as s:
        s.sendall(payload)
        s.shutdown(socket.SHUT_WR)
        buf = b""
        while True:
            chunk = s.recv(4096)
            if not chunk:
                break
            buf += chunk
    line = buf.split(b"\r\n", 1)[0].decode(errors="replace")
    return int(line.split()[1]) if line.startswith("HTTP/1.1 ") else -1


# ---- helpers for EXPR -------------------------------------------------
def pt(s, pid):
    return next(p for p in s["points"] if p["id"] == pid)


def link(s):
    return s["link"]["state"]


def good(p):
    return p["value"] is not None and p["stale_reason"] is None and not any(p["quality"].values())


def all_good(s):
    return all(good(pt(s, i)) for i in POINTS)


def all_stale(s, reason):
    return all(
        pt(s, i)["stale_reason"] == reason and pt(s, i)["quality"]["iv"] and pt(s, i)["quality"]["nt"] for i in POINTS
    )


def kinds(j):
    return [e["kind"] for e in j["entries"]]


def last(j, kind):
    found = [e for e in j["entries"] if e["kind"] == kind]
    return found[-1] if found else None


def get_json(base, path):
    status, text = request(base, "GET", path)
    if status != 200:
        raise RuntimeError(f"GET {path} -> {status} {text}")
    return json.loads(text)


def wait(base, seconds, expr):
    deadline = time.monotonic() + seconds
    s = j = None
    while True:
        try:
            s = get_json(base, "/api/state")
            j = get_json(base, "/api/journal?after=0")
            if eval(expr, {**globals(), "s": s, "j": j}):  # harness-authored expressions only
                print(json.dumps(s))
                return 0
        except (OSError, RuntimeError, KeyError, StopIteration) as e:
            print(f"probe: {e}", file=sys.stderr)
        if time.monotonic() >= deadline:
            print(json.dumps(s))
            print(f"probe: timed out after {seconds}s waiting for: {expr}", file=sys.stderr)
            return 1
        time.sleep(0.25)


def command(base, op):
    return request(base, "POST", "/api/commands", json.dumps({"point": "feeder-breaker-command", "operation": op}))


def overlap(base, delay):
    results = {}

    def first():
        results["first"] = command(base, "open")

    t = threading.Thread(target=first)
    t.start()
    time.sleep(delay)
    results["second"] = command(base, "close")
    t.join()
    for k in ("first", "second"):
        print(f"{k} {results[k][0]} {results[k][1]}")
    return 0


def badinput(base):
    cmd = "/api/commands"
    point = '"point":"feeder-breaker-command"'

    def post(body, path=cmd):
        return request(base, "POST", path, body)

    def get(path):
        return request(base, "GET", path)

    def rawc(payload):
        return raw(base, payload), ""

    cases = [
        ("bad JSON", lambda: post("{not json"), 400),
        ("array body", lambda: post("[]"), 400),
        ("empty body", lambda: post(""), 400),
        ("trailing junk", lambda: post('{%s,"operation":"open"} x' % point), 400),
        ("unknown field", lambda: post('{%s,"operation":"open","force":true}' % point), 400),
        ("duplicate field", lambda: post('{%s,"operation":"open","operation":"close"}' % point), 400),
        ("missing operation", lambda: post("{%s}" % point), 400),
        ("unknown point", lambda: post('{"point":"feeder-current","operation":"open"}'), 400),
        ("bad operation", lambda: post('{%s,"operation":"trip"}' % point), 400),
        ("non-string operation", lambda: post('{%s,"operation":1}' % point), 400),
        ("query on commands", lambda: post('{%s,"operation":"open"}' % point, cmd + "?x=1"), 400),
        ("journal after=abc", lambda: get("/api/journal?after=abc"), 400),
        ("journal other param", lambda: get("/api/journal?since=1"), 400),
        ("state with query", lambda: get("/api/state?x=1"), 400),
        ("GET commands", lambda: get(cmd), 405),
        ("POST state", lambda: post("{}", "/api/state"), 405),
        ("unknown path", lambda: get("/api/nope"), 404),
        ("body too large", lambda: post("x" * 5000), 413),
        ("POST without length", lambda: rawc(b"POST /api/commands HTTP/1.1\r\nHost: cc\r\n\r\n"), 411),
        (
            "chunked body",
            lambda: rawc(b"POST /api/commands HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"),
            501,
        ),
        ("HTTP/2.0 line", lambda: rawc(b"GET /api/state HTTP/2.0\r\n\r\n"), 505),
        ("garbage request", lambda: rawc(b"hello\r\n\r\n"), 400),
        ("header too large", lambda: rawc(b"GET /api/state HTTP/1.1\r\nX: " + b"a" * 9000 + b"\r\n\r\n"), 431),
    ]
    rc = 0
    for name, fn, want in cases:
        status, body = fn()
        ok = status == want
        if ok and body:
            ok = "error" in json.loads(body)
        print(f"{'ok  ' if ok else 'BAD '} {name}: {status} (want {want}) {body}")
        if not ok:
            rc = 1
    return rc


def main():
    base, op = sys.argv[1], sys.argv[2]
    args = sys.argv[3:]
    if op == "get":
        status, text = request(base, "GET", args[0])
        print(status, text)
        return 0
    if op == "cmd":
        status, text = command(base, args[0])
        print(status, text)
        return 0
    if op == "wait":
        return wait(base, float(args[0]), args[1])
    if op == "overlap":
        return overlap(base, float(args[0]))
    if op == "badinput":
        return badinput(base)
    print(f"unknown op {op}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
