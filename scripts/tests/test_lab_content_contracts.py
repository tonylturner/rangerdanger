"""Shared helpers and cross-package contracts for workshop content."""

import importlib.util
import ipaddress
import json
import re
import unittest
from pathlib import Path

import yaml

from scripts.tests.curriculum import read_curriculum

ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = ROOT / "lab-definitions/scenarios"


def _load_inventory():
    """Load the offline command grammar, never a live smoke gate."""
    spec = importlib.util.spec_from_file_location(
        "lab_commands_inventory", ROOT / "scripts/lib/lab_commands_inventory.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def scenario(name):
    return yaml.safe_load((SCENARIOS / f"{name}.yml").read_text())


def reference_action(source, destination, protocol, port):
    """Evaluate the fixture's ordered zone/address/port rules, not a live firewall."""
    def zone(address):
        octet = address.split(".")[1]
        return {"10": "wan", "20": "dmz", "30": "lan1", "40": "lan2"}[octet]

    policy = json.loads((ROOT / "lab-definitions/firewall/substation-improved.json").read_text())
    for rule in policy["firewall"]["rules"]:
        if zone(source) not in rule["sourceZones"] or zone(destination) not in rule["destZones"]:
            continue
        if rule.get("sources") and not any(
            ipaddress.ip_address(source) in ipaddress.ip_network(cidr) for cidr in rule["sources"]
        ):
            continue
        if not any(p["name"] == protocol and p["port"] == str(port) for p in rule["protocols"]):
            continue
        return rule["action"]
    return policy["firewall"]["defaultAction"]


class TerminalContracts(unittest.TestCase):
    def test_all_declared_and_extracted_sources_have_tabs(self):
        topology = yaml.safe_load((ROOT / "lab-definitions/substation-segmentation.yml").read_text())
        containers = {n["container"]: n["id"] for n in topology["nodes"] if n.get("container")}
        docs = {p.stem: scenario(p.stem) for p in SCENARIOS.glob("*.yml")}
        for name, doc in docs.items():
            for step in doc["steps"]:
                sources = [step["node"]] if step.get("node") else []
                sources.extend(t["from"] for t in step.get("action", {}).get("targets", [])
                               if t.get("from"))
                for source in sources:
                    with self.subTest(scenario=name, source=source):
                        self.assertIn(source, doc["nodes"])
        # Import only the offline extractor, never the live smoke gate.
        spec = importlib.util.spec_from_file_location(
            "lab_commands_extract", ROOT / "scripts/lib/lab-commands-extract.py")
        extractor = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(extractor)
        rows = extractor.extract(ROOT)
        self.assertTrue(rows)
        for row in rows:
            name, container = row["scenario"], row["container"]
            with self.subTest(scenario=name, container=container):
                self.assertIn(containers[container], docs[name]["nodes"])

    def test_named_manual_sources_are_available_in_order(self):
        expected = {
            "baseline-assessment": ["fw-1", "eng-ws-1", "kali-1", "rtac-1"],
            "hardening-configurations": ["kali-1", "rtac-1"],
            "firewall-implementation": ["fw-1", "kali-1", "eng-ws-1", "rtac-1", "vendor-jump-1"],
            "validation-evidence": ["kali-1", "vendor-jump-1", "eng-ws-1", "rtac-1",
                                    "fw-1", "historian-1"],
        }
        for name, nodes in expected.items():
            with self.subTest(scenario=name):
                self.assertEqual(scenario(name)["nodes"], nodes)


class CrossPackageContracts(unittest.TestCase):
    def test_all_decisions_explain_answers_and_no_decision_offers_log(self):
        package_scenarios = [
            ROOT / "lab-definitions/packages/us-dnp3-substation/scenarios",
            ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios",
        ]
        paths = list(SCENARIOS.glob("*.yml"))
        for directory in package_scenarios:
            paths.extend(directory.glob("*.yml"))

        decision_count = 0
        for path in paths:
            doc = yaml.safe_load(path.read_text())
            for step in doc.get("steps", []):
                lines = step.get("description", "").splitlines()
                for index, line in enumerate(lines):
                    if not re.match(r"^\s*:::decision\b", line):
                        continue
                    decision_count += 1
                    closing = next(
                        (end for end in range(index + 1, len(lines)) if lines[end].strip() == ":::"),
                        None,
                    )
                    self.assertIsNotNone(closing, f"{path}: unclosed decision")
                    block = lines[index:closing + 1]
                    with self.subTest(scenario=doc["id"], step=step["id"], decision=line.strip()):
                        self.assertEqual(
                            sum(item.strip() == ":::why" for item in block),
                            1,
                            f"{path}: decision lacks exactly one :::why",
                        )
                        self.assertTrue(
                            "\n".join(block).split(":::why", 1)[-1].removesuffix(":::").strip(),
                            f"{path}: empty decision explanation",
                        )
                    options = re.search(r'\boptions=(?:"([^"]*)"|\'([^\']*)\'|([^\s]+))', line)
                    if options:
                        values = next(value for value in options.groups() if value is not None).split(",")
                        self.assertNotIn("LOG", values, f"{path}: standalone LOG decision option")
        self.assertGreater(decision_count, 0)

    def test_attack_and_retest_steps_never_send_simulated_commands(self):
        paths = list(SCENARIOS.glob("*.yml"))
        paths.extend((ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios").glob("*.yml"))

        def sends_command(action):
            return action.get("type") == "sequence" or any(
                sends_command(child) for child in action.get("steps", [])
            )

        reviewed = 0
        for path in paths:
            doc = yaml.safe_load(path.read_text())
            for step in doc["steps"]:
                # Restoration sequences remain operator actions, not attacks.
                if not (step["id"].startswith("attack-") or step["id"] in {
                    "re-test-the-attack-under-the-hardened-policy",
                    "use-the-vendor-foothold-to-attack-field-devices",
                    "re-attempt-the-pivot",
                }):
                    continue
                reviewed += 1
                with self.subTest(scenario=doc["id"], step=step["id"]):
                    self.assertFalse(sends_command(step.get("action") or {}))
        self.assertEqual(reviewed, 8)

    def test_check_run_entries_are_verbatim_step_run_blocks(self):
        # A check action may carry run: the wire commands the check presumes
        # the student sent this step (sent by the Run button, replayed by the
        # workshop test suite, ignored by read-only Execute Step). Every entry
        # must be one of the step's own Run blocks, on that block's node, so
        # the command text has exactly one source — the YAML the student runs.
        inventory = _load_inventory()
        attack_steps = {
            "re-test-the-attack-under-the-hardened-policy",
            "use-the-vendor-foothold-to-attack-field-devices",
            "re-attempt-the-pivot",
        }
        packages = [
            SCENARIOS,
            ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios",
        ]
        entries = 0
        carriers = 0
        attack_carriers = 0
        for directory in packages:
            for path in sorted(directory.glob("*.yml")):
                doc = yaml.safe_load(path.read_text())
                for step in doc["steps"]:
                    action = step.get("action") or {}
                    run = action.get("run") or []
                    if run:
                        self.assertEqual(action.get("type"), "check",
                                         f"{path}: run is only valid on a check action")
                    is_attack = step["id"].startswith("attack-") or step["id"] in attack_steps
                    if is_attack and action.get("type") == "check":
                        attack_carriers += 1
                        self.assertTrue(run, f"{path}: {step['id']} attack/retest check lacks run")
                    if not run:
                        continue
                    carriers += 1
                    blocks = inventory.description_commands(step.get("description") or "")
                    for entry in run:
                        entries += 1
                        with self.subTest(scenario=doc["id"], step=step["id"], command=entry["command"]):
                            sources = [src or step.get("node")
                                       for cmd, _, src, _ in blocks if cmd == entry["command"]]
                            self.assertTrue(
                                sources,
                                f"{path}: run command {entry['command']!r} is not a Run block of this step")
                            self.assertIn(
                                entry["node"], sources,
                                f"{path}: run node {entry['node']!r} is not the block's inferred node")
        # The 8 attack/retest checks in both packages all carry run, and no
        # other step does, so the rule is neither vacuous nor over-broad.
        self.assertEqual(attack_carriers, 8)
        self.assertEqual(carriers, 8)
        self.assertGreaterEqual(entries, 8)

    def test_lab_text_names_plan_actions_by_catalog_title(self):
        # Students pick actions by title in the planner and see firewall rule
        # IDs in containd; a raw plan-action ID in lab prose names neither.
        # Knowledge articles document the pipeline and keep the IDs.
        for package, directory in (
            ("us-dnp3-substation", SCENARIOS),
            ("eu-iec104-substation",
             ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios"),
        ):
            base = ROOT / "lab-definitions/packages" / package
            curriculum = read_curriculum(base)
            catalog = {action["id"]: action["title"] for action in curriculum["catalog"]}
            self.assertTrue(catalog)
            scenarios = sorted(directory.glob("*.yml"))
            self.assertEqual(len(scenarios), 7, scenarios)
            texts = {path.name: path.read_text() for path in scenarios}
            texts["curriculum/implementation.yml"] = (base / "curriculum/implementation.yml").read_text()
            cited = set()
            for name, text in texts.items():
                with self.subTest(package=package, path=name):
                    for action_id, title in catalog.items():
                        # Bare or backticked; a longer slug such as a rule ID
                        # that merely contains the action ID is not a hit.
                        self.assertNotRegex(text, rf"(?<![a-z0-9_-]){re.escape(action_id)}(?![a-z0-9_-])")
                        if title in text:
                            cited.add(action_id)
            # The titles are in use, so the rule is not vacuous.
            self.assertGreaterEqual(len(cited), 5, sorted(cited))

    def test_evidence_status_carries_from_implementation_to_validation(self):
        for directory, prefix in (
            (SCENARIOS, ""),
            (ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios", "eu-"),
        ):
            implementation = yaml.safe_load((directory / f"{prefix}firewall-implementation.yml").read_text())
            validation = yaml.safe_load((directory / f"{prefix}validation-evidence.yml").read_text())
            source = "\n".join(step["description"] for step in implementation["steps"])
            target = validation["steps"][0]["description"]
            with self.subTest(package=prefix or "us"):
                self.assertIn(":::decision id=change-request-review", source)
                self.assertIn(f":::findings-panel from={implementation['id']}", target)
                for kind in ("policy", "positive", "negative", "capture"):
                    opening = next(line for line in source.splitlines()
                                   if f":::decision id=evidence-{kind} " in line)
                    self.assertNotIn("correct=", opening)
                    self.assertIn(f"evidence-{kind}:", target)
                for step in validation["steps"]:
                    for line in step["description"].splitlines():
                        if line.startswith(":::decision"):
                            self.assertNotIn("correct=", line)

    def test_section_one_conduit_and_criteria_questions_match_across_packages(self):
        scenarios = {
            "us": yaml.safe_load((SCENARIOS / "baseline-assessment.yml").read_text()),
            "eu": yaml.safe_load((ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios/eu-baseline-assessment.yml").read_text()),
        }

        def questions(doc, step_id):
            step = next(item for item in doc["steps"] if item["id"] == step_id)
            lines = step["description"].splitlines()
            out = []
            for index, line in enumerate(lines):
                if not re.match(r"^\s*:::decision\b", line):
                    continue
                end = next(pos for pos in range(index + 1, len(lines)) if lines[pos].strip() == ":::")
                why = next((pos for pos in range(index + 1, end) if lines[pos].strip() == ":::why"), end)
                out.append((line.strip(), " ".join(item.strip() for item in lines[index + 1:why]).strip()))
            return out

        conduit_questions = {
            name: questions(doc, "critical-conduits-what-must-flow")
            for name, doc in scenarios.items()
        }
        self.assertEqual(
            [question for _, question in conduit_questions["us"]],
            [question for _, question in conduit_questions["eu"]],
        )
        for name, decisions in conduit_questions.items():
            with self.subTest(package=name):
                self.assertEqual(
                    [re.search(r"\bid=([a-z-]+)", opening).group(1) for opening, _ in decisions],
                    ["required-cross-zone-conduits", "intra-zone-dependencies", "continuity-with-minimal-conduits"],
                )
        criteria_questions = {
            name: questions(yaml.safe_load((SCENARIOS / "segmentation-requirements.yml").read_text())
                           if name == "us"
                           else yaml.safe_load((ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios/eu-segmentation-requirements.yml").read_text()),
                           "define-success-criteria-for-segmentation")
            for name in scenarios
        }
        self.assertEqual(
            [question for _, question in criteria_questions["us"]],
            [question for _, question in criteria_questions["eu"]],
        )
        self.assertEqual(len(criteria_questions["us"]), 3)


class ActionContracts(unittest.TestCase):
    def test_explicit_policy_application_is_non_mutating(self):
        for name in ("firewall-implementation", "hardening-configurations",
                     "vendor-rdp-compromise"):
            doc = scenario(name)
            steps = [s for s in doc["steps"] if
                     s["id"] in ("phase-3-create-minimal-required-rules", "apply-hardened-policy", "apply-the-hardened-policy")]
            self.assertEqual(len(steps), 1)
            with self.subTest(scenario=name):
                self.assertEqual(steps[0]["action"],
                                 {"type": "check", "expect": {"firewall_config": "improved"}})
                self.assertEqual(steps[0].get("expected_config"), "hardened")
                if name == "firewall-implementation":
                    self.assertIn("Implement your approved Lab 1.4 change request", steps[0]["description"])
                else:
                    self.assertIn("without replacing it", steps[0]["description"])

    def test_eu_policy_phases_are_non_mutating_checks(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios"
        phases = (
            ("eu-firewall-implementation.yml", "implement-rules"),
            ("eu-hardening-configurations.yml", "apply-the-hardened-policy"),
            ("eu-vendor-rdp-compromise.yml", "apply-hardened-policy"),
        )
        for filename, step_id in phases:
            with self.subTest(step=step_id):
                doc = yaml.safe_load((package / filename).read_text())
                step = next(item for item in doc["steps"] if item["id"] == step_id)
                self.assertEqual(
                    step["action"],
                    {"type": "check", "expect": {"firewall_config": "improved"}},
                )
                self.assertNotRegex(
                    step["description"],
                    r"(?i)Execute Step.{0,100}(?:applies|loads|replaces)",
                )
                if step_id != "implement-rules":
                    for phrase in ("without replacing it", "running identity", "full diff", "read back"):
                        self.assertIn(phrase, step["description"])


    def test_planning_keeps_decision_ui_and_manual_completion(self):
        doc = scenario("remediation-planning")
        step = doc["steps"][2]
        self.assertEqual(step["action"]["type"], "decision")
        self.assertIn("Select a defensible subset in the planner.", step["description"])
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", step["description"])
        self.assertIn("remain planning-only", step["description"])
        self.assertNotRegex(step["description"], r"`[a-z]+-[a-z-]+`")
        self.assertNotIn("Selection order does not set rule order", step["description"])

    def test_change_request_deliverable_matches_in_both_packages(self):
        us = scenario("remediation-planning")
        eu = yaml.safe_load((ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios/eu-remediation-planning.yml").read_text())
        self.assertEqual(us["steps"][-1]["description"], eu["steps"][-1]["description"])
        for doc in (us, eu):
            text = "\n".join(step["description"] for step in doc["steps"])
            self.assertNotIn("Apply Your Plan", text)
            self.assertIn("change-request scope", text)
            for field in ("Change ID", "Findings and approved scope", "Rule changes", "Order | Source host/zone",
                          "Roles and labor hours", "Dependencies", "Maintenance window", "Pre-change running export",
                          "review full diff", "single commit", "Positive acceptance tests", "Negative acceptance tests",
                          "Abort / rollback triggers", "Rollback owner", "Restore steps", "named risk owner",
                          "Requester:", "Implementer:", "OT reviewer", "Security reviewer", "Approver"):
                self.assertIn(field, doc["steps"][-1]["description"])

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


class TimingContracts(unittest.TestCase):
    # Guided golden-path estimates from the rc.3 student walkthrough QA.
    MINUTES = {
        "baseline-assessment": 20,
        "segmentation-requirements": 10,
        "remediation-planning": 12,
        "firewall-implementation": 22,
        "hardening-configurations": 12,
        "vendor-rdp-compromise": 12,
        "validation-evidence": 7,
    }

    def test_card_chip_time_budget_and_overview_agree(self):
        overview = (ROOT / "docs/workshop-overview.md").read_text()
        for name, minutes in self.MINUTES.items():
            doc = scenario(name)
            with self.subTest(scenario=name):
                self.assertEqual(doc["estimated_minutes"], minutes)
                budgets = re.findall(r"\*\*Time budget:\*\* ~(\d+) minutes", doc["steps"][0]["description"])
                self.assertEqual(budgets, [str(minutes)])
                self.assertRegex(overview, rf"\| `{name}` \| [^|]+ \| ~{minutes} min \|")
        core = sum(m for n, m in self.MINUTES.items() if n != "vendor-rdp-compromise")
        self.assertEqual(core, 83)
        self.assertIn(f"Total ≈ {core} min for the 6 core labs (Guided path), "
                      f"+{self.MINUTES['vendor-rdp-compromise']} min for the bonus.", overview)


if __name__ == "__main__":
    unittest.main()
