"""Embedded containd assertions; native HTTP is mocked, never Docker/curl."""
import json
import unittest

from .test_setup_ps1_root import powershell


GOOD_CHUNK = (
    'window.location.pathname.startsWith("/containd/") ? "/containd" : "";'
    'fetch("/api/v1/auth/me"); "/containd/api/v1/cli/ws";'
    '"/containd/api/v1/pcap/download/capture";'
)

PROBE = r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-ui-probe.ps1"
$fixture = $env:FIXTURE | ConvertFrom-Json
$script:session = $null
$script:seen = @()
function Invoke-WebRequest {
    param($Uri, $Headers, $WebSession, $TimeoutSec, $UseBasicParsing,
          $MaximumRedirection, $ErrorAction, $Method, $Body, $ContentType)
    if ($TimeoutSec -ne 15 -or -not $UseBasicParsing -or $MaximumRedirection -ne 0 -or
        $Headers['Accept-Encoding'] -ne 'identity' -or $ErrorAction -ne 'Stop') { throw 'request bounds lost' }
    if ($script:session -and $script:session -ne $WebSession) { throw 'cookie session lost' }
    $script:session = $WebSession
    $path = ([uri]$Uri).PathAndQuery
    $script:seen += $path
    if ($path -eq '/containd/api/v1/auth/login') {
        $credentials = $Body | ConvertFrom-Json
        if ($Method -ne 'POST' -or $ContentType -ne 'application/json' -or
            $credentials.username -cne 'containd' -or $credentials.password -cne 'containd') { throw 'login lost' }
    }
    if ($path -eq '/containd/api/v1/auth/me' -and $Headers.Authorization -cne 'Bearer lab-token') { throw 'bearer lost' }
    $item = $fixture.PSObject.Properties[$path].Value
    if (-not $item) { throw "unexpected request $path" }
    if ($item.error) { throw $item.error }
    $bytes = [Text.Encoding]::UTF8.GetBytes([string]$item.body)
    if ($item.invalidUtf8) { $bytes = [byte[]]@(255) }
    $stream = New-Object IO.MemoryStream
    $stream.Write($bytes, 0, $bytes.Length)
    [pscustomobject]@{ StatusCode=$item.status; Headers=@{'Content-Type'=$item.type}; RawContentStream=$stream }
}
try {
    Invoke-ContaindUiProbe $env:ORIGIN
    Write-Output ('REQUESTS=' + ($script:seen -join ','))
    exit 0
} catch { Write-Output "FAIL containd UI: $_"; exit 1 }
"""


def fixture():
    return {
        "/containd/": dict(status=200, type="text/html", body='<script src="/containd/_next/static/main.js"></script>'),
        "/containd/_next/static/main.js": dict(status=200, type="application/javascript", body=GOOD_CHUNK),
        "/containd/api/v1/auth/login": dict(status=200, type="application/json", body='{"token":"lab-token"}'),
        "/containd/api/v1/auth/me": dict(status=200, type="application/json", body='{"username":"containd"}'),
    }


class ContaindProbeTests(unittest.TestCase):
    def run_probe(self, data, origin="http://localhost:8088/"):
        return powershell(PROBE, FIXTURE=json.dumps(data), ORIGIN=origin)

    def test_pass_session_bounds_mime_parameters_and_unique_chunks(self):
        data = fixture()
        data["/containd/"]["body"] = """
<!-- <script src="https://wrong.invalid/x.js"></script> -->
<div title='<script src="wrong.js">'>ignored</div>
<script>const text = '<script src="wrong.js">';</script>
<SCRIPT data-note=">" SRC="_next/static/main.js"></SCRIPT>
<script src="/containd/_next/static/main.js"></script>
"""
        data["/containd/_next/static/main.js"]["type"] = "text/javascript; charset=utf-8"
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 chunks", result.stdout)
        self.assertIn("username=containd", result.stdout)
        self.assertEqual(result.stdout.count("static/main.js"), 1)

    def test_bundle_assertions(self):
        for body, message in (
            (GOOD_CHUNK + '"/containd/containd/api/v1/auth/me"', "double /containd/"),
            (GOOD_CHUNK + '"/containd/api/v1/auth/me"', "runtime-based API literal"),
            (GOOD_CHUNK + '"/containd/api/v1/pcap/download"', "runtime-based API literal"),
            ('fetch("/api/v1/auth/me")', "runtime API-base autodetect"),
            ('window.location.pathname.startsWith("/containd/") ? "/containd" : ""', "unprefixed auth/me"),
            (GOOD_CHUNK.upper(), "runtime API-base autodetect"),
        ):
            with self.subTest(message=message, body=body):
                data = fixture()
                data["/containd/_next/static/main.js"]["body"] = body
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(message, result.stdout)

    def test_all_chunks_are_checked_and_required_calls_can_span_chunks(self):
        for invalid in (False, True):
            with self.subTest(invalid=invalid):
                data = fixture()
                data["/containd/"]["body"] = (
                    '<script src="_next/static/main.js"/>'
                    '<script src="_next/static/second.js"></script>'
                )
                data["/containd/_next/static/main.js"]["body"] = (
                    'window.location.pathname.startsWith("/containd/") ? "/containd" : ""'
                )
                data["/containd/_next/static/second.js"] = dict(
                    status=200, type="application/javascript",
                    body='"/api/v1/auth/me";' + ('"/containd/api/v1/other";' if invalid else ""),
                )
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 1 if invalid else 0, result.stdout + result.stderr)
                self.assertIn("runtime-based API literal" if invalid else "2 chunks", result.stdout)

    def test_missing_bundle_and_script_origin_path_extension(self):
        for source in (None, "https://elsewhere.invalid/containd/_next/static/x.js",
                       "//elsewhere.invalid/containd/_next/static/x.js",
                       "http://LOCALHOST:8088/containd/_next/static/main.js",
                       "/_next/static/x.js", "/containd/_next/static/x.css",
                       "/containd/_next/static/x.JS"):
            with self.subTest(source=source):
                data = fixture()
                data["/containd/"]["body"] = "" if source is None else f'<script src="{source}"></script>'
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("no JavaScript chunks" if source is None else "Unexpected embedded script", result.stdout)

    def test_html_entities_queries_and_single_quoted_unquoted_attributes(self):
        data = fixture()
        data["/containd/"]["body"] = (
            "<script src='_next/static/main.js?a=1&amp;b=2'></script>"
            "<script src=/containd/_next/static/main.js?a=1&amp;b=2></script>"
        )
        data["/containd/_next/static/main.js?a=1&b=2"] = data.pop("/containd/_next/static/main.js")
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("1 chunks", result.stdout)

    def test_auth_failure_token_and_identity_types_json(self):
        for path, bodies, message in (
            ("/containd/api/v1/auth/login", ('{}', '{"token":""}', '{"token":3}', '[]', 'null',
                                          '[{"token":"lab-token"}]', '{"Token":"lab-token"}',
                                          '{"token":["lab-token"]}'), "no session token"),
            ("/containd/api/v1/auth/me", ('{}', '{"username":"other"}', '{"username":"CONTAIND"}', '[]', 'null',
                                       '[{"username":"containd"}]', '{"Username":"containd"}',
                                       '{"username":["containd"]}'), "logged-in lab user"),
            ("/containd/api/v1/auth/login", ('not JSON',), "FAIL containd UI"),
        ):
            for body in bodies:
                with self.subTest(path=path, body=body):
                    data = fixture()
                    data[path]["body"] = body
                    result = self.run_probe(data)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn(message, result.stdout)

    def test_each_fetch_rejects_status_mime_timeout_and_invalid_utf8(self):
        for path in fixture():
            for change, message in (
                (dict(status=302), "expected 200"),
                (dict(status=401), "expected 200"),
                (dict(type="text/plain"), "expected 200"),
                (dict(error="fixture timeout"), "fixture timeout"),
                (dict(invalidUtf8=True), "FAIL containd UI"),
            ):
                with self.subTest(path=path, change=change):
                    data = fixture()
                    data[path].update(change)
                    result = self.run_probe(data)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn(message, result.stdout)

    def test_origin_must_not_have_path_credentials_query_or_fragment(self):
        for origin in ("file:///tmp/x", "http://localhost/path", "http://a:b@localhost",
                       "http://localhost?q=1", "http://localhost#x", "localhost:8088"):
            with self.subTest(origin=origin):
                result = self.run_probe(fixture(), origin)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn("Supply a portal origin", result.stdout)
