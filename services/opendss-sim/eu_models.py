"""EU response shapes: unknown observations are required keys with null values."""

from typing import Literal

from typing_extensions import TypedDict


class DeliveryEvidence(TypedDict):
    delivered: bool
    solved_revision: int | None
    revision: int | None


class DeviceSnapshot(DeliveryEvidence):
    current_a: float | None
    p_mw: float | None
    q_mvar: float | None


class BusSnapshot(TypedDict):
    kv: float | None
    live: bool | None


class ProfileSnapshot(TypedDict):
    enabled: bool
    load_factors: dict[str, float | None]
    source_pu: float | None


class ElectricalSnapshot(TypedDict):
    schema: Literal[3]
    profile_id: str
    solver_epoch: str | None
    solve_seq: int
    solved_at: str | None
    truth_stale: bool
    fault_section: str
    load_multiplier: float
    source_pu: float
    tap: int | None
    f2_tripped: bool | None
    profile: ProfileSnapshot
    buses: dict[str, BusSnapshot]
    devices: dict[str, DeviceSnapshot]


class HealthSnapshot(TypedDict):
    status: Literal["degraded", "ok"]
    service: Literal["opendss-sim"]
    engine: Literal["opendssdirect.py"]
    last_solved_at: str | None
    last_converged: bool | None
    consecutive_failures: int
    truth_stale: bool
