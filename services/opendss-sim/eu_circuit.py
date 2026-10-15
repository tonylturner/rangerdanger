"""One vector/one DSS solve for the CIGRE-derived EU teaching feeder."""

import math
import threading

import opendssdirect as dss
from eu_controls import ProcessControls
from eu_profile import EUProfile
from time_utils import utc_timestamp

# These edges describe section connectivity, not a substitute power-flow model.
EDGES = {
    "feeder-breaker": ("bus20", "s1"), "rmu-1": ("s1", "s2"),
    "rmu-2": ("s3", "s2"), "f2": ("bus20", "s3"),
}


def fault_paths(closed: dict[str, bool], targets: set[str]) -> set[str]:
    """Union of all simple energised source-to-fault paths, including a ring."""
    result = set()

    def walk(bus: str, seen: set[str], path: set[str]) -> None:
        if bus in targets:
            result.update(path)
        for edge, (a, b) in EDGES.items():
            if not closed[edge] or bus not in (a, b):
                continue
            peer = b if bus == a else a
            if peer not in seen:
                walk(peer, seen | {peer}, path | {edge})

    walk("bus20", {"bus20"}, set())
    return result


def reachable(closed: dict[str, bool]) -> set[str]:
    live = {"bus20"}
    for _ in EDGES:
        for edge, (a, b) in EDGES.items():
            if closed[edge] and (a in live or b in live):
                live.update((a, b))
    return live


class EUFeederSolver:
    def __init__(self, profile: EUProfile):
        self.profile = profile
        self._dss = dss.NewContext()
        self._lock = threading.Lock()
        self._compiled = False

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
            self._compiled = True

    def solve(self, truths: dict[str, dict], controls: ProcessControls) -> dict:
        """Return pre-protection measurements; the loop owns any correction solve.

        The F2 trip is reported, not silently solved a second time here. Devices
        must see this vector's fault passage before its protection changes state.
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
            self._dss.Text.Command(f"Edit {elements['oltc-1']} wdg=2 tap={1 + .0125 * tap}")
            self._dss.Text.Command(f"Edit {elements['source']} pu={controls.source_pu}")
            self._dss.Solution.LoadMult(controls.load_multiplier)
            targets = set() if controls.fault_section == "none" else {controls.fault_section.lower()}
            # Both RMUs' earth switch is on S2, including the normally-open tie.
            if "s2" in reachable(closed) and any(truths[k]["earth_closed"] for k in ("rmu-1", "rmu-2")):
                targets.add("s2")
            for section in ("s1", "s2"):
                enabled = 'yes' if section in targets else 'no'
                self._dss.Text.Command(f"Edit {elements['fault_' + section]} enabled={enabled}")
            paths = fault_paths(closed, targets)
            self._dss.Solution.Solve()
            solved_at = utc_timestamp()
            if not self._dss.Solution.Converged():
                return {"converged": False, "solved_at": solved_at}
            buses = {key: self._bus(name) for key, name in self.profile.buses.items()}
            sides = {
                "feeder-breaker": ("bus20", "s1"), "rmu-1": ("s1", "s2"),
                "rmu-2": ("s3", "s2"), "oltc-1": ("hv", "bus20"),
            }
            measurements = {}
            for key, (source, section) in sides.items():
                measurements[key] = {
                    "current_a": self._current(elements[key], 2 if key == "oltc-1" else 1),
                    "voltage_kv_ll": buses[section]["kv"],
                    "source_kv_ll": buses[source]["kv"],
                    "section_live": buses[section]["live"],
                    "source_live": buses[source]["live"],
                    "fault_current": bool(paths) if key == "oltc-1" else key in paths,
                }
            return {
                "converged": True, "solved_at": solved_at, "tap": tap,
                "buses": {key: buses[key] for key in ("bus20", "s1", "s2", "s3")},
                "measurements": measurements, "f2_trip": "f2" in paths,
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
