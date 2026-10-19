"""Offline guards for the proxy routes that repair links baked into the v0.1.35 images.

proxy/nginx.conf is bind-mounted and restarted by apply-repo-update, so it is the
only fix path for the Network Map's firewall and OpenPLC links and for containd's
root-relative icons. No nginx binary is assumed: the config is tokenised here and
nginx's location selection (exact, longest prefix, first regex) is modelled.
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
NGINX_CONFIG = ROOT / "proxy/nginx.conf"
CONTAIND_UPSTREAM = "http://firewall_ui/"
OPENPLC_LEGACY = "/api/labs/instances/workshop/nodes/openplc-1/ui/"


def tokenize(text):
    tokens, index, length = [], 0, len(text)
    while index < length:
        char = text[index]
        if char.isspace():
            index += 1
        elif char == "#":
            index = text.find("\n", index)
            index = length if index < 0 else index
        elif char in "{};":
            tokens.append(char)
            index += 1
        elif char in "\"'":
            end = index + 1
            while text[end] != char:
                end += 2 if text[end] == "\\" else 1
            tokens.append(text[index + 1 : end])
            index = end + 1
        else:
            end = index
            while end < length and not text[end].isspace() and text[end] not in "{};":
                end += 1
            tokens.append(text[index:end])
            index = end
    return tokens


def parse(tokens):
    """Return a list of (name, args, children) directives; children is None for leaves."""
    directives = []
    stack = [directives]
    current = []
    for token in tokens:
        if token == ";":
            stack[-1].append((current[0], current[1:], None))
            current = []
        elif token == "{":
            children = []
            stack[-1].append((current[0], current[1:], children))
            stack.append(children)
            current = []
        elif token == "}":
            if current or len(stack) == 1:
                raise ValueError("unbalanced block in nginx.conf")
            stack.pop()
        else:
            current.append(token)
    if current or len(stack) != 1:
        raise ValueError("unterminated directive or block in nginx.conf")
    return directives


def find(directives, name):
    return [directive for directive in directives if directive[0] == name]


class Location:
    def __init__(self, args, children):
        self.modifier = args[0] if len(args) == 2 else ""
        self.pattern = args[-1]
        self.children = children

    def matches(self, path):
        if self.modifier == "=":
            return path == self.pattern
        if self.modifier in ("~", "~*"):
            return re.search(self.pattern, path, re.I if self.modifier == "~*" else 0) is not None
        return path.startswith(self.pattern)

    def directive(self, name):
        found = find(self.children, name)
        return found[0][1] if found else None

    def resolve(self, path, args=""):
        """Model the response for ``path``: ('redirect', target) or ('proxy', upstream_url)."""
        rewrite = self.directive("rewrite")
        if rewrite:
            regex, replacement, flag = rewrite
            if flag != "redirect":
                raise ValueError(f"unexpected rewrite flag {flag} in {self.pattern}")
            target = re.sub(regex, replacement.replace("$1", r"\1"), path)
            return "redirect", target + (f"?{args}" if args else "")
        returned = self.directive("return")
        if returned and returned[0] == "302":
            target = returned[1].replace("$is_args$args", f"?{args}" if args else "")
            return "redirect", target
        upstream = self.directive("proxy_pass")[0]
        suffix = path[len(self.pattern):] if upstream.endswith("/") else path
        return "proxy", upstream + suffix


def load_server():
    http = find(parse(tokenize(NGINX_CONFIG.read_text())), "http")[0][2]
    return find(http, "server")[0][2]


def select(locations, path):
    """nginx: exact match, else longest prefix (^~ wins), else first regex, else prefix."""
    prefixes = [loc for loc in locations if loc.modifier in ("", "^~") and loc.matches(path)]
    best = max(prefixes, key=lambda loc: len(loc.pattern), default=None)
    for loc in locations:
        if loc.modifier == "=" and loc.matches(path):
            return loc
    if best is not None and best.modifier == "^~":
        return best
    for loc in locations:
        if loc.modifier in ("~", "~*") and loc.matches(path):
            return loc
    return best


class ProxyRoutesTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = load_server()
        cls.locations = [Location(args, children) for _, args, children in find(cls.server, "location")]

    def route(self, path, args=""):
        return select(self.locations, path).resolve(path, args)

    def test_config_parses_with_balanced_blocks_and_relative_redirects(self):
        self.assertIn(("absolute_redirect", ["off"], None), self.server)
        self.assertEqual(find(self.server, "listen")[0][1], ["8080"])

    def test_legacy_firewall_path_redirects_into_containd_with_subpath_and_query(self):
        self.assertEqual(self.route("/apps/firewall/"), ("redirect", "/containd/"))
        self.assertEqual(self.route("/apps/firewall"), ("redirect", "/containd/"))
        self.assertEqual(self.route("/apps/firewall/login", "next=%2Fvpn"), ("redirect", "/containd/login?next=%2Fvpn"))
        self.assertEqual(
            self.route("/apps/firewall/_next/static/chunks/main-app.js"),
            ("redirect", "/containd/_next/static/chunks/main-app.js"),
        )
        self.assertEqual(self.route("/containd/login")[0], "proxy")

    def test_legacy_workshop_openplc_ui_path_wins_over_api_and_redirects_to_openplc(self):
        self.assertEqual(self.route(OPENPLC_LEGACY), ("redirect", "/apps/openplc/"))
        self.assertEqual(self.route(OPENPLC_LEGACY + "login", "x=1"), ("redirect", "/apps/openplc/login?x=1"))
        self.assertEqual(self.route(OPENPLC_LEGACY + "static/style.css"), ("redirect", "/apps/openplc/static/style.css"))
        self.assertEqual(self.route("/apps/openplc/login"), ("proxy", "http://openplc/login"))
        # Real legacy instances and everything else under /api/ still reach the backend.
        for path in (
            "/api/labs/instances/other/nodes/openplc-1/ui/",
            "/api/labs/instances/workshop/nodes/ews-1/ui/",
            "/api/labs/instances/workshop/nodes/openplc-1/ui",
            "/api/workshop/graph",
        ):
            with self.subTest(path=path):
                self.assertEqual(self.route(path), ("proxy", "http://backend" + path))

    def test_icons_are_served_from_the_containd_upstream_like_containd_assets(self):
        for name in ("docker", "openvpn", "wireguard", "nginx", "envoyproxy"):
            with self.subTest(name=name):
                self.assertEqual(
                    self.route(f"/icons/{name}.svg"), ("proxy", f"{CONTAIND_UPSTREAM}icons/{name}.svg")
                )
                self.assertEqual(
                    self.route(f"/containd/icons/{name}.svg"), ("proxy", f"{CONTAIND_UPSTREAM}icons/{name}.svg")
                )
        self.assertEqual(self.route("/icons"), ("proxy", "http://frontend/icons"))

    def test_nothing_else_in_the_repo_claims_the_icons_prefix(self):
        self.assertFalse((ROOT / "frontend/public/icons").exists())
        self.assertFalse((ROOT / "frontend/app/icons").exists())
        icons = [loc for loc in self.locations if loc.matches("/icons/docker.svg") and loc.modifier != ""]
        self.assertEqual(icons, [])

    def test_apps_hint_names_the_working_containd_route(self):
        hint = select(self.locations, "/apps").directive("return")[1]
        self.assertIn("/containd/", hint)
        self.assertNotIn("/apps/firewall/", hint)

    def test_no_location_proxies_the_legacy_firewall_path(self):
        legacy = [loc for loc in self.locations if loc.pattern.startswith("/apps/firewall")]
        self.assertEqual(len(legacy), 2)
        for loc in legacy:
            with self.subTest(pattern=loc.pattern):
                self.assertIsNone(loc.directive("proxy_pass"))


class ParserTests(unittest.TestCase):
    def test_tokenizer_keeps_hashes_and_braces_inside_quotes(self):
        tokens = tokenize("sub_filter '<body' '<body style=\"a:#fff;{}\"'; # trailing\nx y;")
        self.assertEqual(tokens, ["sub_filter", "<body", '<body style="a:#fff;{}"', ";", "x", "y", ";"])

    def test_parser_rejects_unbalanced_blocks(self):
        for text in ("http { server {", "http { } }", "a b"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                parse(tokenize(text))

    def test_select_prefers_exact_then_regex_then_longest_prefix(self):
        locations = [
            Location(["/"], []),
            Location(["/api/"], []),
            Location(["=", "/api/exact"], []),
            Location(["~", r"^/api/regex"], []),
            Location(["/api/regular/"], []),
        ]
        self.assertEqual(select(locations, "/api/exact").pattern, "/api/exact")
        self.assertEqual(select(locations, "/api/regular/x").pattern, "/api/regular/")
        self.assertEqual(select(locations, "/api/regex").pattern, r"^/api/regex")
        self.assertEqual(select(locations, "/other").pattern, "/")


if __name__ == "__main__":
    unittest.main()
