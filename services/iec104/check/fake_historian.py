#!/usr/bin/env python3
"""Capture actual snapshot POSTs and source addresses; optionally stall replies."""

import http.server
import json
import threading
import time
import urllib.parse

lock = threading.Lock()
samples = []
delay = 0


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        assert self.path == "/api/ingest"
        state = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        with lock:
            samples.append(
                {"state": state, "peer": self.client_address[0], "at": time.monotonic()}
            )
            del samples[:-200]
            wait = delay
        time.sleep(wait)
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self):
        global delay
        args = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        with lock:
            if "delay" in args:
                delay = float(args["delay"][0])
            body = json.dumps(samples).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
