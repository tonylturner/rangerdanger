"""Contract tests for the shell and PowerShell validation-report clients."""

from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[2]
SH_SCRIPT = ROOT / "scripts/validation-report.sh"
PS_SCRIPT = ROOT / "scripts/validation-report.ps1"
PWSH = "/usr/local/bin/pwsh" if Path("/usr/local/bin/pwsh").is_file() else shutil.which("pwsh")
MARKDOWN = "# Stub validation report\n"


@contextmanager
def report_api(scenario):
    """Serve a ready EU range and one backend report response."""
    scenarios = {
        "proof_pass_acceptance_fail": (True, True, False),
        "proof_fail_acceptance_pass": (True, False, True),
        "no_plan_acceptance_pass": (False, False, True),
        "no_plan_acceptance_fail": (False, False, False),
    }
    if scenario == "HTTP_ERROR":
        applicable, proof_pass, acceptance_pass = False, False, False
    else:
        applicable, proof_pass, acceptance_pass = scenarios[scenario]
    outcome = {
        "markdown": MARKDOWN,
        # Plan proof is independent of Hardened acceptance.
        "planProof": {"applicable": applicable, "pass": proof_pass},
        "acceptance": {"pass": acceptance_pass},
        "summary": {"result": "PASS" if acceptance_pass else "FAIL"},
    }

    class Handler(BaseHTTPRequestHandler):
        requests = []

        def log_message(self, _format, *_args):
            pass

        def _respond(self, status, body):
            encoded = json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)

        def do_GET(self):
            self.requests.append(("GET", self.path, b""))
            if self.path != "/api/range":
                self._respond(404, {"error": "unexpected path"})
                return
            self._respond(200, {"package": "eu-iec104-substation", "phase": "ready"})

        def do_POST(self):
            length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(length)
            self.requests.append(("POST", self.path, body))
            if self.path != "/api/firewall/validation-report" or body:
                self._respond(404, {"error": "unexpected request"})
                return
            if scenario == "HTTP_ERROR":
                self._respond(500, {"error": "stub failure"})
                return
            self._respond(200, outcome)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}", Handler.requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def run_script(client, api, out_path=None):
    env = os.environ.copy()
    env["RANGERDANGER_API"] = api
    if client == "sh":
        command = ["bash", str(SH_SCRIPT)]
        if out_path:
            command.extend(["--out", str(out_path)])
    else:
        command = [PWSH, "-NoProfile", "-File", str(PS_SCRIPT)]
        if out_path:
            command.extend(["-Out", str(out_path)])
    return subprocess.run(command, cwd=ROOT, env=env, capture_output=True,
                          text=True, timeout=30)


class ValidationReportScriptTests(unittest.TestCase):
    clients = ("sh", "ps1") if PWSH else ("sh",)

    def assert_requests(self, requests):
        self.assertEqual(
            [(method, path, body) for method, path, body in requests],
            [
                ("GET", "/api/range", b""),
                ("POST", "/api/firewall/validation-report", b""),
            ],
        )

    def test_pass_fail_and_http_error(self):
        cases = (
            ("proof_pass_acceptance_fail", 0, "plan proof: PASS"),
            ("proof_fail_acceptance_pass", 1, "plan proof: FAIL"),
            ("no_plan_acceptance_pass", 0, "Hardened acceptance: PASS"),
            ("no_plan_acceptance_fail", 1, "Hardened acceptance: FAIL"),
            ("HTTP_ERROR", 1, None),
        )
        for client in self.clients:
            for scenario, expected_code, expected_judgment in cases:
                with self.subTest(client=client, scenario=scenario), report_api(scenario) as (api, requests):
                    completed = run_script(client, api)
                    self.assertEqual(completed.returncode, expected_code,
                                     completed.stdout + completed.stderr)
                    if scenario == "HTTP_ERROR":
                        self.assertEqual(completed.stdout, "")
                    else:
                        self.assertEqual(completed.stdout, MARKDOWN)
                        self.assertIn(f"validation-report: {expected_judgment}", completed.stderr)
                    self.assert_requests(requests)

    def test_out_file_receives_markdown_without_stdout(self):
        for client in self.clients:
            with self.subTest(client=client), report_api("no_plan_acceptance_pass") as (api, requests):
                with tempfile.TemporaryDirectory() as directory:
                    out_path = Path(directory) / "validation.md"
                    completed = run_script(client, api, out_path)
                    self.assertEqual(completed.returncode, 0, completed.stderr)
                    self.assertEqual(completed.stdout, "")
                    self.assertEqual(out_path.read_text(), MARKDOWN)
                self.assert_requests(requests)


if __name__ == "__main__":
    unittest.main()
