import copy
import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
HELPER = ROOT / "scripts/lib/suite-accounting.py"
FIXTURES = ROOT / "scripts/tests/fixtures/suite-accounting"


class SuiteAccountingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("accounting", HELPER)
        cls.accounting = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.accounting)

    def test_positive_fixture(self):
        table = self.accounting.expected_table(FIXTURES)
        response = json.loads((FIXTURES / "valid.json").read_text())
        self.assertIn("example | 1 | 1 | 1", self.accounting.validate(response, table))

    def test_bad_fixtures_fail_cli(self):
        fixtures = sorted(FIXTURES.glob("bad-*"))
        self.assertGreaterEqual(len(fixtures), 5)
        for path in fixtures:
            with self.subTest(path=path.name):
                result = subprocess.run(
                    [sys.executable, str(HELPER), "--scenarios", str(FIXTURES)],
                    input=path.read_text(), capture_output=True, text=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("suite accounting:", result.stderr)

    def test_current_yaml_table_not_golden_counts(self):
        table = self.accounting.expected_table(ROOT / "lab-definitions/scenarios")
        self.assertEqual(len(table), 7)
        self.assertEqual(
            {s["kind"] for s in table["remediation-planning"]}, {"manual", "decision"})
        altered = copy.deepcopy(table)
        altered["remediation-planning"][0]["kind"] = "executed"
        self.assertNotEqual(table, altered)

    def test_eu_package_yaml_is_reconciled_by_the_smoke_gate(self):
        package = "eu-iec104-substation"
        scenarios_dir = ROOT / "lab-definitions/packages" / package / "scenarios"
        table = self.accounting.expected_table(scenarios_dir)
        self.assertEqual(len(table), 7)
        scenarios = []
        auto_passed = total = 0
        for scenario_id, expected_steps in table.items():
            steps = []
            for index, expected in enumerate(expected_steps):
                auto_pass = expected["kind"] != "executed"
                auto_passed += int(auto_pass)
                total += 1
                steps.append({
                    "step_index": index,
                    "step_title": expected["title"],
                    "auto_pass": auto_pass,
                    "passed": True,
                })
            scenarios.append({
                "scenario_id": scenario_id,
                "steps": steps,
                "passed": True,
                "reset_ok": True,
            })
        self.assertEqual(total, 43)
        suite = {
            "scenarios": scenarios,
            "total_tests": total,
            "passed": total,
            "failed": 0,
            "auto_passed": auto_passed,
            "reset_failures": 0,
        }

        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            fake_bin = fixture / "bin"
            fake_bin.mkdir()
            curl = fake_bin / "curl"
            curl.write_text("""#!/usr/bin/env bash
set -euo pipefail
url="${!#}"
printf '%s\\n' "$*" >>"$CURL_TRACE"
case "$url" in
  */api/health) printf '{"status":"ok"}\\n' ;;
  */api/range) printf '%s\\n' "$FIXTURE_RANGE_STATUS" ;;
  */api/workshop/test-suite) cat "$FIXTURE_SUITE_RESPONSE" ;;
  *) exit 99 ;;
esac
""")
            curl.chmod(0o755)
            response = fixture / "suite.json"
            response.write_text(json.dumps(suite))
            trace = fixture / "curl.log"
            environment = os.environ.copy()
            environment.update({
                "PATH": str(fake_bin) + os.pathsep + environment["PATH"],
                "RANGERDANGER_API": "http://fixture.invalid",
                "FIXTURE_SUITE_RESPONSE": str(response),
                "CURL_TRACE": str(trace),
                "FIXTURE_RANGE_STATUS": json.dumps({
                    "package": package,
                    "phase": "ready",
                    "containd": {"applyId": "fixture", "protection": "protecting"},
                }, separators=(",", ":")),
            })
            result = subprocess.run(
                ["bash", "scripts/test-suite-smoke.sh", package],
                cwd=ROOT, env=environment, capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn(f"range {package} ready", result.stdout)
            self.assertIn(f"passed={total}/{total}", result.stdout)
            self.assertIn("--max-time 1800 -X POST http://fixture.invalid/api/workshop/test-suite",
                          trace.read_text())

            trace.write_text("")
            environment["FIXTURE_RANGE_STATUS"] = json.dumps({
                "package": "us-dnp3-substation",
                "phase": "ready",
                "containd": {"applyId": "fixture", "protection": "protecting"},
            }, separators=(",", ":"))
            result = subprocess.run(
                ["bash", "scripts/test-suite-smoke.sh", package],
                cwd=ROOT, env=environment, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("/api/workshop/test-suite", trace.read_text())
            self.assertIn(f"range is not {package} ready", result.stderr)

    def test_failure_names_each_failed_step_and_reset(self):
        response = json.loads((FIXTURES / "valid.json").read_text())
        scenario = response["scenarios"][0]
        scenario["steps"][0].update(passed=False, detail="1 checks, all pass: false")
        scenario.update(passed=False, reset_ok=False, reset_detail="relay did not close")
        response.update(passed=2, failed=1, reset_failures=1)
        with self.assertRaises(ValueError) as caught:
            self.accounting.validate(response, self.accounting.expected_table(FIXTURES))
        message = str(caught.exception)
        self.assertIn('example step 0 "Execute a check": 1 checks, all pass: false', message)
        self.assertIn("example reset: relay did not close", message)

    def test_auto_pass_true_is_not_execution(self):
        response = json.loads((FIXTURES / "valid.json").read_text())
        response["scenarios"][0]["steps"][0]["auto_pass"] = True
        with self.assertRaisesRegex(ValueError, "auto_pass"):
            self.accounting.validate(response, self.accounting.expected_table(FIXTURES))


if __name__ == "__main__":
    unittest.main()
