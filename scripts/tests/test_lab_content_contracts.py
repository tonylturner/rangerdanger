"""Offline contracts for content consumed by the frozen v0.1.35 images."""

from pathlib import Path
import importlib.util
import ipaddress
import json
import re
import shlex
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = ROOT / "lab-definitions/scenarios"

# Exact decoded headers from 5805c11 (v0.1.35); no Git dependency at test time.
RELEASE_HEADERS = {
    "baseline-assessment": (
        'Before you can segment a substation network, you need to understand two things: '
        'what traffic actually exists between zones, and which of those flows are required '
        "for normal operations. In this exercise you'll capture live traffic at the "
        'firewall, use it to identify the critical conduits operations depends on, and frame'
        ' concrete success criteria for segmentation - without building diagrams or '
        'matrices.\n'
    ),
    "firewall-implementation": (
        'You have identified the required flows, designed your requirements, and planned '
        'which remediations to execute. Now you implement. You will replace the permissive '
        'weak baseline on the containd NGFW with a least-privilege policy - applying it with'
        ' one click on the Guided track, or authoring the rules yourself on the Advanced '
        'track. By the end, unauthorized cross-zone traffic will be blocked and logged, '
        'while critical substation operations continue without interruption.\n'
    ),
    "hardening-configurations": (
        'Stress-test the policy you built in Lab 2.2 against the canonical '
        'distribution-substation attack: a DNP3 Direct Operate (FC05) injection from Kali '
        "that disables the recloser's auto-reclose and trips it, turning a brief flicker "
        "into a sustained downstream outage. You'll run the attack under the weak baseline "
        "(it succeeds), then under the hardened policy (it's blocked - by L4 source-pinning,"
        ' by ICS DPI, or both, which is the defense-in-depth lesson). Optional Modbus '
        'FC5/FC6 bonus attacks in the same step show the same defense generalizes.\n'
    ),
    "remediation-planning": (
        'You have findings from the baseline assessment but not unlimited time, people, or '
        'political capital. Pick which remediations to execute this quarter - knowing some '
        "attack paths stay open because you can't fix everything at once. Your choices carry"
        ' forward: skip vendor-access restriction and the later vendor-compromise attack '
        'stays easier; skip protocol-aware filtering and the Modbus/DNP3 abuse labs stay '
        'easier. Your plan has consequences.\n'
    ),
    "segmentation-requirements": (
        'Using the baseline assessment findings, define what communications should be '
        'allowed between each zone pair. Apply the principle of least privilege: deny by '
        'default, allow only named flows required for operations. Then compare your '
        'requirements to the improved containd configuration to see how they translate to '
        'firewall rules.\n'
    ),
    "validation-evidence": (
        'After applying the hardened firewall configuration in Lab 2.2 and stress-testing it'
        ' in Lab 2.3, this exercise builds the formal evidence package: positive tests '
        'confirming authorized flows still work, negative tests confirming unauthorized '
        'flows are blocked, and PCAP evidence from the firewall. You generate it in one '
        'click against the policy you have active - then read it like a reviewer would. This'
        ' is the validation step required before any segmentation change goes to production.\n'
    ),
    "vendor-rdp-compromise": (
        'Optional companion to Lab 2.3. Phished vendor credentials let an attacker RDP/VNC '
        'into the vendor jump box from an enterprise foothold, then pivot east-west to OT '
        "field devices. You'll walk the kill chain under the weak baseline, then apply "
        'containd rules that block remote-access protocols (RDP/3389, VNC/5900) from '
        'enterprise into the vendor DMZ - cutting the lateral path before it ever reaches '
        'OT.\n'
    ),
}

# Reviewed false claims: plan-driven attacks (reference-only enforcement retires them)
# and "DPI or the pin" (on the frozen images only the L4 source pin stops the write).
HEADER_EXCEPTIONS = {
    "hardening-configurations": (
        "Stress-test the policy you built in Lab 2.2 against the canonical "
        "distribution-substation attack: a DNP3 Direct Operate (FC05) injection from Kali "
        "that disables the recloser's auto-reclose and trips it, turning a brief flicker "
        "into a sustained downstream outage. You'll run the attack under the weak baseline "
        "(it succeeds), then under the hardened policy (it's blocked by the L4 source pin, "
        "the control that enforces on this build; the ICS DPI layer records the attempt). "
        "The lesson: verify enforcement by device state, not by policy labels. Optional "
        "Modbus FC5/FC6 bonus attacks in the same step are closed by the same source pin.\n"
    ),
    "remediation-planning": (
        "You have findings from the baseline assessment but not unlimited time, people, "
        "or political capital. Pick which remediations to execute this quarter - knowing "
        "some attack paths stay open because you can't fix everything at once. "
        "Your plan guides design review; workshop enforcement uses the reference policy.\n"
    ),
}


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


class ErrataContracts(unittest.TestCase):
    def test_headers_match_release_except_reviewed_false_claim(self):
        self.assertEqual({p.stem for p in SCENARIOS.glob("*.yml")}, set(RELEASE_HEADERS))
        for name, original in RELEASE_HEADERS.items():
            with self.subTest(scenario=name):
                self.assertEqual(scenario(name)["description"],
                                 HEADER_EXCEPTIONS.get(name, original))

    def test_lab_text_has_no_release_strings(self):
        for path in SCENARIOS.glob("*.yml"):
            with self.subTest(path=path.name):
                self.assertNotRegex(path.read_text(), r"(?i)\bv\d+\.\d+(?:\.\d+)?|\brc\.\d+")

    def test_lab22_titles_cannot_trigger_frozen_body_replacement(self):
        # Frozen frontend/lib/scenario-runner-logic.ts:9-11; copy, do not import.
        fragments = ("create minimal", "phase 3", "validate allowed", "phase 5",
                     "validate blocked", "phase 6")
        steps = scenario("firewall-implementation")["steps"]
        for step in steps:
            with self.subTest(title=step["title"]):
                self.assertFalse(any(f in step["title"].lower() for f in fragments))
        for number, step in enumerate(steps[2:8], 1):
            self.assertTrue(step["title"].startswith(f"Stage {number} - "))
        self.assertNotRegex(" ".join(s["description"] for s in steps), r"[Pp]hases? [1-6]")

    def test_workshop_notes_are_short_and_at_the_action(self):
        expected = {
            "remediation-planning": {2: ("Selections save automatically.", "click Mark Complete")},
            "firewall-implementation": {
                4: ("not **Apply Your Plan**", "without replacing it"),
                7: ("authorized 8/8", "unauthorized blocked 11/11", "0 skipped"),
                8: ("does not support shell redirection", "CLI JSON-file edit/import exercise is deferred")},
            "hardening-configurations": {
                3: ("not **Apply Your Plan**", "without replacing it"),
                4: ("not a packet verdict", "does not prove FC5/6 rejection"),
                5: ("PARTIAL may include", "Record current retest timestamps")},
            "vendor-rdp-compromise": {
                4: ("not **Apply Your Plan**", "without replacing it"),
                5: ("not a packet verdict", "then Mark Complete")},
            "validation-evidence": {
                0: ("not **Apply Your Plan**",),
                1: ("authorized 8/8", "unauthorized blocked 11/11", "0 skipped",
                    "Trust the report, not the banner.")},
        }
        for name, locations in expected.items():
            steps = scenario(name)["steps"]
            for index, phrases in locations.items():
                text = steps[index]["description"]
                with self.subTest(scenario=name, step=index + 1):
                    self.assertEqual(text.count("**Workshop note:**"), 1)
                    note = text.split("**Workshop note:**\n", 1)[1].split("\n\n", 1)[0]
                    self.assertGreaterEqual(len(re.findall(r"^- ", note, re.M)), 1)
                    self.assertLessEqual(len(re.findall(r"^- ", note, re.M)), 3)
                    for phrase in phrases:
                        self.assertIn(phrase, note)
                        self.assertEqual(sum(s["description"].count(phrase) for s in steps), 1)
        for path in SCENARIOS.glob("*.yml"):
            self.assertNotIn("Workshop errata", path.read_text())
            for step in scenario(path.stem)["steps"]:
                for note in step["description"].split("**Workshop note:**\n")[1:]:
                    bullets = re.findall(r"^- ", note.split("\n\n", 1)[0], re.M)
                    self.assertTrue(1 <= len(bullets) <= 3, path.name)

    def test_dnp3_and_self_check_details_are_in_rendered_steps(self):
        for name in ("hardening-configurations", "validation-evidence"):
            # Neither of these labs has Lab 2.2's generated-step replacement.
            step_index = 4 if name == "hardening-configurations" else 1
            text = scenario(name)["steps"][step_index]["description"]
            self.assertIn("does not prove FC5/6 rejection", text)
            self.assertIn("Enforcement proof is device state" if step_index == 4
                          else "Protocol-level enforcement evidence is Lab 2.3's re-test", text)
        text = scenario("validation-evidence")["steps"][1]["description"]
        self.assertIn("may PASS and auto-complete with no report or a FAIL report", text)
        self.assertIn("Trust the report, not the banner.", text)
        self.assertIn("The report is your proof:", text)
        self.assertIn("Save the report, PCAP and notes.", text)
        self.assertIn("check Hardened reference is active, fix the ✗ rows, and re-run", text)
        text = " ".join(scenario("hardening-configurations")["steps"][3]["description"].split())
        self.assertIn("the reference DNP3 rule permits reads only (FC1), even from RTAC", text)
        self.assertIn("records a Direct Operate sent from the RTAC as a firewall event, "
                      "but it does not stop that write", text)

    def test_export_recipes_print_and_copy_without_fictitious_files(self):
        for name in ("firewall-implementation", "validation-evidence"):
            doc = scenario(name)
            text = doc["description"] + "\n" + "\n".join(s["description"] for s in doc["steps"])
            self.assertNotRegex(text, r"export config\s*>")
            self.assertNotIn("cat /tmp/student-policy.json", text)
            self.assertIn("Copy that JSON to your evidence document", text)
        text = " ".join(scenario("firewall-implementation")["steps"][8]["description"].split())
        self.assertIn("does not support shell redirection", text)
        self.assertIn("CLI JSON-file edit/import exercise is deferred", text)
        self.assertIn("Browser export is optional; the copied CLI JSON is your evidence.", text)

    def test_rejection_and_historical_audit_details_are_in_rendered_steps(self):
        for name in ("hardening-configurations", "vendor-rdp-compromise"):
            step_index = 4 if name == "hardening-configurations" else 5
            text = scenario(name)["steps"][step_index]["description"]
            self.assertIn("That label is not a packet verdict", text)
            self.assertIn("then Mark Complete", text)
        text = scenario("hardening-configurations")["steps"][5]["description"]
        self.assertIn("Record current retest timestamps", text)
        self.assertIn("compare those events with earlier weak-policy writes", text)
        self.assertIn("Current successful unauthorized writes still fail", " ".join(text.split()))

    def test_workshop_guidance_has_no_instructor_grading_framing(self):
        paths = [
            *(ROOT / "lab-definitions").rglob("*.yml"),
            ROOT / "docs/v0.1.36-hotfix.md",
            ROOT / "docs/quickstart.md",
            ROOT / "docs/workshop-ssd.md",
            ROOT / "stage-ssd-repo-update.sh",
        ]
        grading = re.compile(
            r"\binstructor\s+(?:review\w*|verif\w*|sign[- ]off)"
            r"|\bfinal acceptance\b|\bseparate review\b|\bcourse completion\b"
            r"|\bdo not accept\b|\brequire the (?:actual )?report\b"
            r"|\bbeing graded against\b", re.I)
        for path in paths:
            with self.subTest(path=str(path.relative_to(ROOT))):
                self.assertNotRegex(path.read_text(), grading)
        text = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        self.assertIn("**Not covered by automated tests:**", text)
        self.assertIn("Trust the report, not the banner.", text)
        self.assertIn("Browser export/download is optional; the copied CLI JSON is your evidence.",
                      text)

    def test_vnc_auth_and_headless_fallback_are_explicit(self):
        text = scenario("vendor-rdp-compromise")["steps"][1]["description"]
        self.assertIn("fallback; password `rangerdanger`", text)
        self.assertIn("Native RDP/VNC viewers require a GUI/DISPLAY", text)
        self.assertIn("terminal-only Kali use the existing SSH fallback", text)
        self.assertNotIn("no auth required", text)

    def test_instructor_reference_matches_new_lab22_navigation(self):
        text = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        for step in scenario("firewall-implementation")["steps"][2:8]:
            self.assertIn(step["title"], text)
        self.assertIn("no longer see the plan-driven rule table", text)
        self.assertIn("not in labs", text)
        self.assertNotRegex(text, r"[Pp]hases? [1-6]")

    def test_errata_has_repo_only_update_and_every_mitigation(self):
        path = ROOT / "docs/v0.1.36-hotfix.md"
        self.assertTrue(path.exists(), "student/instructor errata is missing")
        text = path.read_text()
        self.assertIn("git fetch origin tag v0.1.36-rc.2 && git checkout --detach "
                      "v0.1.36-rc.2 && ./scripts/apply-repo-update.sh", text)
        for phrase in ("How to update", "PowerShell", "scripts/apply-repo-update.ps1",
                       "SSD", "Do not run `./setup.sh --version v0.1.36-rc.2`",
                       "no pull", "no rebuild", "checksum"):
            self.assertIn(phrase, text)
        for number in (1, 2, 3, 5, 6, 8, 12, 15, 18, 19, 23):
            self.assertIn(f"RD-{number:02}", text)


class EnforcementContracts(unittest.TestCase):
    """Lab 2.3 states what the frozen images do: the L4 source pin blocks, DPI records."""

    OVERCLAIMS = (
        "blocks Direct Operate even from the RTAC",
        "would catch it even if L4 were loosened",
        "same DPI defense",
        "independent defenses",
        "Direct Operate must still be rejected",
        "Verify a Direct Operate is rejected",
        "blocked at the DPI layer even from the RTAC",
    )

    def flat(self, text):
        return " ".join(text.split())

    def test_keyed_answers_name_the_source_pin(self):
        text = scenario("hardening-configurations")["steps"][4]["description"]
        decisions = {m.group(1): m.group(0)
                     for m in re.finditer(r":::decision id=([\w-]+) [^\n]*", text)}
        self.assertIn("correct=L4_SOURCE_PIN", decisions["retest-defense-in-depth"])
        self.assertNotIn("correct=EITHER_LAYER", decisions["retest-defense-in-depth"])
        self.assertIn("correct=NORMAL_OPS", decisions["retest-hmi-state"])
        # The keyed HMI answer is asked about the Kali re-test, which is NORMAL_OPS for
        # every student; the optional RTAC write comes after it with its own restore.
        self.assertLess(text.index("id=retest-hmi-state"), text.index("RTAC-origin write"))
        self.assertIn("after the Kali re-test", text)

    def test_rtac_write_hint_states_outcome_and_restore(self):
        steps = scenario("hardening-configurations")["steps"]
        hint = steps[4]["description"].split(":::hint Optional: see what the DPI layer", 1)[1]
        hint = self.flat(hint.split("\n:::\n", 1)[0])
        self.assertIn("recorded, not stopped", hint)
        self.assertIn("Expected result: `Result: SUCCESS`", hint)
        self.assertIn("**Supervisory Control** → **Enable Reclose**", hint)
        self.assertIn("re-apply **Hardened reference**", hint)
        # The inline command keeps the Run button (which targets Kali) away from it.
        self.assertNotIn("\n    dnp3cmd", steps[4]["description"].split(":::hint Optional: see", 1)[1]
                         .split("\n:::\n", 1)[0])
        self.assertIn("Enable Reclose", steps[5]["description"])
        self.assertEqual(steps[5]["action"]["expect"]["reclose_enabled"], True)

    def test_no_independent_dpi_overclaim_remains(self):
        texts = {
            "hardening-configurations": scenario("hardening-configurations"),
            "validation-evidence": scenario("validation-evidence"),
        }
        for name, doc in texts.items():
            body = self.flat(doc["summary"] + doc["description"]
                             + " ".join(s["description"] for s in doc["steps"]))
            for claim in self.OVERCLAIMS:
                with self.subTest(scenario=name, claim=claim):
                    self.assertNotIn(claim, body)
        for path in ("docs/workshop-overview.md", "docs/v0.1.36-hotfix.md"):
            body = self.flat((ROOT / path).read_text())
            for claim in self.OVERCLAIMS:
                with self.subTest(path=path, claim=claim):
                    self.assertNotIn(claim, body)
        doc = texts["hardening-configurations"]
        self.assertIn("source pin is what enforces", doc["summary"])
        self.assertIn("the control that enforces", doc["description"])
        self.assertIn("the control that enforces", self.flat(doc["steps"][3]["description"]))

    def test_validation_report_is_described_as_tcp_matrix(self):
        text = self.flat(scenario("validation-evidence")["steps"][1]["description"])
        self.assertIn("The report is a TCP reachability matrix", text)
        self.assertIn("no row sends a control write", text)
        self.assertIn("blocked at the TCP level; it says nothing about function codes", text)
        overview = (ROOT / "docs/workshop-overview.md").read_text()
        self.assertIn("The report is a TCP reachability matrix", overview)
        self.assertIn("the L4 source pin is the control that enforces", overview)
        errata = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        self.assertIn("**RD-24:**", errata)
        self.assertIn("L4 source pin is the control that enforces", errata)
        self.assertIn("returns `Result: SUCCESS` and auto-reclose turns off", errata)
        self.assertIn("(RD-24)", errata)


class BaselineContracts(unittest.TestCase):
    def test_vendor_observation_depends_on_capture_window(self):
        doc = scenario("baseline-assessment")
        self.assertIn("NOT_OBSERVED", doc["steps"][3]["description"])
        text = doc["steps"][5]["description"]
        decision = next(line for line in text.splitlines() if "id=vendor-to-ot " in line)
        self.assertNotIn("correct=", decision)
        self.assertIn("if your capture contains", text)

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
            self.assertIn("in this workshop", row)
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
                     s["title"].startswith(("Stage 3", "Apply hardened", "Apply the hardened"))]
            self.assertEqual(len(steps), 1)
            with self.subTest(scenario=name):
                self.assertEqual(steps[0]["action"],
                                 {"type": "check", "expect": {"firewall_config": "improved"}})
                self.assertEqual(steps[0].get("expected_config"), "hardened")
                self.assertIn("without replacing it", steps[0]["description"])

    def test_planning_keeps_decision_ui_and_manual_completion(self):
        doc = scenario("remediation-planning")
        step = doc["steps"][2]
        self.assertEqual(step["action"]["type"], "decision")
        self.assertIn("Selections save automatically.", step["description"])
        self.assertIn("click Mark Complete", step["description"])
        self.assertIn("Do not use Execute Step", step["description"])
        self.assertIn("design artifact", step["description"])

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
                # Parentheses delimit plain-text header examples; backticks delimit Markdown.
                for match in re.finditer(r"\bdnp3poll ((?:-|10\.)[^`\n)]+)", line):
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

if __name__ == "__main__":
    unittest.main()
