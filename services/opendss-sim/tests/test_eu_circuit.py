import json
from pathlib import Path

from eu_circuit import EUFeederSolver
from eu_profile import load_profile

SERVICE_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = SERVICE_DIR.parents[1]
PROCESS_DIR = REPO_ROOT / "lab-definitions/packages/eu-iec104-substation/process"


def test_eu_feeder_reference_values_and_engine_frequency(tmp_path: Path) -> None:
    config = json.loads((PROCESS_DIR / "profile.json").read_text(encoding="utf-8"))
    config["circuit_file"] = str(PROCESS_DIR / "feeder.dss")
    profile_path = tmp_path / "profile.json"
    profile_path.write_text(json.dumps(config), encoding="utf-8")
    solver = EUFeederSolver(load_profile(profile_path))
    solver.compile_circuit()

    closed = solver.solve(breaker_closed=True)
    assert closed["converged"] is True
    assert 27.4 <= closed["current_a"] <= 30.4
    assert 19.5 <= closed["load_voltage_kv_ll"] <= 20.5
    assert 970 <= closed["load_kw"] <= 1030
    assert 19.5 <= closed["source_voltage_kv_ll"] <= 20.5
    assert closed["energized"] is True
    assert solver.solution_frequency_hz == 50

    opened = solver.solve(breaker_closed=False)
    assert opened["converged"] is True
    assert opened["current_a"] < 0.1
    assert opened["load_voltage_kv_ll"] < 0.1
    assert opened["load_kw"] < 0.1
    assert opened["energized"] is False
    assert solver.solution_frequency_hz == 50
