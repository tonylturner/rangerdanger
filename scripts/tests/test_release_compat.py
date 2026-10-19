import hashlib
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]


class FragmentedSocket:
    def __init__(self, data):
        self.data = data
        self.sent = b""

    def recv(self, count):
        chunk, self.data = self.data[:1], self.data[1:]
        return chunk

    def sendall(self, data):
        self.sent += data


class ProxyProbeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        python = source.split("<<'RFB_PY'\n", 1)[1].split("\nRFB_PY", 1)[0]
        cls.rfb = {"__name__": "offline_fixture"}
        exec(compile(python, "rfb-probe", "exec"), cls.rfb)

    def test_rfb_vnc_auth_fragmented(self):
        sock = FragmentedSocket(b"RFB 003.008\n\x01\x02")
        self.assertEqual(self.rfb["negotiate"](sock), [2])
        self.assertEqual(sock.sent, b"RFB 003.008\n")

    def test_rfb_none_missing_auth_and_truncation_fail(self):
        for data in (b"RFB 003.008\n\x01\x01", b"RFB 003.008\n\x02\x01\x02",
                     b"RFB 003.008\n\x00", b"RFB 003.008\n\x01",
                     b"RFB 003.003\n\x01\x02"):
            with self.subTest(data=data), self.assertRaises(ValueError):
                self.rfb["negotiate"](FragmentedSocket(data))

    def test_proxy_probe_entry_points(self):
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        for required in ("containd-ui-probe.py", ":3000/", "apps/vendor-jump/",
                         "10.20.20.10:80/", "sites-enabled/default", "base + \"programs\""):
            self.assertIn(required, source)
        self.assertIn("RD-22, non-blocking", source)


class CompatTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = (ROOT / "scripts/release-compat-smoke.sh").read_text()
        cls.workflow = yaml.safe_load((ROOT / ".github/workflows/smoke.yml").read_text())

    def test_collision_guard_without_running_any_smoke_or_docker(self):
        # Evaluate ONLY the pure guard, supplying a shell function instead of
        # Docker. Never execute a smoke script, even with mocked executables.
        guard = self.source.split("# Guard BEFORE", 1)[1]
        guard = guard[guard.index("running="):guard.index('mkdir -p "$receipts"')]
        for running, replace, existing, expected in (
                ("foreign-container", 0, 0, 1),
                ("foreign-container", 1, 0, 0), ("", 0, 0, 0),
                ("compat-container", 0, 1, 0), ("", 0, 1, 1),
                ("compat-container", 1, 1, 2)):
            with self.subTest(running=running, replace=replace, existing=existing):
                result = subprocess.run(
                    ["bash", "-c", "docker_bounded() { printf '%s' \"$1\"; }\n"
                     .replace('"$1"', json.dumps(running))
                     + f"\nreplace={replace}\nexisting={existing}\n" + guard],
                    capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, expected, result.stderr)
                if expected and not existing:
                    self.assertIn("REFUSED", result.stderr)

    def test_build_version_is_exact(self):
        function = self.source.split("assert_version() {", 1)[1].split("\n}", 1)[0]
        for version, expected in (("v0.1.35", 0), ("v0.1.36-rc.1", 1), ("latest", 1)):
            with tempfile.TemporaryDirectory() as directory:
                script = ("set -e\nAPI=unused\nreceipts=" + json.dumps(directory)
                          + "\ncurl() { printf '%s' "
                          + "'" + json.dumps({"version": version}) + "'; }\n" + function)
                result = subprocess.run(["bash", "-c", script],
                                        capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, expected, result.stderr)

    def test_no_build_or_pull_in_local_twin(self):
        self.assertIn("export VERSION=v0.1.35", self.source)
        self.assertIn("compose up -d --no-build --pull never", self.source)
        self.assertNotIn("compose build", self.source)
        self.assertNotIn("compose pull", self.source)
        self.assertIn("image inspect", self.source)
        self.assertLess(self.source.index("running=$("), self.source.index("compose down"))
        self.assertNotIn("sixth original smoke gate NOT RUN", self.source)
        self.assertIn('exit "$failed"', self.source)
        self.assertIn("if [ \"$existing\" -ne 1 ]; then\n  started=1", self.source)
        self.assertIn('[ "$started" -eq 1 ] && [ "$keep" -ne 1 ]', self.source)

    def test_all_six_original_gates_and_auxiliary_proxy_run(self):
        gates = self.source.split("for gate in ", 1)[1].split("; do", 1)[0].split()
        self.assertEqual(set(gates), {
            "smoke-test", "proxy-smoke", "firewall-smoke", "lab-commands-smoke",
            "events-smoke", "substation-smoke", "test-suite-smoke"})
        self.assertEqual(len(gates), 7)
        self.assertIn('if [ "$gate" = smoke-test ]; then args=(--existing-stack); fi',
                      self.source)
        hard = next(s for s in self.workflow["jobs"]["release-compat"]["steps"]
                    if s.get("id") == "hard-gates")
        self.assertIn("up -d --no-build --pull never", hard["run"])
        self.assertIn("release-compat-smoke.sh --existing-stack", hard["run"])

    def test_existing_boot_mode_skips_all_lifecycle_and_database_operations(self):
        source = (ROOT / "scripts/smoke-test.sh").read_text()
        # Evaluate only isolated preparation/cleanup functions, not a smoke
        # script. Stubs fail if any lifecycle or DB command is attempted.
        prepare = source.split("prepare_stack() {", 1)[1].split("\n}", 1)[0]
        cleanup = source.split("cleanup() {", 1)[1].split("\n}", 1)[0]
        for function in (prepare, cleanup):
            result = subprocess.run(
                ["bash", "-c", "EXISTING_STACK=1\nKEEP=0\nfail=0\n"
                 "note() { :; }\ndocker() { echo forbidden; return 99; }\n"
                 "rm() { echo forbidden; return 99; }\n"
                 "check() {" + function + "\n}\ncheck"],
                capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn("forbidden", result.stdout)
        self.assertIn('--existing-stack) EXISTING_STACK=1', source)
        self.assertIn("EXISTING_STACK=0", source)

    def test_only_rd01_is_non_blocking(self):
        steps = self.workflow["jobs"]["release-compat"]["steps"]
        waived = [s for s in steps if s.get("continue-on-error")]
        self.assertEqual(len(waived), 1)
        self.assertIn("RD-01", waived[0]["name"])
        self.assertIn("--known-defects --replace", waived[0]["run"])
        hard = next(s for s in steps if s.get("id") == "hard-gates")
        self.assertNotIn("continue-on-error", hard)
        artifacts = next(s for s in steps if s.get("uses", "").startswith("actions/upload-artifact"))
        self.assertEqual(artifacts["if"], "always()")
        self.assertIn("build/release-compat/", artifacts["with"]["path"])
        self.assertIn("21 GB", str(steps))
        self.assertIn("containd:latest", self.source)

    def test_rd01_fixture_is_attributed_to_frozen_generator(self):
        data = json.loads((ROOT / "scripts/tests/fixtures/rd01-full-plan.json").read_text())
        self.assertEqual(data["base"], "5805c11")
        source = (ROOT / data["source"]).read_bytes()
        self.assertEqual(hashlib.sha256(source).hexdigest(), data["source_sha256"])
        self.assertEqual(len(data["selectedActionIds"]), 8)
        # Never quietly replace the full-plan fixture with reference JSON.
        self.assertEqual(data["config"]["firewall"]["rules"][0]["id"], "allow-mgmt-ui")


if __name__ == "__main__":
    unittest.main()
