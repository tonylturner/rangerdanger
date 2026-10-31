# SPDX-License-Identifier: Apache-2.0
"""Compose our existing Modbus fixture with a controlled GPS clock."""

import http.server
import json
import runpy
import threading

breaker = runpy.run_path("/check/fake_breaker.py")
clock_polls = 0
read_lost = threading.Event()
original_pdu = breaker["handle_pdu"]


def controlled_pdu(pdu):
    if read_lost.is_set() and pdu and pdu[0] in (3, 4):
        return None
    return original_pdu(pdu)


# Reuse the existing Modbus framing/register map, changing only our fixture's
# explicit fault mode. No production or IEC implementation is patched.
breaker["modbus_conn"].__globals__["handle_pdu"] = controlled_pdu


class Control(breaker["Control"]):
    def do_GET(self):
        if self.path in ("/read-loss?active=1", "/read-loss?active=0"):
            if self.path.endswith("=1"):
                read_lost.set()
            else:
                read_lost.clear()
            body = (str({"read_lost": read_lost.is_set()}) + "\n").encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            super().do_GET()


class Clock(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        global clock_polls
        if self.path == "/ready":
            with breaker["lock"]:
                data = dict(breaker["stats"], clock_polls=clock_polls)
        elif self.path == "/api/state":
            clock_polls += 1
            data = {"sync_status": "locked",
                    "reported_time": "2026-10-23T12:00:00.000Z"}
        else:
            self.send_error(404)
            return
        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    threading.Thread(target=breaker["serve_modbus"], args=("0.0.0.0", 502),
                     daemon=True).start()
    control = http.server.ThreadingHTTPServer(("0.0.0.0", 8081), Control)
    threading.Thread(target=control.serve_forever, daemon=True).start()
    print("FIXTURE READY", flush=True)
    http.server.ThreadingHTTPServer(("0.0.0.0", 8080), Clock).serve_forever()
