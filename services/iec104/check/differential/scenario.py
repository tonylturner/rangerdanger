# SPDX-License-Identifier: Apache-2.0
"""Strict scenario/role configuration and point-map construction."""

import ipaddress
import json
from pathlib import Path
import re

HERE = Path(__file__).resolve().parent
CHECK = HERE.parent
KINDS = {"raw-rtu", "cmd-rtu", "cc-rtu", "raw-cc"}
OPS = {
    "start", "gi", "stop", "test", "negative", "sbo",
    "fixture", "expect", "server-reports", "window",
}


def unique(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON key: " + key)
        value[key] = item
    return value


def load(path):
    return json.loads(Path(path).read_text(), object_pairs_hook=unique,
                      parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value)))


def exact(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError("expected exactly these keys: " + ", ".join(keys))


def scenario(path):
    value = load(path)
    exact(value, ("schema", "id", "kind", "timeout_s", "capture_modbus",
                  "point_map", "actions", "expected_sessions"))
    if type(value["schema"]) is not int or value["schema"] != 1:
        raise ValueError("unsupported scenario schema")
    if not isinstance(value["id"], str) or not re.fullmatch("[a-z0-9-]+", value["id"]):
        raise ValueError("invalid scenario id")
    if not isinstance(value["kind"], str) or value["kind"] not in KINDS or type(value["capture_modbus"]) is not bool:
        raise ValueError("invalid scenario kind/capture setting")
    for key, upper in (("timeout_s", 600), ("expected_sessions", 10)):
        if type(value[key]) is not int or not 1 <= value[key] <= upper:
            raise ValueError("invalid " + key)
    if value["point_map"] != "feeder-status-v1":
        raise ValueError("unknown point-map fixture")
    if not isinstance(value["actions"], list) or not value["actions"]:
        raise ValueError("actions must be a nonempty list")
    for action in value["actions"]:
        exact(action, ("op", "args", "timeout_s"))
        if not isinstance(action["op"], str) or action["op"] not in OPS or not isinstance(action["args"], dict):
            raise ValueError("invalid action")
        if type(action["timeout_s"]) is not int or not 1 <= action["timeout_s"] <= value["timeout_s"]:
            raise ValueError("invalid action timeout")
        validate_args(action)
        supported = {
            "raw-rtu": OPS - {"server-reports"},
            "cmd-rtu": {"gi", "sbo"},
            "cc-rtu": {"gi"},
            "raw-cc": {"server-reports"},
        }
        if action["op"] not in supported[value["kind"]]:
            raise ValueError("action not supported by role configuration")
    return value


def validate_args(action):
    args = action["args"]
    op = action["op"]
    keys = {"start": (), "gi": (), "stop": (), "test": (),
            "negative": ("type", "cot", "ca", "ioa", "value", "expected_cot"),
            "sbo": ("value",), "fixture": ("query",),
            "expect": ("type", "cot", "ioa", "value"),
            "server-reports": ("count", "value", "timed_invalid"), "window": ()}
    exact(args, keys[op])
    if op == "fixture":
        if not isinstance(args["query"], str) or args["query"] not in {
                "fault?active=1", "reset", "mvalid?valid=0", "mvalid?valid=1",
                "read-loss?active=1", "read-loss?active=0"}:
            raise ValueError("unsupported fixture mutation")
    else:
        for key, item in args.items():
            if key == "timed_invalid":
                if type(item) is not bool:
                    raise ValueError("timed_invalid must be boolean")
            elif type(item) is not int or not 0 <= item <= {
                "count": 32770, "ioa": 16777215, "ca": 65535,
            }.get(key, 255):
                raise ValueError("invalid " + key)
        if op == "server-reports" and args["count"] < 1:
            raise ValueError("report count must be positive")


def roles(path):
    value = load(path)
    exact(value, ("rtu", "cc", "cmd", "peer"))
    for image in value.values():
        if not isinstance(image, str) or not image or image.startswith("-") or "containd" in image.lower():
            raise ValueError("invalid or forbidden role image")
    return value


def point_map(subnet):
    """Small real app profile, not a modified C implementation.

    Status-only inventory has no cyclic analog reporters to disable. Full
    42-point analog/cyclic acceptance remains in check/run.sh.
    """
    data = load(CHECK / "points.test.json")
    prefix = str(ipaddress.ip_network(subnet).network_address).rsplit(".", 1)[0]
    data["devices"] = data["devices"][:1]
    data["devices"][0]["host"] = prefix + ".20"
    data["points"] = data["points"][:2]
    data["commands"] = data["commands"][:1]
    data["clock"]["url"] = "http://" + prefix + ".20:8080/api/state"
    data["timers"].update(k=12, w=1, t1_s=15, t2_s=1, t3_s=60)
    return data
