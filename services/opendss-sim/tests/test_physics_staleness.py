from datetime import datetime
import itertools
import json
import logging
import re

import pytest

import circuit
import main
from circuit import FeederSolver, PowerFlowDidNotConverge
from models import DeviceStates
from runtime_state import RuntimeState


def assert_timestamp(value: str) -> None:
    assert re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z", value)
    datetime.fromisoformat(value.replace("Z", "+00:00"))


@pytest.fixture
def compiled_solver() -> FeederSolver:
    solver = FeederSolver()
    solver.compile_circuit()
    return solver


def test_runtime_and_dead_feeder_results_carry_solve_metadata(
    compiled_solver: FeederSolver,
) -> None:
    result = compiled_solver.solve(True, True, 0, False, False)
    assert result["converged"] is True
    assert_timestamp(result["solved_at"])

    dead_feeder = compiled_solver.solve(False, True, 0, False, False)
    assert dead_feeder["converged"] is True
    assert dead_feeder["downstream_voltage_v"] == 0.0
    assert_timestamp(dead_feeder["solved_at"])


def test_fault_can_be_injected_cleared_and_injected_again(
    compiled_solver: FeederSolver,
) -> None:
    first_fault = compiled_solver.solve(True, True, 0, True, False)
    cleared = compiled_solver.solve(True, True, 0, False, False)
    second_fault = compiled_solver.solve(True, True, 0, True, False)

    assert first_fault["converged"] is True
    assert first_fault["fault_current_a"] > 0
    assert cleared["converged"] is True
    assert cleared["fault_current_a"] == 0
    assert second_fault["converged"] is True
    assert second_fault["fault_current_a"] > 0


def test_nonconvergence_returns_503_preserves_last_good_and_degrades_health(
    compiled_solver: FeederSolver, monkeypatch: pytest.MonkeyPatch
) -> None:
    runtime_state = RuntimeState()
    monkeypatch.setattr(main, "solver", compiled_solver)
    monkeypatch.setattr(main, "runtime_state", runtime_state)

    initial = main.update_state(DeviceStates())
    assert initial.status_code == 200
    good_state = json.loads(initial.body)
    assert good_state["converged"] is True
    assert_timestamp(good_state["solved_at"])

    # OpenDSSDirect freezes method assignments on the engine object, so patch
    # its wrapper method on the type to force this solve's convergence result.
    monkeypatch.setattr(type(circuit.dss.Solution), "Converged", lambda self: False)
    failed = main.update_state(DeviceStates())
    failure_body = json.loads(failed.body)
    assert failed.status_code == 503
    assert failure_body["error"] == "power flow did not converge"
    assert failure_body["converged"] is False
    assert_timestamp(failure_body["solved_at"])
    assert main.get_electrical().body == initial.body

    health = json.loads(main.health().body)
    assert health["status"] == "degraded"
    assert health["last_solved_at"] == good_state["solved_at"]
    assert health["last_attempt_at"] == failure_body["solved_at"]
    assert health["last_converged"] is False
    assert health["consecutive_failures"] == 1


def test_unexpected_solver_exception_returns_500_and_recovers(
    monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    class SequenceSolver:
        calls = 0

        def solve(self, **_kwargs: object) -> dict:
            self.calls += 1
            if self.calls == 2:
                raise RuntimeError("native solver failure")
            return {
                "converged": True,
                "solved_at": f"2026-09-28T12:00:0{self.calls}.000Z",
                "attempt": self.calls,
            }

    runtime_state = RuntimeState()
    monkeypatch.setattr(main, "solver", SequenceSolver())
    monkeypatch.setattr(main, "runtime_state", runtime_state)
    caplog.set_level(logging.ERROR, logger="opendss-sim")

    initial = main.update_state(DeviceStates())
    assert initial.status_code == 200
    good_state = json.loads(initial.body)

    failed = main.update_state(DeviceStates())
    failure_body = json.loads(failed.body)
    assert failed.status_code == 500
    assert failure_body["error"] == "native solver failure"
    assert failure_body["converged"] is False
    assert_timestamp(failure_body["solved_at"])
    assert main.get_electrical().body == initial.body

    health = json.loads(main.health().body)
    assert health["status"] == "degraded"
    assert health["last_solved_at"] == good_state["solved_at"]
    assert health["last_attempt_at"] == failure_body["solved_at"]
    assert health["last_converged"] is False
    assert health["consecutive_failures"] == 1
    assert any(
        record.levelno == logging.ERROR
        and record.exc_info is not None
        and str(record.exc_info[1]) == "native solver failure"
        for record in caplog.records
    )

    recovered = main.update_state(DeviceStates())
    recovered_state = json.loads(recovered.body)
    assert recovered.status_code == 200
    assert recovered_state["converged"] is True
    assert main.get_electrical().body == recovered.body
    health = json.loads(main.health().body)
    assert health["status"] == "ok"
    assert health["last_solved_at"] == recovered_state["solved_at"]
    assert health["last_attempt_at"] == recovered_state["solved_at"]
    assert health["last_converged"] is True
    assert health["consecutive_failures"] == 0


def test_health_counters_reset_after_recovery() -> None:
    state = RuntimeState()
    solved = {
        "converged": True,
        "solved_at": "2026-09-27T14:03:11.412Z",
        "value": 1,
    }
    state.run_attempt(lambda: solved)

    for timestamp, expected_failures in (
        ("2026-09-27T14:03:13.412Z", 1),
        ("2026-09-27T14:03:15.412Z", 2),
    ):
        def fail(timestamp: str = timestamp) -> dict:
            raise PowerFlowDidNotConverge(timestamp)

        result, status_code = state.run_attempt(fail)
        assert status_code == 503
        assert result["converged"] is False
        health = state.health()
        assert health["status"] == "degraded"
        assert health["last_solved_at"] == solved["solved_at"]
        assert health["last_attempt_at"] == timestamp
        assert health["last_converged"] is False
        assert health["consecutive_failures"] == expected_failures

    result, status_code = state.run_attempt(
        lambda: {"converged": True, "solved_at": "2026-09-27T14:03:17.412Z"}
    )
    assert status_code == 200
    assert result["converged"] is True
    health = state.health()
    assert health["status"] == "ok"
    assert health["last_solved_at"] == "2026-09-27T14:03:17.412Z"
    assert health["last_attempt_at"] == "2026-09-27T14:03:17.412Z"
    assert health["last_converged"] is True
    assert health["consecutive_failures"] == 0


@pytest.mark.parametrize(
    (
        "breaker_closed",
        "recloser_closed",
        "fault_seen",
        "capbank_switched_in",
        "tap_position",
    ),
    list(
        itertools.product(
            (False, True),
            (False, True),
            (False, True),
            (False, True),
            (-16, 16),
        )
    ),
    ids=lambda value: str(value),
)
def test_real_solver_device_combination_converges(
    compiled_solver: FeederSolver,
    breaker_closed: bool,
    recloser_closed: bool,
    fault_seen: bool,
    capbank_switched_in: bool,
    tap_position: int,
) -> None:
    result = compiled_solver.solve(
        breaker_closed=breaker_closed,
        recloser_closed=recloser_closed,
        tap_position=tap_position,
        fault_seen=fault_seen,
        capbank_switched_in=capbank_switched_in,
    )
    assert result["converged"] is True
    assert_timestamp(result["solved_at"])


def test_load_simulator_override_sweep_converges(
    compiled_solver: FeederSolver,
) -> None:
    case_count = 0
    for (
        general_load_pct,
        critical_load_pct,
        power_factor,
        tap_position,
        capbank_switched_in,
        recloser_closed,
        fault_seen,
    ) in itertools.product(
        (0, 50, 100),
        (0, 50, 100),
        (0.80, 1.0),
        (-16, 0, 16),
        (False, True),
        (False, True),
        (False, True),
    ):
        result = compiled_solver.solve(
            breaker_closed=True,
            recloser_closed=recloser_closed,
            tap_position=tap_position,
            fault_seen=fault_seen,
            capbank_switched_in=capbank_switched_in,
            lab={
                "active": True,
                "general_load_pct": general_load_pct,
                "critical_load_pct": critical_load_pct,
                "power_factor": power_factor,
            },
        )
        assert result["converged"] is True, {
            "general_load_pct": general_load_pct,
            "critical_load_pct": critical_load_pct,
            "power_factor": power_factor,
            "tap_position": tap_position,
            "capbank_switched_in": capbank_switched_in,
            "recloser_closed": recloser_closed,
            "fault_seen": fault_seen,
        }
        case_count += 1

    assert case_count == 432
