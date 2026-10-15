"""Strict schema-2 process/device wire validation and bounded HTTP transport."""

import json
import re
from datetime import datetime
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from eu_profile import DeviceProfile, EUProfile

BASE = {"schema", "device_id", "kind", "generation", "revision", "event_seq", "changed_at"}
FIELDS = {
    "breaker": {"position", "remote_enabled", "lockout", "protection_tripped"},
    "rmu": {"position", "remote_enabled", "earth_closed", "fault_passage"},
    "oltc": {"tap", "target_tap", "moving", "remote_enabled", "auto", "blocked", "armed"},
}


class TruthError(ValueError):
    """A truth or delivery response cannot be trusted."""


def uint(value: object, maximum: int = 2**64 - 1) -> bool:
    return type(value) is int and 0 <= value <= maximum


def validate_truth(value: object, device: DeviceProfile) -> dict:
    """Validate the exact per-kind shape, identity, types, ranges and UTC time."""
    if not isinstance(value, dict) or set(value) != BASE | FIELDS[device.kind]:
        raise TruthError("truth fields mismatch")
    if type(value["schema"]) is not int or value["schema"] != 2:
        raise TruthError("truth schema must be 2")
    if value["device_id"] != device.id or value["kind"] != device.kind:
        raise TruthError("truth identity/kind mismatch")
    for key in ("generation", "revision", "event_seq"):
        if not uint(value[key], 2**32 - 1 if key == "generation" else 2**64 - 1):
            raise TruthError(f"invalid {key}")
    stamp = value["changed_at"]
    if not isinstance(stamp, str) or not re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z", stamp
    ):
        raise TruthError("invalid changed_at")
    try:
        datetime.fromisoformat(stamp[:-1] + "+00:00")
    except ValueError as error:
        raise TruthError("invalid changed_at") from error
    for key in FIELDS[device.kind] - {"position", "tap", "target_tap"}:
        if type(value[key]) is not bool:
            raise TruthError(f"{key} must be boolean")
    if device.kind == "oltc":
        for key in ("tap", "target_tap"):
            if type(value[key]) is not int or not -8 <= value[key] <= 8:
                raise TruthError(f"invalid {key}")
        distance = abs(value["tap"] - value["target_tap"])
        if distance != (1 if value["moving"] else 0):
            raise TruthError("invalid motion target")
    elif value["position"] not in ("open", "closed"):
        raise TruthError("invalid position")
    return dict(value)


def acknowledgement(status: int, body: object, revision: int) -> tuple[dict, bool]:
    """Return delivery evidence and whether a full-vector reread is required."""
    record = {"delivered": False, "solved_revision": revision, "revision": revision}
    if (status == 409 and isinstance(body, dict)
            and set(body) == {"accepted", "reason"} and body["accepted"] is False
            and body["reason"] == "stale_truth"):
        record["revision"] = None
        return record, True
    if (status == 200 and isinstance(body, dict)
            and set(body) == {"accepted", "solved_revision", "revision"}
            and body["accepted"] is True and uint(body["solved_revision"])
            and body["solved_revision"] == revision and uint(body["revision"])
            and body["revision"] >= revision):
        record.update(delivered=True, revision=body["revision"])
        return record, body["revision"] != revision
    return record, False


class TruthClient:
    """Synchronous bounded operations, executed concurrently off the event loop."""

    def __init__(self, profile: EUProfile, opener=urlopen):
        self.timeout_seconds = profile.request_timeout_ms / 1000
        self._opener = opener

    def _request(self, request: Request) -> tuple[int, object]:
        try:
            with self._opener(request, timeout=self.timeout_seconds) as response:
                return response.status, json.loads(response.read())
        except HTTPError as error:
            try:
                return error.code, json.loads(error.read())
            except (OSError, ValueError) as parse_error:
                raise TruthError(f"invalid HTTP response: {parse_error}") from error
        except (URLError, TimeoutError, OSError, ValueError) as error:
            raise TruthError(f"process HTTP request failed: {error}") from error

    def get_truth(self, device: DeviceProfile) -> dict:
        status, value = self._request(Request(f"{device.truth_url}/api/truth", method="GET"))
        if status != 200:
            raise TruthError(f"truth GET returned HTTP {status}")
        return validate_truth(value, device)

    def post_measurements(self, device: DeviceProfile, payload: dict) -> tuple[int, object]:
        return self._request(Request(
            f"{device.truth_url}/api/measurements", method="POST",
            data=json.dumps(payload, allow_nan=False).encode(),
            headers={"Content-Type": "application/json"},
        ))
