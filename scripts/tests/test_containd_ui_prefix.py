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
from unittest.mock import call, patch
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
# The served webpack runtime (with its CSS map); chunk 3888 has a named file, 6625 is the CLI console.
WEBPACK = (
    'r.u=e=>"static/chunks/"+(3888===e?"e868780c":e)+"."'
    '+({3888:"cd737c579891689c",6625:"caa6f620978d1981"})[e]+".js",'
    'r.miniCssF=e=>"static/css/"+({3786:"659eccb5db697b76"})[e]+".css",r.p="/containd/_next/",'
)
ON_DEMAND = (
    "/containd/_next/static/chunks/e868780c.cd737c579891689c.js",
    "/containd/_next/static/chunks/6625.caa6f620978d1981.js",
)
CLI_CHUNK = DOUBLE_PREFIXED[1].replace('"/containd/api/', '"/api/')
# Shared with the native PowerShell probe tests: runtime shapes and the failure they
# must give (None passes). Unknown shapes fail with an explicit live-inspection warning.
WEBPACK_CASES = (
    (WEBPACK, None),
    ('t.p="/elsewhere/",' + WEBPACK, None),
    (WEBPACK + 'r.p="/containd/_next/";', None),
    # The first ===-test for an id decides its name, as in the JavaScript conditional chain.
    (WEBPACK.replace('(3888===e?"e868780c":e)', '(3888===e?"e868780c":3888===e?"later":e)'), None),
    ("", "unsupported or ambiguous Next.js chunk-map shape"),
    (WEBPACK + WEBPACK, "unsupported or ambiguous Next.js chunk-map shape"),
    (WEBPACK.replace('r.p="/containd/_next/",', ""), "exactly one public path"),
    (WEBPACK + 'r.p="/other/";', "exactly one public path"),
    (WEBPACK.replace('"/containd/_next/"', '"/_next/"'), "on-demand chunk: /_next/static/chunks/"),
    (WEBPACK.replace("r.u=e=>", "r.u=function(e){return ").replace('".js",r.miniCssF', '".js"},r.miniCssF'),
     "unsupported or ambiguous Next.js chunk-map shape"),
    (WEBPACK.replace('6625:"caa6f620978d1981"', '6625:"../../x"'),
     "unsupported or ambiguous Next.js chunk-map shape"),
    (WEBPACK.replace('"e868780c":e', '"e868780c":t'),
     "unsupported or ambiguous Next.js chunk-map shape"),
)

# Route-page literals and the pages they name (shared with the PowerShell probe tests).
ROUTE_CASES = (
    ('href:"/containd/alerts/"', ["/containd/alerts/"]),
    ("x='/containd/system/services/dns/'", ["/containd/system/services/dns/"]),
    ('location.href="/containd/login?reason=logout";"/containd/login"', ["/containd/login/"]),
    ('"/containd/config/?tab=diff";"/containd/config#top"', ["/containd/config/"]),
    ('href:"/identities/";href:\'/a_b/c-d\'', ["/containd/a_b/c-d/", "/containd/identities/"]),
    ('href:"/";"/identities/";href:"/containd";href:"/containd/"', []),
    ('"/containd/";"/containd";"/containd/_next/static/media/";"/containd/api/v1/cli/ws"', []),
    ('"/containd/containd/alerts/";"/containd/docs/ics-dpi/";href:"/docs/"', []),
    ('"/containd/logo.svg";"/containd/a b/";"/containd/alerts/\';`/containd/alerts/`', []),
    # Code-point order, as Python sorts: a culture-aware sort would differ on both.
    ('"/containd/zones/";"/containd/Zones/";"/containd/ab/";"/containd/a-c/"',
     ["/containd/Zones/", "/containd/a-c/", "/containd/ab/", "/containd/zones/"]),
)
# A fake app-router site as a correct proxy serves it. The nav names three routes (one
# with a query) plus documentation, which is not crawled; the alerts page chunk names a
# fourth route, reachable only in the second round; the login page loads no scripts.
NAV = (
    'h=[{href:"/containd/alerts/"},{href:"/containd/identities/"},{href:"/"},{href:"/containd/docs/x/"}];'
    'location.href="/containd/login?reason=logout";'
)
ALERTS, IDENTITIES, USERS = (
    f"/containd/_next/static/chunks/app/{route}/page-{digest}.js"
    for route, digest in (("alerts", "a1"), ("identities", "b2"), ("system/users", "c3"))
)
ROUTE_PAGES = {
    "/containd/alerts/": f'<script src="{ALERTS}"></script><script src="{IDENTITIES}"></script>',
    "/containd/identities/": f'<script src="{IDENTITIES}"></script>',
    "/containd/login/": "<html>no scripts</html>",
    "/containd/system/users/": f'<script src="{USERS}"></script>',
}
ROUTE_CHUNKS = {
    ALERTS: 'n=(0,s.Sw)("/api/v1/alerts");r.push("/containd/system/users/");',
    IDENTITIES: '(0,s.Sw)("/api/v1/identities");',
    USERS: '(0,s.Sw)("/api/v1/users");',
}
# The crawl's requests after the on-demand chunks, round by round.
ROUTE_REQUESTS = [
    "/containd/alerts/", "/containd/identities/", "/containd/login/", ALERTS, IDENTITIES,
    "/containd/system/users/", USERS,
]
# A route page or route chunk the proxy serves wrongly, and the failure it must give.
ROUTE_FAILURES = (
    ("/containd/identities/", ("<html></html>", "text/plain", 200), "expected 200"),
    ("/containd/identities/", ("<html>not found</html>", "text/html", 404), "expected 200"),
    ("/containd/identities/", ('<script src="../assets/javascripts/bundle.js"></script>', "text/html", 200),
     "embedded script source: ../assets/javascripts/bundle.js"),
    ("/containd/system/users/", ('<script src="http://elsewhere/containd/_next/static/x.js"></script>',
                                 "text/html", 200), "embedded script source: http://elsewhere/"),
    (USERS, ("<html>fallback</html>", "text/html", 200), "expected 200"),
)
# Root-relative route links the proxy did not rewrite, and the literal the probe must name
# (None passes). Shared with the PowerShell probe tests.
UNREWRITTEN_CASES = (
    ('h=[{href:"/identities/"}]', 'href:"/identities/"'),
    ("{href:'/system/users'}", "href:'/system/users'"),
    ('<a class="x" href="/alerts/?tab=1">', 'href="/alerts/?tab=1"'),
    ('self.__next_f.push([1,"{\\"href\\":\\"/identities/\\"}"])', '\\"href\\":\\"/identities/\\"'),
    ('href:"/docs/"', 'href:"/docs/"'),
    ('href:"/containd/identities/";href:"/containd";href:"/";href:"/_next/static/x/"', None),
    ('href:"/api/v1/pcap/download/x";\\"href\\":\\"/containd/zones/\\"', None),
    ('href:"/logo.svg";href:"https://example.com/identities/";"/identities/";href:"/a b/"', None),
)
TOO_MANY_ROUTES = "".join(f'"/containd/r{index}/";' for index in range(probe.MAX_ROUTE_PAGES + 1))


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

    def test_route_redirects_and_rewrites_cover_the_served_route_links(self):
        for package in PACKAGES:
            with self.subTest(package=package):
                text = (ROOT / f"lab-definitions/packages/{package}/nginx.routes.conf").read_text()
                redirect = re.search(r"(?m)^\s*location ~ \^/\(([a-z|]+)\)\(/\.\*\)\?\$ \{$", text)
                self.assertIsNotNone(redirect)
                redirects = redirect.group(1).split("|")
                rewrites = [
                    source[2:] for source, target in filters(text)
                    if re.fullmatch(r'"/[a-z]+', source) and target == '"/containd/' + source[2:]
                ]
                self.assertEqual(redirects, sorted(redirects))
                self.assertEqual(rewrites, sorted(rewrites))
                self.assertIn("identities", redirects)
                # /assets is FUXA's at the portal root; /login has its own exact redirect.
                self.assertEqual(sorted(set(rewrites) - set(redirects)), ["assets", "login"])
                self.assertEqual(set(redirects) - set(rewrites), set())
                # The served nav links, rewritten by the actual directives, stay in the UI.
                nav = "".join(f'{{href:"/{route}/"}},' for route in rewrites)
                body = nav
                for source, target in filters(text):
                    body = body.replace(source, target)
                probe.check_links({"nav.js": body}, "chunk")
                missing = nav
                for source, target in filters(text):
                    if source != '"/identities':
                        missing = missing.replace(source, target)
                with self.assertRaisesRegex(probe.ProbeError, 'route link the proxy did not rewrite: href:"/identities/"'):
                    probe.check_links({"nav.js": missing}, "chunk")

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

    def test_ondemand_chunks_follow_the_runtime_chunk_map(self):
        self.assertEqual(probe.ondemand_chunks({"a.js": RUNTIME, "webpack.js": WEBPACK}), list(ON_DEMAND))
        self.assertEqual(
            probe.ondemand_chunks({"w.js": 'n.p="/p/",n.u=t=>"static/chunks/"+t+"."+({})[t]+".js"'}), [],
        )
        self.assertEqual(
            probe.ondemand_chunks({"w.js": 'n.p="/p/";n.u=t=>"static/chunks/"+t+"."+({1:"a",22:"b"})[t]+".js"'}),
            ["/p/static/chunks/1.a.js", "/p/static/chunks/22.b.js"],
        )
        with self.assertRaisesRegex(probe.ProbeError, "unsupported or ambiguous Next.js chunk-map shape"):
            probe.ondemand_chunks({"a.js": WEBPACK, "b.js": WEBPACK})

    def test_route_pages_from_path_literals(self):
        for body, pages in ROUTE_CASES:
            with self.subTest(body=body):
                self.assertEqual(probe.route_pages({"a.js": body}), pages)
        self.assertEqual(
            probe.route_pages({"a.js": 'href:"/identities/"', "b.js": '"/containd/identities/"'}),
            ["/containd/identities/"],
        )

    def test_unrewritten_route_links_fail_naming_the_literal(self):
        for body, literal in UNREWRITTEN_CASES:
            with self.subTest(body=body):
                chunks = {"shared.js": RUNTIME, "nav.js": body}
                if literal is None:
                    probe.check_chunks(chunks)
                else:
                    with self.assertRaisesRegex(
                        probe.ProbeError,
                        "^nav.js: chunk has a route link the proxy did not rewrite: " + re.escape(literal) + "$",
                    ):
                        probe.check_chunks(chunks)

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
                '<script src="/containd/_next/static/chunks/shared.js"></script>'
                '<script src="/containd/_next/static/chunks/webpack.js"></script>',
                "text/html", 200,
            ),
            "/containd/_next/static/chunks/shared.js": (RUNTIME, "application/javascript", 200),
            "/containd/_next/static/chunks/console.js": (SERVED, "text/javascript", 200),
            "/containd/_next/static/chunks/webpack.js": (WEBPACK, "application/javascript", 200),
            ON_DEMAND[0]: ("", "application/javascript", 200),
            ON_DEMAND[1]: (CLI_CHUNK, "application/javascript", 200),
            "/containd/api/v1/auth/login": (
                '{"token":"test-token","user":{"username":"containd"}}', "application/json", 200,
            ),
            "/containd/api/v1/auth/me": ('{"username":"containd"}', "application/json", 200),
            "/containd/api/v1/cli/ws/ticket": (
                '{"ticket":"fixture-ticket","expiresIn":30}', "application/json", 200,
            ),
            probe.API_ROUTES[0]: (
                '{"error":"engine pcap download status 404"}', "application/json", 400,
            ),
        }
        self.requests = []
        self.ticket_count = 0
        self.ticket_expires_in = 30
        self.ticket_missing = False

    def open(self, request, timeout):
        self.assertEqual(timeout, 15)
        self.assertTrue(request.full_url.startswith("http://127.0.0.1:8088/containd/"))
        path = request.full_url.removeprefix("http://127.0.0.1:8088")
        self.requests.append(request)
        if path == "/containd/api/v1/cli/ws/ticket":
            self.ticket_count += 1
            self.assertEqual(request.get_method(), "POST")
            self.assertIsNone(request.data)
            self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
            self.assertIsNone(request.get_header("Cookie"))
            body = {"expiresIn": self.ticket_expires_in}
            if not self.ticket_missing:
                body["ticket"] = f"fixture-ticket-{self.ticket_count}"
            return Response(json.dumps(body), "application/json")
        response = self.responses[path]
        if isinstance(response, Exception):
            raise response
        return Response(*response)

    def run_probe(self):
        self.ticket_count = 0
        self.requests = []
        output = io.StringIO()
        with patch.object(probe, "build_opener") as build, patch.object(probe, "cli_upgrade") as upgrade, \
                redirect_stdout(output):
            build.return_value.open.side_effect = self.open
            probe.probe("http://127.0.0.1:8088/")
            upgrade.assert_has_calls([
                call("http://127.0.0.1:8088", expected_status=401),
                call("http://127.0.0.1:8088", token="test-token", expected_status=401),
                call("http://127.0.0.1:8088", token="test-token", query_token=True, expected_status=401),
                call("http://127.0.0.1:8088", ticket="fixture-ticket-1"),
                call("http://127.0.0.1:8088", ticket="fixture-ticket-2"),
                call("http://127.0.0.1:8088", ticket="fixture-ticket-2", expected_status=401),
            ])
            self.assertEqual(upgrade.call_count, 6)
        return output.getvalue()

    def test_probes_all_chunks_then_logs_in_reads_me_and_checks_api_routes(self):
        output = self.run_probe()
        self.assertIn("5 chunks (2 on demand)", output)
        self.assertIn("/containd/api/v1/auth/me: 200 application/json", output)
        self.assertIn("400 from the containd API", output)
        self.assertEqual(len(self.requests), 11)
        self.assertEqual([req.full_url.removeprefix("http://127.0.0.1:8088") for req in self.requests[4:6]],
                         list(ON_DEMAND))
        login, me, ticket1, ticket2, pcap = self.requests[-5:]
        self.assertEqual(login.get_method(), "POST")
        self.assertEqual(json.loads(login.data), {"username": "containd", "password": "containd"})
        self.assertEqual(me.get_method(), "GET")
        self.assertEqual(me.full_url, "http://127.0.0.1:8088/containd/api/v1/auth/me")
        self.assertEqual(me.get_header("Authorization"), "Bearer test-token")
        for request in (ticket1, ticket2):
            self.assertEqual(request.get_method(), "POST")
            self.assertEqual(request.full_url, "http://127.0.0.1:8088/containd/api/v1/cli/ws/ticket")
            self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
            self.assertIsNone(request.get_header("Cookie"))
        self.assertEqual(pcap.get_method(), "GET")
        self.assertEqual(pcap.full_url, "http://127.0.0.1:8088" + probe.API_ROUTES[0])
        self.assertEqual(pcap.get_header("Authorization"), "Bearer test-token")
        self.assertTrue(all("/containd/containd/" not in req.full_url for req in self.requests))

    def test_ticket_issuance_requires_nonempty_ticket_and_exact_ttl(self):
        for missing, ttl in ((True, 30), (False, 29), (False, 31)):
            with self.subTest(missing=missing, ttl=ttl):
                self.ticket_missing = missing
                self.ticket_expires_in = ttl
                with self.assertRaisesRegex(probe.ProbeError, "expected an opaque ticket and expiresIn=30"):
                    self.run_probe()

    def test_bad_literal_only_in_an_on_demand_chunk_fails_before_login(self):
        # Regression: the CLI console chunk is not a page script; only the runtime map names it.
        self.responses[ON_DEMAND[1]] = (DOUBLE_PREFIXED[1], "application/javascript", 200)
        with self.assertRaisesRegex(
            probe.ProbeError,
            "6625.caa6f620978d1981.js: runtime-based API literal was rewritten: /containd/api/v1/cli/ws$",
        ):
            self.run_probe()
        self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_on_demand_chunk_must_be_served_as_javascript(self):
        self.responses[ON_DEMAND[0]] = ("<html>fallback</html>", "text/html", 200)
        with self.assertRaisesRegex(probe.ProbeError, "e868780c.cd737c579891689c.js: expected 200"):
            self.run_probe()

    def test_webpack_runtime_shapes(self):
        for body, message in WEBPACK_CASES:
            with self.subTest(body=body):
                self.requests = []
                self.responses["/containd/_next/static/chunks/webpack.js"] = (body, "text/javascript", 200)
                if message is None:
                    self.assertIn("5 chunks (2 on demand)", self.run_probe())
                else:
                    with self.assertRaisesRegex(probe.ProbeError, re.escape(message)):
                        self.run_probe()
                    self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def add_routes(self):
        self.responses["/containd/_next/static/chunks/console.js"] = (SERVED + NAV, "text/javascript", 200)
        for path, body in ROUTE_PAGES.items():
            self.responses[path] = (body, "text/html; charset=utf-8", 200)
        for path, body in ROUTE_CHUNKS.items():
            self.responses[path] = (body, "application/javascript", 200)

    def test_route_pages_named_by_the_bundle_are_crawled_round_by_round(self):
        self.add_routes()
        output = self.run_probe()
        self.assertIn("8 chunks (2 on demand), 4 route pages", output)
        paths = [req.full_url.removeprefix("http://127.0.0.1:8088") for req in self.requests]
        self.assertEqual(paths[4:6], list(ON_DEMAND))
        self.assertEqual(paths[6:-5], ROUTE_REQUESTS)
        self.assertTrue(all(req.get_method() == "GET" for req in self.requests[6:-5]))

    def test_bad_literal_only_in_a_route_page_chunk_fails_before_login(self):
        # Regression: a route's page chunk loads only from that route's HTML, here a
        # route that only another route's page chunk names.
        self.add_routes()
        self.responses[USERS] = (DOUBLE_PREFIXED[0], "application/javascript", 200)
        with self.assertRaisesRegex(
            probe.ProbeError,
            "app/system/users/page-c3.js: runtime-based API literal was rewritten: /containd/api/v1/pcap/download/$",
        ):
            self.run_probe()
        self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_served_nav_with_an_unrewritten_route_link_fails_naming_it(self):
        # Regression: the proxy rewrote no "/identities link, so the UI link left containd.
        self.add_routes()
        self.responses["/containd/_next/static/chunks/console.js"] = (
            SERVED + NAV.replace('"/containd/identities/"', '"/identities/"'), "text/javascript", 200,
        )
        with self.assertRaisesRegex(
            probe.ProbeError,
            '^/containd/_next/static/chunks/console.js: chunk has a route link the proxy did not rewrite: '
            'href:"/identities/"$',
        ):
            self.run_probe()
        self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_route_and_root_pages_with_an_unrewritten_route_link_fail(self):
        for path in ("/containd/", "/containd/identities/", "/containd/login/"):
            for link in ('<a href="/identities/">', '<script>self.__next_f.push([1,"{\\"href\\":\\"/zones/\\"}"])</script>'):
                with self.subTest(path=path, link=link):
                    self.setUp()
                    self.add_routes()
                    body, content_type, status = self.responses[path]
                    self.responses[path] = (body + link, content_type, status)
                    literal = re.search(r'href="/[^"]*"|\\"href\\":\\"/[^\\]*\\"', link).group(0)
                    with self.assertRaisesRegex(
                        probe.ProbeError,
                        f"^{re.escape(path)}: page has a route link the proxy did not rewrite: {re.escape(literal)}$",
                    ):
                        self.run_probe()
                    self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_route_pages_and_their_chunks_keep_the_fetch_and_path_guards(self):
        for path, response, message in ROUTE_FAILURES:
            with self.subTest(path=path, response=response):
                self.requests = []
                self.add_routes()
                self.responses[path] = response
                with self.assertRaisesRegex(probe.ProbeError, re.escape(message)):
                    self.run_probe()
                self.assertFalse(any(req.get_method() == "POST" for req in self.requests))

    def test_route_crawl_is_bounded(self):
        self.responses["/containd/_next/static/chunks/console.js"] = (SERVED + TOO_MANY_ROUTES, "text/javascript", 200)
        with self.assertRaisesRegex(probe.ProbeError, f"more than {probe.MAX_ROUTE_PAGES} route pages"):
            self.run_probe()
        self.assertFalse(any("/containd/r" in req.full_url for req in self.requests))

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
