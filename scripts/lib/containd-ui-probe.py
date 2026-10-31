#!/usr/bin/env python3
"""Check the embedded containd bundle, session and API routing without changing range policy.

The bundle check covers the page scripts, transitive on-demand Turbopack imports
(JavaScript and CSS), and the scripts of every route page the bundle names.

Standalone (EU or US): python3 scripts/lib/containd-ui-probe.py http://127.0.0.1:8088
Uses the documented public lab account, containd/containd. No Docker required.
"""

import argparse
import base64
import hashlib
from http.client import HTTPConnection, HTTPSConnection
from html.parser import HTMLParser
from http.cookiejar import CookieJar
import json
import os
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urljoin, urlsplit
from urllib.request import HTTPCookieProcessor, HTTPRedirectHandler, Request, build_opener


class ProbeError(ValueError):
    """An incomplete check is a failure, never a pass."""


def cli_upgrade(origin, *, ticket=None, token=None, query_token=False, expected_status=101):
    """Check one ticketed or deliberately unauthorized browser-origin handshake."""
    url = urlsplit(origin)
    connection = (HTTPSConnection if url.scheme == "https" else HTTPConnection)(
        url.hostname, url.port, timeout=15,
    )
    path = "/containd/api/v1/cli/ws"
    query = {"ticket": ticket} if ticket is not None else (
        {"token": token} if query_token and token is not None else {}
    )
    target = path + ("?" + urlencode(query) if query else "")
    key = base64.b64encode(os.urandom(16)).decode("ascii")
    try:
        headers = {
            "Host": url.netloc, "Origin": origin,
            "Connection": "Upgrade", "Upgrade": "websocket",
            "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": key,
        }
        if token is not None and not query_token:
            headers["Authorization"] = f"Bearer {token}"
        connection.request("GET", target, headers=headers)
        response = connection.getresponse()
        with response:
            if response.status != expected_status:
                raise ProbeError(
                    f"{path}: expected HTTP {expected_status}, got {response.status} {response.reason}"
                )
            if expected_status == 101:
                accept = base64.b64encode(hashlib.sha1(
                    (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")
                ).digest()).decode("ascii")
                if (response.getheader("Upgrade", "").lower() != "websocket"
                        or "upgrade" not in {
                            part.strip().lower() for part in response.getheader("Connection", "").split(",")
                        }
                        or response.getheader("Sec-WebSocket-Accept") != accept):
                    raise ProbeError(f"{path}: invalid websocket upgrade headers")
    finally:
        connection.close()
    if expected_status == 101:
        print(f"PASS {path}: websocket 101 with a fresh ticket and browser Origin {origin}")
    else:
        print(f"PASS {path}: rejected with HTTP {expected_status}")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # A redirect to a login page must not masquerade as a successful API call.
        return None


class Scripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        if tag == "script":
            self.sources.extend(value for key, value in attrs if key == "src" and value)


# Match direct pathname detection or the optional-path fallback in 0.1.33.
# The fallback must test and return the same identifier, not an unrelated value.
AUTODETECT = re.compile(
    r"""(?:window\.location\.pathname|\(\s*null\s*!=\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\?\s*\1\s*:\s*window\.location\.pathname\s*\))\.startsWith\(\s*["']/containd/["']\s*\)\s*\?\s*["']/containd["']"""
)
PREFIXED_API = re.compile(r"""(["'])(/containd/api/[^"']*)\1""")
# The PCAP path must remain an API response, not fall through to the UI page.
API_ROUTES = (
    "/containd/api/v1/pcap/download/rangerdanger-probe-missing.pcap",
)


# Served containd 0.1.36 uses literal dependency arrays, not a webpack id/hash map.
# Pin the observed runtime's base path, string loader and URL builder together;
# a new runtime shape needs inspection of served artifacts, never a silent skip.
TURBOPACK_BASE = re.compile(
    r'let _=i.prototype,k="string"==typeof TURBOPACK_CHUNK_BASE_PATH'
    r'\?TURBOPACK_CHUNK_BASE_PATH:"([^"]*)",P=new Map;'
)
TURBOPACK_LOADER = (
    'async function j(e,t,r){let n;if("string"==typeof r)return function(e,t,r)'
    '{return M(e,t,r)}(e,t,x(r));'
)
TURBOPACK_LOAD_METHOD = '_.l=function(e){return j(o.Parent,this.m.id,e)};'
TURBOPACK_URL = (
    'function x(e,t=k){let r=T.test(e)?e.split("/").map(encodeURIComponent).join("/"):e;'
    'return`${t}${r}${n}`}'
)
# Only the captured Promise.all([...].map(t=>e.l(t))) import form is supported.
# Account for every static/chunks/ reference, including CSS: finding some known
# arrays must not hide another dependency expression that we cannot verify.
TURBOPACK_IMPORT = re.compile(
    r'Promise\.all\(\[(?P<paths>"static/chunks/[\w-]+\.(?:js|css)"'
    r'(?:,"static/chunks/[\w-]+\.(?:js|css)")*)\]\.map\('
    r'(?P<arg>[A-Za-z_$][\w$]*)=>[A-Za-z_$][\w$]*\.l\((?P=arg)\)\)\)'
)
CHUNK_LITERAL = re.compile(r'"(static/chunks/[\w-]+\.(?:js|css))"')
MAX_CHUNKS = 2000


def ondemand_chunks(chunks):
    """Resolve all literal Turbopack imports in discovery order; unknown shapes fail."""
    runtimes = [
        (path, body, match)
        for path, body in chunks.items() for match in TURBOPACK_BASE.finditer(body)
    ]
    if len(runtimes) != 1:
        raise ProbeError(
            "cannot verify on-demand chunks: unsupported or ambiguous Turbopack runtime "
            f"({len(runtimes)} base paths; expected exactly 1); inspect the live bundle"
        )
    path, body, match = runtimes[0]
    prefix = match.group(1)
    if (TURBOPACK_LOADER not in body or TURBOPACK_LOAD_METHOD not in body
            or TURBOPACK_URL not in body
            or sum(text.count("TURBOPACK_CHUNK_BASE_PATH") for text in chunks.values()) != 2):
        raise ProbeError(f"{path}: unsupported Turbopack loader shape; inspect the live bundle")
    sources = []
    for path, body in chunks.items():
        imports = list(TURBOPACK_IMPORT.finditer(body))
        remainder = TURBOPACK_IMPORT.sub("", body)
        if "static/chunks/" in remainder or re.search(r"\.l\s*\(", remainder):
            raise ProbeError(f"{path}: unsupported Turbopack dependency shape; inspect the live bundle")
        for dependency in imports:
            sources.extend(prefix + source for source in CHUNK_LITERAL.findall(dependency.group("paths")))
    return list(dict.fromkeys(sources))


# App-router page chunks load only from their own route's HTML (opaque Turbopack filenames).
# Routes come from the bundle's path literals: proxy-prefixed ones ("/containd/alerts/",
# "/containd/login?reason=") and root-relative link targets (href:"/identities/").
# A root-relative link is also a proxy defect: the route list's sub_filter missed it.
CONTAIND_ROUTE = re.compile(r'''(["'])/containd/([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"']*)?\1''')
LINK_ROUTE = re.compile(
    r'''href[:=](["'])/(?!containd[/?#"'])([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"']*)?\1'''
)
# Not app-router pages: assets, the API, a double prefix, and containd's documentation,
# which is a separate static site whose scripts live outside /_next/static/.
NOT_ROUTES = ("_next", "api", "containd", "docs")
# The same link inside an inline React Server Components payload, where quotes are escaped.
FLIGHT_LINK = re.compile(
    r'''\\"href\\":\\"/(?!containd[/?#\\])([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"\\]*)?\\"'''
)
# Root-relative links that are not routes: assets and the API.
LINK_EXEMPT = ("_next", "api")
MAX_ROUTE_PAGES = 200


def route_pages(chunks):
    """Route pages the chunks name, as sorted /containd/<route>/ paths."""
    routes = set()
    for body in chunks.values():
        for pattern in (CONTAIND_ROUTE, LINK_ROUTE):
            for match in pattern.finditer(body):
                route = match.group(2)
                if route.split("/", 1)[0] not in NOT_ROUTES:
                    routes.add(f"/containd/{route}/")
    return sorted(routes)


def unrewritten_links(body):
    """Root-relative route links in a chunk or page, as the literals that carry them."""
    for pattern, group in ((LINK_ROUTE, 2), (FLIGHT_LINK, 1)):
        for match in pattern.finditer(body):
            if match.group(group).split("/", 1)[0] not in LINK_EXEMPT:
                yield match.group(0)


def check_links(documents, kind):
    """In the proxied UI every route link must carry /containd/; a bare one leaves the UI."""
    for path, body in documents.items():
        link = next(unrewritten_links(body), None)
        if link:
            raise ProbeError(f"{path}: {kind} has a route link the proxy did not rewrite: {link}")


def check_chunks(chunks):
    """containd adds its API base at runtime, so no chunk may carry a proxy-prefixed API literal."""
    for path, body in chunks.items():
        if "/containd/containd/" in body:
            raise ProbeError(f"{path}: double /containd/ prefix")
        match = PREFIXED_API.search(body)
        if match:
            raise ProbeError(f"{path}: runtime-based API literal was rewritten: {match.group(2)}")
    check_links(chunks, "chunk")
    if not any(AUTODETECT.search(body) for body in chunks.values()):
        raise ProbeError("chunks do not contain the /containd/ runtime API-base autodetect")
    if not any(re.search(r"""["']/api/v1/auth/me["']""", body) for body in chunks.values()):
        raise ProbeError("chunks do not contain the unprefixed auth/me call")


def probe(base):
    base = base.rstrip("/")
    origin = urlsplit(base)
    if origin.scheme not in ("http", "https") or not origin.netloc or (
        origin.path or origin.query or origin.fragment or origin.username or origin.password
    ):
        raise ProbeError("supply a portal origin, e.g. http://127.0.0.1:8088")
    opener = build_opener(NoRedirect(), HTTPCookieProcessor(CookieJar()))

    def request(path, data=None, token=None, method=None):
        headers = {"Accept-Encoding": "identity"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        return Request(base + path, data=data, headers=headers, method=method)

    def fetch(path, types, data=None, token=None, method=None, client=None):
        with (client or opener).open(request(path, data, token, method), timeout=15) as response:
            content_type = response.headers.get_content_type()
            if response.status != 200 or content_type not in types:
                raise ProbeError(f"{path}: expected 200 {types}, got {response.status} {content_type}")
            body = response.read().decode("utf-8")
        return body

    def chunk_path(source, kind, extensions=(".js",)):
        url = urlsplit(urljoin(base + "/containd/", source))
        if (url.scheme, url.netloc) != (origin.scheme, origin.netloc) or not (
            url.path.startswith("/containd/_next/static/") and url.path.endswith(extensions)
        ):
            raise ProbeError(f"unexpected {kind}: {source}")
        return url.path + ("?" + url.query if url.query else "")

    scripts = {"application/javascript", "text/javascript"}
    pages = {}

    def page_chunks(path):
        parser = Scripts()
        pages[path] = fetch(path, {"text/html"})
        parser.feed(pages[path])
        return {chunk_path(source, "embedded script source") for source in parser.sources}

    paths = page_chunks("/containd/")
    if not paths:
        raise ProbeError("containd page contains no JavaScript chunks")
    chunks = {path: fetch(path, scripts) for path in sorted(paths)}
    ondemand = 0
    styles = set()
    visited = set()
    # Reach a fixed point across imports AND routes: an imported chunk may import
    # again, and a route-only chunk may itself contain lazy dependencies.
    while True:
        for source in ondemand_chunks(chunks):
            path = chunk_path(source, "on-demand chunk", (".js", ".css"))
            if path in chunks or path in styles:
                continue
            if len(chunks) + len(styles) >= MAX_CHUNKS:
                raise ProbeError(f"bundle names more than {MAX_CHUNKS} chunks")
            if urlsplit(path).path.endswith(".css"):
                fetch(path, {"text/css"})
                styles.add(path)
            else:
                chunks[path] = fetch(path, scripts)
                ondemand += 1
        # Re-scan newly fetched imports before deciding the crawl is complete.
        pending = [page for page in route_pages(chunks) if page not in visited]
        found = set()
        if len(visited) + len(pending) > MAX_ROUTE_PAGES:
            raise ProbeError(f"chunks name more than {MAX_ROUTE_PAGES} route pages")
        for page in pending:
            visited.add(page)
            found |= page_chunks(page)
        for path in sorted(found - chunks.keys()):
            if len(chunks) + len(styles) >= MAX_CHUNKS:
                raise ProbeError(f"bundle names more than {MAX_CHUNKS} chunks")
            chunks[path] = fetch(path, scripts)
        if not pending and all(
            source in chunks or source in styles for source in ondemand_chunks(chunks)
        ):
            break
    check_chunks(chunks)
    check_links(pages, "page")
    print(
        f"PASS containd UI: {len(chunks)} chunks ({ondemand} on demand), {len(visited)} route pages, "
        f"{len(styles)} on-demand stylesheets, runtime API base intact, no double prefix"
    )

    login_path = "/containd/api/v1/auth/login"
    login = json.loads(fetch(
        login_path, {"application/json"},
        json.dumps({"username": "containd", "password": "containd"}).encode(),
    ))
    if not isinstance(login, dict) or not isinstance(login.get("token"), str) or not login["token"]:
        raise ProbeError(f"{login_path}: no session token")
    me_path = "/containd/api/v1/auth/me"
    me = json.loads(fetch(me_path, {"application/json"}, token=login["token"]))
    if not isinstance(me, dict) or me.get("username") != "containd":
        raise ProbeError(f"{me_path}: JSON does not identify the logged-in lab user")
    print(f"PASS {login_path} -> {me_path}: 200 application/json, username=containd")

    ticket_path = "/containd/api/v1/cli/ws/ticket"
    ticket_opener = build_opener(NoRedirect())

    def issue_ticket():
        ticket = json.loads(fetch(
            ticket_path, {"application/json"}, token=login["token"], method="POST",
            client=ticket_opener,
        ))
        if (not isinstance(ticket, dict) or not isinstance(ticket.get("ticket"), str)
                or not ticket["ticket"] or ticket.get("expiresIn") != 30):
            raise ProbeError(f"{ticket_path}: expected an opaque ticket and expiresIn=30")
        return ticket["ticket"]

    print(f"PASS {ticket_path}: authenticated ticket issuance contract (expiresIn=30)")

    # The upgrade endpoint accepts only tickets. Exercise each rejected legacy
    # credential form before opening a fresh ticket for each valid handshake.
    cli_upgrade(base, expected_status=401)
    cli_upgrade(base, token=login["token"], expected_status=401)
    cli_upgrade(base, token=login["token"], query_token=True, expected_status=401)

    first_ticket = issue_ticket()
    cli_upgrade(base, ticket=first_ticket)
    reconnect_ticket = issue_ticket()
    cli_upgrade(base, ticket=reconnect_ticket)
    cli_upgrade(base, ticket=reconnect_ticket, expected_status=401)

    for path in API_ROUTES:
        try:
            response = opener.open(request(path, token=login["token"]), timeout=15)
        except HTTPError as error:
            response = error
        with response:
            status, content_type = response.status, response.headers.get_content_type()
        if status != 400 or content_type == "text/html":
            raise ProbeError(f"{path}: expected a 400 refusal from the containd API, got {status} {content_type}")
    print(f"PASS {', '.join(API_ROUTES)}: 400 from the containd API, not the UI page")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("origin", nargs="?", default="http://127.0.0.1:8088")
    args = parser.parse_args()
    try:
        probe(args.origin)
    except (ProbeError, HTTPError, URLError, OSError, UnicodeError, json.JSONDecodeError) as error:
        print(f"FAIL containd UI: {error}", file=sys.stderr)
        if isinstance(error, HTTPError):
            error.close()
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
