"""Last solved EU process snapshot and delivery/health evidence."""

import copy
import threading
import time

from eu_controls import ProcessControls
from eu_profile import EUProfile


class EUProcessState:
    def __init__(self, profile: EUProfile, clock=time.monotonic):
        self._lock = threading.RLock()
        self._clock = clock
        self._stale_after = profile.truth_stale_after_ms / 1000
        self._last_truth_at = None
        self._truth_lost = True
        self._last_converged = None
        self._failures = 0
        self._pending = False
        self._latest = {
            "schema": 2, "profile_id": profile.id, "solver_epoch": None,
            "solve_seq": 0, "solved_at": None, "truth_stale": True,
            **ProcessControls().snapshot(), "tap": None,
            "buses": {k: {"kv": None, "live": None} for k in ("bus20", "s1", "s2", "s3")},
            "devices": {d.id: {"delivered": False, "solved_revision": None, "revision": None}
                        for d in profile.devices},
        }

    def set_epoch(self, epoch: str) -> None:
        with self._lock:
            self._latest["solver_epoch"] = epoch

    def controls_changed(self, controls: ProcessControls) -> None:
        with self._lock:
            self._latest.update(controls.snapshot())
            self._pending = True

    def truth_received(self) -> None:
        with self._lock:
            self._last_truth_at = self._clock()
            self._truth_lost = False

    def truth_lost(self) -> None:
        with self._lock:
            self._truth_lost = True
            self._failures += 1
            for device in self._latest["devices"].values():
                device["delivered"] = False

    def truth_stale(self) -> bool:
        with self._lock:
            return (self._truth_lost or self._last_truth_at is None
                    or self._clock() - self._last_truth_at >= self._stale_after)

    def solver_failed(self) -> None:
        with self._lock:
            self._last_converged = False
            self._failures += 1
            for device in self._latest["devices"].values():
                device["delivered"] = False

    def publish(self, result: dict, pending: bool = False) -> None:
        with self._lock:
            self._latest = copy.deepcopy(result)
            self._last_converged = True
            self._pending = pending
            good = not pending and all(v["delivered"] for v in result["devices"].values())
            self._failures = 0 if good else self._failures + 1

    def electrical(self) -> dict:
        with self._lock:
            result = copy.deepcopy(self._latest)
            result["truth_stale"] = self.truth_stale()
            return result

    def health(self) -> dict:
        with self._lock:
            degraded = (self.truth_stale() or self._last_converged is not True
                        or self._pending or not all(
                            d["delivered"] and d["solved_revision"] == d["revision"]
                            for d in self._latest["devices"].values()))
            return {
                "status": "degraded" if degraded else "ok", "service": "opendss-sim",
                "engine": "opendssdirect.py", "last_solved_at": self._latest["solved_at"],
                "last_converged": self._last_converged, "consecutive_failures": self._failures,
                "truth_stale": self.truth_stale(),
            }
