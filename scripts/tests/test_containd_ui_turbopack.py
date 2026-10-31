"""Served-artifact Turbopack conformance tests; no live range or containd source."""

import io
import unittest
from urllib.error import HTTPError

from scripts.tests import test_containd_ui_prefix as prefix

probe = prefix.probe
FIXTURES = prefix.FIXTURES
PAGE = (FIXTURES / "page.js").read_text()
CONSOLE = (FIXTURES / "console.js").read_text()
STYLE = "/containd/_next/static/chunks/42x_0tvgvtycd.css"
LAZY = "/containd/_next/static/chunks/3lhospwwj0xgp.js"


class TurbopackTests(unittest.TestCase):
    def setUp(self):
        self.harness = prefix.ProbeTests()
        self.harness.setUp()
        self.responses = self.harness.responses
        self.responses["/containd/_next/static/chunks/console.js"] = (
            prefix.SERVED + PAGE, "text/javascript", 200,
        )
        self.responses[STYLE] = (".console{}", "text/css", 200)
        self.responses[LAZY] = (CONSOLE, "text/javascript", 200)

    def test_captured_arrays_include_css_and_transitive_javascript(self):
        chunks = {"runtime.js": prefix.TURBOPACK, "page.js": PAGE}
        self.assertEqual(probe.ondemand_chunks(chunks), [STYLE, LAZY])
        chunks["console.js"] = CONSOLE
        self.assertEqual(probe.ondemand_chunks(chunks), [STYLE, LAZY, *prefix.TURBO_PATHS])
        output = self.harness.run_probe()
        self.assertIn("6 chunks (3 on demand)", output)
        self.assertIn("1 on-demand stylesheets", output)

    def test_unknown_runtime_and_dependency_shapes_fail_before_login(self):
        for runtime, imports in (
            ("", PAGE),
            (prefix.WEBPACK, PAGE),
            (prefix.TURBOPACK * 2, PAGE),
            (prefix.TURBOPACK.replace("return M(e,t,r)", "return other(e,t,r)"), PAGE),
            (prefix.TURBOPACK.replace("return`${t}${r}${n}`", "return r"), PAGE),
            (prefix.TURBOPACK + 'TURBOPACK_CHUNK_BASE_PATH="/other/";', PAGE),
            (prefix.TURBOPACK, PAGE.replace(".l(t)", ".l(x)")),
            (prefix.TURBOPACK, PAGE.replace("Promise.all", "load")),
            (prefix.TURBOPACK, PAGE + 'load("static/chunks/unknown.js")'),
            (prefix.TURBOPACK, PAGE.replace("3lhospwwj0xgp.js", "../other.js")),
            (prefix.TURBOPACK, 'e.l(dynamicPath);'),
            (prefix.TURBOPACK, PAGE + 'e.l("static/"+name+".js");'),
        ):
            with self.subTest(runtime=runtime, imports=imports):
                self.responses["/containd/_next/static/chunks/turbopack.js"] = (
                    runtime, "text/javascript", 200,
                )
                self.responses["/containd/_next/static/chunks/console.js"] = (
                    imports, "text/javascript", 200,
                )
                with self.assertRaisesRegex(probe.ProbeError, "unsupported"):
                    self.harness.run_probe()
                self.assert_no_login()

    def assert_no_login(self):
        self.assertFalse(any(req.get_method() == "POST" for req in self.harness.requests))

    def test_missing_lazy_chunk_and_css_or_javascript_html_fallback_fail(self):
        for path in (STYLE, LAZY, prefix.TURBO_PATHS[1]):
            for response in (
                HTTPError("http://127.0.0.1:8088" + path, 404, "Not Found", {}, io.BytesIO()),
                ("<html>fallback</html>", "text/html", 200),
            ):
                with self.subTest(path=path, response=response):
                    self.setUp()
                    self.responses[path] = response
                    with self.assertRaises((HTTPError, probe.ProbeError)):
                        self.harness.run_probe()
                    if isinstance(response, HTTPError):
                        response.close()
                    self.assert_no_login()

    def test_route_only_lazy_imports_and_their_routes_are_crawled(self):
        self.harness.add_routes()
        self.responses[prefix.USERS] = (PAGE, "text/javascript", 200)
        self.responses[LAZY] = (CONSOLE + 'href:"/containd/console/";', "text/javascript", 200)
        self.responses["/containd/console/"] = ("<html>console</html>", "text/html", 200)
        output = self.harness.run_probe()
        self.assertIn("9 chunks (3 on demand), 5 route pages", output)
        self.assertIn("1 on-demand stylesheets", output)

    def test_unknown_transitive_or_route_only_dependency_cannot_be_skipped(self):
        for path in (LAZY, prefix.USERS):
            with self.subTest(path=path):
                self.setUp()
                if path == prefix.USERS:
                    self.harness.add_routes()
                self.responses[path] = ('load("static/chunks/new.js");', "text/javascript", 200)
                with self.assertRaisesRegex(probe.ProbeError, "unsupported Turbopack dependency"):
                    self.harness.run_probe()
                self.assert_no_login()

    def test_prefix_guard_and_chunk_bound_apply_to_lazy_dependencies(self):
        self.responses["/containd/_next/static/chunks/turbopack.js"] = (
            prefix.TURBOPACK.replace('"/containd/_next/"', '"/_next/"'),
            "text/javascript", 200,
        )
        with self.assertRaisesRegex(probe.ProbeError, "unexpected on-demand chunk"):
            self.harness.run_probe()
        self.setUp()
        with prefix.patch.object(probe, "MAX_CHUNKS", 3), self.assertRaisesRegex(
            probe.ProbeError, "more than 3 chunks",
        ):
            self.harness.run_probe()

    def test_duplicate_and_cyclic_imports_are_fetched_once(self):
        self.responses[LAZY] = (PAGE + CONSOLE + PAGE, "text/javascript", 200)
        self.assertIn("6 chunks (3 on demand)", self.harness.run_probe())
        paths = [request.full_url.removeprefix("http://127.0.0.1:8088") for request in self.harness.requests]
        self.assertEqual(paths.count(LAZY), 1)
        self.assertEqual(paths.count(STYLE), 1)

    def test_known_runtime_without_lazy_imports_is_verifiable(self):
        self.assertEqual(probe.ondemand_chunks({"runtime.js": prefix.TURBOPACK}), [])


if __name__ == "__main__":
    unittest.main()
