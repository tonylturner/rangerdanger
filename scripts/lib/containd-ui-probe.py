#!/usr/bin/env python3
"""Check the embedded containd bundle and session without changing range policy.

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


AUTODETECT = re.compile(
    r"""window\.location\.pathname\.startsWith\(\s*["']/containd/["']\s*\)\s*\?\s*["']/containd["']"""
)
PREFIXED_API = re.compile(r"""(["'])(/containd/api/[^"']*)\1""")


def check_chunks(chunks):
    """Only the two API-base bypasses may have proxy-prefixed literals."""
    for path, body in chunks.items():
        if "/containd/containd/" in body:
            raise ProbeError(f"{path}: double /containd/ prefix")
        for match in PREFIXED_API.finditer(body):
            literal = match.group(2)
            if literal != "/containd/api/v1/cli/ws" and not literal.startswith(
                "/containd/api/v1/pcap/download/"
            ):
                raise ProbeError(f"{path}: runtime-based API literal was rewritten: {literal}")
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

    def fetch(path, types, data=None, token=None):
        headers = {"Accept-Encoding": "identity"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        request = Request(base + path, data=data, headers=headers)
        with opener.open(request, timeout=15) as response:
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
