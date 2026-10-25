import copy
import importlib.util
import json
import subprocess
import sys
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

    def test_auto_pass_true_is_not_execution(self):
        response = json.loads((FIXTURES / "valid.json").read_text())
        response["scenarios"][0]["steps"][0]["auto_pass"] = True
        with self.assertRaisesRegex(ValueError, "auto_pass"):
            self.accounting.validate(response, self.accounting.expected_table(FIXTURES))


if __name__ == "__main__":
    unittest.main()
