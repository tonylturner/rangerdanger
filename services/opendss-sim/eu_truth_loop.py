"""Autonomous concurrent GET-all → one vector solve → POST-all EU cycle."""

import asyncio
import logging
import time
from uuid import uuid4

from eu_circuit import EUFeederSolver
from eu_controls import ProcessControls
from eu_profile import EUProfile
from eu_state import EUProcessState
from eu_wire import TruthClient, acknowledgement, validate_truth

logger = logging.getLogger("opendss-sim.eu")


class TruthSolveLoop:
    def __init__(self, profile: EUProfile, solver: EUFeederSolver, client=None,
                 state=None, clock=time.monotonic):
        self.profile, self.solver = profile, solver
        self.client = client or TruthClient(profile)
        self.clock = clock
        self.state = state or EUProcessState(profile, clock)
        self.controls = ProcessControls(profile_enabled=profile.profile.enabled)
        self.solver_epoch = str(uuid4())
        self.solve_seq = 0
        self.state.set_epoch(self.solver_epoch)
        # One action or complete tick owns process controls throughout HTTP I/O.
        self._lock = asyncio.Lock()

    async def run(self, stop: asyncio.Event) -> None:
        interval = self.profile.solve_interval_ms / 1000
        next_tick = self.clock()
        while not stop.is_set():
            await self.run_tick()
            next_tick += interval
            delay = max(0, next_tick - self.clock())
            if delay == 0:
                next_tick = self.clock() + interval
                delay = interval
            try:
                await asyncio.wait_for(stop.wait(), timeout=delay)
            except TimeoutError:
                pass

    async def action(self, body: object) -> dict:
        async with self._lock:
            self.controls.action(body)
            self.state.controls_changed(self.controls)
            await self._tick()
            return self.state.electrical()

    async def run_tick(self) -> None:
        async with self._lock:
            await self._tick()

    async def _truths(self) -> dict | None:
        responses = await asyncio.gather(*(
            asyncio.to_thread(self.client.get_truth, d) for d in self.profile.devices
        ), return_exceptions=True)
        vector = {}
        for device, response in zip(self.profile.devices, responses):
            try:
                if isinstance(response, BaseException):
                    raise response
                vector[device.id] = validate_truth(response, device)
            except Exception as error:
                logger.warning("EU truth unavailable for %s: %s", device.id, error)
                self.state.truth_lost()
                return None
        self.state.truth_received()
        return vector

    async def _tick(self) -> None:
        for attempt in range(2):
            truths = await self._truths()
            if truths is None:
                return
            try:
                result = await asyncio.to_thread(self.solver.solve, truths, self.controls)
                if result.get("converged") is not True:
                    raise RuntimeError("OpenDSS did not converge")
            except Exception:
                logger.exception("EU solve failed")
                self.state.solver_failed()
                return
            self.solve_seq += 1
            payloads = {}
            for device in self.profile.devices:
                truth = truths[device.id]
                payloads[device.id] = {
                    "schema": 3, "device_id": device.id,
                    "generation": truth["generation"], "revision": truth["revision"],
                    "solver_epoch": self.solver_epoch, "solve_seq": self.solve_seq,
                    "solved_at": result["solved_at"], **result["measurements"][device.id],
                }
            # No failed/rejected device prevents healthy peers receiving this vector.
            responses = await asyncio.gather(*(
                asyncio.to_thread(self.client.post_measurements, d, payloads[d.id])
                for d in self.profile.devices
            ), return_exceptions=True)
            delivery = {}
            changed = False
            for device, response in zip(self.profile.devices, responses):
                revision = truths[device.id]["revision"]
                if isinstance(response, BaseException):
                    logger.warning("EU feedback failed for %s: %s", device.id, response)
                    status, body = 0, None
                else:
                    status, body = response
                record, retry = acknowledgement(status, body, revision)
                record.update({k: payloads[device.id][k] for k in ("current_a", "p_mw", "q_mvar")})
                delivery[device.id] = record
                changed |= retry
                if not record["delivered"]:
                    logger.warning("EU feedback rejected for %s: HTTP %s", device.id, status)
            snapshot = {
                "schema": 3, "profile_id": self.profile.id,
                "solver_epoch": self.solver_epoch, "solve_seq": self.solve_seq,
                "solved_at": result["solved_at"], "truth_stale": False,
                **{k: result[k] for k in ("fault_section", "f2_tripped", "load_multiplier",
                                         "source_pu", "tap", "buses", "profile")},
                "devices": delivery,
            }
            self.state.publish(snapshot, pending=changed)
            if not changed or attempt == 1:
                return
