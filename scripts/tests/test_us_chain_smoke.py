"""Offline US consequence-chain coordinator tests; no Docker or live HTTP."""
from datetime import datetime, timedelta, timezone
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts/lib"))
try:
    SPEC = importlib.util.spec_from_file_location("us_chain_smoke", ROOT / "scripts/lib/us_chain_smoke.py")
    M = importlib.util.module_from_spec(SPEC)
    sys.modules[SPEC.name] = M
    SPEC.loader.exec_module(M)
finally:
    sys.path.pop(0)


def stamp(seconds=0):
    return (datetime(2026, 11, 1, tzinfo=timezone.utc) + timedelta(seconds=seconds)).isoformat()


def state(seconds=1, energized=True):
    return {
        "electrical": {
            "solved_at": stamp(seconds), "converged": True, "breaker_closed": energized,
            "recloser_closed": True, "general_load_energized": energized,
            "critical_load_energized": energized,
            "downstream_voltage_v": 120.0 if energized else 0.0,
            "critical_load_voltage_v": 119.0 if energized else 0.0,
            "feeder_current_a": 100.0 if energized else 0.0,
        },
        "physics": {"solved_at": stamp(seconds), "stale": False,
                    "consecutive_failures": 0, "last_good_at": stamp(seconds)},
        "devices": {"relay": {"breaker_closed": energized},
                    "recloser": {"closed": True, "reclose_enabled": True}},
        "device_comms": {name: True for name in ("relay", "recloser", "regulator", "capbank")},
        "last_poll": stamp(seconds),
    }


def receipt():
    return {"applyId": "opaque-apply-id", "protection": "protecting"}


def completed(argv, out="", err="", rc=0):
    return subprocess.CompletedProcess(argv, rc, out, err)


POLL_OUTPUT = """DNP3 Outstation 10.40.40.20:20000 (addr 1) — IIN: 0x0000

Binary Inputs (Group 1):
  [ 0] = ON

Binary Outputs (Group 10):
  [ 0] = ON
  [ 1] = OFF

Analog Inputs (Group 30):
  [ 0] = 120.000
"""


class Fixture:
    """The real coordinator runs against these two in-memory transports."""
    def __init__(self, directory):
        self.now = 0.0
        self.sequence = 0
        self.policy = "weak"
        self.energized = True
        self.calls = []
        self.http_mutation = lambda method, path, body, reply: reply
        self.state_mutation = lambda value: value
        self.process_mutation = lambda argv, result: result
        self.fail_stage = None
        self.failed = False
        self.gate = M.ChainGate(
            directory, http=self.http, runner=self.process, clock=lambda: self.now,
            sleep=self.sleep, utc=lambda: stamp(self.now))

    def sleep(self, delay):
        self.now += delay

    def maybe_fail(self):
        if self.gate.stage == self.fail_stage and not self.failed:
            self.failed = True
            raise M.GateError("injected stage failure")

    def http(self, method, path, body, timeout):
        self.maybe_fail()
        assert 0 < timeout <= 8
        self.now += 0.05
        self.calls.append((self.gate.stage, method, path, body))
        if path == "/api/range":
            reply = {"package": "us-dnp3-substation", "phase": "ready", "containd": receipt()}
        elif path == "/api/firewall/apply":
            self.policy = body["config"]
            reply = {"status": "applied", "active_config": self.policy, "containd": receipt()}
        elif path.startswith("/api/substation/command/"):
            if path.endswith("/relay") and body["command"] == "close":
                self.energized = True
            reply = {"result": "executed"}
        elif path == "/api/substation/state":
            self.sequence += 1
            reply = self.state_mutation(state(self.sequence, self.energized))
        else:
            raise AssertionError(f"unexpected HTTP {method} {path}")
        reply = self.http_mutation(method, path, body, reply)
        return reply if isinstance(reply, M.HTTPReply) else M.HTTPReply(200, json.dumps(reply))

    def process(self, argv, **kwargs):
        self.maybe_fail()
        assert kwargs["capture_output"] is True and kwargs["text"] is True
        assert 0 < kwargs["timeout"] <= 10
        self.now += 0.1
        self.calls.append((self.gate.stage, "process", argv))
        argv = tuple(argv)
        if argv == M.SOURCE:
            result = completed(argv, json.dumps({"source": "10.10.10.50", "utc": stamp(self.now)}))
        elif argv == M.OPERATE:
            self.now += 3
            if self.policy == "weak":
                self.energized = False
                result = completed(argv, "  Result: SUCCESS\n")
            else:
                result = completed(argv, "  Result: FAILED: dial tcp 10.40.40.20:20000: i/o timeout\n")
        elif argv == M.CONTROL:
            result = completed(argv, POLL_OUTPUT)
        elif argv[1] == "inspect":
            result = completed(argv, json.dumps({"Running": True, "Paused": False}))
        elif argv[-1] == "command -v dnp3poll":
            result = completed(argv, "/usr/local/bin/dnp3poll\n")
        else:
            raise AssertionError(f"unexpected subprocess {argv}")
        return self.process_mutation(argv, result)


class CoordinatorTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.f = Fixture(self.directory.name)

    def assert_fails(self, message=None, cleanup=True):
        self.assertEqual(self.f.gate.run(), 1)
        record = json.loads((Path(self.directory.name) / "us-chain.json").read_text())
        self.assertEqual(record["result"], "FAIL")
        if message:
            self.assertIn(message, "\n".join(record["errors"]))
        cleanup_calls = [c for c in self.f.calls if c[0] == "cleanup"]
        if cleanup:
            self.assertTrue(cleanup_calls)
            self.assertEqual(cleanup_calls[-1][2:], ("/api/firewall/apply", {"config": "weak"}))
        else:
            self.assertEqual(cleanup_calls, [])
        return record

    def test_complete_chain_order_identical_command_and_retained_inputs(self):
        self.assertEqual(self.f.gate.run(), 0)
        record = json.loads((Path(self.directory.name) / "us-chain.json").read_text())
        events = record["events"]
        proofs = [e["name"] for e in events if e["kind"] == "state-proof"]
        self.assertEqual(proofs, ["recovery", "outage", "recovery", "denied-energized", "recovery"])
        for e in events:
            if e["kind"] == "state-proof":
                self.assertGreater(M.timestamp(e["state"]["electrical"]["solved_at"]),
                                   M.timestamp(e["boundary"]))
        operates = [e for e in events if e["kind"] == "process-end" and e["argv"] == list(M.OPERATE)]
        self.assertEqual(len(operates), 2)
        self.assertIn("SUCCESS", operates[0]["stdout"])
        self.assertIn("i/o timeout", operates[1]["stdout"])
        self.assertTrue(all("stderr" in e for e in operates))
        for e in operates:
            for stream in ("stdout", "stderr"):
                self.assertEqual((Path(self.directory.name) / e[stream + "_file"]).read_text(),
                                 e[stream])
        self.assertEqual([e["policy"] for e in events if e["kind"] == "policy"],
                         ["weak", "improved", "weak"])
        self.assertEqual([e["observation"]["source"] for e in events if e["kind"] == "source"],
                         ["10.10.10.50"] * 3)
        controls = [i for i, e in enumerate(events) if e["kind"] == "listener-control"]
        denied = events.index(operates[1])
        self.assertLess(controls[0], denied)
        self.assertGreater(controls[1], denied)
        self.assertEqual(events[-1]["result"], "PASS")
        self.assertEqual(events[-2]["stage"], "cleanup")
        self.assertEqual([e["sequence"] for e in events], list(range(1, len(events) + 1)))
        self.assertEqual(sorted(e["monotonic"] for e in events), [e["monotonic"] for e in events])
        raw = [e for e in events if e["kind"] == "http-end"]
        self.assertTrue(raw)
        self.assertTrue(all(isinstance(e["raw"], str) for e in raw))
        self.assertTrue(all((Path(self.directory.name) / e["raw_file"]).read_text() == e["raw"]
                            for e in raw))
        commands = [c for c in self.f.calls if c[1] == "POST" and "/command/" in c[2]]
        self.assertEqual(len(commands), len(M.RESTORE) * 3)
        self.assertFalse(any("/reset" in str(c) or "/apply-custom" in str(c) for c in self.f.calls))

    def test_wrong_range_phase_or_receipt_fails_without_mutation(self):
        mutations = (
            lambda r: r.update(package="eu-iec104-substation"),
            lambda r: r.update(phase="starting"),
            lambda r: r.pop("containd"),
            lambda r: r.update(containd={"applyId": "id", "protection": "degraded"}),
        )
        for mutate in mutations:
            with self.subTest(mutate=mutate), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                def change(method, path, body, reply):
                    if path == "/api/range":
                        mutate(reply)
                    return reply
                f.http_mutation = change
                self.assertEqual(f.gate.run(), 1)
                self.assertFalse(f.gate.mutable)
                self.assertFalse(any(c[1] == "POST" for c in f.calls))

    def test_all_stages_fail_closed_and_retain_original_failure(self):
        for stage in ("preflight", "weak-baseline", "weak-operate", "recovery",
                      "improved", "improved-operate"):
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                f.fail_stage = stage
                self.assertEqual(f.gate.run(), 1)
                record = json.loads((Path(directory) / "us-chain.json").read_text())
                self.assertIn(stage + ": GateError: injected stage failure", record["errors"])
                self.assertEqual(f.policy, "weak")
                if stage != "preflight":
                    self.assertTrue(any(c[0] == "cleanup" for c in f.calls))

    def test_apply_receipts_are_strict_at_each_policy_phase(self):
        for stage in ("weak-baseline", "improved"):
            for bad in (None, [], {}, {"applyId": "", "protection": "protecting"},
                        {"applyId": "id", "protection": "degraded"},
                        {"applyId": "id", "protection": "failed"},
                        {"applyId": "id", "protection": {"state": "protecting"}}):
                with self.subTest(stage=stage, bad=bad), tempfile.TemporaryDirectory() as directory:
                    f = Fixture(directory)
                    def change(method, path, body, reply):
                        if path == "/api/firewall/apply" and f.gate.stage == stage:
                            reply["containd"] = bad
                        return reply
                    f.http_mutation = change
                    self.assertEqual(f.gate.run(), 1)
                    self.assertIn("not protecting", f.gate.record["errors"][0])
                    self.assertEqual(f.policy, "weak")

    def test_http_errors_never_recommit_or_retry(self):
        for status in (409, 502, 403, 302):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                def change(method, path, body, reply):
                    if path == "/api/firewall/apply" and f.gate.stage == "improved":
                        return M.HTTPReply(status, '{"error":"fixture"}')
                    return reply
                f.http_mutation = change
                self.assertEqual(f.gate.run(), 1)
                calls = [c for c in f.calls if c[0] == "improved" and c[1] == "POST"]
                self.assertEqual(len(calls), 1)
                self.assertIn(f"HTTP {status}", f.gate.record["errors"][0])
                self.assertEqual(f.policy, "weak")

    def test_active_policy_mismatch_fails(self):
        def change(method, path, body, reply):
            if path == "/api/firewall/apply" and self.f.gate.stage == "improved":
                reply["active_config"] = "weak"
            return reply
        self.f.http_mutation = change
        self.assert_fails("policy apply mismatch")

    def test_http_200_rejected_command_is_not_recovery(self):
        def change(method, path, body, reply):
            if path.endswith("/relay") and body["command"] == "clear_fault":
                return {"result": "rejected"}
            return reply
        self.f.http_mutation = change
        record = self.assert_fails("supervisory command rejected")
        self.assertTrue(any("cleanup feeder" in e for e in record["errors"]))
        self.assertTrue(any(c[0] == "cleanup" and c[2].endswith("/regulator") for c in self.f.calls))

    def test_rc_zero_failed_command_does_not_prove_weak_write(self):
        def change(argv, result):
            if argv == M.OPERATE:
                return completed(argv, "Result: FAILED: protocol rejected\n")
            return result
        self.f.process_mutation = change
        self.assert_fails("operate rejected")

    def test_denied_write_must_not_change_breaker(self):
        def change(argv, result):
            if argv == M.OPERATE and self.f.policy == "improved":
                self.f.energized = False
            return result
        self.f.process_mutation = change
        self.assert_fails("denied-energized poll deadline")
        self.assertLess(self.f.now, 50)

    def test_listener_control_failure_before_and_after_denial(self):
        for number in (1, 2):
            with self.subTest(control=number), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                count = [0]
                def change(argv, result):
                    if argv == M.CONTROL:
                        count[0] += 1
                        if count[0] == number:
                            return completed(argv, err="connection refused", rc=1)
                    return result
                f.process_mutation = change
                self.assertEqual(f.gate.run(), 1)
                self.assertIn("tool-error", f.gate.record["errors"][0])
                self.assertEqual(f.policy, "weak")
                operations = [c for c in f.calls if c[1] == "process" and tuple(c[2]) == M.OPERATE]
                self.assertEqual(len(operations), number)

    def test_bad_source_container_or_tool_fails_before_mutation(self):
        for kind in ("source", "paused", "stopped", "missing-tool"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                def change(argv, result):
                    if kind == "source" and argv == M.SOURCE:
                        return completed(argv, json.dumps({"source": "10.30.30.50", "utc": stamp()}))
                    if argv[1] == "inspect":
                        if kind == "paused":
                            return completed(argv, '{"Running":true,"Paused":true}')
                        if kind == "stopped":
                            return completed(argv, '{"Running":false,"Paused":false}')
                    if kind == "missing-tool" and argv[-1] == "command -v dnp3poll":
                        return completed(argv, rc=127)
                    return result
                f.process_mutation = change
                self.assertEqual(f.gate.run(), 1)
                self.assertFalse(f.gate.mutable)
                self.assertFalse(any(c[1] == "POST" for c in f.calls))

    def test_unchanged_stale_or_malformed_solve_never_passes(self):
        for mutation in (
                lambda r: r["physics"].update(stale=True),
                lambda r: r["electrical"].update(solved_at=stamp()),
                lambda r: r["electrical"].update(solved_at="invalid"),
                lambda r: r["electrical"].pop("breaker_closed"),
                lambda r: r["electrical"].update(critical_load_energized=True)):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                def change(value):
                    if f.gate.stage == "weak-operate":
                        mutation(value)
                    return value
                f.state_mutation = change
                self.assertEqual(f.gate.run(), 1)
                self.assertEqual(f.policy, "weak")
                self.assertLess(f.now, 60)

    def test_cleanup_failure_overrides_chain_success(self):
        def change(method, path, body, reply):
            if self.f.gate.stage == "cleanup" and path == "/api/firewall/apply":
                reply["containd"]["protection"] = "failed"
            return reply
        self.f.http_mutation = change
        record = self.assert_fails("cleanup weak")
        self.assertTrue(any(e.get("name") == "denied-energized" for e in record["events"]))
        self.assertEqual(record["events"][-1]["result"], "FAIL")

    def test_recovery_requires_fresh_energized_electrical_consequence(self):
        def change(value):
            if self.f.gate.stage == "recovery":
                value["electrical"]["feeder_current_a"] = 0
            return value
        self.f.state_mutation = change
        self.assert_fails("recovery poll deadline: electrical consequence mismatch")
        self.assertFalse(any(c[0] == "improved" for c in self.f.calls))

    def test_malformed_http_input_is_retained_and_fails_before_mutation(self):
        self.f.http_mutation = lambda *args: M.HTTPReply(200, "{broken-json")
        record = self.assert_fails("malformed JSON", cleanup=False)
        response = next(e for e in record["events"] if e["kind"] == "http-end")
        self.assertEqual((Path(self.directory.name) / response["raw_file"]).read_text(),
                         "{broken-json")

    def test_cleanup_preserves_original_error_and_attempts_weak_after_recovery_failure(self):
        self.f.fail_stage = "weak-operate"
        def change(method, path, body, reply):
            if self.f.gate.stage == "cleanup" and "/command/" in path:
                return M.HTTPReply(502, "bad gateway")
            return reply
        self.f.http_mutation = change
        record = self.assert_fails("injected stage failure")
        self.assertIn("cleanup feeder", record["errors"][1])
        self.assertEqual(len([c for c in self.f.calls if c[0] == "cleanup" and "/command/" in c[2]]),
                         len(M.RESTORE))

    def test_host_deadline_and_missing_executable_are_not_denial_and_keep_streams(self):
        for exception in (
                subprocess.TimeoutExpired(list(M.OPERATE), 10, output=b"partial", stderr=b"timeout"),
                FileNotFoundError("missing docker")):
            with self.subTest(exception=exception), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                def change(argv, result):
                    if argv == M.OPERATE and f.policy == "improved":
                        raise exception
                    return result
                f.process_mutation = change
                self.assertEqual(f.gate.run(), 1)
                self.assertEqual(f.policy, "weak")
                self.assertTrue(any(e["kind"] == "process-end" and "error" in e
                                    for e in f.gate.record["events"]))
                if isinstance(exception, subprocess.TimeoutExpired):
                    self.assertIn("subprocess-deadline", f.gate.record["errors"][0])
                    self.assertTrue(any(e.get("stdout") == "partial" and e.get("stderr") == "timeout"
                                        for e in f.gate.record["events"]))

    def test_gate_deadline_cannot_prevent_separately_bounded_cleanup(self):
        def change(argv, result):
            if argv == M.OPERATE and self.f.policy == "weak":
                self.f.now += 211
            return result
        self.f.process_mutation = change
        self.assert_fails("gate deadline exceeded")

    def test_evidence_write_time_does_not_count_toward_dial_timeout(self):
        original = self.f.gate.event
        def slow_event(kind, **data):
            original(kind, **data)
            self.f.now += 3
        self.f.gate.event = slow_event
        self.f.gate.runner = lambda argv, **kw: completed(
            argv, "Result: FAILED: dial tcp 10.40.40.20:20000: i/o timeout\n")
        result, elapsed = self.f.gate.process(M.OPERATE)
        self.assertEqual(elapsed, 0)
        with self.assertRaisesRegex(M.GateError, "too short"):
            M.operate_result(result, elapsed, True)

    def test_cleanup_reserves_time_for_weak_after_feeder_deadline(self):
        self.f.fail_stage = "weak-operate"
        original = self.f.gate.http
        def slow(method, path, body, timeout):
            if self.f.gate.stage == "cleanup" and "/command/" in path:
                self.f.now += timeout
                raise TimeoutError("cleanup HTTP deadline")
            return original(method, path, body, timeout)
        self.f.gate.http = slow
        self.assert_fails("cleanup HTTP deadline")
        self.assertEqual(self.f.policy, "weak")
        self.assertLess(self.f.now, 120)

    def test_lost_evidence_fails_chain_but_cannot_prevent_safety_cleanup(self):
        original = Path.write_text
        for permanent in (True, False):
            with self.subTest(permanent=permanent), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                writes = [0]
                def change(argv, result):
                    if argv == M.OPERATE:
                        writes[0] = 1
                    return result
                def write(path, data, *args, **kwargs):
                    if writes[0]:
                        if not permanent:
                            writes[0] = 0
                        raise OSError("disk full fixture")
                    return original(path, data, *args, **kwargs)
                f.process_mutation = change
                with patch.object(Path, "write_text", write):
                    self.assertEqual(f.gate.run(), 1)
                self.assertEqual(f.gate.record["result"], "FAIL")
                self.assertIn("evidence write failed: disk full fixture", f.gate.record["errors"])
                self.assertEqual(f.policy, "weak")
                self.assertTrue(f.energized)
                self.assertTrue(any(c[0] == "cleanup" and c[2] == "/api/firewall/apply"
                                    for c in f.calls))


class OracleTests(unittest.TestCase):
    def test_operate_success_requires_single_success_result_and_successful_process(self):
        M.operate_result(completed([], "Result: SUCCESS\n"), 0.1, False)
        for out, err, rc in (
                ("", "", 0), ("Result: SUCCESS\nResult: SUCCESS\n", "", 0),
                ("Result: FAILED: dial tcp x: i/o timeout", "", 0),
                ("Result: SUCCESS", "tool warning", 0), ("Result: SUCCESS", "", 1),
                ("Result: NOT_SUPPORTED", "", 0), ("Result: SENT", "", 0)):
            with self.subTest(out=out, err=err, rc=rc), self.assertRaises(M.GateError):
                M.operate_result(completed([], out, err, rc), 3.1, False)

    def test_dial_timeout_oracle_rejects_every_other_failure_class(self):
        prefix = "Result: "
        correct = "FAILED: dial tcp 10.40.40.20:20000: i/o timeout"
        M.operate_result(completed([], prefix + correct), 3.1, True)
        for value, classification in (
                ("SUCCESS", "unexpected-success"),
                ("FAILED: dial tcp 10.40.40.20:20000: connect: connection refused", "refusal"),
                ("FAILED: dial tcp 10.40.40.20:20000: connect: no route to host", "no-route"),
                ("FAILED: dial tcp x: network is unreachable", "no-route"),
                ("FAILED: read tcp 10.10.10.50:4->10.40.40.20:20000: i/o timeout", "application-read"),
                ("FAILED: write tcp x: i/o timeout", "application-write"),
                ("FAILED: dial tcp: lookup missing: no such host", "dns-error"),
                ("FAILED: dial tcp 10.40.40.21:20000: i/o timeout", "not an explicit"),
                ("FAILED: protocol timeout", "not an explicit")):
            with self.subTest(value=value), self.assertRaisesRegex(M.GateError, classification):
                M.operate_result(completed([], prefix + value), 3.1, True)
        with self.assertRaisesRegex(M.GateError, "too short"):
            M.operate_result(completed([], prefix + correct), 0.1, True)

    def test_state_positive_and_analytic_outage(self):
        for energized in (True, False):
            M.check_state(state(2, energized), M.timestamp(stamp(1)), energized)

    def test_strict_fields_and_freshness(self):
        mutations = [
            lambda s: s.pop("device_comms"),
            lambda s: s.update(devices=[]),
            lambda s: s["device_comms"].update(relay=1),
            lambda s: s["device_comms"].update(capbank=False),
            lambda s: s["devices"]["relay"].update(breaker_closed=1),
            lambda s: s["devices"]["recloser"].update(reclose_enabled=False),
            lambda s: s["devices"]["recloser"].update(closed=False),
            lambda s: s["electrical"].update(breaker_closed=False),
            lambda s: s["electrical"].update(recloser_closed=False),
            lambda s: s["electrical"].update(general_load_energized=1),
            lambda s: s["electrical"].update(critical_load_energized=False),
            lambda s: s["electrical"].update(converged=False),
            lambda s: s["electrical"].update(converged=1),
            lambda s: s["electrical"].update(solved_at=stamp(1)),
            lambda s: s["physics"].update(solved_at=stamp(1)),
            lambda s: s["physics"].update(stale=0),
            lambda s: s["physics"].update(consecutive_failures=False),
            lambda s: s["physics"].update(consecutive_failures=1),
            lambda s: s["physics"].update(last_good_at=stamp(-20)),
            lambda s: s.update(last_poll=stamp(20)),
            lambda s: s.update(last_poll=stamp(-20)),
            lambda s: s["electrical"].update(downstream_voltage_v=0),
            lambda s: s["electrical"].update(critical_load_voltage_v=False),
            lambda s: s["electrical"].update(feeder_current_a="100"),
            lambda s: s["electrical"].update(feeder_current_a=float("nan")),
            lambda s: s["electrical"].update(feeder_current_a=float("inf")),
        ]
        for mutate in mutations:
            value = state(2)
            mutate(value)
            with self.subTest(mutation=mutate), self.assertRaises(M.GateError):
                M.check_state(value, M.timestamp(stamp(1)), True)

    def test_outage_requires_all_zero_and_both_loads_off(self):
        for field, value in (
                ("downstream_voltage_v", 0.01), ("critical_load_voltage_v", 120),
                ("feeder_current_a", 1), ("general_load_energized", True),
                ("critical_load_energized", 0), ("breaker_closed", True)):
            s = state(2, False)
            s["electrical"][field] = value
            with self.subTest(field=field), self.assertRaises(M.GateError):
                M.check_state(s, M.timestamp(stamp(1)), False)

    def test_timestamp_and_json_are_strict(self):
        for value in (None, "", "garbage", "2026-11-01", "0001-01-01T00:00:00Z"):
            with self.subTest(value=value), self.assertRaises(M.GateError):
                M.timestamp(value)
        for raw in ("oops", "null", "[]", "true"):
            with self.subTest(raw=raw), self.assertRaises(M.GateError):
                M.object_json(raw)

    def test_control_requires_relay_header_and_decoded_closed_output(self):
        bad_outputs = ("", POLL_OUTPUT.replace("Group 10", "Group 11"),
                       POLL_OUTPUT.replace("20000 (addr 1)", "20000 (addr 2)"),
                       POLL_OUTPUT.replace("[ 0] = ON\n  [ 1]", "[ 0] = OFF\n  [ 1]"),
                       POLL_OUTPUT.split("Binary Outputs")[0])
        for out in bad_outputs:
            with self.subTest(out=out), tempfile.TemporaryDirectory() as directory:
                f = Fixture(directory)
                f.process_mutation = lambda argv, result: completed(argv, out)
                with self.assertRaisesRegex(M.GateError, "positive control"):
                    f.gate.control()

    def test_production_curl_transport_is_bounded_no_retry_and_retains_raw_reply(self):
        with tempfile.TemporaryDirectory() as directory:
            calls = []
            def runner(argv, **kwargs):
                calls.append((argv, kwargs))
                return completed(argv, '{"x":true}\n200')
            gate = M.ChainGate(directory, runner=runner)
            self.assertEqual(gate.request("POST", "/fixture", {"a": 1}), {"x": True})
            argv, options = calls[0]
            self.assertIn("--max-time", argv)
            self.assertEqual(argv[argv.index("--retry") + 1], "0")
            self.assertEqual(json.loads(argv[argv.index("--data-binary") + 1]), {"a": 1})
            self.assertLessEqual(options["timeout"], 10)
            self.assertEqual(gate.record["events"][-1]["raw"], '{"x":true}')

    def test_curl_malformed_and_nonzero_responses_fail(self):
        for out, rc in (("nonsense", 0), ("{}\nbad", 0), ("partial", 28)):
            with self.subTest(out=out), tempfile.TemporaryDirectory() as directory:
                gate = M.ChainGate(directory, runner=lambda argv, **kw: completed(argv, out, rc=rc))
                with self.assertRaises(M.GateError):
                    gate.request("GET", "/fixture")


class EntrypointTests(unittest.TestCase):
    def test_shell_delegates_from_any_cwd_and_propagates_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            fake = Path(directory) / "python3"
            fake.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\nexit 7\n')
            fake.chmod(0o755)
            result = subprocess.run(
                ["bash", str(ROOT / "scripts/us-chain-smoke.sh"), "--api", "http://fixture"],
                cwd=directory, env={**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]},
                capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 7)
            self.assertEqual(result.stdout.splitlines(),
                             [str(ROOT / "scripts/lib/us_chain_smoke.py"), "--api", "http://fixture"])

    def test_cli_help_never_touches_range(self):
        result = subprocess.run(
            [sys.executable, str(ROOT / "scripts/lib/us_chain_smoke.py"), "--help"],
            capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0)
        self.assertIn("--evidence-dir", result.stdout)

    def test_main_never_prints_pass_before_successful_cleanup(self):
        for code in (0, 1):
            with self.subTest(code=code), patch.object(M.ChainGate, "run", return_value=code), \
                    patch("builtins.print") as output:
                self.assertEqual(M.main([]), code)
                message = output.call_args_list[0].args[0]
                self.assertIn("PASS" if code == 0 else "FAIL", message)
                if code:
                    self.assertNotIn("PASS", message)

    def test_workflow_insertion_anchors_and_existing_always_upload_cover_gate(self):
        # The orchestrator owns the workflow edit. Pin its existing integration
        # anchors without requiring an out-of-fence edit to make this lane pass.
        source = (ROOT / ".github/workflows/smoke.yml").read_text()
        physics = source.index("- name: Substation physics smoke")
        workshop = source.index("- name: Workshop test-suite")
        upload = source.split("- name: Upload gate evidence", 1)[1].split("\n      - name:", 1)[0]
        self.assertLess(physics, workshop)
        self.assertIn("if: always()", upload)
        self.assertIn("build/smoke-receipts/", upload)
        self.assertNotIn("continue-on-error", source[physics:workshop])
        self.assertEqual(M.ROOT / "build/smoke-receipts/us-chain",
                         ROOT / "build/smoke-receipts/us-chain")


if __name__ == "__main__":
    unittest.main()
