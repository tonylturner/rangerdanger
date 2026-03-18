"""Pydantic models matching the existing opendss-sim API contract."""

from pydantic import BaseModel


class RelayState(BaseModel):
    breaker_closed: bool = True


class RecloserState(BaseModel):
    closed: bool = True
    reclose_enabled: bool = True
    fault_seen: bool = False


class RegulatorState(BaseModel):
    tap_position: int = 0


class DeviceStates(BaseModel):
    relay: RelayState = RelayState()
    recloser: RecloserState = RecloserState()
    regulator: RegulatorState = RegulatorState()


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
    # Bonus fields from real power flow
    total_losses_kw: float = 0.0
    power_factor: float = 0.0
    source_power_kw: float = 0.0
    fault_current_a: float = 0.0
