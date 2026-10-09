"""Deterministic OpenDSS model for the EU single-feeder process."""

import cmath
import logging
import math
import threading

import opendssdirect as dss
from eu_profile import EUProfile
from time_utils import utc_timestamp

logger = logging.getLogger("opendss-sim.eu")


class EUFeederSolver:
    """Serialize access to the shared OpenDSS engine for the EU feeder."""

    def __init__(self, profile: EUProfile) -> None:
        self.profile = profile
        self._lock = threading.Lock()
        self._dss = dss.NewContext()
        self._compiled = False

    @property
    def solution_frequency_hz(self) -> float:
        """Return the frequency currently configured in this engine context."""
        with self._lock:
            return self._dss.Solution.Frequency()

    def compile_circuit(self) -> None:
        """Compile the configured circuit and enforce its solution frequency."""
        with self._lock:
            self._dss.Text.Command(f'Compile "{self.profile.circuit_file}"')
            if not self._dss.Circuit.Name():
                raise RuntimeError("Failed to compile EU OpenDSS circuit")
            self._dss.Solution.Frequency(self.profile.frequency_hz)
            if self._dss.Solution.Frequency() != self.profile.frequency_hz:
                raise RuntimeError("OpenDSS solution frequency is not 50 Hz")
            self._dss.Text.Command(
                f"{self.profile.load_element}.kW={self.profile.load_kw}"
            )
            self._dss.Text.Command(
                f"{self.profile.load_element}.PF={self.profile.load_pf}"
            )
            self._compiled = True
            logger.info(
                "EU circuit compiled: %s (%d buses, %d elements, %.0f Hz)",
                self._dss.Circuit.Name(),
                self._dss.Circuit.NumBuses(),
                self._dss.Circuit.NumCktElements(),
                self._dss.Solution.Frequency(),
            )

    def solve(self, breaker_closed: bool) -> dict:
        """Solve the state from device truth and return measurements from DSS."""
        with self._lock:
            if not self._compiled:
                raise RuntimeError("EU circuit not compiled")
            self._dss.Solution.Frequency(self.profile.frequency_hz)
            if breaker_closed:
                self._dss.Text.Command(f"Close {self.profile.breaker_element} 1")
            else:
                self._dss.Text.Command(f"Open {self.profile.breaker_element} 1")
            self._dss.Solution.Solve()
            solved_at = utc_timestamp()
            if not self._dss.Solution.Converged():
                return {"converged": False, "solved_at": solved_at}

            source_voltage = self._bus_voltage_kv("sourcebus")
            load_voltage = self._bus_voltage_kv(self.profile.load_bus)
            current = self._element_current(self.profile.breaker_element)
            load_kw = self._load_power_kw(self.profile.load_element)
            return {
                "source_voltage_kv_ll": round(source_voltage, 4),
                "load_voltage_kv_ll": round(load_voltage, 4),
                "current_a": round(current, 4),
                "load_kw": round(load_kw, 4),
                "energized": load_voltage >= 0.1,
                "converged": True,
                "solved_at": solved_at,
            }

    def _bus_voltage_kv(self, bus_name: str) -> float:
        self._dss.Circuit.SetActiveBus(bus_name)
        voltages = self._dss.Bus.VMagAngle()
        phases = self._dss.Bus.NumNodes()
        if not voltages or phases < 3:
            return 0.0
        phase_phasors = [
            cmath.rect(voltages[index * 2], math.radians(voltages[index * 2 + 1]))
            for index in range(phases)
        ]
        line_voltages = [
            abs(phase_phasors[index] - phase_phasors[(index + 1) % phases])
            for index in range(phases)
        ]
        return sum(line_voltages) / len(line_voltages) / 1000.0

    def _element_current(self, element_name: str) -> float:
        self._dss.Circuit.SetActiveElement(element_name)
        currents = self._dss.CktElement.CurrentsMagAng()
        phases = self._dss.CktElement.NumPhases()
        if not currents or phases == 0:
            return 0.0
        return sum(currents[index * 2] for index in range(phases)) / phases

    def _load_power_kw(self, element_name: str) -> float:
        self._dss.Circuit.SetActiveElement(element_name)
        powers = self._dss.CktElement.Powers()
        phases = self._dss.CktElement.NumPhases()
        if not powers or phases == 0:
            return 0.0
        return abs(sum(powers[index * 2] for index in range(phases)))
