"""Keep authoring guidance aligned with policy-derived scenario behavior."""
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class LabAuthoringDocsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = (ROOT / "docs/lab-authoring.md").read_text()

    def test_expected_config_is_a_package_owned_expectation_not_an_apply(self):
        start = self.text.index("### `expected_config`")
        end = self.text.index("### `node`", start)
        section = " ".join(self.text[start:end].split())
        for phrase in ("active package", "| `weak` |", "| `improved` |",
                       "| `hardened` |", "`expected_config` does not apply policy",
                       "guidance, not a one-click reset"):
            self.assertIn(phrase, section)
        self.assertNotIn("improved` is rejected", section)
        self.assertNotIn("substation-weak.json", section)
        self.assertNotIn("substation-improved.json", section)

    def test_probe_verdict_comes_from_policy_not_an_authored_outcome(self):
        start = self.text.index("- `probe` -")
        end = self.text.index("\n- ", start + 1)
        section = self.text[start:end]
        for phrase in ("There is no `probe.outcome` field",
                       "expected verdict derives from the judging policy",
                       "expected_config: weak", "reference or compiled plan"):
            self.assertIn(phrase, section)
        self.assertNotIn("Set `outcome:", section)

    def test_smoke_checklist_requires_explicit_package_selection(self):
        start = self.text.index("## Authoring checklist")
        self.assertIn("./scripts/lab-commands-smoke.sh --package <package-id>",
                      self.text[start:])
