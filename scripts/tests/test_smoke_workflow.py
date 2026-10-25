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
        self.assertIn("grep -qx '/etc/nginx/sites-available/default'", source)

    def desktop_verdict(self, body, code):
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        self.assertEqual(source.count("-w '\\n%{http_code}\\n'"), 2)
        self.assertIn("is_desktop_shell() {", source)
        function = source.split("is_desktop_shell() {", 1)[1].split("\n}", 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            receipt = Path(directory) / "desktop.html"
            receipt.write_text(body + "\n" + code + "\n")
            # Evaluate only the pure receipt oracle, never a smoke script.
            return subprocess.run(
                ["bash", "-c", "is_desktop_shell() {" + function +
                 '\n}\nis_desktop_shell "$1"', "fixture", str(receipt)],
                capture_output=True, text=True, timeout=5).returncode

    def test_selkies_desktop_shell_without_old_brand_keywords_passes(self):
        shell = (ROOT / "scripts/tests/fixtures/vendor-selkies-shell.html").read_text()
        for keyword in ("kasm", "novnc", "webtop"):
            self.assertNotIn(keyword, shell.lower())
        self.assertEqual(self.desktop_verdict(shell, "200"), 0)
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        for receipt in ("vendor-3000.html", "desktop.html"):
            self.assertIn(f'is_desktop_shell "$logs/{receipt}"', source)

    def test_desktop_oracle_rejects_bad_status_portal_and_missing_markers(self):
        shell = (ROOT / "scripts/tests/fixtures/vendor-selkies-shell.html").read_text()
        for body, code in (
                (shell, "502"), (shell, "302"), (shell, "000"),
                ("<h1>Vendor Remote Access Portal</h1>", "200"),
                ("<html>kasm novnc webtop</html>", "200"),
                (shell.replace("touch-gamepad-host", "missing"), "200"),
                (shell.replace("universalTouchGamepad.js", "missing.js"), "200")):
            with self.subTest(body=body, code=code):
                self.assertNotEqual(self.desktop_verdict(body, code), 0)


class SmokeWorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = yaml.safe_load((ROOT / ".github/workflows/smoke.yml").read_text())

    def test_all_six_original_gates_and_auxiliary_proxy_run(self):
        gates = ("smoke-test", "proxy-smoke", "firewall-smoke", "lab-commands-smoke",
                 "events-smoke", "substation-smoke", "test-suite-smoke")
        self.assertNotIn("release-compat", self.workflow["jobs"])
        steps = self.workflow["jobs"]["smoke"]["steps"]
        commands = "\n".join(s.get("run", "") for s in steps)
        self.assertNotIn("release-compat-smoke.sh", commands)
        for gate in gates:
            if gate != "smoke-test":  # boot/API/inventory are inline in the built job
                self.assertIn(f"./scripts/{gate}.sh", commands)
        probe = next(s for s in steps if s.get("name") == "Probe API endpoints")["run"]
        self.assertIn("set -euo pipefail", probe)
        self.assertIn("jq -e . /tmp/health.json", probe)
        self.assertIn("jq -e . /tmp/build.json", probe)

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

    def test_compiled_full_plan_is_a_hard_gate(self):
        steps = self.workflow["jobs"]["smoke"]["steps"]
        self.assertFalse([s for s in steps if s.get("continue-on-error")])
        gate = next(s for s in steps if s.get("name", "").startswith("Compiled full plan"))
        check = gate["run"]
        for expected in ("scripts/tests/fixtures/rd01-full-plan.json", "/api/firewall/apply-plan",
                         "/api/firewall/validation-report", ".authorized_pass == 8",
                         ".authorized_total == 8", ".unauthorized_pass == 13",
                         ".unauthorized_total == 13", ".skipped == 0", '.result == "PASS"',
                         ".planProof.applicable", ".planProof.pass", ".planProof.hashStable",
                         ".acceptance.pass", '.warnings == []',
                         "trap restore_weak EXIT", "trap - EXIT", '{"config":"weak"}'):
            self.assertIn(expected, check)
        self.assertNotIn("/apply-custom", check)
        self.assertLess(next(i for i, s in enumerate(steps) if s.get("name") == "Workshop test-suite"),
                        steps.index(gate))
        artifacts = next(s for s in steps if s.get("name") == "Upload gate evidence")
        self.assertEqual(artifacts["if"], "always()")
        self.assertIn("build/smoke-receipts/", artifacts["with"]["path"])
        self.assertIn("receipts=build/smoke-receipts/rd01", check)

    def test_full_plan_fixture_selects_the_entire_current_catalog(self):
        data = json.loads((ROOT / "scripts/tests/fixtures/rd01-full-plan.json").read_text())
        package = ROOT / "lab-definitions/packages/us-dnp3-substation"
        curriculum = yaml.safe_load((package / "curriculum.yml").read_text())
        manifest = yaml.safe_load((package / "package.yml").read_text())
        self.assertEqual(set(data), {"packageId", "revision", "actionIds"})
        self.assertEqual(data["packageId"], manifest["id"])
        self.assertEqual(data["revision"], manifest["revision"])
        self.assertEqual(data["actionIds"], [a["id"] for a in curriculum["catalog"]])

    def test_possibly_empty_arrays_expand_safely_on_macos_bash(self):
        # macOS /bin/bash 3.2 aborts on "${empty[@]}" under set -u, and the
        # EXIT trap then sees $?=0: a silent green. Arrays that may stay empty
        # (assigned "=()", no "-gt 0" guard) must use ${x[@]+"${x[@]}"}.
        import re
        for script in sorted((ROOT / "scripts").glob("*.sh")):
            source = script.read_text()
            for name in set(re.findall(r"^\s*(\w+)=\(\)", source, re.M)):
                if re.search(r"\$\{#" + name + r"\[@\]\}\"? -gt 0", source):
                    continue  # guarded non-empty before any expansion
                bare = re.compile(r'(?<!\+)"\$\{' + name + r'\[@\]\}"')
                with self.subTest(script=script.name, array=name):
                    self.assertIsNone(bare.search(source), f"use ${{{name}[@]+...}}")


if __name__ == "__main__":
    unittest.main()
