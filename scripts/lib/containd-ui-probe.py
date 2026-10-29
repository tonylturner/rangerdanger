#!/usr/bin/env python3
"""Check the embedded containd bundle, session and API routing without changing range policy.

The bundle check covers the chunks the page loads, those its webpack runtime loads on demand,
and the chunks of every route page the bundle names.

Standalone (EU or US): python3 scripts/lib/containd-ui-probe.py http://127.0.0.1:8088
Uses the documented public lab account, containd/containd. No Docker required.
"""

import argparse
from html.parser import HTMLParser
from http.cookiejar import CookieJar
import json
import re
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin, urlsplit
from urllib.request import HTTPCookieProcessor, HTTPRedirectHandler, Request, build_opener


class ProbeError(ValueError):
    """An incomplete check is a failure, never a pass."""


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
# The paths the CLI console and PCAP download build from the runtime base. Without an
# Upgrade header, or for a capture that does not exist, the API refuses with a 400;
# a double-prefixed path instead gets the UI page (200 text/html).
API_ROUTES = (
    "/containd/api/v1/cli/ws",
    "/containd/api/v1/pcap/download/rangerdanger-probe-missing.pcap",
)


# The served webpack runtime, black-box (current bundle):
#   r.p="/containd/_next/",r.u=e=>"static/chunks/"+(3888===e?"e868780c":e)+"."+({747:"5cee...",...})[e]+".js"
# The chunk-URL function maps each on-demand chunk id to a file; ids not named in the
# optional ===-chain use the id as the name. Any other shape fails rather than skipping chunks.
WEBPACK_CHUNKS = re.compile(
    r'(?<![\w$])([A-Za-z_$][\w$]*)\.u=([A-Za-z_$][\w$]*)=>"static/chunks/"\+(\2|\((?:\d+===\2\?"[\w-]+":)+\2\))\+"\."\+\((\{(?:\d+:"[\w-]+"(?:,\d+:"[\w-]+")*)?\})\)\[\2\]\+"\.js"'
)
CHUNK_NAME = re.compile(r'(\d+)===[A-Za-z_$][\w$]*\?"([\w-]+)"')
CHUNK_HASH = re.compile(r'(\d+):"([\w-]+)"')
PUBLIC_PATH = re.compile(r'(?<![\w$])([A-Za-z_$][\w$]*)\.p="([^"]*)"')


def ondemand_chunks(chunks):
    """Chunk URLs the webpack runtime loads on demand (the CLI console is one), in map order."""
    runtimes = [
        (path, body, match) for path, body in chunks.items() for match in WEBPACK_CHUNKS.finditer(body)
    ]
    if len(runtimes) != 1:
        raise ProbeError(f"chunks contain {len(runtimes)} webpack runtime chunk maps, expected 1")
    path, body, match = runtimes[0]
    public = {found.group(2) for found in PUBLIC_PATH.finditer(body) if found.group(1) == match.group(1)}
    if len(public) != 1:
        raise ProbeError(f"{path}: webpack runtime does not set exactly one public path")
    prefix = public.pop() + "static/chunks/"
    # The first ===-test for an id decides its name, as in the JavaScript conditional chain.
    names = dict(reversed(CHUNK_NAME.findall(match.group(3))))
    return [f"{prefix}{names.get(chunk, chunk)}.{digest}.js" for chunk, digest in CHUNK_HASH.findall(match.group(4))]


# App-router page chunks (app/<route>/page-*.js) load only from their own route's HTML.
# Routes come from the bundle's path literals: proxy-prefixed ones ("/containd/alerts/",
# "/containd/login?reason=") and root-relative link targets (href:"/identities/").
CONTAIND_ROUTE = re.compile(r'''(["'])/containd/([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"']*)?\1''')
LINK_ROUTE = re.compile(
    r'''href:(["'])/(?!containd[/?#"'])([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"']*)?\1'''
)
# Not app-router pages: assets, the API, a double prefix, and containd's documentation,
# which is a separate static site whose scripts live outside /_next/static/.
NOT_ROUTES = ("_next", "api", "containd", "docs")
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


def check_chunks(chunks):
    """containd adds its API base at runtime, so no chunk may carry a proxy-prefixed API literal."""
    for path, body in chunks.items():
        if "/containd/containd/" in body:
            raise ProbeError(f"{path}: double /containd/ prefix")
        match = PREFIXED_API.search(body)
        if match:
            raise ProbeError(f"{path}: runtime-based API literal was rewritten: {match.group(2)}")
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

    def request(path, data=None, token=None):
        headers = {"Accept-Encoding": "identity"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        return Request(base + path, data=data, headers=headers)

    def fetch(path, types, data=None, token=None):
        with opener.open(request(path, data, token), timeout=15) as response:
            content_type = response.headers.get_content_type()
            if response.status != 200 or content_type not in types:
                raise ProbeError(f"{path}: expected 200 {types}, got {response.status} {content_type}")
            body = response.read().decode("utf-8")
        return body

    def chunk_path(source, kind):
        url = urlsplit(urljoin(base + "/containd/", source))
        if (url.scheme, url.netloc) != (origin.scheme, origin.netloc) or not (
            url.path.startswith("/containd/_next/static/") and url.path.endswith(".js")
        ):
            raise ProbeError(f"unexpected {kind}: {source}")
        return url.path + ("?" + url.query if url.query else "")

    scripts = {"application/javascript", "text/javascript"}
    def page_chunks(path):
        parser = Scripts()
        parser.feed(fetch(path, {"text/html"}))
        return {chunk_path(source, "embedded script source") for source in parser.sources}

    paths = page_chunks("/containd/")
    if not paths:
        raise ProbeError("containd page contains no JavaScript chunks")
    chunks = {path: fetch(path, scripts) for path in sorted(paths)}
    ondemand = 0
    for source in ondemand_chunks(chunks):
        path = chunk_path(source, "on-demand chunk")
        if path not in chunks:
            chunks[path] = fetch(path, scripts)
            ondemand += 1
    # Each round fetches the newly named route pages, then their new chunks, which may name more.
    visited = set()
    while pending := [page for page in route_pages(chunks) if page not in visited]:
        if len(visited) + len(pending) > MAX_ROUTE_PAGES:
            raise ProbeError(f"chunks name more than {MAX_ROUTE_PAGES} route pages")
        found = set()
        for page in pending:
            visited.add(page)
            found |= page_chunks(page)
        for path in sorted(found - chunks.keys()):
            chunks[path] = fetch(path, scripts)
    check_chunks(chunks)
    print(
        f"PASS containd UI: {len(chunks)} chunks ({ondemand} on demand), {len(visited)} route pages, "
        "runtime API base intact, no double prefix"
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
