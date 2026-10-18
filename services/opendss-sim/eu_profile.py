"""Strict schema-3 configuration for the CIGRE-derived EU teaching feeder."""

import json
import math
import re
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError


class ProfileError(ValueError):
    """The explicitly configured EU profile is unusable."""


DEVICE_KINDS = {
    "feeder-breaker": "breaker", "f2-breaker": "breaker", "rmu-1": "rmu", "rmu-2": "rmu", "oltc-1": "oltc"
}
ELEMENT_KEYS = set(DEVICE_KINDS) | {"source", "fault_s1", "fault_s2"}
BUS_KEYS = {"hv", "bus20", "f1", "s1", "s2", "s3"}
PROFILE_KEYS = {
    "schema", "id", "frequency_hz", "hv_kv", "mv_kv", "circuit_file",
    "devices", "elements", "buses", "solve_interval_ms", "request_timeout_ms",
    "truth_stale_after_ms",
    "profile",
}


@dataclass(frozen=True)
class ProfileSettings:
    enabled: bool
    seed: int | None
    timezone: str
    noise_sigma: float
    noise_tau_s: float
    source_tau_s: float
    source_pu_min: float
    source_pu_max: float


@dataclass(frozen=True)
class DeviceProfile:
    id: str
    kind: str
    truth_url: str


@dataclass(frozen=True)
class EUProfile:
    id: str
    frequency_hz: int
    hv_kv: int
    mv_kv: int
    circuit_file: Path
    devices: tuple[DeviceProfile, ...]
    elements: dict[str, str]
    buses: dict[str, str]
    solve_interval_ms: int
    request_timeout_ms: int
    truth_stale_after_ms: int
    profile: ProfileSettings


def _number(value: object, name: str, minimum: float = 0) -> float:
    if type(value) not in (int, float):
        raise ProfileError(f"invalid {name}")
    try:
        number = float(value)
    except OverflowError as error:
        raise ProfileError(f"invalid {name}") from error
    if not math.isfinite(number) or number < minimum:
        raise ProfileError(f"invalid {name}")
    return number


def _settings(value: object) -> ProfileSettings:
    if not isinstance(value, dict) or set(value) != set(ProfileSettings.__annotations__):
        raise ProfileError("invalid profile settings")
    if (type(value["enabled"]) is not bool
            or (value["seed"] is not None and type(value["seed"]) is not int)):
        raise ProfileError("invalid profile enabled/seed")
    try:
        ZoneInfo(value["timezone"])
    except (TypeError, ValueError, ZoneInfoNotFoundError) as error:
        raise ProfileError("invalid profile timezone") from error
    for key in ("noise_sigma", "noise_tau_s", "source_tau_s", "source_pu_min", "source_pu_max"):
        _number(value[key], key)
    if (value["noise_tau_s"] <= 0 or value["source_tau_s"] <= 0
            or not 0 < value["source_pu_min"] <= 1 <= value["source_pu_max"]
            or value["source_pu_min"] == value["source_pu_max"]):
        raise ProfileError("invalid profile time constants/source bounds")
    return ProfileSettings(**value)


def _positive_integer(value: object, name: str) -> int:
    if type(value) is not int or value <= 0:
        raise ProfileError(f"{name} must be a positive integer")
    return value


def _origin(value: object) -> str:
    if not isinstance(value, str):
        raise ProfileError("truth_url must be an HTTP origin")
    try:
        url = urlsplit(value)
        port = url.port
    except ValueError as error:
        raise ProfileError("truth_url must be an HTTP origin") from error
    if (url.scheme != "http" or not url.hostname or url.username is not None
            or url.password is not None or url.path not in ("", "/")
            or url.query or url.fragment or (port is not None and not 1 <= port <= 65535)):
        raise ProfileError("truth_url must be an HTTP origin")
    return value.rstrip("/")


def _mapping(value: object, keys: set[str], field: str) -> dict:
    if (not isinstance(value, dict) or set(value) != keys
            or any(not isinstance(v, str) or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_.-]*", v)
                   for v in value.values())):
        raise ProfileError(f"invalid {field} mapping")
    if len(set(v.lower() for v in value.values())) != len(value):
        raise ProfileError(f"duplicate {field} mapping")
    return value


def load_profile(path: str | Path) -> EUProfile:
    """Reject older profiles, missing devices and ambiguous element mappings."""
    try:
        value = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ProfileError(f"cannot load profile: {error}") from error
    if not isinstance(value, dict) or set(value) != PROFILE_KEYS:
        raise ProfileError("profile fields mismatch")
    if type(value["schema"]) is not int or value["schema"] != 3:
        raise ProfileError("schema must be 3")
    if value["id"] != "eu-cigre-mv-teaching":
        raise ProfileError("id must be eu-cigre-mv-teaching")
    for name, expected in (("frequency_hz", 50), ("hv_kv", 110), ("mv_kv", 20)):
        if _positive_integer(value[name], name) != expected:
            raise ProfileError(f"{name} must be {expected}")
    name = value["circuit_file"]
    if not isinstance(name, str) or not Path(name).is_absolute() or '"' in name or '\n' in name:
        raise ProfileError("circuit_file must be an absolute path")
    circuit_file = Path(name)
    if not circuit_file.is_file():
        raise ProfileError(f"circuit_file does not exist: {circuit_file}")
    devices = value["devices"]
    if not isinstance(devices, list) or len(devices) != 5:
        raise ProfileError("devices must contain all five devices")
    parsed = []
    for device in devices:
        if (not isinstance(device, dict) or set(device) != {"id", "kind", "truth_url"}
                or not isinstance(device["id"], str)
                or device["id"] not in DEVICE_KINDS
                or device["kind"] != DEVICE_KINDS[device["id"]]):
            raise ProfileError("invalid device identity/kind")
        parsed.append(DeviceProfile(device["id"], device["kind"], _origin(device["truth_url"])))
    if len({d.id for d in parsed}) != 5 or len({d.truth_url for d in parsed}) != 5:
        raise ProfileError("duplicate device or truth_url")
    elements = _mapping(value["elements"], ELEMENT_KEYS, "elements")
    buses = _mapping(value["buses"], BUS_KEYS, "buses")
    interval = _positive_integer(value["solve_interval_ms"], "solve_interval_ms")
    timeout = _positive_integer(value["request_timeout_ms"], "request_timeout_ms")
    stale = _positive_integer(value["truth_stale_after_ms"], "truth_stale_after_ms")
    if interval != 1000 or stale != 3000 or timeout >= interval:
        raise ProfileError("require interval=1000, stale=3000 and timeout < interval")
    return EUProfile(value["id"], 50, 110, 20, circuit_file, tuple(parsed),
                     elements, buses, interval, timeout, stale,
                     _settings(value["profile"]))
