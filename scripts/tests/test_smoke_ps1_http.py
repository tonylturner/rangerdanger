"""Loopback portal fixture proves sessions and browser-origin CLI upgrades in both twins."""
import base64
from contextlib import contextmanager
import hashlib
from http.client import HTTPConnection
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import subprocess
import sys
import threading
import unittest
from urllib.parse import urlsplit

from .test_setup_ps1_root import powershell
from .test_smoke_ps1_probe import CLI, GOOD_CHUNK, PCAP, RUNTIME_SCRIPT
from .test_containd_ui_prefix import ROOT

# A webpack runtime with no on-demand chunks: this fixture covers the HTTP session.
EMPTY_RUNTIME = 'r.u=e=>"static/chunks/"+e+"."+({})[e]+".js",r.p="/containd/_next/";'


@contextmanager
def portal(mode):
    errors = []
    seen = []
    closed = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *args):
            pass

        def reply(self, status, mime, body, **headers):
            data = body.encode()
            self.close_connection = True
            self.send_response(status)
            self.send_header("Content-Type", mime)
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Connection", "close")
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            seen.append(self.path)
            if self.path == CLI and self.headers.get("Upgrade", "").lower() == "websocket":
                if self.headers.get("Origin") != f"http://127.0.0.1:{self.server.server_port}":
                    errors.append("browser Origin lost")
                if self.headers.get("Authorization") != "Bearer lab-token":
                    self.reply(401, "application/json", '{"error":"unauthorized"}')
                    return
                # Model the upstream's Host/Origin rule; strip-host simulates nginx's old $host.
                host = self.headers.get("Host", "")
                if mode == "strip-host":
                    host = urlsplit("//" + host).hostname
                if host != urlsplit(self.headers.get("Origin", "")).netloc:
                    self.reply(403, "text/plain", "Forbidden\n")
                    return
                key = self.headers.get("Sec-WebSocket-Key", "")
                if (len(base64.b64decode(key)) != 16 or self.headers.get("Sec-WebSocket-Version") != "13"
                        or self.headers.get("Connection", "").lower() != "upgrade"):
                    errors.append("invalid websocket request")
                    self.reply(400, "text/plain", "Bad Request")
                    return
                if mode == "fallback":
                    self.reply(200, "text/html", "<html>UI fallback</html>")
                    return
                accept = base64.b64encode(hashlib.sha1(
                    (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")
                ).digest()).decode("ascii")
                self.send_response(101)
                self.send_header("Upgrade", "websocket")
                self.send_header("Connection", "Upgrade")
                self.send_header("Sec-WebSocket-Accept", "wrong" if mode == "bad-accept" else accept)
                self.end_headers()
                self.close_connection = True
                self.connection.settimeout(2)
                try:
                    if self.rfile.read(1):
                        errors.append("probe sent data after upgrade")
                    else:
                        closed.set()
                except OSError:
                    errors.append("probe did not close socket after upgrade")
            elif self.path == "/containd/":
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
        yield f"http://127.0.0.1:{server.server_port}", errors, seen, closed
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


class ContaindHttpTests(unittest.TestCase):
    def check_twin(self, twin):
        for mode in ("ok", "redirect", "auth", "strip-host", "fallback", "bad-accept"):
            with self.subTest(twin=twin, mode=mode), portal(mode) as (origin, errors, seen, closed):
                if twin == "powershell":
                    result = powershell(r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-ui-probe.ps1"
try { Invoke-ContaindUiProbe $env:ORIGIN; exit 0 }
catch { Write-Output "FAIL: $_"; exit 1 }
""", ORIGIN=origin)
                else:
                    result = subprocess.run(
                        [sys.executable, str(ROOT / "scripts/lib/containd-ui-probe.py"), origin],
                        capture_output=True, text=True, timeout=30,
                    )
                self.assertEqual(result.returncode, 0 if mode == "ok" else 1, result.stdout + result.stderr)
                self.assertNotIn("VoidTaskResult", result.stdout)
                if mode in ("ok", "bad-accept"):
                    self.assertTrue(closed.wait(3), "probe must close immediately after 101")
                if mode in ("strip-host", "fallback"):
                    status = "403" if mode == "strip-host" else "200"
                    version = "" if twin == "python" else "HTTP/1.1 "
                    self.assertIn(f"expected websocket 101, got {version}{status}",
                                  result.stdout + result.stderr)
                if mode == "bad-accept":
                    self.assertIn("invalid websocket upgrade headers", result.stdout + result.stderr)
                self.assertEqual(errors, [])
                self.assertEqual(seen, [
                    "/containd/", "/containd/_next/static/main.js", "/containd/_next/static/webpack.js",
                    "/containd/api/v1/auth/login",
                ] + ({"redirect": ["/containd/api/v1/auth/me"], "auth": []}.get(
                    mode, ["/containd/api/v1/auth/me", CLI, PCAP, CLI])))

    def test_native_powershell_session_and_browser_origin_upgrade(self):
        self.check_twin("powershell")

    def test_native_python_session_and_browser_origin_upgrade(self):
        self.check_twin("python")

    def test_fixture_rejects_host_without_origins_port(self):
        with portal("ok") as (origin, errors, seen, closed):
            url = urlsplit(origin)
            connection = HTTPConnection(url.hostname, url.port, timeout=3)
            try:
                connection.request("GET", CLI, headers={
                    "Host": url.hostname, "Origin": origin, "Authorization": "Bearer lab-token",
                    "Upgrade": "websocket", "Connection": "Upgrade",
                    "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
                })
                with connection.getresponse() as response:
                    self.assertEqual(response.status, 403)
            finally:
                connection.close()
            self.assertEqual(errors, [])
