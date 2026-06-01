"""OpenDSS physics engine — FastAPI service replacing the Go stub.

Runs real 3-phase unbalanced power flow via opendssdirect.py on a custom
distribution feeder circuit. Same REST API contract as the original Go service.
"""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import JSONResponse

from circuit import FeederSolver
from models import DeviceStates, ElectricalResponse

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(name)s] %(levelname)s %(message)s",
)
logger = logging.getLogger("opendss-sim")

solver = FeederSolver()

# Cache latest electrical state for GET /api/electrical
_latest_state: dict = {}


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Compile the OpenDSS circuit at startup."""
    logger.info("Compiling OpenDSS circuit...")
    solver.compile_circuit()
    # Store initial state
    global _latest_state
    _latest_state = solver.solve(
        breaker_closed=True,
        recloser_closed=True,
        tap_position=0,
        fault_seen=False,
        capbank_switched_in=False,
    )
    logger.info("OpenDSS physics engine ready")
    yield


app = FastAPI(
    title="OpenDSS Physics Engine",
    description="Real power flow calculations for RangerDanger substation lab",
    lifespan=lifespan,
)


@app.post("/api/update-state")
def update_state(devices: DeviceStates) -> JSONResponse:
    """Receive device states from RTAC, run power flow, return electrical state."""
    global _latest_state

    result = solver.solve(
        breaker_closed=devices.relay.breaker_closed,
        recloser_closed=devices.recloser.closed,
        tap_position=devices.regulator.tap_position,
        fault_seen=devices.recloser.fault_seen,
        capbank_switched_in=devices.capbank.switched_in,
        lab=devices.lab.model_dump(),
    )

    _latest_state = result
    return JSONResponse(content=result)


@app.get("/api/electrical")
def get_electrical() -> JSONResponse:
    """Return current electrical state (last solved)."""
    return JSONResponse(content=_latest_state)


@app.get("/api/health")
def health() -> JSONResponse:
    """Health check."""
    return JSONResponse(
        content={"status": "ok", "service": "opendss-sim", "engine": "opendssdirect.py"}
    )


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8080)
