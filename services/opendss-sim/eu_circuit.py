"""One vector/one DSS solve for the CIGRE-derived EU teaching feeder."""

import math
import threading
import time
from datetime import datetime, timezone

import opendssdirect as dss
from eu_controls import ProcessControls
from eu_dynamics import LOAD_CATEGORIES, ProfileDynamics
from eu_profile import EUProfile


class EUFeederSolver:
    def __init__(self, profile: EUProfile, clock=time.time):
        self.profile = profile
        self._dss = dss.NewContext()
        self._lock = threading.Lock()
        self._compiled = False
        self._clock = clock
        self._last_solved_ms = None
        self.dynamics = ProfileDynamics(profile.profile, clock)
        self._base_loads = {}

    @property
    def solution_frequency_hz(self) -> float:
        with self._lock:
            return self._dss.Solution.Frequency()

    def compile_circuit(self) -> None:
        with self._lock:
            self._dss.Text.Command(f'Redirect "{self.profile.circuit_file}"')
            if not self._dss.Circuit.Name():
                raise RuntimeError("EU circuit did not compile")
            names = {name.lower() for name in self._dss.Circuit.AllElementNames()}
            if not all(v.lower() in names for v in self.profile.elements.values()):
                raise RuntimeError("EU profile maps a missing circuit element")
            buses = {name.lower() for name in self._dss.Circuit.AllBusNames()}
            if not all(v.lower() in buses for v in self.profile.buses.values()):
                raise RuntimeError("EU profile maps a missing bus")
            if self._dss.RegControls.Count() != 0:
                raise RuntimeError("EU circuit must not contain autonomous RegControl")
            self._dss.Solution.Frequency(self.profile.frequency_hz)
            self._dss.Text.Command("Set ControlMode=Off")
            load_names = {name.lower() for name in self._dss.Loads.AllNames()}
            if load_names != {name.lower() for name in LOAD_CATEGORIES}:
                raise RuntimeError("Every DSS load must have exactly one profile category")
            for name in LOAD_CATEGORIES:
                self._dss.Loads.Name(name)
                self._base_loads[name] = (self._dss.Loads.kW(), self._dss.Loads.kvar())
            self._compiled = True

    def solve(self, truths: dict[str, dict], controls: ProcessControls) -> dict:
        """Return pre-protection measurements; the loop owns any correction solve.

        F2 current is sampled here; the loop owns its protection and correction.
        No topological fault indicator competes with solved current.
        """
        with self._lock:
            if not self._compiled:
                raise RuntimeError("EU circuit not compiled")
            elements = self.profile.elements
            closed = {key: truths[key]["position"] == "closed"
                      for key in ("feeder-breaker", "rmu-1", "rmu-2")}
            closed["f2"] = not controls.f2_tripped
            for key, is_closed in closed.items():
                self._dss.Text.Command(f"{'Close' if is_closed else 'Open'} {elements[key]} 1")
            tap = truths["oltc-1"]["tap"]
            self._dss.Text.Command(f"Edit {elements['oltc-1']} wdg=1 tap={1 - .0125 * tap}")
            factors = self.dynamics.sample(controls.profile_enabled)
            for name, category in LOAD_CATEGORIES.items():
                kw, kvar = self._base_loads[name]
                factor = factors["load_factors"][category]
                self._dss.Text.Command(f"Edit Load.{name} kw={kw * factor} kvar={kvar * factor}")
            self._dss.Text.Command(
                f"Edit {elements['source']} pu={controls.source_pu * factors['source_pu']}")
            self._dss.Solution.LoadMult(controls.load_multiplier)
            targets = set() if controls.fault_section == "none" else {controls.fault_section.lower()}
            # Both RMUs' earth switch is on S2, including the normally-open tie.
            if any(truths[k]["earth_closed"] for k in ("rmu-1", "rmu-2")):
                targets.add("s2")
            for section in ("s1", "s2"):
                enabled = 'yes' if section in targets else 'no'
                self._dss.Text.Command(f"Edit {elements['fault_' + section]} enabled={enabled}")
            self._dss.Solution.Solve()
            # Correction solves can share a millisecond, and wall time can step
            # backwards. Preserve strict ordering within this solver epoch.
            solved_ms = int(self._clock() * 1000)
            if self._last_solved_ms is not None:
                solved_ms = max(solved_ms, self._last_solved_ms + 1)
            self._last_solved_ms = solved_ms
            solved_at = datetime.fromtimestamp(solved_ms / 1000, timezone.utc).isoformat(
                timespec="milliseconds").replace("+00:00", "Z")
            if not self._dss.Solution.Converged():
                return {"converged": False, "solved_at": solved_at}
            buses = {key: self._bus(name) for key, name in self.profile.buses.items()}
            sides = {
                "feeder-breaker": ("bus20", "s1"), "rmu-1": ("s1", "s2"),
                "rmu-2": ("s3", "s2"), "oltc-1": ("hv", "bus20"),
            }
            measurements = {}
            for key, (source, section) in sides.items():
                terminal = 2 if key == "oltc-1" else 1
                p, q = self._power(elements[key], terminal)
                if key == "oltc-1":
                    p, q = -p, -q
                elif not closed[key]:
                    p, q = 0.0, 0.0
                measurements[key] = {
                    "current_a": self._current(elements[key], terminal),
                    "voltage_kv_ll": buses[section]["kv"],
                    "source_kv_ll": buses[source]["kv"],
                    "section_live": buses[section]["live"],
                    "source_live": buses[source]["live"],
                    "p_mw": p, "q_mvar": q,
                }
            return {
                "converged": True, "solved_at": solved_at, "tap": tap,
                "buses": {key: buses[key] for key in ("bus20", "s1", "s2", "s3")},
                "measurements": measurements, "f2_current_a": self._current(elements["f2"], 1),
                "profile": factors,
                **controls.snapshot(),
            }

    def _bus(self, name: str) -> dict:
        self._dss.Circuit.SetActiveBus(name)
        values = self._dss.Bus.VLL()
        if len(values) != 6:
            raise RuntimeError(f"Expected a three-phase bus: {name}")
        kv = sum(math.hypot(*values[i:i+2]) for i in range(0, 6, 2)) / 3000
        if not math.isfinite(kv):
            raise RuntimeError("Nonfinite DSS voltage")
        return {"kv": kv, "live": kv >= 0.1}

    def _current(self, name: str, terminal: int) -> float:
        self._dss.Circuit.SetActiveElement(name)
        values = self._dss.CktElement.CurrentsMagAng()
        offset = (terminal - 1) * self._dss.CktElement.NumConductors() * 2
        value = sum(values[offset + 2*i] for i in range(3)) / 3
        if not math.isfinite(value) or value < 0:
            raise RuntimeError("Invalid DSS current")
        return value

    def _power(self, name: str, terminal: int) -> tuple[float, float]:
        self._dss.Circuit.SetActiveElement(name)
        values = self._dss.CktElement.Powers()
        offset = (terminal - 1) * self._dss.CktElement.NumConductors() * 2
        p, q = (sum(values[offset + component + 2*i] for i in range(3)) / 1000
                for component in (0, 1))
        if not math.isfinite(p) or not math.isfinite(q):
            raise RuntimeError("Nonfinite DSS power")
        return p, q
