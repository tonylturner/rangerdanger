#!/usr/bin/env python3
"""Check the embedded containd bundle, session and API routing without changing range policy.

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

    page = fetch("/containd/", {"text/html"})
    parser = Scripts()
    parser.feed(page)
    paths = set()
    for source in parser.sources:
        url = urlsplit(urljoin(base + "/containd/", source))
        if (url.scheme, url.netloc) != (origin.scheme, origin.netloc) or not (
            url.path.startswith("/containd/_next/static/") and url.path.endswith(".js")
        ):
            raise ProbeError(f"unexpected embedded script source: {source}")
        paths.add(url.path + ("?" + url.query if url.query else ""))
    if not paths:
        raise ProbeError("containd page contains no JavaScript chunks")
    chunks = {
        path: fetch(path, {"application/javascript", "text/javascript"})
        for path in sorted(paths)
    }
    check_chunks(chunks)
    print(f"PASS containd UI: {len(chunks)} chunks, runtime API base intact, no double prefix")

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
