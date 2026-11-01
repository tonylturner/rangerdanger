"""Loopback portal fixture proves sessions and browser-origin CLI upgrades in both twins."""
import base64
import hashlib
import json
import subprocess
import sys
import threading
import unittest
from contextlib import contextmanager
from http.client import HTTPConnection
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

from .test_containd_ui_prefix import ROOT, TURBOPACK, probe
from .test_setup_ps1_root import powershell
from .test_smoke_ps1_probe import CLI, GOOD_CHUNK, PCAP, RUNTIME_SCRIPT, TICKET

UPLOAD = "/containd/api/v1/pcap/analyze"
UPLOAD_ERROR = {
    "code": "rangerdanger_proxy_upload_limit",
    "error": "RangerDanger proxy upload limit is 10 MB; this file is larger. "
             "The limit is set by RangerDanger, not containd.",
}


@contextmanager
def portal(mode):
    errors = []
    seen = []
    closed = threading.Event()
    issued = set()
    consumed = set()
    next_ticket = 0

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        server_version = "nginx"
        sys_version = ""

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

        def upload_refusal(self):
            body = dict(UPLOAD_ERROR)
            if mode == "bare-limit":
                self.reply(413, "text/html", "<html>413 Request Entity Too Large</html>")
            elif mode == "upstream-limit":
                self.reply(413, "application/json", '{"code":"body_too_large","error":"containd limit"}')
            else:
                if mode == "wrong-limit-code":
                    body["code"] = "other"
                elif mode == "wrong-limit-message":
                    body["error"] = "Upload too large"
                elif mode == "wrong-case-code":
                    body["Code"] = body.pop("code")
                elif mode == "array-limit":
                    body = [body]
                self.reply(413, "application/json", json.dumps(body))

        def handle_expect_100(self):
            if self.path == UPLOAD:
                size = int(self.headers["Content-Length"])
                threshold = (1 if mode == "small-limit" else 10) * 1024 * 1024
                if size > threshold and mode not in ("no-limit", "late-limit"):
                    seen.append(self.path)
                    self.check_upload_headers()
                    self.upload_refusal()
                    return False
            return super().handle_expect_100()

        def check_upload_headers(self):
            if self.headers.get("Authorization") or self.headers.get("Cookie"):
                errors.append("upload unexpectedly authenticated")
            if self.headers.get("Content-Type") != "multipart/form-data; boundary=rangerdanger-upload-probe":
                errors.append("upload not multipart")
            if self.headers.get("Expect") != "100-continue":
                errors.append("upload must respect early refusal")
            if self.headers.get("Accept-Encoding") != "identity":
                errors.append("upload response must not be compressed")
            if self.headers.get("Transfer-Encoding"):
                errors.append("upload must declare full length")

        def do_GET(self):
            seen.append(self.path)
            request = urlsplit(self.path)
            if request.path == CLI and self.headers.get("Upgrade", "").lower() == "websocket":
                if self.headers.get("Origin") != f"http://127.0.0.1:{self.server.server_port}":
                    errors.append("browser Origin lost")
                query = parse_qs(request.query)
                ticket = query.get("ticket", [""])[0]
                if (not ticket or query.get("token") or self.headers.get("Authorization")
                        or ticket not in issued or ticket in consumed):
                    self.reply(401, "application/json", '{"error":"ticket required"}')
                    return
                consumed.add(ticket)
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
            elif self.path == "/containd/_next/static/turbopack.js":
                self.reply(200, "application/javascript", TURBOPACK)
            elif request.path == PCAP:
                # A missing PCAP must remain an API refusal, not an HTML fallback.
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
            nonlocal next_ticket
            request = urlsplit(self.path)
            if request.path == UPLOAD:
                self.check_upload_headers()
                size = int(self.headers["Content-Length"])
                if size not in (2 * 1024 * 1024, 11 * 1024 * 1024):
                    errors.append(f"unexpected upload size {size}")
                # Consume in chunks; verify the generated multipart framing
                # without keeping an entire upload in memory or on disk.
                left, first, tail = size, b"", b""
                while left:
                    chunk = self.rfile.read(min(left, 65536))
                    if not chunk:
                        errors.append("incomplete upload body")
                        break
                    if not first:
                        first = chunk[:256]
                    tail = (tail + chunk)[-64:]
                    left -= len(chunk)
                if (not first.startswith(b"--rangerdanger-upload-probe\r\n")
                        or b'name="file"; filename="probe.pcap"' not in first
                        or not tail.endswith(b"\r\n--rangerdanger-upload-probe--\r\n")):
                    errors.append("invalid multipart framing")
                if mode == "late-limit" and size > 10 * 1024 * 1024:
                    self.upload_refusal()
                else:
                    self.reply(401, "application/json", '{"error":"unauthorized"}')
                return
            if request.path == TICKET:
                if self.headers.get("Authorization") != "Bearer lab-token":
                    errors.append("Bearer credential missing on ticket issuance")
                    self.reply(401, "application/json", '{"error":"unauthorized"}')
                    return
                if self.headers.get("Cookie"):
                    errors.append("ticket issuance unexpectedly used ambient cookie")
                next_ticket += 1
                ticket = f"fixture-ticket-{next_ticket}"
                issued.add(ticket)
                self.reply(200, "application/json", json.dumps({"ticket": ticket, "expiresIn": 30}))
                return
            if request.path != "/containd/api/v1/auth/login":
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
                        capture_output=True, text=True, timeout=30, check=False,
                    )
                success = mode in ("ok", "strip-host")
                self.assertEqual(result.returncode, 0 if success else 1, result.stdout + result.stderr)
                self.assertNotIn("VoidTaskResult", result.stdout)
                if mode in ("ok", "strip-host", "bad-accept"):
                    self.assertTrue(closed.wait(3), "probe must close immediately after 101")
                if mode == "fallback":
                    version = "" if twin == "python" else "HTTP/1.1 "
                    self.assertIn(f"expected HTTP 101, got {version}200",
                                  result.stdout + result.stderr)
                if mode == "bad-accept":
                    self.assertIn("invalid websocket upgrade headers", result.stdout + result.stderr)
                self.assertEqual(errors, [])
                common = [
                    "/containd/", "/containd/_next/static/main.js", "/containd/_next/static/turbopack.js",
                    "/containd/api/v1/auth/login",
                ]
                if mode == "redirect":
                    expected = common + ["/containd/api/v1/auth/me"]
                elif mode == "auth":
                    expected = common
                else:
                    expected = common + [
                        "/containd/api/v1/auth/me", CLI, CLI, CLI + "?token=lab-token",
                        TICKET, CLI + "?ticket=fixture-ticket-1",
                    ]
                    if mode not in ("fallback", "bad-accept"):
                        expected += [
                            TICKET, CLI + "?ticket=fixture-ticket-2",
                            CLI + "?ticket=fixture-ticket-2", PCAP,
                            UPLOAD, UPLOAD,
                        ]
                self.assertEqual(seen, expected)

    def test_native_powershell_session_and_browser_origin_upgrade(self):
        self.check_twin("powershell")

    def test_native_python_session_and_browser_origin_upgrade(self):
        self.check_twin("python")

    def check_upload_twin(self, twin):
        for mode, message in (
            ("ok", None),
            ("late-limit", None),
            ("small-limit", "2 MiB upload expected 401"),
            ("no-limit", "11 MiB upload expected 413"),
            ("bare-limit", "bare nginx proxy"),
            ("upstream-limit", "containd/API or non-RangerDanger response"),
            ("wrong-limit-code", "11 MiB upload expected 413"),
            ("wrong-limit-message", "11 MiB upload expected 413"),
            ("wrong-case-code", "11 MiB upload expected 413"),
            ("array-limit", "11 MiB upload expected 413"),
        ):
            with self.subTest(twin=twin, mode=mode), portal(mode) as (origin, errors, seen, _closed):
                if twin == "powershell":
                    result = powershell(r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-ui-probe.ps1"
try { Confirm-ContaindUploadLimits $env:ORIGIN; exit 0 }
catch { Write-Output "FAIL: $_"; exit 1 }
""", ORIGIN=origin)
                else:
                    result = subprocess.run(
                        [sys.executable, "-c",
                         "import sys; from scripts.tests.test_containd_ui_prefix import probe; "
                         "probe.check_upload_limits(sys.argv[1])", origin],
                        capture_output=True, text=True, timeout=30, check=False,
                    )
                output = result.stdout + result.stderr
                self.assertEqual(result.returncode, 0 if message is None else 1, output)
                if message:
                    self.assertIn(message, output)
                else:
                    self.assertIn("2 MiB upload -> HTTP 401", output)
                    self.assertIn("11 MiB upload -> HTTP 413 (RangerDanger nginx proxy)", output)
                self.assertEqual(seen, [UPLOAD] if mode == "small-limit" else [UPLOAD, UPLOAD])
                self.assertEqual(errors, [])

    def test_native_python_upload_limits(self):
        self.check_upload_twin("python")

    def test_native_powershell_upload_limits(self):
        self.check_upload_twin("powershell")

    def test_upload_contract_matches_the_package_json(self):
        self.assertEqual(probe.UPLOAD_LIMIT_CODE, UPLOAD_ERROR["code"])
        self.assertEqual(probe.UPLOAD_LIMIT_ERROR, UPLOAD_ERROR["error"])

    def test_upgrade_without_ticket_is_rejected_even_with_bearer(self):
        with portal("ok") as (origin, errors, _seen, _closed):
            url = urlsplit(origin)
            connection = HTTPConnection(url.hostname, url.port, timeout=3)
            try:
                connection.request("GET", CLI, headers={
                    "Host": url.hostname, "Origin": origin, "Authorization": "Bearer lab-token",
                    "Upgrade": "websocket", "Connection": "Upgrade",
                    "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
                })
                with connection.getresponse() as response:
                    self.assertEqual(response.status, 401)
            finally:
                connection.close()
            self.assertEqual(errors, [])
