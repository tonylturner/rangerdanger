"""Loopback-only HTTP fixture proves real cookie/bearer and redirect behavior."""
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading
import unittest

from .test_setup_ps1_root import powershell
from .test_smoke_ps1_probe import CLI, GOOD_CHUNK, PCAP, RUNTIME_SCRIPT

# A webpack runtime with no on-demand chunks: this fixture covers the HTTP session.
EMPTY_RUNTIME = 'r.u=e=>"static/chunks/"+e+"."+({})[e]+".js",r.p="/containd/_next/";'


@contextmanager
def portal(mode):
    errors = []
    seen = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def reply(self, status, mime, body, **headers):
            data = body.encode()
            self.send_response(status)
            self.send_header("Content-Type", mime)
            self.send_header("Content-Length", str(len(data)))
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            seen.append(self.path)
            if self.path == "/containd/":
                self.reply(200, "text/html", '<script src="_next/static/main.js"></script>' + RUNTIME_SCRIPT,
                           **{"Set-Cookie": "session=fixture; Path=/containd/; HttpOnly"})
            elif self.path == "/containd/_next/static/main.js":
                self.reply(200, "application/javascript", GOOD_CHUNK)
            elif self.path == "/containd/_next/static/webpack.js":
                self.reply(200, "application/javascript", EMPTY_RUNTIME)
            elif self.path in (CLI, PCAP):
                # The containd API refuses a plain GET to the websocket and a missing capture.
                if self.headers.get("Authorization") != "Bearer lab-token":
                    errors.append(f"bearer missing on {self.path}")
                self.reply(400, "application/json", '{"error":"bad request"}')
            elif self.path == "/containd/api/v1/auth/me":
                if self.headers.get("Cookie") != "session=fixture":
                    errors.append("cookie missing on me")
                if self.headers.get("Authorization") != "Bearer lab-token":
                    errors.append("bearer missing on me")
                if mode == "redirect":
                    self.reply(302, "application/json", "{}", Location="/login")
                else:
                    self.reply(200, "application/json", '{"username":"containd"}')
            elif self.path == "/login":
                errors.append("redirect was followed")
                self.reply(200, "application/json", '{"username":"containd"}')
            else:
                errors.append(f"unexpected path {self.path}")
                self.reply(404, "text/plain", "not found")

        def do_POST(self):
            seen.append(self.path)
            if self.path != "/containd/api/v1/auth/login":
                errors.append(f"unexpected POST {self.path}")
            if self.headers.get("Cookie") != "session=fixture":
                errors.append("cookie missing on login")
            credentials = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if credentials != {"username": "containd", "password": "containd"}:
                errors.append("wrong credentials")
            self.reply(401 if mode == "auth" else 200, "application/json", '{"token":"lab-token"}')

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}", errors, seen
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


class ContaindHttpTests(unittest.TestCase):
    def test_native_http_session_and_rejected_redirect_or_auth_failure(self):
        for mode in ("ok", "redirect", "auth"):
            with self.subTest(mode=mode), portal(mode) as (origin, errors, seen):
                result = powershell(r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-ui-probe.ps1"
try { Invoke-ContaindUiProbe $env:ORIGIN; exit 0 }
catch { Write-Output "FAIL: $_"; exit 1 }
""", ORIGIN=origin)
                self.assertEqual(result.returncode, 0 if mode == "ok" else 1, result.stdout + result.stderr)
                self.assertEqual(errors, [])
                self.assertEqual(seen, [
                    "/containd/", "/containd/_next/static/main.js", "/containd/_next/static/webpack.js",
                    "/containd/api/v1/auth/login",
                ] + {"ok": ["/containd/api/v1/auth/me", CLI, PCAP],
                     "redirect": ["/containd/api/v1/auth/me"], "auth": []}[mode])
