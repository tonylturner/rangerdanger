"""Strict loader for the opt-in EU process profile."""

import json
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit


class ProfileError(ValueError):
    """Raised when an explicitly configured EU profile is unusable."""


@dataclass(frozen=True)
class EUProfile:
    id: str
    frequency_hz: int
    primary_kv: int
    circuit_file: Path
    load_kw: int
    load_pf: float
    breaker_element: str
    load_element: str
    load_bus: str
    device_id: str
    truth_url: str
    solve_interval_ms: int
    request_timeout_ms: int
    truth_stale_after_ms: int


_PROFILE_KEYS = {
    "schema",
    "id",
    "frequency_hz",
    "primary_kv",
    "circuit_file",
    "load_kw",
    "load_pf",
    "elements",
    "device",
    "solve_interval_ms",
    "request_timeout_ms",
    "truth_stale_after_ms",
}


def _positive_integer(value: object, name: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise ProfileError(f"{name} must be a positive integer")
    return value


def _profile_object(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ProfileError(f"cannot load profile {path}: {error}") from error
    if not isinstance(value, dict):
        raise ProfileError("profile must be a JSON object")
    if set(value) != _PROFILE_KEYS:
        missing = sorted(_PROFILE_KEYS - set(value))
        unknown = sorted(set(value) - _PROFILE_KEYS)
        raise ProfileError(
            f"profile fields mismatch (missing={missing}, unknown={unknown})"
        )
    return value


def load_profile(path: str | Path) -> EUProfile:
    """Load and validate the complete profile and its circuit file."""
    profile_path = Path(path)
    value = _profile_object(profile_path)
    if value["schema"] != 1 or isinstance(value["schema"], bool):
        raise ProfileError("schema must be 1")
    profile_id = value["id"]
    if not isinstance(profile_id, str) or not profile_id:
        raise ProfileError("id must be a non-empty string")
    frequency_hz = _positive_integer(value["frequency_hz"], "frequency_hz")
    primary_kv = _positive_integer(value["primary_kv"], "primary_kv")
    if frequency_hz != 50 or primary_kv != 20:
        raise ProfileError(
            "EU feeder profile requires frequency_hz=50 and primary_kv=20"
        )

    circuit_name = value["circuit_file"]
    if not isinstance(circuit_name, str) or not Path(circuit_name).is_absolute():
        raise ProfileError("circuit_file must be an absolute path")
    circuit_file = Path(circuit_name)
    if not circuit_file.is_file():
        raise ProfileError(f"circuit_file does not exist: {circuit_file}")

    load_kw = _positive_integer(value["load_kw"], "load_kw")
    load_pf = value["load_pf"]
    if (
        isinstance(load_pf, bool)
        or not isinstance(load_pf, (int, float))
        or not 0.0 < load_pf <= 1.0
    ):
        raise ProfileError("load_pf must be greater than 0 and no greater than 1")
    if load_kw != 1000 or load_pf != 1.0:
        raise ProfileError("EU reference feeder requires load_kw=1000 and load_pf=1.0")

    elements = value["elements"]
    expected_elements = {
        "breaker": "Line.Breaker",
        "load": "Load.FeederLoad",
        "load_bus": "loadbus",
    }
    if not isinstance(elements, dict) or elements != expected_elements:
        raise ProfileError(f"elements must be exactly {expected_elements}")
    device = value["device"]
    if (
        not isinstance(device, dict)
        or set(device) != {"id", "truth_url"}
        or device["id"] != "feeder-breaker"
        or not isinstance(device["truth_url"], str)
    ):
        raise ProfileError("device must identify feeder-breaker and its truth_url")
    parsed_url = urlsplit(device["truth_url"])
    try:
        url_port = parsed_url.port
    except ValueError as error:
        raise ProfileError("device.truth_url has an invalid port") from error
    if (
        parsed_url.scheme != "http"
        or not parsed_url.hostname
        or (url_port is not None and not 1 <= url_port <= 65535)
        or parsed_url.username is not None
        or parsed_url.password is not None
        or parsed_url.path not in ("", "/")
        or parsed_url.query
        or parsed_url.fragment
    ):
        raise ProfileError("device.truth_url must be an HTTP origin URL")

    solve_interval_ms = _positive_integer(
        value["solve_interval_ms"], "solve_interval_ms"
    )
    request_timeout_ms = _positive_integer(
        value["request_timeout_ms"], "request_timeout_ms"
    )
    truth_stale_after_ms = _positive_integer(
        value["truth_stale_after_ms"], "truth_stale_after_ms"
    )
    if request_timeout_ms >= truth_stale_after_ms:
        raise ProfileError("request_timeout_ms must be less than truth_stale_after_ms")

    return EUProfile(
        id=profile_id,
        frequency_hz=frequency_hz,
        primary_kv=primary_kv,
        circuit_file=circuit_file,
        load_kw=load_kw,
        load_pf=float(load_pf),
        breaker_element=elements["breaker"],
        load_element=elements["load"],
        load_bus=elements["load_bus"],
        device_id=device["id"],
        truth_url=device["truth_url"].rstrip("/"),
        solve_interval_ms=solve_interval_ms,
        request_timeout_ms=request_timeout_ms,
        truth_stale_after_ms=truth_stale_after_ms,
    )
