"""Read-only acceptance through the same REST/Socket.IO path as FUXA's viewer.

Defaults to the live portal; skips only when the FUXA service is absent.
FUXA_LIVE_URL can point at an isolated acceptance container instead.
"""
from __future__ import annotations

import json
import math
import os
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request


class ViewerClient:
    """The pinned FUXA client's Engine.IO 4 polling transport, without packages."""

    def __init__(self, base: str):
        self.base = base.rstrip("/")
        self.socket_url = self.base + "/socket.io/?EIO=4&transport=polling"
        self.deadline = time.monotonic() + 25

    def request(self, url: str, payload: str | None = None) -> str:
        request = urllib.request.Request(
            url, data=None if payload is None else payload.encode(),
            headers={"Content-Type": "text/plain;charset=UTF-8"},
        )
        with urllib.request.urlopen(
            request, timeout=max(0.1, min(5, self.deadline - time.monotonic()))
        ) as response:
            return response.read().decode()

    def connect(self):
        opened = self.request(self.socket_url)
        assert opened.startswith("0"), opened
        sid = json.loads(opened[1:])["sid"]
        self.socket_url += "&sid=" + urllib.parse.quote(sid)
        self.request(self.socket_url, "40")
        assert self.request(self.socket_url).startswith("40")

    def subscribe(self, tag_ids: list[str]):
        self.request(self.socket_url, "42" + json.dumps([
            "device-tags-subscribe", {"tagsId": tag_ids, "sendLastValue": True},
        ]))

    def events(self):
        while time.monotonic() < self.deadline:
            for packet in self.request(self.socket_url).split("\x1e"):
                if packet == "2":
                    self.request(self.socket_url, "3")
                elif packet.startswith("42"):
                    yield json.loads(packet[2:])
                elif packet.startswith(("1", "41", "44")):
                    raise AssertionError(f"Viewer disconnected: {packet}")
        raise AssertionError("No complete, fresh set of six values within 25 seconds")

    def close(self):
        # Close only our client, never a device or the runtime.
        self.deadline = time.monotonic() + 2
        self.request(self.socket_url, "41")


class FuxaSeedLiveTests(unittest.TestCase):
    def test_viewer_receives_six_fresh_read_only_values(self):
        client = ViewerClient(os.environ.get("FUXA_LIVE_URL", "http://localhost:8088"))
        try:
            project = json.loads(client.request(client.base + "/api/project"))
        except urllib.error.HTTPError as error:
            with error:
                if error.code in (404, 502, 503):
                    # An unavailable upstream in an active US range is a failure,
                    # not an absent-range skip. Ask the platform's authority.
                    state = json.loads(client.request(client.base + "/api/range"))
                    if state["phase"] == "none" or (
                        state["phase"] == "ready"
                        and state["package"] != "us-dnp3-substation"
                    ):
                        self.skipTest(f"No US range at {client.base}: {state}")
                raise
        except urllib.error.URLError as error:
            if isinstance(error.reason, ConnectionRefusedError):
                self.skipTest(f"No range at {client.base}")
            raise
        device = project["devices"]["rd_rtac"]
        view, = project["hmi"]["views"]
        self.assertEqual(view["id"], "rd_us_monitor")
        self.assertEqual(len(view["items"]), 6)
        tag_ids = []
        for item in view["items"].values():
            self.assertEqual(item["type"], "svg-ext-value")
            self.assertEqual(item["property"]["events"], [])
            self.assertEqual(item["property"]["variableSrc"], device["id"])
            tag_ids.append(item["property"]["variableId"])
        # FUXA subscribes with widget variableId, not device-qualified legacy IDs.
        self.assertEqual(set(tag_ids), set(device["tags"]))
        client.connect()
        try:
            client.subscribe(tag_ids)
            first = None
            for event, message in client.events():
                if event != "device-values" or message["id"] != device["id"]:
                    continue
                values = {tag["id"]: tag for tag in message["values"]}
                if set(values) != set(tag_ids):
                    continue
                if not all(
                    type(tag.get("value")) in (int, float)
                    and math.isfinite(tag["value"])
                    and isinstance(tag.get("timestamp"), (int, float))
                    for tag in values.values()
                ):
                    continue
                if first is None:
                    first = values
                elif all(
                    values[key]["timestamp"] > first[key]["timestamp"]
                    for key in tag_ids
                ):
                    print("FUXA viewer values:",
                          {key: tag["value"] for key, tag in values.items()})
                    return
        finally:
            client.close()


if __name__ == "__main__":
    unittest.main()
