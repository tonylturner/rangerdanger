"""Pydantic models matching the existing opendss-sim API contract.

RTAC sends full device state (with extra fields like lockout, comms_ok, etc.)
so all sub-models must allow extra fields.
"""

from pydantic import BaseModel, ConfigDict


class RelayState(BaseModel):
    model_config = ConfigDict(extra="ignore")
    breaker_closed: bool = True


class RecloserState(BaseModel):
    model_config = ConfigDict(extra="ignore")
    closed: bool = True
    reclose_enabled: bool = True
    fault_seen: bool = False


class RegulatorState(BaseModel):
    model_config = ConfigDict(extra="ignore")
    tap_position: int = 0


class CapbankState(BaseModel):
    model_config = ConfigDict(extra="ignore")
    switched_in: bool = False


class LabState(BaseModel):
    """Training-only load override from the HMI Load Simulator. When `active`,
    the feeder loads are driven by these slider values instead of the default
    fixed loads — a bonus exploration control, off by default so it never
    changes baseline RangerDanger behavior."""
    model_config = ConfigDict(extra="ignore")
    active: bool = False
    general_load_pct: float = 0.0   # 0-100, scaled to GENERAL_LOAD_MAX_KW
    critical_load_pct: float = 0.0  # 0-100, scaled to CRITICAL_LOAD_MAX_KW
    power_factor: float = 1.0       # 0.80-1.00, applied to both loads


class DeviceStates(BaseModel):
    model_config = ConfigDict(extra="ignore")
    relay: RelayState = RelayState()
    recloser: RecloserState = RecloserState()
    regulator: RegulatorState = RegulatorState()
    capbank: CapbankState = CapbankState()
    lab: LabState = LabState()


class ElectricalResponse(BaseModel):
    substation_bus_voltage_kv: float
    substation_bus_voltage_v: float
    downstream_voltage_v: float
    critical_load_voltage_v: float
    feeder_current_a: float
    general_load_energized: bool
    critical_load_energized: bool
    general_load_kw: float
    critical_load_kw: float
    breaker_closed: bool
    recloser_closed: bool
    regulator_tap: int
    capbank_switched_in: bool = False
    # Bonus fields from real power flow
    total_losses_kw: float = 0.0
    power_factor: float = 0.0
    source_power_kw: float = 0.0
    fault_current_a: float = 0.0
