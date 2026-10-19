"""Offline contracts for content consumed by the frozen v0.1.35 images."""

from pathlib import Path
import ipaddress
import json
import re
import shlex
import subprocess
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = ROOT / "lab-definitions/scenarios"


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
        # Execute only the shell extractor function, never the live smoke gate.
        # Retains its CLI boundary when the inline Python becomes a separate tool.
        shell = (ROOT / "scripts/lab-commands-smoke.sh").read_text()
        function = shell.split("extract_commands() {", 1)[1].split("\n}\n", 1)[0]
        result = subprocess.run(["bash", "-c", "extract_commands() {" + function +
                                 "\n}\nextract_commands"], cwd=ROOT, text=True,
                                capture_output=True, timeout=30, check=True)
        self.assertTrue(result.stdout.strip())
        for line in result.stdout.splitlines():
            name, _, _, _, container, _ = line.split("|", 5)
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


class ErrataContracts(unittest.TestCase):
    def test_reference_workflow_is_visible_outside_injected_steps(self):
        for name in ("remediation-planning", "firewall-implementation",
                     "hardening-configurations", "vendor-rdp-compromise", "validation-evidence"):
            text = scenario(name)["description"]
            with self.subTest(scenario=name):
                for phrase in ("v0.1.35 application images", "not Apply Your Plan",
                               "authorized 8/8", "unauthorized blocked 11/11", "no skipped tests"):
                    self.assertIn(phrase, text)
        ui = (ROOT / "frontend/components/scenario-runner.tsx").read_text()
        self.assertIn("{scenario.description}</p>", ui)
        self.assertNotIn("injectDynamicContent(scenario.description", ui)

    def test_dnp3_and_grading_warnings_are_scenario_level(self):
        for name in ("hardening-configurations", "validation-evidence"):
            text = scenario(name)["description"]
            self.assertIn("Verify a Direct Operate is rejected and reclose remains enabled", text)
        text = scenario("validation-evidence")["description"]
        self.assertIn("may PASS and auto-complete with no report or a FAIL report", text)
        self.assertIn("Do not accept the exercise banner alone", text)
        text = scenario("hardening-configurations")["steps"][3]["description"]
        self.assertIn("DNP3 permits reads only (FC1), even from RTAC", text)

    def test_export_recipes_print_and_copy_without_fictitious_files(self):
        for name in ("firewall-implementation", "validation-evidence"):
            doc = scenario(name)
            text = doc["description"] + "\n" + "\n".join(s["description"] for s in doc["steps"])
            self.assertNotRegex(text, r"export config\s*>")
            self.assertNotIn("cat /tmp/student-policy.json", text)
            self.assertIn("Copy that JSON to your evidence document", text)
        text = scenario("firewall-implementation")["description"]
        self.assertIn("does not support shell redirection", text)
        self.assertIn("CLI JSON-file edit/import exercise is deferred", text)
        self.assertIn("rangerdanger@10.30.30.20 hostname", text)

    def test_rejection_and_historical_audit_warnings_survive_ui_injection(self):
        for name in ("hardening-configurations", "vendor-rdp-compromise"):
            text = scenario(name)["description"]
            self.assertIn("That label is not a packet verdict", text)
            self.assertIn("then Mark Complete", text)
        text = scenario("hardening-configurations")["description"]
        self.assertIn("Record current retest timestamps", text)
        self.assertIn("Current successful unauthorized writes still fail", text)

    def test_vnc_auth_and_headless_fallback_are_explicit(self):
        text = scenario("vendor-rdp-compromise")["steps"][1]["description"]
        self.assertIn("fallback; password `rangerdanger`", text)
        self.assertIn("Native RDP/VNC viewers require a GUI/DISPLAY", text)
        self.assertIn("terminal-only Kali use the existing SSH fallback", text)
        self.assertNotIn("no auth required", text)

    def test_errata_has_repo_only_update_and_every_mitigation(self):
        path = ROOT / "docs/v0.1.36-rc.1-hotfix.md"
        self.assertTrue(path.exists(), "student/instructor errata is missing")
        text = path.read_text()
        self.assertIn("git fetch origin tag v0.1.36-rc.1 && git checkout --detach "
                      "v0.1.36-rc.1 && ./scripts/apply-repo-update.sh", text)
        for phrase in ("How to update", "PowerShell", "scripts/apply-repo-update.ps1",
                       "SSD", "Do not run `./setup.sh --version v0.1.36-rc.1`",
                       "no pull", "no rebuild", "checksum"):
            self.assertIn(phrase, text)
        for number in (1, 2, 3, 5, 6, 8, 12, 15, 18, 19, 23):
            self.assertIn(f"RD-{number:02}", text)


class BaselineContracts(unittest.TestCase):
    def test_observation_key_does_not_invent_writes(self):
        doc = scenario("baseline-assessment")
        text = doc["steps"][5]["description"]
        self.assertRegex(text, r"id=unauth-modbus-writes [^\n]*correct=NOT_OBSERVED")
        self.assertIn("NOT_OBSERVED in the prescribed read-only capture", text)
        self.assertNotIn("eng-ws mbpoll write commands", text)
        self.assertIn("Four categories", text)
        self.assertIn("Four of your passive findings", doc["steps"][6]["description"])
        self.assertNotIn("capture showed (vendor → OT, unauth writes)",
                         doc["steps"][6]["description"])

    def test_flow_statuses_agree_with_reference_policy(self):
        text = scenario("baseline-assessment")["steps"][4]["description"]
        rows = [line for line in text.splitlines() if re.match(r"\| \*\*[A-G]\.", line)]
        self.assertEqual(len(rows), 8)  # E has separate Modbus and DNP3 rows.
        for row in rows:
            with self.subTest(row=row):
                # Status is authored per version, not an assertion these tuples
                # must be denied forever. A future policy and text can change together.
                status = row.split("|")[-2].lower()
                self.assertTrue("preserve" in status or "blocked" in status)
                expected = "DENY" if "blocked" in status else "ALLOW"
                addresses = re.findall(r"`(10\.[^`]+)`", row)
                source, destinations = addresses[:2]
                if "{" in destinations:
                    prefix, suffixes = destinations.split("{")
                    destinations = [prefix + s for s in suffixes.rstrip("}").split(",")]
                else:
                    destinations = [destinations]
                match = re.search(r"\b(UDP|TCP|HTTP|FUXA|Modbus) (\d+)", row)
                self.assertIsNotNone(match)
                protocol = "udp" if match[1] == "UDP" else "tcp"
                for destination in destinations:
                    self.assertEqual(reference_action(source, destination, protocol, match[2]),
                                     expected)
        for letter in "BCD":
            row = next(r for r in rows if f"**{letter}." in r)
            self.assertIn("in v0.1.36", row)
        requirements = scenario("segmentation-requirements")["steps"][2]["description"]
        self.assertNotIn("all five critical conduits", requirements)
        self.assertIn("A/E autonomous and F/G intra-zone", requirements)
        self.assertIn("in this version", requirements)
        self.assertNotIn("deliberately retired", text)

    def test_first_capture_teaches_prompt_transition(self):
        text = scenario("baseline-assessment")["steps"][2]["description"]
        self.assertIn("containd#", text)
        self.assertIn("Type `shell` to enter bash", text)
        self.assertLess(text.index("Type `shell`"), text.index("tcpdump -i"))
        self.assertIn("containd cli", text)
        self.assertIn("| Prompt |", text)

    def test_capture_scope_does_not_hide_management(self):
        text = scenario("baseline-assessment")["steps"][2]["description"]
        self.assertIn("This filter excludes loopback only.", text)
        self.assertIn("10.99.99.0/24", text)
        self.assertIn("does not by itself prove delivery/forwarding", text)
        self.assertIn("'not host 127.0.0.1'", text)
        self.assertNotIn("no management UI traffic", text)

    def test_map_task_uses_policy_rows_not_missing_edges(self):
        text = scenario("baseline-assessment")["steps"][0]["description"]
        self.assertIn("Open Segmentation Policy", text)
        self.assertIn("Policy rows show configured permission", " ".join(text.split()))
        self.assertNotIn("what did the tooltip show", text)
        for decision in ("zones-located", "ent-to-field-protocols", "ot-to-field-protocols"):
            self.assertIn(f"id={decision}", text)


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
        doc = scenario("remediation-planning")
        step = doc["steps"][2]
        self.assertEqual(step["action"]["type"], "decision")
        self.assertIn("Selections save automatically.", step["description"])
        self.assertIn("click Mark Complete", step["description"])
        self.assertIn("Do not use Execute Step", step["description"])
        self.assertIn("Selections save automatically.", doc["description"])

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
