"""Offline post-commit readiness and supersession contracts (no Docker)."""
import importlib.util
import json
import subprocess
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "readiness", Path(__file__).resolve().parents[1] / "lib/containd_readiness.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def protection(state="protecting", revision="ours", *, desired="ours", fault=None, required=True):
    return {"state": state, "ready": state == "protecting", "protectingRevision": revision,
            "desiredRevision": desired, "appliedRevision": revision,
            "subsystems": [{"required": required, "state": "failed" if fault else "ready",
                            "fault": {"kind": fault, "reason": "diagnostic"} if fault else None}]}


def commit(**kwargs):
    return {"status": "committed", "runningRevision": "ours", **kwargs}


class ReadinessTests(unittest.TestCase):
    def wait(self, observations, receipt=None, **kwargs):
        now = [0]
        self.reads = []
        self.delays = []

        def read(path, remaining):
            self.reads.append((path, remaining))
            return observations.pop(0) if len(observations) > 1 else observations[0]

        def sleep(delay):
            self.delays.append(delay)
            now[0] += delay
        return MODULE.wait_ready(receipt if receipt is not None else commit(), read,
                                 clock=lambda: now[0], sleep=sleep, **kwargs)

    def test_commit_protection_is_sufficient_without_apply_id(self):
        self.wait([], commit(protection=protection()))
        self.assertEqual(self.reads, [])

    def test_ready_at_older_revision_polls_then_succeeds(self):
        self.wait([protection(revision="old"), protection()])
        self.assertEqual(self.delays, [0.5])
        self.assertTrue(all(path == "/dataplane/protection" for path, _ in self.reads))

    def test_terminal_states_have_subsystem_reasons(self):
        for value in (protection("disabled"), protection("failed", fault="config"),
                      protection("failed", fault="render")):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "disabled|diagnostic"):
                self.wait([value])
            self.assertEqual(len(self.reads), 1)

    def test_degraded_times_out_with_last_observation(self):
        value = protection("degraded", fault="delivery")
        with self.assertRaisesRegex(ValueError, "timeout.*last observation.*diagnostic"):
            self.wait([value])
        self.assertEqual(sum(self.delays), 30)

    def test_nonterminal_states_poll(self):
        for value in (protection("pending"), protection("failed"), protection("unknown"),
                      protection("failed", fault="config", required=False),
                      protection("failed", fault="delivery"), protection("failed", fault="unknown")):
            with self.subTest(value=value):
                self.wait([value, protection()])
                self.assertEqual(len(self.reads), 2)
                self.assertEqual(self.delays, [0.5])

    def test_desired_revision_supersedes_immediately(self):
        with self.assertRaisesRegex(ValueError, "superseded"):
            self.wait([protection(desired="other")])
        self.assertEqual(len(self.reads), 1)

    def test_null_desired_revision_does_not_supersede(self):
        self.wait([protection("pending", desired=None), protection()])
        self.assertEqual(len(self.reads), 2)

    def test_missing_status_or_running_revision_fails_closed(self):
        for receipt in ({"status": "committed", "applyId": "informational"},
                        {"runningRevision": "ours"}, commit(runningRevision="")):
            with self.subTest(receipt=receipt), self.assertRaisesRegex(ValueError, "status or runningRevision"):
                self.wait([], receipt)

    def test_apply_id_does_not_gate_readiness(self):
        self.wait([protection("pending"), protection()], commit(applyId="unrelated"))
        self.assertEqual(len(self.reads), 2)

    def test_parent_deadline_caps_timeout(self):
        with self.assertRaisesRegex(ValueError, "timeout"):
            self.wait([protection("pending")], timeout=0.75)
        self.assertEqual(self.delays, [0.5, 0.25])

    def test_health_enforcing_is_polled(self):
        self.wait([{"enforcement": {"state": "pending"}},
                   {"enforcement": {"state": "enforcing"}}], health=True)
        self.assertEqual(self.reads[0][0], "/health")
        self.assertEqual(self.delays, [0.5])

    def test_503_polls_with_delay_and_retains_last_observation(self):
        self.wait([None, None, protection()])
        self.assertEqual(self.delays, [0.5, 0.5])
        with self.assertRaisesRegex(ValueError, "timeout.*last observation.*diagnostic"):
            self.wait([protection("pending", fault="delivery"), None], timeout=1)
        self.assertEqual(self.delays, [0.5, 0.5])

    def test_transport_deadline_keeps_last_observation(self):
        for error in (subprocess.TimeoutExpired("curl", 1),
                      subprocess.CalledProcessError(28, "curl")):
            def read(path, remaining):
                raise error
            with self.subTest(error=error), self.assertRaisesRegex(ValueError, "timeout.*diagnostic"):
                MODULE.wait_ready(commit(protection=protection("pending", fault="delivery")),
                                  read, sleep=lambda delay: None)

    def test_docker_reader_status_boundaries(self):
        # Real curl output framing, including its trailing HTTP status line.
        for status in (200, 502, 503):
            body = protection("unknown") if status == 502 else protection()
            if status == 503:
                body = {"error": "runtime coordinator unavailable"}
            completed = subprocess.CompletedProcess([], 0, json.dumps(body) + f"\n{status}")
            with self.subTest(status=status), patch.object(MODULE.subprocess, "run", return_value=completed) as run:
                reader = MODULE.docker_reader("fixture", "token")
                value = reader("/dataplane/protection", 1)
                self.assertEqual(value, None if status == 503 else body)
                self.wait([value, protection()])
                self.assertEqual(len(self.reads), 1 if status == 200 else 2)
                self.assertEqual(run.call_args.kwargs["timeout"], 1)
                if status != 200:
                    with self.assertRaisesRegex(ValueError, f"HTTP {status}"):
                        reader("/health", 1)

    def test_real_curl_boundary_with_starting_runtime(self):
        responses = [(502, protection("unknown")),
                     (503, {"error": "runtime coordinator unavailable"}), (200, protection())]
        paths = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                paths.append(self.path)
                status, body = responses.pop(0)
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        run = subprocess.run

        def local_curl(command, **kwargs):
            self.assertEqual(command[:3], ["docker", "exec", "fixture"])
            command = command[3:]
            command[-1] = command[-1].replace("127.0.0.1:8080", f"127.0.0.1:{server.server_port}")
            return run(command, **kwargs)

        try:
            with patch.object(MODULE.subprocess, "run", side_effect=local_curl):
                now = [0]

                def sleep(delay):
                    now[0] += delay

                MODULE.wait_ready(commit(), MODULE.docker_reader("fixture", "token"),
                                  clock=lambda: now[0], sleep=sleep)
            self.assertEqual(paths, ["/api/v1/dataplane/protection"] * 3)
            self.assertEqual(now[0], 1)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

    def test_docker_reader_rejects_bad_http_and_json(self):
        for output in ('{}\n401', 'not-json\n502'):
            completed = subprocess.CompletedProcess([], 0, output)
            with self.subTest(output=output), patch.object(MODULE.subprocess, "run", return_value=completed):
                with self.assertRaises(ValueError):
                    MODULE.docker_reader("fixture", "token")("/dataplane/protection", 1)


class BackendReceiptTests(unittest.TestCase):
    def test_protecting_receipt_is_returned_without_reads(self):
        receipt = {"applyId": "opaque-revision-not-a-sequence", "protection": "protecting"}
        self.assertIs(MODULE.require_backend_protection({"containd": receipt}), receipt)

    def test_missing_malformed_or_degraded_receipts_fail(self):
        for response in (None, [], {}, {"applyId": "id", "protection": "protecting"},
                         {"containd": None}, {"containd": {"protection": "protecting"}},
                         {"containd": {"applyId": "", "protection": "protecting"}}):
            with self.subTest(response=response), self.assertRaises(ValueError):
                MODULE.require_backend_protection(response)
        for state in ("pending", "degraded", "failed", "disabled", "unknown", "Protecting", None):
            with self.subTest(state=state), self.assertRaisesRegex(ValueError, "logging unavailable"):
                MODULE.require_backend_protection({"containd": {
                    "applyId": "id", "protection": state, "warning": "logging unavailable"}})


if __name__ == "__main__":
    unittest.main()
