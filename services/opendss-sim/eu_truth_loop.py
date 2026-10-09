"""Process-net truth client, autonomous solve loop, and EU runtime state."""

import asyncio
import json
import logging
import math
import threading
import time
from collections.abc import Callable
from datetime import datetime
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen
from uuid import uuid4

from eu_circuit import EUFeederSolver
from eu_profile import EUProfile
from time_utils import utc_timestamp

logger = logging.getLogger("opendss-sim.eu")

_TRUTH_KEYS = {
    "schema",
    "device_id",
    "generation",
    "revision",
    "position",
    "remote_enabled",
    "lockout",
    "fault_active",
    "protection_tripped",
    "event_seq",
    "changed_at",
}


class TruthError(ValueError):
    """Raised for a failed or malformed device-truth response."""


def _timestamp(value: object, field: str) -> str:
    if not isinstance(value, str) or not value.endswith("Z"):
        raise TruthError(f"{field} must be an RFC3339 UTC timestamp")
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError as error:
        raise TruthError(f"{field} must be an RFC3339 UTC timestamp") from error
    if parsed.utcoffset() is None:
        raise TruthError(f"{field} must be an RFC3339 UTC timestamp")
    return value


def _nonnegative_integer(value: object, field: str, maximum: int) -> int:
    if (
        isinstance(value, bool)
        or not isinstance(value, int)
        or value < 0
        or value > maximum
    ):
        raise TruthError(f"{field} must be an unsigned integer")
    return value


def validate_truth(value: object, expected_device_id: str) -> dict:
    """Validate the strict truth shape before it reaches the solver."""
    if not isinstance(value, dict) or set(value) != _TRUTH_KEYS:
        raise TruthError("truth response fields do not match the device contract")
    if value["schema"] != 1 or isinstance(value["schema"], bool):
        raise TruthError("truth schema must be 1")
    if value["device_id"] != expected_device_id:
        raise TruthError("truth device_id does not match the profile")
    if value["position"] not in ("open", "closed"):
        raise TruthError("truth position must be open or closed")
    for field in ("remote_enabled", "lockout", "fault_active", "protection_tripped"):
        if not isinstance(value[field], bool):
            raise TruthError(f"truth {field} must be boolean")
    truth = dict(value)
    truth["generation"] = _nonnegative_integer(
        value["generation"], "generation", 2**32 - 1
    )
    truth["revision"] = _nonnegative_integer(value["revision"], "revision", 2**64 - 1)
    truth["event_seq"] = _nonnegative_integer(
        value["event_seq"], "event_seq", 2**64 - 1
    )
    truth["changed_at"] = _timestamp(value["changed_at"], "changed_at")
    return truth


class TruthClient:
    """Bounded synchronous JSON operations offloaded from the solver loop."""

    def __init__(
        self,
        profile: EUProfile,
        opener: Callable = urlopen,
    ) -> None:
        self.profile = profile
        self._opener = opener
        self.timeout_seconds = profile.request_timeout_ms / 1000.0

    def get_truth(self) -> dict:
        request = Request(f"{self.profile.truth_url}/api/truth", method="GET")
        try:
            with self._opener(request, timeout=self.timeout_seconds) as response:
                if response.status != 200:
                    raise TruthError(f"truth GET returned HTTP {response.status}")
                payload = json.loads(response.read())
        except (
            HTTPError,
            URLError,
            TimeoutError,
            OSError,
            json.JSONDecodeError,
        ) as error:
            raise TruthError(f"truth GET failed: {error}") from error
        return validate_truth(payload, self.profile.device_id)

    def post_measurements(self, payload: dict) -> tuple[int, dict]:
        request = Request(
            f"{self.profile.truth_url}/api/measurements",
            data=json.dumps(payload, allow_nan=False).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with self._opener(request, timeout=self.timeout_seconds) as response:
                return response.status, json.loads(response.read())
        except HTTPError as error:
            try:
                return error.code, json.loads(error.read())
            except (OSError, json.JSONDecodeError) as parse_error:
                raise TruthError(
                    f"invalid measurements error response: {parse_error}"
                ) from error
        except (URLError, TimeoutError, OSError, json.JSONDecodeError) as error:
            raise TruthError(f"measurements POST failed: {error}") from error


class EUProcessState:
    """Thread-safe last-good result and health for the EU process."""

    def __init__(
        self, stale_after_ms: int, clock: Callable[[], float] = time.monotonic
    ) -> None:
        self._lock = threading.Lock()
        self._stale_after = stale_after_ms / 1000.0
        self._clock = clock
        self._started_at = clock()
        self._last_truth_at: float | None = None
        self._latest: dict | None = None
        self._last_solved_at: str | None = None
        self._last_attempt_at: str | None = None
        self._last_converged: bool | None = None
        self._consecutive_failures = 0
        self._feedback_failed = False

    def truth_received(self) -> None:
        with self._lock:
            self._last_truth_at = self._clock()

    def truth_stale(self) -> bool:
        with self._lock:
            last_seen = self._last_truth_at
            reference = self._started_at if last_seen is None else last_seen
            return self._clock() - reference > self._stale_after

    def solver_failed(self, solved_at: str) -> None:
        with self._lock:
            self._last_attempt_at = solved_at
            self._last_converged = False
            self._consecutive_failures += 1
            self._feedback_failed = False

    def feedback_failed(self) -> None:
        with self._lock:
            self._last_attempt_at = utc_timestamp()
            self._consecutive_failures += 1
            self._feedback_failed = True

    def accepted(self, result: dict) -> None:
        with self._lock:
            self._latest = dict(result)
            self._last_solved_at = result["solved_at"]
            self._last_attempt_at = result["solved_at"]
            self._last_converged = True
            self._consecutive_failures = 0
            self._feedback_failed = False

    def electrical(self) -> dict | None:
        with self._lock:
            if self._latest is None:
                return None
            result = dict(self._latest)
            if self._last_converged is False:
                result["converged"] = False
        result["truth_stale"] = self.truth_stale()
        return result

    def health(self) -> dict:
        stale = self.truth_stale()
        with self._lock:
            degraded = (
                stale
                or self._latest is None
                or self._last_converged is False
                or self._feedback_failed
            )
            return {
                "status": "degraded" if degraded else "ok",
                "service": "opendss-sim",
                "engine": "opendssdirect.py",
                "last_solved_at": self._last_solved_at,
                "last_attempt_at": self._last_attempt_at,
                "last_converged": self._last_converged,
                "consecutive_failures": self._consecutive_failures,
                "truth_stale": stale,
            }


class TruthSolveLoop:
    """Single-owner GET → solve → POST loop, independent of other clients."""

    def __init__(
        self,
        profile: EUProfile,
        solver: EUFeederSolver,
        client: TruthClient | None = None,
        state: EUProcessState | None = None,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        self.profile = profile
        self.solver = solver
        self.client = client or TruthClient(profile)
        self.clock = clock
        self.state = state or EUProcessState(profile.truth_stale_after_ms, clock)
        self.solver_epoch = str(uuid4())
        self.solve_seq = 0

    async def run(self, stop: asyncio.Event) -> None:
        """Run periodic solves until the service lifespan ends."""
        interval = self.profile.solve_interval_ms / 1000.0
        next_tick = self.clock()
        while not stop.is_set():
            await self.run_tick()
            next_tick += interval
            delay = max(0.0, next_tick - self.clock())
            if delay == 0.0:
                next_tick = self.clock() + interval
                delay = interval
            try:
                await asyncio.wait_for(stop.wait(), timeout=delay)
            except TimeoutError:
                continue

    async def run_tick(self) -> None:
        """Run one truth read and, at most, one conflict re-read/solve."""
        try:
            truth = await asyncio.to_thread(self.client.get_truth)
        except TruthError as error:
            logger.warning("EU device truth unavailable: %s", error)
            return
        self.state.truth_received()
        for attempt in range(2):
            try:
                result = self.solver.solve(truth["position"] == "closed")
            except Exception:
                self.state.solver_failed(utc_timestamp())
                logger.exception("EU OpenDSS solve raised an exception")
                return
            if not isinstance(result, dict):
                self.state.solver_failed(utc_timestamp())
                logger.error("EU OpenDSS solver returned a non-object result")
                return
            if result.get("converged") is not True:
                self.state.solver_failed(result.get("solved_at", utc_timestamp()))
                logger.warning("EU OpenDSS solve did not converge")
                return

            self.solve_seq += 1
            payload = {
                "schema": 1,
                "device_id": self.profile.device_id,
                "generation": truth["generation"],
                "revision": truth["revision"],
                "solver_epoch": self.solver_epoch,
                "solve_seq": self.solve_seq,
                "solved_at": result["solved_at"],
                "current_a": result["current_a"],
                "voltage_kv_ll": result["load_voltage_kv_ll"],
            }
            if not all(
                math.isfinite(value) and value >= 0
                for value in (payload["current_a"], payload["voltage_kv_ll"])
            ):
                self.state.feedback_failed()
                logger.error("EU solver returned invalid measurements")
                return
            try:
                status, response = await asyncio.to_thread(
                    self.client.post_measurements, payload
                )
            except TruthError as error:
                self.state.feedback_failed()
                logger.warning("EU measurement feedback failed: %s", error)
                return
            if not isinstance(response, dict):
                self.state.feedback_failed()
                logger.warning("EU measurement feedback returned a non-object response")
                return

            if (
                status == 409
                and response == {"accepted": False, "reason": "stale_truth"}
                and attempt == 0
            ):
                try:
                    truth = await asyncio.to_thread(self.client.get_truth)
                except TruthError as error:
                    logger.warning(
                        "EU truth re-read failed after stale feedback: %s", error
                    )
                    return
                self.state.truth_received()
                continue
            if (
                status == 200
                and set(response) == {"accepted", "revision"}
                and response["accepted"] is True
                and isinstance(response["revision"], int)
                and not isinstance(response["revision"], bool)
                and response.get("revision") == truth["revision"]
            ):
                result.update(
                    schema=1,
                    profile_id=self.profile.id,
                    frequency_hz=self.profile.frequency_hz,
                    primary_kv=self.profile.primary_kv,
                    breaker=truth["position"],
                    solver_epoch=self.solver_epoch,
                    solve_seq=self.solve_seq,
                    truth_revision=truth["revision"],
                    truth_generation=truth["generation"],
                    truth_stale=False,
                )
                self.state.accepted(result)
                return

            self.state.feedback_failed()
            logger.warning(
                "EU measurement feedback rejected: HTTP %s (%s)",
                status,
                response.get("reason", "invalid response"),
            )
            return
