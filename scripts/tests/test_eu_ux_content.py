"""Student-facing mechanics and source-restriction coverage in both packages."""
import json
import ipaddress
import re
import unittest
from pathlib import Path

import yaml

from scripts.tests.curriculum import read_curriculum

ROOT = Path(__file__).resolve().parents[2]


class LearningContracts(unittest.TestCase):
    def packages(self):
        for package_id in ("eu-iec104-substation", "us-dnp3-substation"):
            base = ROOT / "lab-definitions/packages" / package_id
            manifest = yaml.safe_load((base / "package.yml").read_text())
            yield base, manifest, read_curriculum(base)

    def test_every_restrict_eligible_requirement_has_block_actions(self):
        parser = (ROOT / "frontend/lib/scenario-description.ts").read_text()
        default = json.loads(re.search(
            r"DECISION_DEFAULT_OPTIONS = (\[[^\n]+]);", parser)[1])
        checked = 0
        for base, manifest, curriculum in self.packages():
            scenario_id = manifest["curriculum"]["roles"]["requirements"]
            doc = yaml.safe_load(
                (base / manifest["scenarios"] / f"{scenario_id}.yml").read_text())
            declarations = {}
            for step in doc["steps"]:
                for line in step["description"].splitlines():
                    match = re.match(r":::decision id=([\w-]+)(.*)", line)
                    if match:
                        options = re.search(r'options="?([^"]+?)(?=\s+\w+=|"$|$)', match[2])
                        declarations[match[1]] = options[1].split(",") if options else default
            catalog = {action["id"] for action in curriculum["catalog"]}
            for requirement_id, requirement in curriculum["requirements"].items():
                with self.subTest(package=manifest["id"], requirement=requirement_id):
                    self.assertIn(requirement_id, declarations)
                    if "RESTRICT" not in declarations[requirement_id]:
                        continue
                    actions = requirement["impl"].get("RESTRICT", [])
                    self.assertTrue(actions, "RESTRICT must not disappear from coverage")
                    self.assertEqual(actions, requirement["impl"]["BLOCK"])
                    self.assertTrue(set(actions) <= catalog)
                    checked += 1
        self.assertEqual(checked, 10)

    def test_weak_demonstrations_warn_before_reset_and_restore_via_rollback_or_backup_with_plan_catch_up_only(self):
        for base, manifest, _ in self.packages():
            prefix = "eu-" if manifest["id"].startswith("eu-") else ""
            for name in ("hardening-configurations", "vendor-rdp-compromise"):
                doc = yaml.safe_load((base / manifest["scenarios"] /
                                      f"{prefix}{name}.yml").read_text())
                first = " ".join(doc["steps"][0]["description"].split())
                with self.subTest(scenario=doc["id"]):
                    for phrase in (
                            "Before proceeding, save your confirmed Lab 2.2 running export and Notes.",
                            "Use **Reset to Weak** for this lesson's initial demonstration only.",
                            "It replaces the running policy, but `rollback` can restore your Lab 2.2 policy",
                            "one level only, and only while no other commit came after Reset to Weak.",
                            "Do not apply another policy or commit edits in between.",
                            "If that previous-running slot has changed, import your Lab 2.2 backup and commit it",
                            "**Apply Your Plan** is catch-up for students without their own policy, "
                            "not restoration of hand-written rules.",
                            "An empty selection loads Weak; without a saved request, "
                            "use the full Hardened reference instead."):
                        self.assertIn(phrase, first)
                    self.assertLess(first.index("Before proceeding, save"),
                                    first.index("Use **Reset to Weak**"))
                    apply = next(step["description"] for step in doc["steps"]
                                 if step["id"] in ("apply-the-hardened-policy",
                                                   "apply-hardened-policy"))
                    self.assertTrue(apply.startswith(
                        "Restore your own Lab 2.2 policy after restoring the plant."))
                    self.assertRegex(apply, r"\n    rollback\n")
                    recovery, hint = apply.split(":::hint Optional catch-up", 1)
                    recovery = " ".join(recovery.split())
                    for phrase in (
                            "Only if no other commit came after **Reset to Weak**",
                            "This restores the previous running config immediately; it is one level only.",
                            "A second rollback does not go further back.",
                            "do not accidentally commit it.",
                            "If another commit came after Reset to Weak, use "
                            "`import config /data/lab-2.2-policy.json`",
                            "stage the saved Lab 2.2 backup.",
                            "Review the full diff and commit to activate it.",
                            "`commit confirmed <seconds>`, check management and required operational "
                            "conduits, then `confirm` before the deadline.",
                            "Read back the running rules and record the running identity and UTC time."):
                        self.assertIn(phrase, recovery)
                    hint = " ".join(hint.split())
                    for phrase in (
                            "For students without their own Lab 2.2 policy, **Apply Your Plan**",
                            "it does not import hand-written rules.",
                            "An empty selection loads Weak.",
                            "Without a saved request, use **Apply Hardened** for the full reference.",
                            "Read back the running rules and record the identity and UTC time.",
                            "Do not use it before rollback: another commit replaces the "
                            "previous-running slot needed to recover your own policy."):
                        self.assertIn(phrase, hint)

    def test_capture_marker_keeps_reviewed_capture_lifecycle(self):
        from scripts.lib.lab_commands_inventory import extract
        for base, manifest, _ in self.packages():
            prefix = "eu-" if manifest["id"].startswith("eu-") else ""
            doc = yaml.safe_load((base / manifest["scenarios"] /
                                  f"{prefix}validation-evidence.yml").read_text())
            start, review = doc["steps"][1]["description"], doc["steps"][3]["description"]
            if manifest["id"].startswith("eu-"):
                self.assertTrue(start.startswith(
                    "### Do this\n- Start the interactive **fw-1** capture below before any source test;"))
                self.assertIn("**Start the boundary capture now", start)
            else:
                self.assertTrue(start.startswith("**Start the boundary capture now"))
            self.assertRegex(start, r":::interactive\n    tcpdump .* -G 1200 -W 1 .*validation.pcap")
            self.assertIn("second fw-1 terminal", start)
            self.assertIn("-e frame.time_utc", review)
            self.assertIn("not applicable — default drop", review)
            self.assertIn("Default-drop negatives have no rule-hit", review)
            self.assertIn("timeout + source-zone SYN + destination-zone", review)
            self.assertIn("Save your evidence before any reset", review)
            rows = [row for row in extract(package_id=manifest["id"])
                    if row["scenario"] == doc["id"] and row["command"].startswith("tcpdump ")]
            self.assertEqual(len(rows), 1)
            self.assertEqual((rows[0]["node"], rows[0]["prompt"], rows[0]["entry"],
                              rows[0]["action"]), ("fw-1", "shell", "exit", "RUN"))

    def test_worked_rule_mechanics_exist_in_both_render_paths(self):
        for base, manifest, curriculum in self.packages():
            scenario_id = manifest["curriculum"]["roles"]["implementation"]
            doc = yaml.safe_load(
                (base / manifest["scenarios"] / f"{scenario_id}.yml").read_text())
            static = next(step["description"] for step in doc["steps"]
                          if step["id"] in ("implement-rules",
                                            "phase-3-create-minimal-required-rules"))
            for text in (static, curriculum["phases"]["rules"]):
                for phrase in ("**Create rule**", "add firewall rule",
                               "set firewall rule", "move firewall rule",
                               "deferred", "logged diagnostic denies"):
                    self.assertIn(phrase, text)
            commands = lambda text: [line.strip() for line in text.splitlines()
                                     if re.match(r"\s+(?:add|set|move) firewall rule", line)]
            self.assertEqual(commands(static), commands(curriculum["phases"]["rules"]))

    def test_eu_steps_lead_with_three_to_five_actions(self):
        base = ROOT / "lab-definitions/packages/eu-iec104-substation"
        counts = {"eu-baseline-assessment": 8, "eu-firewall-implementation": 9,
                  "eu-validation-evidence": 6}
        for name, count in counts.items():
            doc = yaml.safe_load((base / "scenarios" / f"{name}.yml").read_text())
            self.assertEqual(len(doc["steps"]), count)
            for step in doc["steps"]:
                with self.subTest(scenario=name, step=step["id"]):
                    lead = step["description"].split("\n\n", 1)[0].splitlines()
                    self.assertEqual(lead[0], "### Do this")
                    self.assertTrue(3 <= len(lead[1:]) <= 5)
                    self.assertTrue(all(line.startswith("- ") for line in lead[1:]))

    def test_eu_phase_prose_has_one_authority(self):
        base = ROOT / "lab-definitions/packages/eu-iec104-substation"
        authored = yaml.safe_load((base / "curriculum/implementation.yml").read_text())
        resolved = read_curriculum(base)
        doc = yaml.safe_load((base / "scenarios/eu-firewall-implementation.yml").read_text())
        steps = {step["id"]: step["description"].strip() for step in doc["steps"]}
        for phase, step_id in (("rules", "implement-rules"),
                               ("positive", "test-positive"), ("negative", "test-negative")):
            with self.subTest(phase=phase):
                template = authored["phases"][phase]
                self.assertTrue(template.startswith("{{step:" + step_id + "}}\n\n### "))
                # Only the reference and generated section are authored here.
                lines = [line for line in template.splitlines() if line]
                self.assertEqual(len(lines), 4 if phase == "rules" else 3)
                self.assertTrue(resolved["phases"][phase].startswith(steps[step_id] + "\n\n### "))
                self.assertNotIn("{{step:", resolved["phases"][phase])

    def test_reference_negative_classes_match_ordered_policy(self):
        cases = {
            "eu-iec104-substation": [
                ("10.10.10.50", "wan", "10.30.30.20", "lan1", 2404, "deny-unauthorized-iec104"),
                ("10.20.20.10", "dmz", "10.40.40.21", "lan2", 502, "deny-unauthorized-modbus"),
                ("10.30.30.30", "lan1", "10.40.40.20", "lan2", 502, "deny-unauthorized-modbus"),
                ("10.10.10.50", "wan", "10.30.30.30", "lan1", 8080, "deny-unauthorized-plc-web"),
                ("10.20.20.10", "dmz", "10.70.70.40", "lan5", 8081, "deny-unauthorized-historian-ingest"),
                ("10.60.60.20", "lan4", "10.30.30.50", "lan1", 2404, "deny-unauthorized-iec104"),
                ("10.10.10.50", "wan", "10.20.20.10", "dmz", 3389, None),
                ("10.20.20.10", "dmz", "10.70.70.40", "lan5", 8080, None),
            ],
            "us-dnp3-substation": [
                ("10.10.10.50", "wan", "10.30.30.20", "lan1", 20000, "deny-enterprise-to-ot"),
                ("10.10.10.50", "wan", "10.40.40.20", "lan2", 20000, "deny-enterprise-to-field"),
                ("10.20.20.10", "dmz", "10.30.30.20", "lan1", 502, "deny-vendor-to-ot"),
                ("10.20.20.20", "dmz", "10.40.40.20", "lan2", 502, "deny-vendor-to-field"),
                ("10.30.30.40", "lan1", "10.40.40.22", "lan2", 20000, "deny-ot-to-field-direct"),
                ("10.10.10.50", "wan", "10.20.20.10", "dmz", 5900, None),
            ],
        }

        def address_matches(rule, field, address):
            return not rule.get(field) or any(
                ipaddress.ip_address(address) in ipaddress.ip_network(cidr)
                for cidr in rule[field])

        for base, manifest, _ in self.packages():
            policy_path = (base / "firewall/improved.json" if manifest["id"].startswith("eu-")
                           else ROOT / "lab-definitions/firewall/substation-improved.json")
            policy = json.loads(policy_path.read_text())["firewall"]
            self.assertEqual(policy["defaultAction"], "DENY")
            for src, src_zone, dst, dst_zone, port, expected in cases[manifest["id"]]:
                with self.subTest(package=manifest["id"], src=src, dst=dst, port=port):
                    deciding = next((rule for rule in policy["rules"]
                                     if rule.get("ics", {}).get("mode") != "monitor"
                                     and src_zone in rule["sourceZones"]
                                     and dst_zone in rule["destZones"]
                                     and address_matches(rule, "sources", src)
                                     and address_matches(rule, "destinations", dst)
                                     and any(p["name"] == "tcp" and p["port"] == str(port)
                                             for p in rule["protocols"])), None)
                    self.assertEqual(deciding["id"] if deciding else None, expected)
                    if deciding:
                        self.assertEqual(deciding["action"], "DENY")
                        self.assertTrue(deciding.get("log"))
