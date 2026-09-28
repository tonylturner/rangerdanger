"""Track the last good electrical state and solve-attempt health."""

import logging
import threading
from collections.abc import Callable

from circuit import PowerFlowDidNotConverge
from time_utils import utc_timestamp

logger = logging.getLogger("opendss-sim")


class RuntimeState:
    """Serialize solve attempts and expose the last successful state."""

    def __init__(self) -> None:
        self._attempt_lock = threading.Lock()
        self._state_lock = threading.Lock()
        self._latest_state: dict = {}
        self._last_solved_at: str | None = None
        self._last_attempt_at: str | None = None
        self._last_converged: bool | None = None
        self._consecutive_failures = 0

    def run_attempt(self, solve: Callable[[], dict]) -> tuple[dict, int]:
        """Run one solve, updating health without replacing good data on failure."""
        with self._attempt_lock:
            try:
                result = solve()
            except PowerFlowDidNotConverge as error:
                with self._state_lock:
                    self._last_attempt_at = error.solved_at
                    self._last_converged = False
                    self._consecutive_failures += 1
                return {
                    "error": "power flow did not converge",
                    "converged": False,
                    "solved_at": error.solved_at,
                }, 503
            except Exception as error:
                attempt_at = utc_timestamp()
                with self._state_lock:
                    self._last_attempt_at = attempt_at
                    self._last_converged = False
                    self._consecutive_failures += 1
                logger.exception("Power flow solve failed at %s", attempt_at)
                return {
                    "error": str(error),
                    "converged": False,
                    "solved_at": attempt_at,
                }, 500

            with self._state_lock:
                self._latest_state = result.copy()
                self._last_solved_at = result["solved_at"]
                self._last_attempt_at = result["solved_at"]
                self._last_converged = result["converged"]
                self._consecutive_failures = 0
            return result.copy(), 200

    def latest_state(self) -> dict:
        """Return a copy of the last successful electrical state."""
        with self._state_lock:
            return self._latest_state.copy()

    def health(self) -> dict:
        """Return solve freshness and consecutive failure counters."""
        with self._state_lock:
            return {
                "status": "degraded" if self._last_converged is False else "ok",
                "service": "opendss-sim",
                "engine": "opendssdirect.py",
                "last_solved_at": self._last_solved_at,
                "last_attempt_at": self._last_attempt_at,
                "last_converged": self._last_converged,
                "consecutive_failures": self._consecutive_failures,
            }
