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
API_FILTERS = {
    '"/api/v1/pcap/download/': '"/containd/api/v1/pcap/download/',
    '"/api/v1/cli/ws': '"/containd/api/v1/cli/ws',
}
RUNTIME = (
    'window.location.pathname.startsWith("/containd/")?"/containd":"";'
    'U("/api/v1/auth/me");'
)
BYPASSES = (
    'function q(e){return"/api/v1/pcap/download/".concat(e)};'
    'a.concat(window.location.host,"/api/v1/cli/ws");'
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
    if len(api) != len(API_FILTERS) or dict(api) != API_FILTERS:
        raise ValueError(f"only the two API-base bypass rewrites are allowed: {api}")


class RoutesTests(unittest.TestCase):
    def test_both_packages_allow_only_the_two_api_base_bypasses(self):
        for package in PACKAGES:
            with self.subTest(package=package):
                text = (ROOT / f"lab-definitions/packages/{package}/nginx.routes.conf").read_text()
                guard_routes(text)
                # Model nginx's literal substitutions using the actual directives.
                body = RUNTIME + BYPASSES
                for source, target in filters(text):
                    body = body.replace(source, target)
                self.assertIn(RUNTIME, body)
                self.assertIn('"/containd/api/v1/pcap/download/".concat', body)
                self.assertIn('window.location.host,"/containd/api/v1/cli/ws"', body)
                probe.check_chunks({"fixture.js": body})
                # Demonstrate that adding the old directive fails both defenses.
                broken = text + '\nsub_filter \'"/api/\' \'"/containd/api/\';\n'
                with self.assertRaises(ValueError):
                    guard_routes(broken)
                with self.assertRaises(probe.ProbeError):
                    probe.check_chunks({"fixture.js": body.replace('"/api/', '"/containd/api/')})

    def test_guard_rejects_broad_filter_with_alternate_whitespace_and_quotes(self):
        with self.assertRaises(ValueError):
            guard_routes('  sub_filter  "\\\"/api/"  "\\\"/containd/api/" ;')


class ChunkTests(unittest.TestCase):
    def test_unmodified_runtime_calls_and_narrow_rewrites_pass(self):
        probe.check_chunks({
            "shared.js": RUNTIME,
            "console.js": '"/containd/api/v1/cli/ws"',
            "pcap.js": '"/containd/api/v1/pcap/download/".concat(e)',
        })

    def test_every_chunk_is_checked_not_just_the_first(self):
        for body in (
            'U("/containd/api/v1/events")',
            "U('/containd/api/v1/auth/me')",
            '"/containd/api/v1/cli/ws/extra"',
            '"/containd/api/v1/pcap/download-not-allowed"',
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
            "/containd/_next/static/chunks/console.js": (
                '"/containd/api/v1/cli/ws"', "text/javascript", 200,
            ),
            "/containd/api/v1/auth/login": (
                '{"token":"test-token","user":{"username":"containd"}}', "application/json", 200,
            ),
            "/containd/api/v1/auth/me": ('{"username":"containd"}', "application/json", 200),
        }
        self.requests = []

    def open(self, request, timeout):
        self.assertEqual(timeout, 15)
        self.assertTrue(request.full_url.startswith("http://127.0.0.1:8088/containd/"))
        path = request.full_url.removeprefix("http://127.0.0.1:8088")
        self.requests.append(request)
        return Response(*self.responses[path])

    def run_probe(self):
        output = io.StringIO()
        with patch.object(probe, "build_opener") as build, redirect_stdout(output):
            build.return_value.open.side_effect = self.open
            probe.probe("http://127.0.0.1:8088/")
        return output.getvalue()

    def test_probes_all_chunks_then_logs_in_and_reads_me_as_json(self):
        output = self.run_probe()
        self.assertIn("2 chunks", output)
        self.assertIn("/containd/api/v1/auth/me: 200 application/json", output)
        self.assertEqual(len(self.requests), 5)
        login, me = self.requests[-2:]
        self.assertEqual(login.get_method(), "POST")
        self.assertEqual(json.loads(login.data), {"username": "containd", "password": "containd"})
        self.assertEqual(me.get_method(), "GET")
        self.assertEqual(me.get_header("Authorization"), "Bearer test-token")
        self.assertTrue(all("/containd/containd/" not in req.full_url for req in self.requests))

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
