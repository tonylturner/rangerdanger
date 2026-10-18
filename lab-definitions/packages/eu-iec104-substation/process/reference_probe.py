"""Numerical operating-point evidence, not a substitute PLC.

Print actual reference cases and exit nonzero if an operating requirement fails.
Uses the active schema-3 feeder without altering its physical parameters.
"""

import json
import math
from pathlib import Path

import opendssdirect as dss

ROOT = Path(__file__).resolve().parent
SETPOINT_KV = 20.4
LOW_KV, HIGH_KV = round(SETPOINT_KV * .99, 3), round(SETPOINT_KV * 1.01, 3)


class Probe:
    def __init__(self, load_multiplier: float = 1.0, source_pu: float = 1.0):
        self.engine = dss.NewContext()
        # Redirect avoids changing the host process's working directory.
        self.engine.Text.Command(f'Redirect "{ROOT / "feeder.dss"}"')
        self.engine.Text.Command(f"Edit Vsource.Source pu={source_pu}")
        self.engine.Solution.LoadMult(load_multiplier)

    def sample(self, tap: int) -> dict:
        # Positive operator tap reduces the HV turns ratio, raising MV voltage.
        self.engine.Text.Command(
            f"Edit Transformer.T1 wdg=1 tap={1.0 - 0.0125 * tap}"
        )
        self.engine.Solution.Solve()
        if not self.engine.Solution.Converged():
            raise RuntimeError("Reference feeder did not converge")
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
    nominal_trace = demand_trace(1.0, 1.0)
    nominal = nominal_trace[0]
    steady = nominal_trace[-1]
    lower = Probe().sample(-1)
    raise_ = Probe().sample(1)
    triple_load = demand_trace(3.0, 1.0)
    low_source = demand_trace(1.0, 0.90)
    high_source = demand_trace(1.0, 1.10)
    checks = {
        "steady_state_within_half_percent": abs(steady["bus20_kv"] - SETPOINT_KV) <= .102,
        "nominal_steady_state_tap_plus_two": steady["tap"] == 2,
        "steady_state_in_plc_deadband": steady["in_band"],
        "positive_tap_raises_voltage": (
            lower["bus20_kv"] < nominal["bus20_kv"] < raise_["bus20_kv"]
        ),
        "triple_load_initially_out_of_band": not triple_load[0]["in_band"],
        "triple_load_returns_in_at_most_three_steps": (
            not triple_load[0]["in_band"]
            and triple_load[-1]["in_band"]
            and 1 <= len(triple_load) - 1 <= 3
        ),
        "low_source_reaches_plus_eight_below_band": (
            low_source[-1]["tap"] == 8 and low_source[-1]["bus20_kv"] < LOW_KV
        ),
        "high_source_settles_at_negative_tap_in_band": (
            high_source[-1]["tap"] < 0 and high_source[-1]["in_band"]
        ),
    }
    return {
        "schema": 3,
        "profile_enabled": False,
        "initial_tap": 0,
        "setpoint_kv": SETPOINT_KV,
        "configured_delay_s": 30,
        "motion_s": 2,
        "status": "passed" if all(checks.values()) else "blocked",
        "engine": dss.__version__,
        "circuit": "feeder.dss",
        "circuit_selected_by_profile": True,
        "nominal_load_kw": 2340.85,
        "band_kv": [LOW_KV, HIGH_KV],
        "checks": checks,
        "nominal": nominal,
        "nominal_trace": nominal_trace,
        "steady_state": steady,
        "tap_polarity": {"lower": lower, "raise": raise_},
        "triple_load": triple_load,
        "source_0_90": low_source,
        "source_1_10": high_source,
        "source_1_10_settle_tap": high_source[-1]["tap"],
        "scope": (
            "Converged static OpenDSS solves; not PLC timing or protocol evidence. "
            "EU realism contract rev 2; profile disabled; no physics tuning to meet acceptance values."
        ),
    }


if __name__ == "__main__":
    result = reference_cases()
    print(json.dumps(result, indent=2, allow_nan=False))
    raise SystemExit(0 if result["status"] == "passed" else 1)
