"""Embedded containd assertions; native HTTP is mocked, never Docker/curl."""
import json
import re
import unittest

from .test_containd_ui_prefix import (
    AUTODETECT_CASES, CLI_CHUNK, DOUBLE_PREFIXED, NAV, ON_DEMAND, ROOT, ROUTE_CASES, ROUTE_CHUNKS,
    ROUTE_FAILURES, ROUTE_PAGES, ROUTE_REQUESTS, SERVED, TOO_MANY_ROUTES, UNREWRITTEN_CASES, USERS, WEBPACK,
    WEBPACK_CASES, probe,
)
from .test_setup_ps1_root import powershell


GOOD_CHUNK = (
    'window.location.pathname.startsWith("/containd/") ? "/containd" : "";'
    'fetch("/api/v1/auth/me");' + SERVED
)
CLI, PCAP = probe.API_ROUTES
RUNTIME_SCRIPT = '<script src="/containd/_next/static/webpack.js"></script>'

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
    if ($path -in @('/containd/api/v1/auth/me', $env:CLI, $env:PCAP) -and
        ($Headers.Authorization -cne 'Bearer lab-token' -or $Method)) { throw 'bearer lost' }
    $item = $fixture.PSObject.Properties[$path].Value
    if (-not $item) { throw "unexpected request $path" }
    if ($item.error) { throw $item.error }
    if ($item.raise) {
        $media = ([string]$item.type -split ';', 2)[0]
        if ($item.raise -eq 'legacy') {
            # Windows PowerShell 5.1 shape: WebException.Response is an HttpWebResponse.
            $exception = New-Object Exception 'The remote server returned an error.'
            $response = [pscustomobject]@{ StatusCode = [Net.HttpStatusCode][int]$item.status; ContentType = $item.type }
            $exception | Add-Member -NotePropertyName Response -NotePropertyValue $response
            throw $exception
        }
        $message = New-Object Net.Http.HttpResponseMessage ([Net.HttpStatusCode][int]$item.status)
        $message.Content = New-Object Net.Http.StringContent ([string]$item.body, [Text.Encoding]::UTF8, $media)
        throw (New-Object Microsoft.PowerShell.Commands.HttpResponseException 'Response status code does not indicate success.', $message)
    }
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
} catch {
    Write-Output "FAIL containd UI: $_"
    Write-Output ('REQUESTS=' + ($script:seen -join ','))
    exit 1
}
"""
ROUTES = r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-ui-probe.ps1"
foreach ($body in ($env:BODIES | ConvertFrom-Json)) {
    $chunks = New-Object Collections.Specialized.OrderedDictionary ([StringComparer]::Ordinal)
    $chunks['a.js'] = [string]$body
    Write-Output (ConvertTo-Json -Compress -InputObject @(Get-ContaindRoutePages $chunks))
}
"""


def with_routes(data):
    data["/containd/_next/static/main.js"]["body"] += NAV
    for path, body in ROUTE_PAGES.items():
        data[path] = dict(status=200, type="text/html; charset=utf-8", body=body)
    for path, body in ROUTE_CHUNKS.items():
        data[path] = dict(status=200, type="application/javascript", body=body)
    return data


def fixture():
    return {
        "/containd/": dict(status=200, type="text/html",
                           body='<script src="/containd/_next/static/main.js"></script>' + RUNTIME_SCRIPT),
        "/containd/_next/static/main.js": dict(status=200, type="application/javascript", body=GOOD_CHUNK),
        "/containd/_next/static/webpack.js": dict(status=200, type="application/javascript", body=WEBPACK),
        ON_DEMAND[0]: dict(status=200, type="application/javascript", body=""),
        ON_DEMAND[1]: dict(status=200, type="text/javascript; charset=utf-8", body=CLI_CHUNK),
        "/containd/api/v1/auth/login": dict(status=200, type="application/json", body='{"token":"lab-token"}'),
        "/containd/api/v1/auth/me": dict(status=200, type="application/json", body='{"username":"containd"}'),
        # Real Invoke-WebRequest throws on a 4xx; the API's live refusals.
        CLI: dict(status=400, type="text/plain; charset=utf-8", body="Bad Request", raise_="ps7"),
        PCAP: dict(status=400, type="application/json; charset=utf-8",
                   body='{"error":"engine pcap download status 404"}', raise_="ps7"),
    }


class ContaindProbeTests(unittest.TestCase):
    def run_probe(self, data, origin="http://localhost:8088/"):
        for item in data.values():
            if "raise_" in item:
                item["raise"] = item.pop("raise_")
        return powershell(PROBE, FIXTURE=json.dumps(data), ORIGIN=origin, CLI=CLI, PCAP=PCAP)

    def test_autodetect_regex_is_identical_to_python(self):
        text = (ROOT / "scripts/lib/containd-ui-probe.ps1").read_text()
        match = re.search(
            r"if \(\$body -cmatch '((?:''|[^'])*)'\) \{ \$autodetect = \$true \}", text,
        )
        self.assertIsNotNone(match)
        self.assertEqual(match.group(1).replace("''", "'"), probe.AUTODETECT.pattern)

    def test_webpack_runtime_regexes_are_identical_to_python(self):
        text = (ROOT / "scripts/lib/containd-ui-probe.ps1").read_text()
        for name, pattern in (("webpackChunks", probe.WEBPACK_CHUNKS), ("chunkName", probe.CHUNK_NAME),
                              ("chunkHash", probe.CHUNK_HASH), ("publicPath", probe.PUBLIC_PATH)):
            with self.subTest(name=name):
                match = re.search(rf"^    \${name} = '((?:''|[^'])*)'$", text, re.M)
                self.assertIsNotNone(match)
                self.assertEqual(match.group(1).replace("''", "'"), pattern.pattern)

    def test_route_patterns_are_identical_to_python(self):
        text = (ROOT / "scripts/lib/containd-ui-probe.ps1").read_text()
        for name, pattern in (("containdRoute", probe.CONTAIND_ROUTE), ("linkRoute", probe.LINK_ROUTE),
                              ("flightLink", probe.FLIGHT_LINK)):
            with self.subTest(name=name):
                match = re.search(rf"^    \${name} = '((?:''|[^'])*)'$", text, re.M)
                self.assertIsNotNone(match)
                self.assertEqual(match.group(1).replace("''", "'"), pattern.pattern)
        for name, names in (("notRoutes", probe.NOT_ROUTES), ("linkExempt", probe.LINK_EXEMPT)):
            with self.subTest(name=name):
                match = re.search(rf"^    \${name} = @\(((?:'[^']*'(?:, )?)*)\)$", text, re.M)
                self.assertIsNotNone(match)
                self.assertEqual(tuple(re.findall(r"'([^']*)'", match.group(1))), names)
        self.assertIn(f"$maxRoutePages = {probe.MAX_ROUTE_PAGES}\n", text)

    def test_route_pages_from_path_literals_match_python(self):
        result = powershell(ROUTES, BODIES=json.dumps([body for body, _ in ROUTE_CASES]))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual([json.loads(line) for line in result.stdout.splitlines()],
                         [pages for _, pages in ROUTE_CASES])

    def test_route_pages_are_crawled_in_python_order(self):
        result = self.run_probe(with_routes(fixture()))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("7 chunks (2 on demand), 4 route pages", result.stdout)
        self.assertIn(
            "REQUESTS=/containd/,/containd/_next/static/main.js,/containd/_next/static/webpack.js,"
            + ",".join(list(ON_DEMAND) + ROUTE_REQUESTS) + ",/containd/api/v1/auth/login,", result.stdout,
        )

    def test_bad_literal_only_in_a_route_page_chunk_fails_before_login(self):
        # Regression: a route's page chunk loads only from that route's HTML, here a
        # route that only another route's page chunk names.
        data = with_routes(fixture())
        data[USERS]["body"] = DOUBLE_PREFIXED[0]
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(
            "app/system/users/page-c3.js: runtime-based API literal was rewritten: /containd/api/v1/pcap/download/\n",
            result.stdout,
        )
        self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_unrewritten_route_links_match_python(self):
        for body, literal in UNREWRITTEN_CASES:
            with self.subTest(body=body):
                # The links name routes the probe crawls, so serve the fake route site too.
                data = with_routes(fixture())
                data["/containd/_next/static/main.js"]["body"] += body
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 0 if literal is None else 1, result.stdout + result.stderr)
                self.assertIn("7 chunks (2 on demand), 4 route pages" if literal is None else (
                    "FAIL containd UI: /containd/_next/static/main.js: chunk has a route link the proxy did not "
                    f"rewrite: {literal}\n"), result.stdout)

    def test_served_nav_with_an_unrewritten_route_link_fails_naming_it(self):
        # Regression: the proxy rewrote no "/identities link, so the UI link left containd.
        data = with_routes(fixture())
        data["/containd/_next/static/main.js"]["body"] = data["/containd/_next/static/main.js"]["body"].replace(
            '"/containd/identities/"', '"/identities/"')
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('/containd/_next/static/main.js: chunk has a route link the proxy did not rewrite: '
                      'href:"/identities/"\n', result.stdout)
        self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_route_and_root_pages_with_an_unrewritten_route_link_fail(self):
        for path in ("/containd/", "/containd/identities/", "/containd/login/"):
            for link, literal in (('<a href="/identities/">', 'href="/identities/"'),
                                  ('<script>self.__next_f.push([1,"{\\"href\\":\\"/zones/\\"}"])</script>',
                                   '\\"href\\":\\"/zones/\\"')):
                with self.subTest(path=path, link=link):
                    data = with_routes(fixture())
                    data[path]["body"] += link
                    result = self.run_probe(data)
                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                    self.assertIn(f"FAIL containd UI: {path}: page has a route link the proxy did not rewrite: "
                                  f"{literal}\n", result.stdout)
                    self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_route_pages_and_their_chunks_keep_the_fetch_and_path_guards(self):
        for path, (body, content_type, status), message in ROUTE_FAILURES:
            with self.subTest(path=path, body=body, status=status):
                data = with_routes(fixture())
                data[path] = dict(status=status, type=content_type, body=body)
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn(message, result.stdout)
                self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_route_crawl_is_bounded(self):
        data = fixture()
        data["/containd/_next/static/main.js"]["body"] += TOO_MANY_ROUTES
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(f"more than {probe.MAX_ROUTE_PAGES} route pages", result.stdout)
        self.assertNotIn("/containd/r", result.stdout.split("REQUESTS=")[-1])

    def test_webpack_runtime_shapes_match_python(self):
        for body, message in WEBPACK_CASES:
            with self.subTest(body=body):
                data = fixture()
                data["/containd/_next/static/webpack.js"]["body"] = body
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 0 if message is None else 1, result.stdout + result.stderr)
                self.assertIn("4 chunks (2 on demand)" if message is None else message, result.stdout)
                if message is not None:
                    self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_on_demand_chunks_are_fetched_in_map_order_after_page_chunks(self):
        result = self.run_probe(fixture())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(
            "REQUESTS=/containd/,/containd/_next/static/main.js,/containd/_next/static/webpack.js,"
            + ",".join(ON_DEMAND) + ",/containd/api/v1/auth/login", result.stdout,
        )

    def test_bad_literal_only_in_an_on_demand_chunk_fails_before_login(self):
        # Regression: the CLI console chunk is not a page script; only the runtime map names it.
        data = fixture()
        data[ON_DEMAND[1]]["body"] = DOUBLE_PREFIXED[1]
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(
            "6625.caa6f620978d1981.js: runtime-based API literal was rewritten: /containd/api/v1/cli/ws\n",
            result.stdout,
        )
        self.assertNotIn("/containd/api/v1/auth/login", result.stdout)

    def test_pathname_autodetect_shapes_match_python(self):
        for body, accepted in AUTODETECT_CASES:
            with self.subTest(body=body):
                data = fixture()
                data["/containd/_next/static/main.js"]["body"] = body + ';U("/api/v1/auth/me");'
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 0 if accepted else 1, result.stdout + result.stderr)
                self.assertIn("4 chunks" if accepted else "runtime API-base autodetect", result.stdout)

    def test_pass_session_bounds_mime_parameters_and_unique_chunks(self):
        data = fixture()
        data["/containd/"]["body"] = """
<!-- <script src="https://wrong.invalid/x.js"></script> -->
<div title='<script src="wrong.js">'>ignored</div>
<script>const text = '<script src="wrong.js">';</script>
<SCRIPT data-note=">" SRC="_next/static/main.js"></SCRIPT>
<script src="/containd/_next/static/main.js"></script>
""" + RUNTIME_SCRIPT
        data["/containd/_next/static/main.js"]["type"] = "text/javascript; charset=utf-8"
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("4 chunks (2 on demand)", result.stdout)
        self.assertIn("username=containd", result.stdout)
        self.assertIn("400 from the containd API, not the UI page", result.stdout)
        self.assertEqual(result.stdout.count("static/main.js"), 1)
        self.assertTrue(result.stdout.rstrip().endswith(f"/containd/api/v1/auth/me,{CLI},{PCAP}"), result.stdout)

    def test_bundle_assertions(self):
        for body, message in (
            (GOOD_CHUNK + '"/containd/containd/api/v1/auth/me"', "double /containd/"),
            (GOOD_CHUNK + '"/containd/api/v1/auth/me"', "runtime-based API literal"),
            (GOOD_CHUNK + '"/containd/api/v1/pcap/download"', "runtime-based API literal"),
            (GOOD_CHUNK + DOUBLE_PREFIXED[0], "runtime-based API literal"),
            (GOOD_CHUNK + DOUBLE_PREFIXED[1], "runtime-based API literal"),
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
                    '<script src="_next/static/second.js"></script>' + RUNTIME_SCRIPT
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
                self.assertIn("runtime-based API literal" if invalid else "5 chunks", result.stdout)

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
            "<script src=/containd/_next/static/main.js?a=1&amp;b=2></script>" + RUNTIME_SCRIPT
        )
        data["/containd/_next/static/main.js?a=1&b=2"] = data.pop("/containd/_next/static/main.js")
        result = self.run_probe(data)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("4 chunks (2 on demand)", result.stdout)

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

    def test_api_route_refusals_in_both_powershell_shapes_pass(self):
        for raise_ in ("ps7", "legacy", None):
            with self.subTest(raise_=raise_):
                data = fixture()
                for path in (CLI, PCAP):
                    data[path].pop("raise_")
                    if raise_:
                        data[path]["raise_"] = raise_
                result = self.run_probe(data)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("400 from the containd API", result.stdout)

    def test_api_routes_answered_by_the_ui_page_or_not_refused_fail(self):
        for path in (CLI, PCAP):
            for change, message in (
                (dict(status=200, type="text/html; charset=utf-8", raise_=None), "expected a 400 refusal"),
                (dict(type="text/html"), "expected a 400 refusal"),
                (dict(type="text/html", raise_="legacy"), "expected a 400 refusal"),
                (dict(status=401, type="application/json"), "expected a 400 refusal"),
                (dict(status=302, raise_=None), "expected a 400 refusal"),
                (dict(error="fixture timeout"), "fixture timeout"),
            ):
                with self.subTest(path=path, change=change):
                    data = fixture()
                    data[path].update(change)
                    if data[path]["raise_"] is None:
                        data[path].pop("raise_")
                    result = self.run_probe(data)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn(message, result.stdout)

    def test_each_fetch_rejects_status_mime_timeout_and_invalid_utf8(self):
        for path in (path for path in fixture() if path not in (CLI, PCAP)):
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
