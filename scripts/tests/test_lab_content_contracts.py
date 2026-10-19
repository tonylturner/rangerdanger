"""Offline contracts for content consumed by the frozen v0.1.35 images."""

from pathlib import Path
import re
import shlex
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = ROOT / "lab-definitions/scenarios"


def scenario(name):
    return yaml.safe_load((SCENARIOS / f"{name}.yml").read_text())


class ActionContracts(unittest.TestCase):
    def test_explicit_policy_application_is_non_mutating(self):
        for name in ("firewall-implementation", "hardening-configurations",
                     "vendor-rdp-compromise"):
            doc = scenario(name)
            steps = [s for s in doc["steps"] if
                     s["title"].startswith(("Phase 3", "Apply hardened", "Apply the hardened"))]
            self.assertEqual(len(steps), 1)
            with self.subTest(scenario=name):
                self.assertEqual(steps[0]["action"],
                                 {"type": "check", "expect": {"firewall_config": "improved"}})
                self.assertEqual(steps[0].get("expected_config"), "hardened")
                self.assertIn("without replacing it", doc["description"])

    def test_planning_keeps_decision_ui_and_manual_completion(self):
        step = scenario("remediation-planning")["steps"][2]
        self.assertEqual(step["action"]["type"], "decision")
        self.assertIn("Selections save automatically.", step["description"])
        self.assertIn("click Mark Complete", step["description"])
        self.assertIn("Do not use Execute Step", step["description"])

    def test_frozen_runner_action_accounting(self):
        # evaluateTestStep auto-passes missing actions and decisions only.
        expected = {
            "baseline-assessment": (5, 0, 3),
            "firewall-implementation": (4, 0, 5),
            "hardening-configurations": (0, 0, 6),
            "remediation-planning": (3, 1, 0),
            "segmentation-requirements": (3, 0, 0),
            "validation-evidence": (1, 0, 2),
            "vendor-rdp-compromise": (1, 0, 6),
        }
        auto_total = 0
        for name, counts in expected.items():
            steps = scenario(name)["steps"]
            manual = sum(not s.get("action") for s in steps)
            decision = sum(s.get("action", {}).get("type") == "decision" for s in steps)
            self.assertEqual((manual, decision, len(steps) - manual - decision), counts)
            auto_total += manual + decision
        self.assertEqual(auto_total, 18)


class CommandContracts(unittest.TestCase):
    def test_dnp3poll_flags_precede_endpoint_and_durations_have_units(self):
        for path in sorted(SCENARIOS.glob("*.yml")):
            for number, line in enumerate(path.read_text().splitlines(), 1):
                for match in re.finditer(r"\bdnp3poll ((?:-|10\.)[^`\n]+)", line):
                    command = match.group(1).strip()
                    with self.subTest(path=path.name, line=number):
                        self.assertRegex(
                            command, r"^-a \d+ -t \d+(?:ms|s) 10\.[\d.]+:20000$"
                        )

    def test_modbus_examples_use_supported_reads(self):
        for path in [*sorted(SCENARIOS.glob("*.yml")),
                     ROOT / "docs/lab-authoring.md"]:
            for number, line in enumerate(path.read_text().splitlines(), 1):
                for match in re.finditer(r"\bmbpoll ([^`\n]+)", line):
                    args = shlex.split(match.group(1), comments=True)
                    if "-t" not in args:
                        continue
                    kind = args[args.index("-t") + 1]
                    with self.subTest(path=path.name, line=number):
                        self.assertNotEqual(kind, "1", "FC2 is not implemented")
                        if kind == "0" and any(
                            ip in args for ip in ("10.40.40.20", "10.40.40.23")
                        ):
                            count = int(args[args.index("-c") + 1]) if "-c" in args else 1
                            self.assertLessEqual(count, 4)

    def test_generated_command_overrides_are_scenario_level(self):
        description = scenario("firewall-implementation")["description"]
        for fragment in ("Phase 5/6", "dnp3poll -a 2 -t 3s 10.40.40.21:20000",
                         "mbpoll -a 1 -r 1 -c 4 -1 -t 0"):
            self.assertIn(fragment, description)


if __name__ == "__main__":
    unittest.main()
