"""Numerical preflight, not a process implementation or substitute PLC.

Print actual reference cases and exit nonzero if an operating requirement fails.
This deliberately does not change the active schema-1 profile or circuit.
"""

import json
import math
from pathlib import Path

import opendssdirect as dss

ROOT = Path(__file__).resolve().parent
LOW_KV, HIGH_KV = 19.8, 20.2


class Probe:
    def __init__(self, load_multiplier: float = 1.0, source_pu: float = 1.0):
        self.engine = dss.NewContext()
        # Redirect avoids changing the host process's working directory.
        self.engine.Text.Command(f'Redirect "{ROOT / "reference-candidate.dss"}"')
        self.engine.Text.Command(f"Edit Vsource.Source pu={source_pu}")
        self.engine.Solution.LoadMult(load_multiplier)

    def sample(self, tap: int) -> dict:
        # Positive tap raises the LV winding voltage, not the HV winding ratio.
        self.engine.Text.Command(
            f"Edit Transformer.T1 wdg=2 tap={1.0 + 0.0125 * tap}"
        )
        self.engine.Solution.Solve()
        if not self.engine.Solution.Converged():
            raise RuntimeError("Reference candidate did not converge")
        self.engine.Circuit.SetActiveBus("bus20")
        phasors = self.engine.Bus.VLL()
        kv = sum(math.hypot(*phasors[i : i + 2]) for i in range(0, 6, 2)) / 3000
        if not math.isfinite(kv):
            raise RuntimeError("Nonfinite bus voltage")
        return {
            "tap": tap,
            "bus20_kv": round(kv, 8),
            "in_band": LOW_KV <= kv <= HIGH_KV,
        }


def demand_trace(load_multiplier: float, source_pu: float) -> list[dict]:
    """Quasi-static deadband decisions only; no claim of PLC/wire validation."""
    probe = Probe(load_multiplier, source_pu)
    tap = 0
    trace = []
    for _ in range(17):
        sample = probe.sample(tap)
        trace.append(sample)
        if sample["in_band"]:
            return trace
        direction = 1 if sample["bus20_kv"] < LOW_KV else -1
        target = tap + direction
        if not -8 <= target <= 8:
            return trace
        tap = target
    raise RuntimeError("Deadband decisions did not settle within the tap range")


def reference_cases() -> dict:
    nominal = Probe().sample(0)
    lower = Probe().sample(-1)
    raise_ = Probe().sample(1)
    double_load = demand_trace(2.0, 1.0)
    low_source = demand_trace(1.0, 0.90)
    high_source = demand_trace(1.0, 1.10)
    checks = {
        "nominal_within_half_percent": abs(nominal["bus20_kv"] - 20.0) <= 0.1,
        "positive_tap_raises_voltage": (
            lower["bus20_kv"] < nominal["bus20_kv"] < raise_["bus20_kv"]
        ),
        "double_load_initially_out_of_band": not double_load[0]["in_band"],
        "double_load_returns_in_at_most_three_steps": (
            not double_load[0]["in_band"]
            and double_load[-1]["in_band"]
            and 1 <= len(double_load) - 1 <= 3
        ),
        "low_source_reaches_plus_eight_out_of_band": (
            low_source[-1]["tap"] == 8 and not low_source[-1]["in_band"]
        ),
        "high_source_reaches_minus_eight_out_of_band": (
            high_source[-1]["tap"] == -8 and not high_source[-1]["in_band"]
        ),
    }
    return {
        "schema": 2,
        "status": "passed" if all(checks.values()) else "blocked",
        "candidate_only": True,
        "engine": dss.__version__,
        "circuit": "reference-candidate.dss",
        "circuit_selected_by_profile": False,
        "nominal_load_kw": 2340.85,
        "band_kv": [LOW_KV, HIGH_KV],
        "checks": checks,
        "nominal": nominal,
        "tap_polarity": {"lower": lower, "raise": raise_},
        "double_load": double_load,
        "double_load_forced_plus_one": Probe(load_multiplier=2.0).sample(1),
        "source_0_90": low_source,
        "source_1_10": high_source,
        "source_1_10_forced_minus_eight": Probe(source_pu=1.10).sample(-8),
        "scope": (
            "Converged static OpenDSS solves; not PLC timing or protocol evidence. "
            "No physics tuning after the failed operating-point preflight."
        ),
    }


if __name__ == "__main__":
    result = reference_cases()
    print(json.dumps(result, indent=2, allow_nan=False))
    raise SystemExit(0 if result["status"] == "passed" else 1)
