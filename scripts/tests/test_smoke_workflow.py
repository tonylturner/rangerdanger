import json
import shlex
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
                ("<html><head><title>Welcome to nginx!</title></head></html>", "200"),
                ("<html>kasm novnc webtop</html>", "200"),
                (shell.replace("touch-gamepad-host", "missing"), "200"),
                (shell.replace("<title>Selkies</title>", "<title>missing</title>"), "200")):
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

    def plan_gate(self, selection):
        return next(s for s in self.workflow["jobs"]["smoke"]["steps"]
                    if s.get("name", "").startswith(f"Compiled {selection} plan"))

    def test_compiled_plans_are_golden_backed_hard_gates(self):
        steps = self.workflow["jobs"]["smoke"]["steps"]
        self.assertFalse([s for s in steps if s.get("continue-on-error")])
        for selection in ("full", "partial"):
            check = self.plan_gate(selection)["run"]
            for expected in (f"golden=frontend/lib/testdata/plans/us-{selection}.json",
                             "oracle=scripts/tests/fixtures", 'include "plan-gate"',
                             'plan_request', 'plan_applied($golden[0])',
                             'plan_report($golden[0])', '--slurpfile golden "$golden"',
                             '--data-binary @"$receipts/plan.json"',
                             "/api/firewall/apply-plan", "/api/firewall/validation-report",
                             "trap restore_weak EXIT", "trap - EXIT", '{"config":"weak"}',
                             "set -euo pipefail"):
                self.assertIn(expected, check)
            self.assertNotIn("/apply-custom", check)
            self.assertNotIn("rd01-full-plan.json", check)
        self.assertLess(next(i for i, s in enumerate(steps) if s.get("name") == "Workshop test-suite"),
                        steps.index(self.plan_gate("full")))
        self.assertLess(steps.index(self.plan_gate("full")), steps.index(self.plan_gate("partial")))
        artifacts = next(s for s in steps if s.get("name") == "Upload gate evidence")
        self.assertEqual(artifacts["if"], "always()")
        self.assertIn("build/smoke-receipts/", artifacts["with"]["path"])

    def run_plan_gate(self, selection, mutation=None, changed_golden=False,
                      restore_fail=False):
        """Execute the actual YAML shell with offline HTTP/sleep stubs."""
        golden = json.loads((ROOT / f"frontend/lib/testdata/plans/us-{selection}.json").read_text())
        if changed_golden:
            golden["revision"] += 1
            golden["label"] += "-fixture"
            for action in golden["actions"]:
                action["id"] += "-fixture"
            for row in golden["expectations"]:
                row["id"] += "-fixture"
            extra = dict(next((r for r in golden["expectations"]
                               if r["planVerdict"] != r["hardenedVerdict"]),
                              golden["expectations"][0]))
            extra["id"] += "-extra"
            golden["expectations"].append(extra)
        rows = golden["expectations"]
        gaps = [r["id"] for r in rows
                if r["planVerdict"] == "allow" and r["hardenedVerdict"] == "deny"]
        accepted = all(r["planVerdict"] == r["hardenedVerdict"] for r in rows)
        request = {k: golden[k] for k in ("packageId", "revision")}
        request["actionIds"] = [a["id"] for a in golden["actions"] if a["chosen"]]
        apply = {"applied": True, "warnings": [], "plan": json.loads(json.dumps(golden))}
        report = {
            "planProof": {"applicable": True, "pass": True, "hashStable": True, "skipped": 0,
                          "rows": [{"id": r["id"], "expected": r["planVerdict"],
                                    "observed": r["planVerdict"], "pass": True} for r in rows]},
            "acceptance": {"pass": accepted, "skipped": 0, "preventionGaps": gaps,
                           "rows": [{"id": r["id"], "expected": r["hardenedVerdict"],
                                     "observed": r["planVerdict"],
                                     "pass": r["planVerdict"] == r["hardenedVerdict"]}
                                    for r in rows]},
            "summary": {"skipped": 0, "result": "PASS" if accepted else "FAIL"}}
        for category in ("authorized", "unauthorized"):
            subset = [r for r in rows if r["category"] == category]
            report["summary"][category + "_total"] = len(subset)
            report["summary"][category + "_pass"] = sum(
                r["planVerdict"] == r["hardenedVerdict"] for r in subset)
        if mutation:
            mutation(apply, report)
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            for name, data in (("golden", golden), ("apply", apply), ("report", report)):
                (fixture / f"{name}.json").write_text(json.dumps(data))
            script = self.plan_gate(selection)["run"]
            receipt_line = next(line for line in script.splitlines() if line.startswith("receipts="))
            script = script.replace(receipt_line, f"receipts={shlex.quote(directory)}/receipts")
            script = script.replace(f"frontend/lib/testdata/plans/us-{selection}.json",
                                    shlex.quote(str(fixture / "golden.json")))
            stubs = r'''
curl() {
  local url="${!#}"
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --data-binary ]; then cp "${2#@}" "$fixture/sent.json"; fi
    shift
  done
  case "$url" in
    */apply-plan) cat "$fixture/apply.json" ;;
    */validation-report) cat "$fixture/report.json" ;;
    */apply) echo restored >>"$fixture/cleanup"; return "$restore_fail" ;;
    *) return 99 ;;
  esac
}
sleep() { :; }
'''
            result = subprocess.run(
                ["bash", "-c", f"fixture={shlex.quote(directory)}\n"
                 f"restore_fail={int(restore_fail)}\n" + stubs + script],
                cwd=ROOT, capture_output=True, text=True, timeout=10)
            self.assertEqual(json.loads((fixture / "sent.json").read_text()), request)
            self.assertEqual((fixture / "cleanup").read_text(), "restored\n")
            return result

    def test_plan_steps_execute_against_goldens_and_follow_changed_values(self):
        for selection in ("full", "partial"):
            for changed in (False, True):
                with self.subTest(selection=selection, changed=changed):
                    result = self.run_plan_gate(selection, changed_golden=changed)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotEqual(self.run_plan_gate(selection, restore_fail=True).returncode, 0)

    def test_plan_steps_fail_closed_and_restore_weak(self):
        mutations = [
            lambda a, r: a.update(warnings=["fixture warning"]),
            lambda a, r: a.pop("warnings"),
            lambda a, r: a["plan"].update(label="wrong"),
            lambda a, r: r["planProof"].update(applicable=False),
            lambda a, r: r["planProof"].update({"pass": False}),
            lambda a, r: r["planProof"].update(hashStable=False),
            lambda a, r: r["planProof"].update(skipped=1),
            lambda a, r: r["planProof"].update(rows=[]),
            lambda a, r: r["acceptance"].update(skipped=1),
            lambda a, r: r["acceptance"].update(rows=[]),
            lambda a, r: r["acceptance"].update({"pass": not r["acceptance"]["pass"]}),
            lambda a, r: r["acceptance"]["preventionGaps"].append("unexpected"),
            lambda a, r: r["summary"].update(unauthorized_total=0),
        ]
        for selection in ("full", "partial"):
            for index, mutation in enumerate(mutations):
                with self.subTest(selection=selection, mutation=index):
                    self.assertNotEqual(self.run_plan_gate(selection, mutation).returncode, 0)
        result = self.run_plan_gate(
            "partial", lambda a, r: r["acceptance"]["preventionGaps"].pop())
        self.assertNotEqual(result.returncode, 0)

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
