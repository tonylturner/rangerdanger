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
                         "127.0.0.1:80/", "sites-enabled/default", "base + \"programs\""):
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
    def test_eu_commands_follow_gate_with_dependency_and_evidence(self):
        steps = self.workflow["jobs"]["eu-smoke"]["steps"]
        commands = next(s for s in steps if s.get("name") == "EU reviewed lab commands smoke")
        gate = next(s for s in steps if s.get("name") == "EU IEC104 smoke gate")
        dependency = next(s for s in steps if s.get("name") == "Install EU command inventory dependency")
        self.assertLess(steps.index(dependency), steps.index(gate))
        self.assertLess(steps.index(gate), steps.index(commands))
        self.assertEqual(commands["run"], "./scripts/lab-commands-smoke.sh eu-iec104-substation")
        upload = next(s for s in steps if s.get("name") == "Upload EU evidence and logs")
        self.assertEqual(upload["if"], "always()")
        self.assertIn(commands["env"]["LAB_CMD_LOG_DIR"], upload["with"]["path"])

    def test_eu_workshop_suite_runs_last_before_log_collection(self):
        job = self.workflow["jobs"]["eu-smoke"]
        steps = job["steps"]
        commands = next(s for s in steps if s.get("name") == "EU reviewed lab commands smoke")
        suite = next(s for s in steps if s.get("name") == "EU workshop test-suite")
        collect = next(s for s in steps if s.get("name") == "Collect range logs and EU gate evidence")
        self.assertLess(steps.index(commands), steps.index(suite))
        self.assertEqual(steps.index(collect), steps.index(suite) + 1)
        self.assertEqual(suite["run"], "./scripts/test-suite-smoke.sh eu-iec104-substation")
        self.assertEqual(suite["if"], "success() || failure()")
        self.assertEqual(suite["timeout-minutes"], 35)
        self.assertEqual(job["timeout-minutes"], 100)

    def test_every_package_command_guard_runs_offline_in_ci(self):
        ci = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        steps = ci["jobs"]["release-tooling"]["steps"]
        guard = next(s for s in steps if s.get("run") ==
                     "python3 scripts/lib/lab-commands-extract.py --guard")
        self.assertLess(next(i for i, s in enumerate(steps)
                             if s.get("run") == "python3 -m pip install pyyaml"), steps.index(guard))

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
        suite = next(s for s in steps if s.get("name") == "Workshop test-suite")
        self.assertIn("set -euo pipefail", suite["run"])
        self.assertIn("./scripts/test-suite-smoke.sh us-dnp3-substation", suite["run"])
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

    def test_compiled_plans_use_one_local_and_ci_entrypoint(self):
        steps = self.workflow["jobs"]["smoke"]["steps"]
        gates = [s for s in steps if "compiled-plan" in s.get("name", "")]
        self.assertEqual(len(gates), 1)
        self.assertEqual(gates[0]["run"], "./scripts/us-plan-smoke.sh")
        commands = "\n".join(s.get("run", "") for s in steps)
        self.assertNotIn("/api/firewall/apply-plan", commands)
        self.assertFalse([s for s in steps if s.get("continue-on-error")])
        self.assertLess(next(i for i, s in enumerate(steps)
                             if s.get("name") == "Workshop test-suite"), steps.index(gates[0]))
        artifacts = next(s for s in steps if s.get("name") == "Upload gate evidence")
        self.assertEqual(artifacts["if"], "always()")
        self.assertIn("build/smoke-receipts/", artifacts["with"]["path"])

    def plan_script(self, directory):
        source = (ROOT / "scripts/us-plan-smoke.sh").read_text()
        source = "\n".join(
            f"SCRIPT_DIR={shlex.quote(str(ROOT / 'scripts'))}"
            if line.startswith("SCRIPT_DIR=") else line for line in source.splitlines())
        return source.replace("base=build/smoke-receipts", f"base={shlex.quote(directory)}/receipts")

    def run_plan_gate(self, selection, mutation=None, changed_golden=False,
                      restore_fail=False, restore_degraded=False):
        """Execute the script's case runner and cleanup with offline HTTP stubs."""
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
        apply = {"applied": True, "warnings": [], "plan": json.loads(json.dumps(golden)),
                 "containd": {"applyId": "fixture", "protection": "protecting"}}
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
            script = self.plan_script(directory).split('\nrun_case "$catalog"', 1)[0]
            script += f'\nrun_case {shlex.quote(str(fixture / "golden.json"))} "$base/case"\n'
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
    */apply)
      echo restored >>"$fixture/cleanup"
      printf '{"containd":{"applyId":"fixture","protection":"%s"}}\n' "$restore_protection"
      return "$restore_fail" ;;
    */api/range)
      echo '{"package":"us-dnp3-substation","phase":"ready","containd":{"applyId":"fixture","protection":"protecting"}}' ;;
    *) return 99 ;;
  esac
}
'''
            result = subprocess.run(
                ["bash", "-c", f"fixture={shlex.quote(directory)}\n"
                 f"restore_fail={int(restore_fail)}\n"
                 f"restore_protection={'degraded' if restore_degraded else 'protecting'}\n"
                 + stubs + script],
                cwd=ROOT, capture_output=True, text=True, timeout=10)
            self.assertEqual(json.loads((fixture / "sent.json").read_text()), request)
            self.assertEqual((fixture / "cleanup").read_text(), "restored\n")
            return result

    def run_singleton_gate(self, bad_action=None, restore_fail=False):
        """Run the complete catalog-driven US script with local HTTP stubs."""
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            plans = fixture / "plans"
            plans.mkdir()
            for source in (ROOT / "frontend/lib/testdata/plans").glob("us-*.json"):
                (plans / source.name).write_bytes(source.read_bytes())
            script = self.plan_script(directory).replace(
                "frontend/lib/testdata/plans", shlex.quote(str(plans)))
            stubs = r'''
make_report() {
  jq '
    .expectations as $rows
    | all($rows[]; .planVerdict == .hardenedVerdict) as $accepted
    | {
      planProof:{applicable:true,pass:true,hashStable:true,skipped:0,
        rows:[$rows[]|{id,expected:.planVerdict,observed:.planVerdict,pass:true}]},
      acceptance:{pass:$accepted,skipped:0,
        rows:[$rows[]|{id,expected:.hardenedVerdict,observed:.planVerdict,
          pass:(.planVerdict==.hardenedVerdict)}],
        preventionGaps:[$rows[]|select(.planVerdict=="allow"
          and .hardenedVerdict=="deny")|.id]},
      summary:{
        authorized_total:([$rows[]|select(.category=="authorized")]|length),
        authorized_pass:([$rows[]|select(.category=="authorized"
          and .planVerdict==.hardenedVerdict)]|length),
        unauthorized_total:([$rows[]|select(.category=="unauthorized")]|length),
        unauthorized_pass:([$rows[]|select(.category=="unauthorized"
          and .planVerdict==.hardenedVerdict)]|length),
        skipped:0,result:(if $accepted then "PASS" else "FAIL" end)}
    }' "$1"
}
curl() {
  local url="${!#}" data="" golden action
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --data-binary ]; then data="${2#@}"; fi
    shift
  done
  case "$url" in
    */api/firewall/apply-plan)
      case "$data" in
        */rd01/plan.json) action=full ;;
        */rd01-partial/plan.json) action=partial ;;
        *) action=$(jq -er '.actionIds | if length == 1 then .[0] else error("not a singleton") end' "$data") ;;
      esac
      golden="$fixture/plans/us-$action.json"
      echo "$action" >>"$fixture/cases"
      printf '%s\n' "$golden" >"$fixture/current"
      jq -n --slurpfile plan "$golden" \
        '{applied:true,warnings:[],plan:$plan[0],containd:{applyId:"fixture",protection:"protecting"}}' \
        | if [ "$action" = "$bad_action" ]; then jq '.warnings=["fixture warning"]'; else cat; fi
      ;;
    */api/firewall/validation-report)
      make_report "$(cat "$fixture/current")"
      ;;
    */api/firewall/apply)
      echo restored >"$fixture/cleanup"
      [ "$restore_fail" -eq 0 ] || return 1
      echo '{"warnings":[],"containd":{"applyId":"fixture","protection":"protecting"}}'
      ;;
    */api/range)
      echo '{"package":"us-dnp3-substation","phase":"ready","containd":{"applyId":"fixture","protection":"protecting"}}' ;;
    *) return 99 ;;
  esac
}
'''
            result = subprocess.run(
                ["bash", "-c", f"fixture={shlex.quote(directory)}\n"
                 f"bad_action={shlex.quote(bad_action or '')}\n"
                 f"restore_fail={int(restore_fail)}\n" + stubs + script],
                cwd=ROOT, capture_output=True, text=True, timeout=20)
            full = json.loads((plans / "us-full.json").read_text())
            actions = [a["id"] for a in full["actions"] if a["kind"] in ("enforce", "detect")]
            self.assertEqual((fixture / "cleanup").read_text(), "restored\n")
            self.assertTrue((fixture / "receipts/rd01-singletons/restore.json").is_file())
            if bad_action:
                return result
            self.assertEqual((fixture / "cases").read_text().splitlines(),
                             ["full", "partial"] + actions)
            for action in actions:
                case = fixture / f"receipts/rd01-singletons/{action}"
                self.assertEqual(json.loads((case / "plan.json").read_text())["actionIds"], [action])
                self.assertTrue((case / "apply.json").is_file())
                self.assertTrue((case / "report.json").is_file())
            return result

    def test_compiled_singleton_step_executes_each_catalog_action_and_restores(self):
        result = self.run_singleton_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        actions = [a["id"] for a in json.loads(
            (ROOT / "frontend/lib/testdata/plans/us-full.json").read_text())["actions"]
            if a["kind"] in ("enforce", "detect")]
        self.assertNotEqual(self.run_singleton_gate(bad_action=actions[0]).returncode, 0)
        self.assertNotEqual(self.run_singleton_gate(restore_fail=True).returncode, 0)

    def test_plan_steps_execute_against_goldens_and_follow_changed_values(self):
        for selection in ("full", "partial"):
            for changed in (False, True):
                with self.subTest(selection=selection, changed=changed):
                    result = self.run_plan_gate(selection, changed_golden=changed)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotEqual(self.run_plan_gate(selection, restore_fail=True).returncode, 0)
            self.assertNotEqual(self.run_plan_gate(selection, restore_degraded=True).returncode, 0)

    def test_plan_steps_fail_closed_and_restore_weak(self):
        mutations = [
            lambda a, r: a.update(warnings=["fixture warning"]),
            lambda a, r: a.pop("warnings"),
            lambda a, r: a["plan"].update(label="wrong"),
            lambda a, r: a.pop("containd"),
            lambda a, r: a["containd"].update(protection="degraded"),
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
