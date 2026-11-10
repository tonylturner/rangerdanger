"""Capture sequencing and real SLL2/HTTP boundaries, without Docker or a range."""
import importlib
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import unittest
from contextlib import redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import Mock, patch

from .test_lab_commands_extract import ROOT

sys.path.insert(0, str(ROOT / "scripts/lib"))
worker = importlib.import_module("lab_commands_smoke")
capture_module = importlib.import_module("lab_commands_capture")
eu = importlib.import_module("lab_commands_eu")


class CaptureCoordinatorTests(unittest.TestCase):
    def fixture(self, package, fault=None):
        rows = [r for r in worker.extract(package_id=package)
                if r["scenario"].endswith("validation-evidence")]
        data = json.loads(worker.gate.expectations_path(ROOT, package).read_text())
        capture = next(r for r in rows if r["command"].startswith("tcpdump "))
        positive = next(r for r in rows if r["command"].startswith("nmap ")
                        and data["commands"][worker.gate.key(r)]["oracle"]["intent"] == "read")
        negative = next(r for r in rows if (data["commands"][worker.gate.key(r)]["oracle"] or {}).get("intent") == "deny")
        reader = next(r for r in rows if r["command"].startswith("tshark "))
        rows = [capture, positive, negative, reader]
        log = Mock()
        lifecycle, active = Mock(), Mock(row=capture, oracle={"file": "/data/captures/validation.pcap"})
        active.finish.return_value = fault != "empty"
        log.attach_mock(lifecycle, "life")
        log.attach_mock(active, "capture")
        lifecycle.execute.return_value = True
        if fault == "timeout":
            lifecycle.execute.side_effect = [True, TimeoutError("node HTTP deadline")]
        if fault == "start":
            active.start.side_effect = ValueError("not listening")
        if fault == "early":
            active.require_running.side_effect = ValueError("expired")
        if fault == "stop":
            active.finish.side_effect = ValueError("SIGINT failed")
        if fault == "cleanup":
            active.cleanup.side_effect = ValueError("cleanup failed")
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(worker, "extract", return_value=rows), \
                patch.object(worker.gate, "validate_inventory", return_value=4), \
                patch.object(worker, "preflight"), \
                patch.object(worker, "Lifecycle", return_value=lifecycle), \
                patch.object(worker, "Capture", return_value=active), \
                redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()) as errors:
            result = worker.run(package, "", "http://fixture", directory, 60)
            summary = json.loads((Path(directory) / "summary.json").read_text())
        return rows, log.mock_calls, summary, result, errors.getvalue()

    def test_both_packages_share_start_sources_sigint_decode_before_reader(self):
        for package in (worker.US, eu.EU):
            with self.subTest(package=package):
                rows, calls, summary, result, errors = self.fixture(package)
                self.assertEqual((result, summary["passed"], errors), (0, 4, ""))
                names = [c[0] for c in calls]
                start, stop = names.index("capture.start"), names.index("capture.finish")
                executes = [i for i, name in enumerate(names) if name == "life.execute"]
                # Weak positive control precedes capture; source rows follow it;
                # decode/stop and cleanup precede the actual tshark reader.
                self.assertLess(executes[0], start)
                self.assertEqual(calls[executes[0]].args[0], rows[2])
                self.assertTrue(calls[executes[0]].kwargs["positive"])
                self.assertTrue(start < executes[1] < executes[2] < stop < executes[3])
                self.assertFalse(any(c[0] == "life.policy" for c in calls[start:stop]))
                self.assertTrue(calls[executes[2]].kwargs["control"])
                self.assertEqual(calls[stop].kwargs, {"wait": False})

    def test_every_capture_and_http_failure_is_nonzero_with_explicit_row_diagnostics(self):
        for package in (worker.US, eu.EU):
            for fault in ("empty", "start", "early", "timeout", "stop", "cleanup"):
                with self.subTest(package=package, fault=fault):
                    rows, calls, summary, result, errors = self.fixture(package, fault)
                    self.assertEqual(result, 1)
                    self.assertGreater(summary["failed"], 0)
                    if fault != "empty":
                        self.assertIn("FAIL:", errors)
                        self.assertIn(rows[0]["scenario"], errors)
                    if fault in ("start", "early", "timeout"):
                        self.assertIn("NOT RUN after failure", errors)
                    self.assertIn("capture.cleanup", [c[0] for c in calls])
                    self.assertEqual([c[0] for c in calls][-2:], ["life.reset", "life.policy"])


class DetachedCaptureTests(unittest.TestCase):
    def test_docker_exec_options_precede_container_not_the_linux_command(self):
        active = capture_module.Capture(worker.gate, {"container": "fixture"}, {}, "unused")
        with patch.object(capture_module.subprocess, "run") as run:
            active.docker("bash", "-c", "fixture command", detached=True)
            self.assertEqual(run.call_args.args[0],
                             ["docker", "exec", "-d", "fixture", "bash", "-c", "fixture command"])
            active.docker("cat", "fixture-file")
            self.assertEqual(run.call_args.args[0], ["docker", "exec", "fixture", "cat", "fixture-file"])

    def test_finish_stops_before_decoding_and_accepts_only_our_sigint_or_natural_exit(self):
        row = next(r for r in worker.extract(target="validation-evidence")
                   if r["command"].startswith("tcpdump "))
        for rc, frames, expected in (("0", 1, True), ("130", 1, True),
                                     ("143", 1, False), ("0", 0, False)):
            with self.subTest(rc=rc, frames=frames), tempfile.TemporaryDirectory() as directory:
                active = capture_module.Capture(worker.gate, row, {
                    "intent": "capture", "file": "/data/captures/validation.pcap"}, directory)
                log = Mock()
                with patch.object(active, "stop") as stop, \
                        patch.object(active, "status", return_value=dict(rc=rc, stderr="", running=False)), \
                        patch.object(active, "docker", return_value=subprocess.CompletedProcess([], 0, "", "")), \
                        patch.object(capture_module, "decode", return_value=frames) as decode, \
                        redirect_stdout(io.StringIO()):
                    log.attach_mock(stop, "stop")
                    log.attach_mock(decode, "decode")
                    self.assertEqual(active.finish(), expected)
                self.assertEqual([c[0] for c in log.mock_calls], ["stop", "decode"])

    def test_start_uses_exact_bounded_row_detached_not_a_run_timeout(self):
        row = next(r for r in worker.extract(target="validation-evidence")
                   if r["command"].startswith("tcpdump "))
        with tempfile.TemporaryDirectory() as directory:
            active = capture_module.Capture(worker.gate, row, {
                "intent": "capture", "file": "/data/captures/validation.pcap"}, directory)
            with patch.object(worker.gate, "run_staged_shell", return_value=
                              subprocess.CompletedProcess([], 0, "", "")), \
                    patch.object(active, "docker") as docker, \
                    patch.object(active, "status", return_value=dict(
                        rc="", running=True, stderr="tcpdump: listening on any")), \
                    redirect_stdout(io.StringIO()):
                active.start()
            self.assertEqual(docker.call_args.args[:2], ("bash", "-c"))
            self.assertEqual(docker.call_args.kwargs, {"detached": True})
            script = docker.call_args.args[2]
            self.assertIn(row["command"], script)
            self.assertNotIn("timeout ", script)
            # Exercise the actual generated wrapper locally: a stand-in tcpdump
            # must receive SIGINT rather than inherit SIG_IGN from backgrounding.
            state = Path(directory)
            fake = state / "tcpdump"
            fake.write_text(
                f"#!{sys.executable}\nimport signal, time, sys\n"
                "signal.signal(signal.SIGINT, lambda *_: sys.exit(130))\n"
                "print('listening on any', file=sys.stderr, flush=True)\n"
                "time.sleep(10)\n")
            fake.chmod(0o755)
            script = script.replace(active.state, directory)
            process = subprocess.Popen(["bash", "-c", script], stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, env={
                                           **os.environ, "PATH": directory + os.pathsep + os.environ["PATH"]})
            try:
                import time
                deadline = time.monotonic() + 5
                while not (state / "stderr").exists() or not (state / "stderr").read_text():
                    if time.monotonic() > deadline:
                        self.fail("detached fixture did not become ready")
                    time.sleep(0.01)
                os.kill(int((state / "pid").read_text()), signal.SIGINT)
                process.communicate(timeout=5)
                self.assertEqual((state / "rc").read_text().strip(), "130")
            finally:
                if process.poll() is None:
                    process.kill()
                process.communicate(timeout=5)

    def test_empty_corrupt_and_decoder_failure_never_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            for rc, stdout in ((0, ""), (1, ""), (2, "partial packet\n"), (0, "packet\n")):
                with self.subTest(rc=rc, stdout=stdout), patch.object(
                        capture_module.subprocess, "run",
                        return_value=subprocess.CompletedProcess([], rc, stdout, "")):
                    self.assertEqual(capture_module.decode(path, "fixture", "test.pcap"),
                                     1 if rc == 0 and stdout else 0)


def sll2_packet(ifindex, port):
    """Classic PCAP linktype 276: cooked v2 + IPv4 + one TCP SYN."""
    cooked = struct.pack("!HHIHBB8s", 0x0800, 0, ifindex, 1, 0, 6, b"\0" * 8)
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 40, 1, 0, 64, 6, 0,
                     socket.inet_aton("10.10.10.50"), socket.inet_aton("10.40.40.20"))
    tcp = struct.pack("!HHIIBBHHH", 32123, port, 1, 0, 0x50, 2, 65535, 0, 0)
    return cooked + ip + tcp


class ReaderBoundaryTests(unittest.TestCase):
    def test_show_events_oracles_require_deny_rule_hits_not_audit_or_monitor_decisions(self):
        for package in (worker.US, eu.EU):
            rows = worker.extract(package_id=package)
            data = json.loads(worker.gate.expectations_path(ROOT, package).read_text())
            for row in (r for r in rows if r["command"] == "show events"):
                oracle = data["commands"][worker.gate.key(row)]["oracle"]
                event = ("evt-1 2026-11-10T12:00:00Z flow-1 tcp firewall.rule.hit "
                         "10.10.10.50:32123 10.40.40.20:502 "
                         "action=DENY port=502 proto=tcp ruleId=deny-field")
                self.assertTrue(worker.gate.verdict(oracle, 0, event, ""))
                for before, after in (("firewall.rule.hit", "config.commit"),
                                      ("firewall.rule.hit", "ics.decision"),
                                      ("action=DENY", "action=ALLOW"),
                                      ("ruleId=deny-field", "")):
                    self.assertFalse(worker.gate.verdict(oracle, 0, event.replace(before, after), ""))

    @unittest.skipUnless(shutil.which("tshark"), "host tshark unavailable; required in EU offline gate")
    def test_real_tshark_exposes_sll_ifindex_and_short_syn_rows_with_reviewed_oracles(self):
        tshark = shutil.which("tshark")
        for package, port in ((worker.US, 20000), (eu.EU, 502)):
            rows = worker.extract(package_id=package)
            reader = next(r for r in rows if r["scenario"].endswith("validation-evidence")
                          and r["command"].startswith("tshark "))
            data = json.loads(worker.gate.expectations_path(ROOT, package).read_text())
            oracle = data["commands"][worker.gate.key(reader)]["oracle"]
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "sll2.pcap"
                content = struct.pack("<IHHIIII", 0xa1b2c3d4, 2, 4, 0, 0, 65535, 276)
                for index in (7, 11):
                    packet = sll2_packet(index, port)
                    content += struct.pack("<IIII", 1700000000, 123456, len(packet), len(packet)) + packet
                path.write_bytes(content)
                args = shlex.split(reader["command"])
                args[0], args[2] = tshark, str(path)
                result = subprocess.run(args, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                lines = result.stdout.splitlines()
                self.assertEqual(len(lines), 3)
                self.assertIn("sll.ifindex", lines[0])
                self.assertIn("frame.time_utc", lines[0])
                self.assertEqual([line.split("\t")[1] for line in lines[1:]],
                                 ["2023-11-14T22:13:20.123456000Z"] * 2)
                self.assertEqual([line.split("\t")[2] for line in lines[1:]], ["7", "11"])
                self.assertTrue(worker.gate.verdict(oracle, 0, result.stdout, result.stderr))
                self.assertFalse(worker.gate.verdict(oracle, 0, lines[0], ""))
                self.assertFalse(worker.gate.verdict(oracle, 0, result.stdout, "Unknown command"))
                # A field typo must remain a real decoder error, not an oracle waiver.
                args[args.index("sll.ifindex")] = "sll.nonexistent"
                bad = subprocess.run(args, capture_output=True, text=True, timeout=10)
                self.assertFalse(worker.gate.verdict(oracle, bad.returncode, bad.stdout, bad.stderr))

    def test_node_http_deadline_tracks_command_budget_not_fixed_forty_seconds(self):
        row = next(r for r in worker.extract(package_id=eu.EU)
                   if r["command"].startswith("nmap "))
        response = dict(stdout="2404/tcp open", stderr="", exit_code=0)
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(eu, "request", return_value=response) as request, \
                redirect_stdout(io.StringIO()):
            adapter = eu.EUAdapter("http://fixture", directory)
            adapter.execute(worker.gate, row, {"intent": "read", "match": "open"},
                            Path(directory), 60)
            self.assertEqual(request.call_args.kwargs["timeout"], 75)
            self.assertEqual(request.call_args.args[2]["timeout_sec"], 60)

    def test_request_uses_real_stdlib_http_boundary(self):
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                body = json.dumps({"stdout": data["command"], "stderr": "", "exit_code": 0}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            response = eu.request(f"http://127.0.0.1:{server.server_port}", "/exec",
                                  {"command": "fixture"}, timeout=2)
            self.assertEqual(response, dict(stdout="fixture", stderr="", exit_code=0))
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
