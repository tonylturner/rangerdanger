"""OpenDSS circuit builder, power flow solver, and result extraction.

Uses opendssdirect.py (DSS-Extensions) to run real 3-phase unbalanced power
flow on a custom distribution feeder circuit modeled after the lab's substation
topology, with IEEE 13-bus line codes for realistic impedances.

Learn more about OpenDSS:
  https://www.epri.com/pages/sa/opendss
  https://dss-extensions.org/
  https://github.com/dss-extensions/OpenDSSDirect.py
"""

import logging
import math
import os
import random
import threading
from pathlib import Path

import opendssdirect as dss

logger = logging.getLogger("opendss-sim")

# Path to DSS circuit files
DSS_DIR = Path(__file__).parent / "dss"

# Nominal values
NOMINAL_KV_LL = 12.47
NOMINAL_KV_LN = NOMINAL_KV_LL / math.sqrt(3)
NOMINAL_V_SECONDARY = 120.0  # 120V base for per-unit display

# Tap calculation: ±10% range over 32 steps = 0.625% per step
TAP_STEP_PU = 0.00625

# Load variation: ±3% random walk around nominal to simulate real demand
# fluctuation on a distribution feeder. Each solve applies a small random
# multiplier to each load's kW, producing realistic drift in voltage,
# current, and power flow values between polling cycles.
LOAD_VARIATION_PCT = 0.03

# Nominal load values (must match substation_feeder.dss)
GENERAL_LOAD_KW = 500.0
CRITICAL_LOAD_KW = 200.0

# Switched capacitor bank rating (must match Capacitor.CapBank in the .dss and
# capbank-sim's kvar_rating). Used to credit the cap's VAR injection in the
# reported power factor when it's switched in.
CAPBANK_KVAR = 300.0


class FeederSolver:
    """Thread-safe wrapper around the OpenDSS engine for the substation feeder."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._compiled = False
        self._dss_file = str(DSS_DIR / "substation_feeder.dss")
        self._has_fault = False
        # Load multipliers — random-walk state that persists between solves
        self._gen_load_mult = 1.0
        self._crit_load_mult = 1.0

    def compile_circuit(self) -> None:
        """Compile the DSS circuit and run a validation solve."""
        with self._lock:
            if not os.path.exists(self._dss_file):
                raise FileNotFoundError(f"DSS circuit file not found: {self._dss_file}")

            dss.Text.Command(f'Compile "{self._dss_file}"')

            if dss.Circuit.Name() == "":
                raise RuntimeError("Failed to compile OpenDSS circuit")

            logger.info(
                "Circuit compiled: %s (%d buses, %d elements)",
                dss.Circuit.Name(),
                dss.Circuit.NumBuses(),
                dss.Circuit.NumCktElements(),
            )
            self._compiled = True

            # Validation solve (same thread, lock already held)
            dss.Solution.Solve()
            if not dss.Solution.Converged():
                raise RuntimeError("Initial power flow did not converge")

            sub_v = self._get_bus_voltage_pu("sourcebus") * NOMINAL_V_SECONDARY
            down_v = self._get_bus_voltage_pu("load_bus") * NOMINAL_V_SECONDARY
            cur = self._get_element_current("Line.Breaker")
            logger.info(
                "Initial solve OK: source=%.1fV, downstream=%.1fV, current=%.1fA",
                sub_v, down_v, cur,
            )

    def _update_load_multipliers(self) -> None:
        """Random-walk the load multipliers to simulate demand fluctuation.

        Each call nudges the multiplier by a small random step (±0.5% per cycle),
        clamped to ±LOAD_VARIATION_PCT of nominal. This produces realistic slow
        drift rather than jumpy noise.
        """
        step = 0.005  # max step per poll cycle
        self._gen_load_mult += random.uniform(-step, step)
        self._crit_load_mult += random.uniform(-step, step)
        # Clamp to variation band
        lo = 1.0 - LOAD_VARIATION_PCT
        hi = 1.0 + LOAD_VARIATION_PCT
        self._gen_load_mult = max(lo, min(hi, self._gen_load_mult))
        self._crit_load_mult = max(lo, min(hi, self._crit_load_mult))

    def solve(
        self,
        breaker_closed: bool,
        recloser_closed: bool,
        tap_position: int,
        fault_seen: bool,
        capbank_switched_in: bool,
    ) -> dict:
        """Run power flow with given device states and return electrical results."""
        with self._lock:
            if not self._compiled:
                raise RuntimeError("Circuit not compiled")

            # Reset switches to closed (default state from compile)
            dss.Text.Command("Close Line.Breaker 1")
            dss.Text.Command("Close Line.Recloser 1")

            # Remove any previous fault
            if self._has_fault:
                dss.Text.Command("Disable Fault.F1")
                self._has_fault = False

            # -- Apply demand variation --
            self._update_load_multipliers()
            gen_kw = GENERAL_LOAD_KW * self._gen_load_mult
            crit_kw = CRITICAL_LOAD_KW * self._crit_load_mult
            dss.Text.Command(f"Load.GeneralLoad.kw={gen_kw:.2f}")
            dss.Text.Command(f"Load.CriticalLoad.kw={crit_kw:.2f}")

            # -- Set breaker state --
            if not breaker_closed:
                dss.Text.Command("Open Line.Breaker 1")

            # -- Set recloser state --
            if not recloser_closed:
                dss.Text.Command("Open Line.Recloser 1")

            # -- Set capacitor bank state --
            if capbank_switched_in:
                dss.Text.Command("Enable Capacitor.CapBank")
            else:
                dss.Text.Command("Disable Capacitor.CapBank")

            # -- Set regulator tap --
            tap_pu = 1.0 + (tap_position * TAP_STEP_PU)
            dss.Text.Command(f"Transformer.VReg.wdg=2 tap={tap_pu:.6f}")

            # -- Add fault if requested --
            if fault_seen and breaker_closed and recloser_closed:
                if not self._has_fault:
                    dss.Text.Command(
                        "New Fault.F1 bus1=recloser_bus phases=3 r=0.01"
                    )
                else:
                    dss.Text.Command("Enable Fault.F1")
                self._has_fault = True

            # -- Solve power flow --
            dss.Solution.Solve()

            return self._extract_results(
                breaker_closed, recloser_closed, tap_position, fault_seen,
                capbank_switched_in,
            )

    def _extract_results(
        self,
        breaker_closed: bool,
        recloser_closed: bool,
        tap_position: int,
        fault_seen: bool,
        capbank_switched_in: bool,
    ) -> dict:
        """Extract electrical results from the solved circuit."""

        # If breaker is open, everything downstream is dead
        if not breaker_closed:
            return self._dead_feeder(
                breaker_closed, recloser_closed, tap_position, capbank_switched_in
            )

        # Substation bus voltage (always energized from source)
        sub_v = self._get_bus_voltage_pu("sourcebus") * NOMINAL_V_SECONDARY

        # Downstream voltage at load bus
        if recloser_closed:
            down_v = self._get_bus_voltage_pu("load_bus") * NOMINAL_V_SECONDARY
            crit_v = self._get_bus_voltage_pu("reg_bus") * NOMINAL_V_SECONDARY
            gen_energized = True
            crit_energized = True
        else:
            down_v = 0.0
            crit_v = 0.0
            gen_energized = False
            crit_energized = False

        # Feeder current (from breaker element)
        feeder_current = self._get_element_current("Line.Breaker")

        # Load power
        gen_kw = self._get_load_power("Load.GeneralLoad") if gen_energized else 0.0
        crit_kw = self._get_load_power("Load.CriticalLoad") if crit_energized else 0.0

        # Losses — Losses() returns [real_watts, reactive_vars]
        loss_vals = dss.Circuit.Losses()
        losses_kw = loss_vals[0] / 1000.0 if loss_vals else 0.0

        # Source power
        source_kw = self._get_source_power()

        # Power factor from total load, crediting the capacitor bank's VAR
        # injection when switched in (the cap supplies reactive power locally,
        # so the source sees less Q and the PF improves). Cap output scales with
        # voltage^2, so it tapers a little if the bus voltage sags.
        total_kw = gen_kw + crit_kw
        total_kvar = 0.0
        if gen_energized:
            total_kvar += gen_kw * math.tan(math.acos(0.9))
        if crit_energized:
            total_kvar += crit_kw * math.tan(math.acos(0.95))
        if capbank_switched_in and gen_energized:
            total_kvar -= CAPBANK_KVAR * (down_v / NOMINAL_V_SECONDARY) ** 2
        if total_kw > 0:
            pf = math.cos(math.atan2(total_kvar, total_kw))
        else:
            pf = 0.0

        # Fault current
        fault_current = 0.0
        if fault_seen and breaker_closed and recloser_closed:
            fault_current = feeder_current

        return {
            "substation_bus_voltage_kv": NOMINAL_KV_LL,
            "substation_bus_voltage_v": round(sub_v, 2),
            "downstream_voltage_v": round(down_v, 2),
            "critical_load_voltage_v": round(crit_v, 2),
            "feeder_current_a": round(feeder_current, 2),
            "general_load_energized": gen_energized,
            "critical_load_energized": crit_energized,
            "general_load_kw": round(gen_kw, 1),
            "critical_load_kw": round(crit_kw, 1),
            "breaker_closed": breaker_closed,
            "recloser_closed": recloser_closed,
            "regulator_tap": tap_position,
            "capbank_switched_in": capbank_switched_in,
            "total_losses_kw": round(abs(losses_kw), 1),
            "power_factor": round(pf, 3),
            "source_power_kw": round(source_kw, 1),
            "fault_current_a": round(fault_current, 1),
        }

    def _dead_feeder(
        self, breaker_closed: bool, recloser_closed: bool, tap_position: int,
        capbank_switched_in: bool = False,
    ) -> dict:
        """Return all-zero state for a de-energized feeder."""
        return {
            "substation_bus_voltage_kv": NOMINAL_KV_LL,
            "substation_bus_voltage_v": NOMINAL_V_SECONDARY,
            "downstream_voltage_v": 0.0,
            "critical_load_voltage_v": 0.0,
            "feeder_current_a": 0.0,
            "general_load_energized": False,
            "critical_load_energized": False,
            "general_load_kw": 0.0,
            "critical_load_kw": 0.0,
            "breaker_closed": breaker_closed,
            "recloser_closed": recloser_closed,
            "regulator_tap": tap_position,
            "capbank_switched_in": capbank_switched_in,
            "total_losses_kw": 0.0,
            "power_factor": 0.0,
            "source_power_kw": 0.0,
            "fault_current_a": 0.0,
        }

    def _get_bus_voltage_pu(self, bus_name: str) -> float:
        """Get average per-unit voltage magnitude at a bus."""
        dss.Circuit.SetActiveBus(bus_name)
        pu_vals = dss.Bus.puVmagAngle()
        if not pu_vals:
            return 0.0
        magnitudes = [pu_vals[i] for i in range(0, len(pu_vals), 2)]
        return sum(magnitudes) / len(magnitudes) if magnitudes else 0.0

    def _get_element_current(self, element_name: str) -> float:
        """Get average phase current magnitude for a circuit element."""
        dss.Circuit.SetActiveElement(element_name)
        currents = dss.CktElement.CurrentsMagAng()
        if not currents:
            return 0.0
        n_phases = dss.CktElement.NumPhases()
        magnitudes = [currents[i * 2] for i in range(n_phases)]
        return sum(magnitudes) / len(magnitudes) if magnitudes else 0.0

    def _get_load_power(self, element_name: str) -> float:
        """Get real power consumed by a load element (kW)."""
        dss.Circuit.SetActiveElement(element_name)
        powers = dss.CktElement.Powers()
        if not powers:
            return 0.0
        n_phases = dss.CktElement.NumPhases()
        kw = sum(powers[i * 2] for i in range(n_phases))
        return abs(kw)

    def _get_source_power(self) -> float:
        """Get total real power from the voltage source."""
        dss.Circuit.SetActiveElement("Vsource.source")
        powers = dss.CktElement.Powers()
        if not powers:
            return 0.0
        n_phases = dss.CktElement.NumPhases()
        kw = sum(powers[i * 2] for i in range(n_phases))
        return abs(kw)
