"""FastAPI surface and lifespan for the autonomous EU process mode."""

import asyncio
import logging
import os
from contextlib import asynccontextmanager

from eu_circuit import EUFeederSolver
from eu_profile import load_profile
from eu_controls import ActionError
from eu_models import ElectricalSnapshot, HealthSnapshot
from eu_state import EUProcessState
from eu_truth_loop import TruthSolveLoop
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

logger = logging.getLogger("opendss-sim.eu")


def create_eu_app(profile_path: str) -> FastAPI:
    """Load the configured profile and build an independent EU-only app."""
    profile = load_profile(profile_path)
    solver = EUFeederSolver(profile)
    state = EUProcessState(profile)
    solve_loop = TruthSolveLoop(profile, solver, state=state)

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        solver.compile_circuit()
        stop = asyncio.Event()
        worker = asyncio.create_task(solve_loop.run(stop), name="eu-truth-solve-loop")
        logger.info("EU OpenDSS process ready; waiting for device truth")
        try:
            yield
        finally:
            stop.set()
            await worker

    app = FastAPI(
        title="OpenDSS EU Process",
        description="Autonomous CIGRE-derived feeder driven by five schema-3 device truths",
        lifespan=lifespan,
    )
    app.state.eu_profile = profile
    app.state.eu_solver = solver
    app.state.eu_loop = solve_loop
    app.state.eu_state = state

    @app.post("/api/update-state")
    def reject_update_state() -> JSONResponse:
        return JSONResponse(
            content={"error": "not_supported_in_eu_mode"},
            status_code=405,
            headers={"Allow": "GET"},
        )

    # JSONResponse preserves the existing wire representation without coercion;
    # the required, nullable response shapes also document startup/partial truth.
    @app.get("/api/electrical", response_model=ElectricalSnapshot)
    def get_electrical() -> JSONResponse:
        return JSONResponse(content=state.electrical())

    @app.post("/api/process/actions")
    async def process_action(request: Request) -> JSONResponse:
        try:
            body = await request.json()
            peer = "unknown"
            if request.client is not None:
                host = request.client.host
                host = f"[{host}]" if ":" in host else host
                peer = f"{host}:{request.client.port}"
            # No zone map is configured for the process service; do not infer
            # a trusted zone or actor from user-supplied headers/body fields.
            result = await solve_loop.action(
                body, dict(peer=peer, protocol="lab", zone="unknown"))
        except (ActionError, ValueError) as error:
            reason = str(error) if isinstance(error, ActionError) else "invalid_json"
            return JSONResponse(content={"reason": reason}, status_code=400)
        return JSONResponse(content=result)

    @app.get("/api/audit")
    def get_audit(request: Request) -> JSONResponse:
        raw = request.query_params.get("after", "0")
        if not raw.isascii() or not raw.isdecimal() or len(raw) > 20:
            return JSONResponse(content={"reason": "invalid_after"}, status_code=400)
        after = int(raw)
        if after > (1 << 64) - 1:
            return JSONResponse(content={"reason": "invalid_after"}, status_code=400)
        return JSONResponse(content=state.audit_after(after))

    @app.get("/api/health", response_model=HealthSnapshot)
    def health() -> JSONResponse:
        return JSONResponse(content=state.health())

    return app


def create_configured_eu_app() -> FastAPI:
    """Select EU mode from its explicit profile environment variable."""
    profile_path = os.environ.get("OPENDSS_PROFILE")
    if profile_path is None:
        raise RuntimeError("OPENDSS_PROFILE must be set for EU mode")
    return create_eu_app(profile_path)
