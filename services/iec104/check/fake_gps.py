#!/usr/bin/env python3
# RangerDanger IEC 60870-5-104 checks: a minimal fake GPS station clock.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: Apache-2.0
#
# Serves the subset of gps-sim's /api/state the RTU station clock reads:
# sync_status and an RFC3339Nano reported_time. A query param flips the
# status (GET /set?status=holdover) so the harness can exercise a
# not-locked clock without pausing the container. stdlib only; `python3 -I`.
import datetime
import http.server
import json
import socketserver
import sys
import threading

status = {"value": "locked"}
lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path.startswith("/set"):
            with lock:
                status["value"] = self.path.split("=", 1)[1] if "=" in self.path else "locked"
            self._json({"ok": True})
            return
        with lock:
            s = status["value"]
        now = datetime.datetime.now(datetime.timezone.utc)
        self._json(
            {
                "sync_status": s,
                "satellite_count": 9,
                "reported_time": now.strftime("%Y-%m-%dT%H:%M:%S.%f") + "Z",
                "actual_time": now.strftime("%Y-%m-%dT%H:%M:%S.%f") + "Z",
            }
        )

    def _json(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    host = sys.argv[1] if len(sys.argv) > 1 else "0.0.0.0"
    httpd = socketserver.ThreadingTCPServer((host, 8080), Handler)
    httpd.allow_reuse_address = True
    httpd.serve_forever()


if __name__ == "__main__":
    main()
