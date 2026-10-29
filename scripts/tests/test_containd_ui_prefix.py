"""Offline route regression guard and embedded-UI probe tests; no live range."""

from contextlib import redirect_stdout
from email.message import Message
import importlib.util
import io
import json
from pathlib import Path
import re
import shlex
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "containd_ui_probe", ROOT / "scripts/lib/containd-ui-probe.py"
)
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)

PACKAGES = ("eu-iec104-substation", "us-dnp3-substation")
OLD_BYPASSES = (
    '\nsub_filter \'"/api/v1/pcap/download/\' \'"/containd/api/v1/pcap/download/\';\n',
    '\nsub_filter \'"/api/v1/cli/ws\' \'"/containd/api/v1/cli/ws\';\n',
    '\nsub_filter \'"/api/\' \'"/containd/api/\';\n',
)
RUNTIME = (
    'function a(e){return(null!=e?e:window.location.pathname).startsWith("/containd/")?"/containd":""};'
    'U("/api/v1/auth/me");'
)
# Shared with the native PowerShell probe tests to pin both accepted shapes
# and reject detections that do not derive the base from the pathname.
AUTODETECT_CASES = (
    ('window.location.pathname.startsWith("/containd/")?"/containd":""', True),
    (RUNTIME, True),
    ("( null != path_1$ ? path_1$ : window.location.pathname ).startsWith( '/containd/' ) ? '/containd' : ''", True),
    ('location.pathname.startsWith("/containd/")?"/containd":""', False),
    ('window.location.search.startsWith("/containd/")?"/containd":""', False),
    ('(null!=e?e:window.location.search).startsWith("/containd/")?"/containd":""', False),
    ('(null!=e?other:window.location.pathname).startsWith("/containd/")?"/containd":""', False),
    ('window.location.pathname; other.startsWith("/containd/")?"/containd":""', False),
    ('window.location.pathname.includes("/containd/")?"/containd":""', False),
    ('window.location.pathname.startsWith("/containd")?"/containd":""', False),
    ('(null!=e?e:window.location.pathname).startsWith("/containd/")?"/other":""', False),
    ('"/containd"; fetch("/api/v1/auth/me");', False),
)
# The served containd bundle passes these literals to its base helpers
# (Sw adds the /containd API base, Le the ws:// origin plus that base).
SERVED = (
    'function w(e){return(0,r.Sw)("/api/v1/pcap/download/".concat(encodeURIComponent(e)))};'
    't=(0,i.Le)("/api/v1/cli/ws",window.location.protocol,window.location.host);'
)
# What the removed proxy rewrites made of it: the base was added twice.
DOUBLE_PREFIXED = (
    'function w(e){return(0,r.Sw)("/containd/api/v1/pcap/download/".concat(encodeURIComponent(e)))};',
    't=(0,i.Le)("/containd/api/v1/cli/ws",window.location.protocol,window.location.host);',
)


def filters(text):
    return [
        shlex.split(arguments)
        for arguments in re.findall(r"(?m)^\s*sub_filter[ \t]+(.+);[ \t]*(?:#.*)?$", text)
    ]


def guard_routes(text):
    api = [
        (source, target) for source, target in filters(text)
        if source.lstrip("\"'").startswith("/api/")
    ]
    if api:
        raise ValueError(f"containd adds its API base at runtime; no API rewrite is allowed: {api}")


class RoutesTests(unittest.TestCase):
    def test_both_packages_leave_every_api_literal_to_the_runtime_base(self):
        for package in PACKAGES:
            with self.subTest(package=package):
                text = (ROOT / f"lab-definitions/packages/{package}/nginx.routes.conf").read_text()
                guard_routes(text)
                # Model nginx's literal substitutions using the actual directives.
                body = RUNTIME + SERVED
                for source, target in filters(text):
                    body = body.replace(source, target)
                self.assertEqual(body, RUNTIME + SERVED)
                probe.check_chunks({"fixture.js": body})
                # Re-adding a removed directive fails both defenses.
                for old in OLD_BYPASSES:
                    with self.subTest(old=old):
                        with self.assertRaises(ValueError):
                            guard_routes(text + old)
                        broken = body
                        for source, target in filters(old):
                            broken = broken.replace(source, target)
                        with self.assertRaisesRegex(probe.ProbeError, "runtime-based API literal"):
                            probe.check_chunks({"fixture.js": broken})

    def test_guard_rejects_broad_filter_with_alternate_whitespace_and_quotes(self):
        with self.assertRaises(ValueError):
            guard_routes('  sub_filter  "\\\"/api/"  "\\\"/containd/api/" ;')


class ChunkTests(unittest.TestCase):
    def test_pathname_autodetect_shapes(self):
        for body, accepted in AUTODETECT_CASES:
            with self.subTest(body=body):
                chunks = {"shared.js": body + ';U("/api/v1/auth/me");'}
                if accepted:
                    probe.check_chunks(chunks)
                else:
                    with self.assertRaisesRegex(probe.ProbeError, "runtime API-base autodetect"):
                        probe.check_chunks(chunks)

    def test_unprefixed_literals_for_the_runtime_base_pass(self):
        probe.check_chunks({"shared.js": RUNTIME, "console.js": SERVED})

    def test_served_bundle_with_prefixed_literal_for_the_base_helper_fails(self):
        # Regression: the CLI websocket and PCAP download resolved to /containd/containd/api/...
        for body in DOUBLE_PREFIXED:
            with self.subTest(body=body), self.assertRaisesRegex(
                probe.ProbeError, "runtime-based API literal was rewritten"
            ):
                probe.check_chunks({"shared.js": RUNTIME, "console.js": body})

    def test_every_chunk_is_checked_not_just_the_first(self):
        for body in (
            'U("/containd/api/v1/events")',
            "U('/containd/api/v1/auth/me')",
            '"/containd/api/v1/cli/ws"',
            '"/containd/api/v1/pcap/download/".concat(e)',
            '"/containd/containd/api/v1/auth/me"',
        ):
            with self.subTest(body=body), self.assertRaises(probe.ProbeError):
                probe.check_chunks({"shared.js": RUNTIME, "bad.js": body})

    def test_no_autodetect_or_no_auth_call_is_incomplete(self):
        for body in ("", 'U("/api/v1/auth/me")', RUNTIME.split(";")[0]):
            with self.subTest(body=body), self.assertRaises(probe.ProbeError):
                probe.check_chunks({"shared.js": body})


class Response(io.BytesIO):
    def __init__(self, body, content_type, status=200):
        super().__init__(body.encode())
        self.status = status
        self.headers = Message()
        self.headers["Content-Type"] = content_type


REFUSED = ("Bad Request", "text/plain; charset=utf-8", 400)


class ProbeTests(unittest.TestCase):
    def setUp(self):
        self.responses = {
            "/containd/": (
                '<script src="/containd/_next/static/chunks/shared.js"></script>'
                '<script src="/containd/_next/static/chunks/console.js"></script>'
                '<script src="/containd/_next/static/chunks/shared.js"></script>',
                "text/html", 200,
            ),
            "/containd/_next/static/chunks/shared.js": (RUNTIME, "application/javascript", 200),
            "/containd/_next/static/chunks/console.js": (SERVED, "text/javascript", 200),
            "/containd/api/v1/auth/login": (
                '{"token":"test-token","user":{"username":"containd"}}', "application/json", 200,
            ),
            "/containd/api/v1/auth/me": ('{"username":"containd"}', "application/json", 200),
            probe.API_ROUTES[0]: REFUSED,
            probe.API_ROUTES[1]: (
                '{"error":"engine pcap download status 404"}', "application/json", 400,
            ),
        }
        self.requests = []

    def open(self, request, timeout):
        self.assertEqual(timeout, 15)
        self.assertTrue(request.full_url.startswith("http://127.0.0.1:8088/containd/"))
        path = request.full_url.removeprefix("http://127.0.0.1:8088")
        self.requests.append(request)
        response = self.responses[path]
        if isinstance(response, Exception):
            raise response
        return Response(*response)

    def run_probe(self):
        output = io.StringIO()
        with patch.object(probe, "build_opener") as build, redirect_stdout(output):
            build.return_value.open.side_effect = self.open
            probe.probe("http://127.0.0.1:8088/")
        return output.getvalue()

    def test_probes_all_chunks_then_logs_in_reads_me_and_checks_api_routes(self):
        output = self.run_probe()
        self.assertIn("2 chunks", output)
        self.assertIn("/containd/api/v1/auth/me: 200 application/json", output)
        self.assertIn("400 from the containd API", output)
        self.assertEqual(len(self.requests), 7)
        login, me, cli, pcap = self.requests[-4:]
        self.assertEqual(login.get_method(), "POST")
        self.assertEqual(json.loads(login.data), {"username": "containd", "password": "containd"})
        for request, path in ((me, "/containd/api/v1/auth/me"), (cli, probe.API_ROUTES[0]),
                              (pcap, probe.API_ROUTES[1])):
            self.assertEqual(request.get_method(), "GET")
            self.assertEqual(request.full_url, "http://127.0.0.1:8088" + path)
            self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
        self.assertTrue(all("/containd/containd/" not in req.full_url for req in self.requests))

    def test_api_route_refusal_raised_by_urllib_passes(self):
        headers = Message()
        headers["Content-Type"] = "text/plain; charset=utf-8"
        self.responses[probe.API_ROUTES[0]] = HTTPError(
            "http://127.0.0.1:8088" + probe.API_ROUTES[0], 400, "Bad Request", headers,
            io.BytesIO(b"Bad Request"),
        )
        self.assertIn("400 from the containd API", self.run_probe())

    def test_api_routes_answered_by_the_ui_page_or_not_refused_fail(self):
        for path in probe.API_ROUTES:
            for response in (
                ("<html>containd</html>", "text/html; charset=utf-8", 200),
                ("<html>Bad Request</html>", "text/html", 400),
                ('{"error":"login required"}', "application/json", 401),
                ("[]", "application/json", 200),
                HTTPError("http://127.0.0.1:8088" + path, 302, "Found", Message(), io.BytesIO()),
            ):
                with self.subTest(path=path, response=response):
                    self.responses[path] = response
                    with self.assertRaisesRegex(probe.ProbeError, "expected a 400 refusal"):
                        self.run_probe()
            self.responses[path] = REFUSED

    def test_200_html_fallback_is_not_a_session_success(self):
        self.responses["/containd/api/v1/auth/me"] = ("<html>fallback</html>", "text/html", 200)
        with self.assertRaises(probe.ProbeError):
            self.run_probe()

    def test_unauthorized_json_is_not_a_session_success(self):
        self.responses["/containd/api/v1/auth/me"] = ('{"error":"unauthorized"}', "application/json", 401)
        with self.assertRaises(probe.ProbeError):
            self.run_probe()

    def test_missing_token_wrong_user_and_invalid_json_fail(self):
        for path, body, error in (
            ("/containd/api/v1/auth/login", '{"mfaRequired":true}', probe.ProbeError),
            ("/containd/api/v1/auth/me", '{"username":"other"}', probe.ProbeError),
            ("/containd/api/v1/auth/me", '<html>fallback</html>', json.JSONDecodeError),
        ):
            with self.subTest(body=body):
                original = self.responses[path]
                self.responses[path] = (body, "application/json", 200)
                with self.assertRaises(error):
                    self.run_probe()
                self.responses[path] = original

    def test_missing_scripts_and_unprefixed_or_external_scripts_fail(self):
        for page in (
            "<html>no chunks</html>",
            '<script src="/_next/static/chunks/shared.js"></script>',
            '<script src="http://elsewhere/containd/_next/static/chunks/shared.js"></script>',
        ):
            with self.subTest(page=page), self.assertRaises(probe.ProbeError):
                self.responses["/containd/"] = (page, "text/html", 200)
                self.run_probe()

    def test_chunk_html_fallback_and_rewritten_api_fail_before_login(self):
        path = "/containd/_next/static/chunks/shared.js"
        for body, content_type in (
            ("<html>fallback</html>", "text/html"),
            (RUNTIME.replace('"/api/', '"/containd/api/'), "application/javascript"),
        ):
            with self.subTest(body=body), self.assertRaises(probe.ProbeError):
                self.responses[path] = (body, content_type, 200)
                self.run_probe()
        self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_invalid_origins_never_make_requests(self):
        for base in ("file:///tmp/page", "http://localhost/containd", "http://localhost?query=1"):
            with self.subTest(base=base), patch.object(probe, "build_opener") as build:
                with self.assertRaises(probe.ProbeError):
                    probe.probe(base)
                build.assert_not_called()

    def test_cli_reports_transport_failure_and_nonzero_status(self):
        for error in (
            URLError("offline"),
            HTTPError("http://127.0.0.1:8088/containd/", 502, "Bad Gateway", {}, io.BytesIO()),
        ):
            with self.subTest(error=error), patch.object(probe, "probe", side_effect=error):
                with patch.object(probe.sys, "argv", ["probe"]), redirect_stdout(io.StringIO()):
                    with patch.object(probe.sys, "stderr", new_callable=io.StringIO) as stderr:
                        self.assertEqual(probe.main(), 1)
                        self.assertIn("FAIL containd UI:", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
