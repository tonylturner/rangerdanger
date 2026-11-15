"""Non-EU reset and SRA admission, without a range or Docker."""
import importlib
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts/lib"))
worker = importlib.import_module("lab_commands_smoke")


class NonEUResetTests(unittest.TestCase):
    def test_us_and_sra_reset_require_success_and_protection(self):
        for package in (worker.US, worker.SRA):
            for success, protection in ((True, "protecting"), (False, "protecting"),
                                        (True, "degraded"), ("true", "protecting")):
                with self.subTest(package=package, success=success, protection=protection):
                    response = {"success": success, "containd": {"applyId": "id", "protection": protection}}
                    lifecycle = worker.Lifecycle(package, "http://fixture", ".")
                    with patch.object(worker, "request", return_value=response) as request:
                        if success is True and protection == "protecting":
                            lifecycle.reset()
                        else:
                            with self.assertRaises(ValueError):
                                lifecycle.reset()
                        request.assert_called_once_with("http://fixture", "/api/workshop/reset", {}, timeout=180)

    def test_non_eu_transport_failure_is_not_ignored(self):
        with patch.object(worker, "request", side_effect=TimeoutError("reset timeout")):
            with self.assertRaises(TimeoutError):
                worker.Lifecycle(worker.SRA, "http://fixture", ".").reset()

    def test_sra_adapter_reaches_preflight(self):
        with tempfile.TemporaryDirectory() as tmp, \
                patch.object(worker, "extract", return_value=[]), \
                patch.object(worker.gate, "expectations_path", return_value=Path(tmp) / "expectations.json"), \
                patch.object(worker.gate, "validate_inventory", return_value=0), \
                patch.object(worker, "preflight", side_effect=RuntimeError("preflight reached")) as preflight:
            (Path(tmp) / "expectations.json").write_text("{}")
            with self.assertRaisesRegex(RuntimeError, "preflight reached"):
                worker.run(worker.SRA, "", "http://fixture", tmp, 10)
            preflight.assert_called_once()


class NonEUResetHTTPTests(unittest.TestCase):
    def setUp(self):
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                self.server.requests.append((self.command, self.path, json.loads(body)))
                payload = json.dumps({"success": self.server.success,
                                      "containd": {"applyId": "real-http", "protection": "protecting"}}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *_args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.requests = []
        self.server.success = True
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": .01})
        self.thread.start()
        self.addCleanup(self.close)
        self.lifecycle = worker.Lifecycle(worker.SRA, f"http://127.0.0.1:{self.server.server_port}", ".")

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        self.assertFalse(self.thread.is_alive())

    def test_real_reset_posts_and_consumes_its_receipt(self):
        self.lifecycle.reset()
        self.assertEqual(self.server.requests, [("POST", "/api/workshop/reset", {})])

    def test_real_partial_reset_is_not_success(self):
        self.server.success = False
        with self.assertRaisesRegex(ValueError, "workshop reset failed"):
            self.lifecycle.reset()
        self.assertEqual(len(self.server.requests), 1)
