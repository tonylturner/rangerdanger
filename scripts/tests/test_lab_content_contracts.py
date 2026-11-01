"""Offline contracts for workshop content and release guidance."""

from pathlib import Path
import importlib.util
import ipaddress
import json
import re
import shlex
import unittest

import yaml

from scripts.tests.curriculum import read_curriculum


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

# Reviewed applied-plan headers replace reference-only promises. Keep exact snapshots.
HEADER_EXCEPTIONS = {
    "firewall-implementation": (
        'You have identified the required flows, designed your requirements, and planned '
        'which remediations to execute. Now you implement. You will replace the permissive '
        'weak baseline on the containd NGFW with a least-privilege policy - applying it with '
        'one click on the Guided track, or authoring the rules yourself on the Advanced '
        'track. The applied plan defines which unauthorized cross-zone paths close or remain '
        'open, while critical substation operations continue without interruption.\n'
    ),
    "hardening-configurations": (
        'Stress-test the policy you built in Lab 2.2 against the canonical '
        'distribution-substation attack: a DNP3 Direct Operate (FC05) injection from Kali '
        "that disables the recloser's auto-reclose and trips it, turning a brief flicker into "
        "a sustained downstream outage. You'll run the attack under the weak baseline (it "
        'succeeds), then under the applied plan. A selected enterprise-to-field closure '
        'denies it; a deferred closure leaves the Weak exposure open. The lesson: verify '
        'enforcement by device state, not by policy labels. Optional Modbus FC5/FC6 cases '
        'follow the same owning action.\n'
    ),
    "remediation-planning": (
        'You have findings from the baseline assessment but not unlimited time, people, or '
        'political capital. Pick which remediations to execute this quarter - knowing some '
        "attack paths stay open because you can't fix everything at once. Apply Your Plan "
        'compiles your selections. Deferred controls leave their Weak exposures open.\n'
    ),
    "validation-evidence": (
        'After applying your plan or the Hardened reference in Lab 2.2 and stress-testing it '
        'in Lab 2.3, this exercise builds the formal evidence package: positive tests '
        'confirming authorized flows still work, tests comparing unauthorized flows with the '
        'applied plan and Hardened, and PCAP evidence from the firewall. You generate it in '
        'one click against the policy you have active - then read it like a reviewer would. '
        'This is the validation step required before any segmentation change goes to '
        'production.\n'
    ),
    "vendor-rdp-compromise": (
        'Optional companion to Lab 2.3. Phished vendor credentials let an attacker RDP/VNC '
        'into the vendor jump box from an enterprise foothold, then pivot east-west to OT '
        "field devices. You'll walk the kill chain under the weak baseline, then apply "
        'containd rules that block remote-access protocols (RDP/3389, VNC/5900) from '
        'enterprise into the vendor DMZ. In an applied plan, each link follows its owning '
        'action.\n'
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

    def test_lab22_generation_is_bound_by_stable_ids_not_frozen_title_workarounds(self):
        package = yaml.safe_load((ROOT / "lab-definitions/packages/us-dnp3-substation/package.yml").read_text())
        binding = package["curriculum"]["dynamicSteps"]
        steps = scenario("firewall-implementation")["steps"]
        self.assertEqual({s["id"] for s in steps} & set(binding), set(binding))
        curriculum = read_curriculum(ROOT / "lab-definitions/packages/us-dnp3-substation")
        for step_id, phase in binding.items():
            self.assertIn(phase, curriculum["phases"])
            self.assertTrue(next(s for s in steps if s["id"] == step_id)["title"].startswith("Phase "))
        rules = curriculum["phases"]["rules"]
        for phrase in ("**Apply Your Plan**", "Deferred rules retain", "export config", "**Commit**",
                       "**Export plan record**", "{{ruleTable}}"):
            self.assertIn(phrase, rules)
        self.assertNotIn("export config >", rules)
        self.assertIn("config.commit", curriculum["phases"]["negative"])

    def test_workshop_notes_are_short_and_at_the_action(self):
        expected = {
            "remediation-planning": {2: ("Selections save automatically.", "Budget overrun is a warning only", "empty", "partial")},
            "firewall-implementation": {
                4: ("**Apply Your Plan**", "without replacing it", "Deferred rules retain"),
                7: ("13 unauthorized TCP rows", "Connection refused means", "prevention gap", "running hash"),
                8: ("does not support shell redirection", "CLI JSON-file edit/import exercise is deferred", "action IDs", "policy hash")},
            "hardening-configurations": {
                3: ("Apply Your Plan", "without replacing it", "block-enterprise-to-field"),
                4: ("With it deferred, the command still lands", "Detection selections do not close it", "device evidence"),
                5: ("re-test timestamps", "do not clear history", "Restoration is common to every plan")},
            "vendor-rdp-compromise": {
                4: ("Apply Your Plan", "without replacing it", "restrict-enterprise-to-vendor"),
                5: ("Deferred means reachable", "Restore auto-reclose", "A refused connection counts as reached")},
            "validation-evidence": {
                0: ("Do not replace a partial plan", "A saved browser plan is not an applied plan"),
                1: ("authorized **8/8**", "**13/13**", "0 skipped", "Validate Exercise does not replace report evidence")},
        }
        for name, locations in expected.items():
            for index, phrases in locations.items():
                text = " ".join(scenario(name)["steps"][index]["description"].split())
                with self.subTest(scenario=name, step=index + 1):
                    for phrase in phrases:
                        self.assertIn(phrase, text)
                    self.assertNotIn("not **Apply Your Plan**", text)
                    self.assertNotIn("fixed blocked outcome", text)
        for path in SCENARIOS.glob("*.yml"):
            self.assertNotIn("Workshop errata", path.read_text())
            for step in scenario(path.stem)["steps"]:
                for note in step["description"].split("**Workshop note:**\n")[1:]:
                    bullets = re.findall(r"^- ", note.split("\n\n", 1)[0], re.M)
                    self.assertTrue(1 <= len(bullets) <= 3, path.name)

    def test_dnp3_and_self_check_details_are_in_rendered_steps(self):
        hardening = " ".join(scenario("hardening-configurations")["steps"][4]["description"].split())
        for phrase in ("auto-reclose still enabled", "auto-reclose becomes disabled", "device evidence",
                       "Detection selections do not close it", "This command alone does not trip the recloser"):
            self.assertIn(phrase, hardening)
        report = " ".join(scenario("validation-evidence")["steps"][1]["description"].split())
        for phrase in ("21-row TCP matrix", "not DNP3 function-code rejection", "Plan proof",
                       "Hardened acceptance", "Validate Exercise does not replace report evidence",
                       "recorded plan hash before and after probing", "partial plan can pass plan proof",
                       "Keep the report, UTC window, policy hash and PCAP", "healthy HMI separately",
                       "These two management ports stay allowed for every plan"):
            self.assertIn(phrase, report)
        apply = " ".join(scenario("hardening-configurations")["steps"][3]["description"].split())
        self.assertIn("`pin-rtac-to-field` owns non-RTAC OT→field attempts, not Kali's enterprise path", apply)
        self.assertIn("Neither `dnp3-dpi` nor `modbus-dpi` changes these verdicts", apply)

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
        hardening = " ".join(scenario("hardening-configurations")["steps"][4]["description"].split())
        for phrase in ("Do not treat an Execute Step label as a packet verdict", "wire result and device evidence"):
            self.assertIn(phrase, hardening)
        vendor = " ".join(scenario("vendor-rdp-compromise")["steps"][5]["description"].split())
        self.assertIn("Record the result, not an assumed successful login", vendor)
        self.assertIn("Restore auto-reclose and normal feeder state before Validate for every plan", vendor)
        text = " ".join(scenario("hardening-configurations")["steps"][5]["description"].split())
        for phrase in ("Audit history can include earlier Weak-policy writes", "re-test timestamps",
                       "do not clear history", "A state check is not TCP proof", "expected verdict"):
            self.assertIn(phrase, text)

    def test_workshop_guidance_has_no_instructor_grading_framing(self):
        paths = [
            *(ROOT / "lab-definitions").rglob("*.yml"),
            ROOT / "docs/v0.1.36-hotfix.md",
            ROOT / "docs/quickstart.md",
            ROOT / "docs/workshop-ssd.md",
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

    def test_historical_errata_identifies_current_package_generation(self):
        text = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        for title in ("Stage 1 - Check weak-baseline access",
                      "Stage 2 - Remove broad trust and establish default-deny",
                      "Stage 3 - Apply the reference rules",
                      "Stage 4 - Enable logging and visibility",
                      "Stage 5 - Check authorized flows", "Stage 6 - Check denied flows"):
            self.assertIn(title, text)
        self.assertIn("Historical release guidance", text)
        self.assertIn("stable IDs, not Stage titles", text)
        self.assertIn("both source YAML and generated steps", text)
        self.assertIn("not the applied policy", text)
        self.assertIn("not in labs", text)

    def test_errata_has_full_release_paths_and_every_mitigation(self):
        path = ROOT / "docs/v0.1.36-hotfix.md"
        self.assertTrue(path.exists(), "student/instructor errata is missing")
        text = path.read_text()
        for phrase in ("How to update", "PowerShell", "full image release",
                       "All 16 first-party images", "`latest` moves", "SSD",
                       "quickstart.md#upgrading-to-v020", "workshop-ssd.md#delta-staging"):
            self.assertIn(phrase, text)
        for stale in ("existing v0.1.35 images", "no pull, no rebuild",
                      "Do not run `./setup.sh --version v0.1.36`", "./scripts/apply-repo-update.sh"):
            self.assertNotIn(stale, text)
        for number in (1, 2, 3, 5, 6, 8, 12, 15, 18, 19, 23, 24, 25, 26):
            self.assertIn(f"RD-{number:02}", text)
        self.assertIn("**RD-26:** On these images the recloser records a Modbus write only as\n"
                      "`modbus-tcp`", text)
        self.assertIn("(RD-26) | Lab 2.3-bonus, Use the vendor foothold", text)
        self.assertIn("**RD-25:** containd web-UI rule **Create** and **Delete** save the running\n"
                      "config but do not enforce it until **Commit**", text)
        self.assertIn("(RD-25) | Lab 2.2, Stage 2 and Stage 3", text)
        self.assertTrue(text.startswith("# v0.1.36 workshop errata\n"))
        for path in ("docs/quickstart.md", "docs/workshop-ssd.md", "docs/v0.1.36-hotfix.md"):
            with self.subTest(path=path):
                doc = (ROOT / path).read_text()
                self.assertNotRegex(doc, r"rc\.[123]\b")
                self.assertNotRegex(doc, r"v0\.1\.36-rc\.[123]\b")

    def test_release_update_uses_standard_setup_and_delta_paths(self):
        quickstart = (ROOT / "docs/quickstart.md").read_text()
        for phrase in ("git fetch origin tag v0.2.0", "git checkout --detach v0.2.0",
                       "./setup.sh --version v0.2.0 --package us-dnp3-substation",
                       "eu-iec104-substation", "PowerShell", "parity",
                       "workshop reset", "DELTA-README.md", "close to a full set"):
            self.assertIn(phrase, quickstart)
        ssd = (ROOT / "docs/workshop-ssd.md").read_text()
        for phrase in ("./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.0",
                       "./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.1 v0.2.0 v0.2.1",
                       "v0.1.x SSD baseline lacks it", "package Compose metadata",
                       "close to a full set", "[rebuild-all]"):
            self.assertIn(phrase, ssd)
        readme = (ROOT / "README.md").read_text()
        self.assertNotIn("> [!IMPORTANT]", readme)
        self.assertNotIn("v0.1.36-rc.", readme)
        for stale in ("Selections in earlier labs rewrite", "plan rewrites the steps",
                      "lab adapts to which actions"):
            self.assertNotIn(stale, readme)
        self.assertIn("Lab 1.4 remediation plan is compiled and applied in Lab 2.2", readme)
        self.assertIn("verify the running policy against your plan", readme)
        self.assertNotIn("#updating-to-v0136", readme)
        self.assertNotIn("docker compose -f docker-compose.release.yml down &&", quickstart)
        self.assertNotIn("stage-ssd-repo-update.sh", ssd)
        self.assertNotIn("scripts/apply-repo-update.sh", ssd)


class EnforcementContracts(unittest.TestCase):
    """Lab 2.3 states what the frozen images do: the L4 source pin blocks, DPI records."""

    def test_eu_protocol_catalog_promises_no_future_enforcement(self):
        curriculum = read_curriculum(ROOT / "lab-definitions/packages/eu-iec104-substation")
        suffix = ("Planning-only protocol review; this range emits no protocol rule. "
                  "The L4 source pin enforces; DPI detects and logs.")
        actions = {action["id"]: action for action in curriculum["catalog"]}
        self.assertTrue(actions["modbus-dpi"]["why"].endswith(suffix))
        observation = actions["iec104-dpi"]
        self.assertEqual(observation["effort_hours"], 80)
        self.assertIn("explicit monitor rules", observation["why"])
        self.assertIn("Monitoring never blocks or satisfies a BLOCK requirement.", observation["why"])
        self.assertTrue(observation["why"].endswith("The L4 source pin enforces; DPI detects and logs."))
        for action_id in ("modbus-dpi", "iec104-dpi"):
            self.assertNotRegex(actions[action_id]["why"], r"(?i)\b(yet|future|soon)\b")

    def test_eu_monitor_lesson_is_present_in_both_implementation_sources(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        curriculum = read_curriculum(package)
        doc = yaml.safe_load((package / "scenarios/eu-firewall-implementation.yml").read_text())
        steps = {step["id"]: step["description"] for step in doc["steps"]}
        for key, step_id in (("rules", "implement-rules"), ("positive", "test-positive")):
            with self.subTest(phase=key):
                authored = curriculum["phases"][key].split("\n\n### Compiled")[0]
                self.assertEqual(authored.strip(), steps[step_id].strip())
        positive = steps["test-positive"]
        for phrase in ("observe-cc-breaker-command", "observe-cc-oltc-step",
                       "observe-cc-oltc-raise-only", "fresh SYN", "snapshotVersion",
                       "since", "available:false", "Firewall/ICS tables", "NFQUEUE",
                       "if the engine is down, the CC conduit drops", "no queue bypass",
                       "**≥1**", "**0**", "**both**", "**not guaranteed**"):
            self.assertIn(phrase, positive)
        self.assertIn("Never overlay a monitor predicate", steps["implement-rules"])

    def test_eu_hardened_staffing_overrun_is_not_hidden(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        text = (package / "scenarios/eu-remediation-planning.yml").read_text()
        for phrase in ("149h vs 140h", "9h overrun", "80h", "partial plan", "GAP", "UNAVAILABLE"):
            self.assertIn(phrase, text)
        curriculum = read_curriculum(package)
        team = next(team for team in curriculum["teams"] if team["name"] == "Firewall Admin")
        self.assertEqual(team["capacity_hours"], 140)
        technical = {"restrict-enterprise-to-vendor", "block-enterprise-to-field",
                     "block-enterprise-to-station", "restrict-vendor-to-station",
                     "block-vendor-to-field", "pin-rtu-to-field",
                     "tighten-firewall-objects", "iec104-dpi"}
        admin_load = sum(int(action["effort_hours"] / len(action["roles"]) + 0.5)
                         for action in curriculum["catalog"]
                         if action["id"] in technical and "Firewall Admin" in action["roles"])
        self.assertEqual(admin_load, 149)
        self.assertEqual(admin_load - team["capacity_hours"], 9)
        self.assertIn("observe", curriculum["groups"]["iec104-dpi"]["description"].lower())
        self.assertIn("Planning-only", curriculum["groups"]["modbus-dpi"]["description"])

    def test_eu_shadow_handover_and_optional_pcap_review_have_limits(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        for rel in ("scenarios/eu-validation-evidence.yml", "knowledge/eu-ics-dpi.md"):
            with self.subTest(path=rel):
                text = (package / rel).read_text()
                for phrase in ("snapshotVersion", "since", "available:false", "NFQUEUE 101",
                               "no queue bypass", "if the engine is down, the CC conduit drops",
                               "withheld", "SBO sequences, setpoint values/QL and bitstrings",
                               "not graded", "does not grade anything", "PCAP Analysis", "Generate suggestions", "Review",
                               "Stage selected", "Commit is a separate deliberate step",
                               "discarding the candidate", "running policy/hash is unchanged"):
                    self.assertIn(phrase.casefold(), text.casefold())
                self.assertNotIn("would have blocked", text)

    def test_eu_content_does_not_retain_dpi_disabled_qualifications(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        paths = list((package / "knowledge").glob("*.md")) + list((package / "scenarios").glob("*.yml"))
        for path in paths:
            with self.subTest(path=path.name):
                self.assertNotRegex(path.read_text(), r"(?i)(?:DPI (?:is |setting is |explicitly )?(?:currently )?disabled|disables DPI|DPI-off|no detection-only action)")

    def test_us_curriculum_dpi_does_not_claim_to_block_or_filter_writes(self):
        curriculum = read_curriculum(ROOT / "lab-definitions/packages/us-dnp3-substation")

        def string_values(value):
            if isinstance(value, dict):
                for child in value.values():
                    yield from string_values(child)
            elif isinstance(value, list):
                for child in value:
                    yield from string_values(child)
            elif isinstance(value, str):
                yield value

        overclaim = re.compile(
            r"(?i)\bDPI\b.{0,100}\b(?:blocks?|filters?|prevents?)\b"
            r".{0,100}\b(?:writes?|Direct Operate)\b"
            r"|\b(?:writes?|Direct Operate)\b.{0,100}"
            r"\b(?:blocked|filtered|prevented)\b.{0,100}\bDPI\b"
        )
        for text in string_values(curriculum):
            with self.subTest(text=text):
                self.assertNotRegex(text, overclaim)

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
        self.assertNotIn("correct=", decisions["retest-hmi-state"])
        self.assertIn("AUTO_RECLOSE_DISABLED", decisions["retest-hmi-state"])
        self.assertIn("With it deferred, the command still lands", text)
        self.assertIn("When the enterprise path is denied", text)
        self.assertNotIn("RTAC-origin write", text)

    def test_retest_states_both_outcomes_and_common_restore(self):
        steps = scenario("hardening-configurations")["steps"]
        text = self.flat(steps[4]["description"])
        for phrase in ("`block-enterprise-to-field` selected", "connection timeout",
                       "auto-reclose still enabled", "With it deferred", "auto-reclose becomes disabled",
                       "This command alone does not trip the recloser", "`[2]` is recloser closed",
                       "`[3]` is auto-reclose enabled", "source, time and policy hash",
                       "Restore every changed device before Validate, for every plan",
                       "enable auto-reclose", "close the recloser and breaker", "clear fault/lockout",
                       "return the regulator to auto and tap zero", "reapply the intended plan"):
            self.assertIn(phrase, text)
        self.assertNotIn("RTAC-origin write", text)
        self.assertEqual(steps[5]["action"]["expect"]["reclose_enabled"], True)
        validate = self.flat(steps[5]["description"])
        for phrase in ("Restoration is common to every plan", "voltage normal and loads energized",
                       "Audit history can include earlier Weak-policy writes", "do not clear history",
                       "21-row matrix, capture and stable-hash evidence"):
            self.assertIn(phrase, validate)

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
        self.assertIn("selected enterprise-to-field closure denies it", self.flat(doc["description"]))
        self.assertIn("The L4 source pin enforces; DPI detects and logs", self.flat(doc["steps"][3]["description"]))

    def test_recloser_attribution_uses_containd_sessions(self):
        # Frozen images: recloser-sim records every Modbus write as "modbus-tcp"
        # (no peer address) and the mbpoll path adds no Command Audit row, so the
        # only surface attributing the live write is containd NETWORK -> Sessions
        # (conntrack, ~17 s TTL). Flows/Events/Live DPI Events are empty under weak.
        raw = scenario("vendor-rdp-compromise")["steps"][2]["description"]
        text = self.flat(raw)
        self.assertIn("**Mid-Feeder Recloser (79)**: its **Last Command** row reads `modbus-tcp`", text)
        self.assertIn("records no peer address", text)
        self.assertIn("[containd firewall](/containd/)", text)
        self.assertIn("`containd` / `containd`", text)
        self.assertIn("**NETWORK → Sessions**", text)
        self.assertIn("**DST `10.40.40.21`** and **DPORT `502`**", text)
        self.assertIn("Their **SRC** is `10.20.20.10`, the vendor jump host", text)
        self.assertIn("There is no `10.10.10.50` row for this write", text)
        self.assertIn("Open **Sessions** before running the write", text)
        self.assertIn("set the list limit above 500 rows", text)
        self.assertIn("filter on `10.20.20.10`", text)
        self.assertIn("These rows expire in about 17 seconds", text)
        self.assertIn("re-run the first `mbpoll` write above and refresh Sessions", text)
        for surface in ("Flows", "Events", "Live DPI", "Traffic Matrix", "Command Audit"):
            self.assertNotIn(surface, raw)
        self.assertNotIn("Last Command** row shows the vendor jump", text)
        decision = next(line for line in raw.splitlines() if "id=vendor-source-attribution " in line)
        self.assertIn("correct=VENDOR_JUMP_IP", decision)
        question = raw.split("id=vendor-source-attribution", 1)[1].split("\n:::", 1)[0]
        self.assertIn("In containd Sessions, which SRC address", question)
        self.assertNotIn("Electrical Detail", question)
        # The sample Sessions row is output, not a Run-button command.
        self.assertRegex(raw, r"(?m)^    tcp   TIME_WAIT   10\.20\.20\.10 ")

    def test_validation_report_is_described_as_tcp_matrix(self):
        text = self.flat(scenario("validation-evidence")["steps"][1]["description"])
        self.assertIn("21-row TCP matrix", text)
        self.assertIn("not DNP3 function-code rejection", text)
        self.assertIn("Acceptance does not prove command-level filtering", text)
        overview = (ROOT / "docs/workshop-overview.md").read_text()
        self.assertIn("The report is a TCP reachability matrix", overview)
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", overview)
        errata = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        self.assertIn("**RD-24:**", errata)
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", errata)
        self.assertNotIn("dnp3cmd ", errata)
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
                     s["id"] in ("phase-3-create-minimal-required-rules", "apply-hardened-policy", "apply-the-hardened-policy")]
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
        self.assertIn("Selecting an action alone does not apply it", step["description"])
        self.assertIn("Budget overrun is a warning only", step["description"])

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


class AdvancedTrackContracts(unittest.TestCase):
    def test_custom_policy_authoring_checks_remote_access_after_commit(self):
        weak = json.loads((ROOT / "lab-definitions/firewall/substation-weak.json").read_text())
        rule = next(r for r in weak["firewall"]["rules"] if r["id"] == "enterprise-to-vendor")
        self.assertTrue({"3389", "5900"} <= {p["port"] for p in rule["protocols"]})
        for port in (3389, 5900):
            self.assertEqual(reference_action("10.10.10.50", "10.20.20.10", "tcp", port), "DENY")
        text = scenario("firewall-implementation")["steps"][4]["description"]
        technical = text.split(":::technical\n", 1)[1].split("\n:::\n", 1)[0]
        flat = " ".join(technical.split())
        for phrase in ("`restrict-enterprise-to-vendor` selected", "TCP 22, 80 and 443",
                       "With it deferred, RDP/3389 and VNC/5900 remain reachable",
                       "**Create rule** and **Delete** do not reload the firewall",
                       "both ports must be `filtered`", "fix the rule and commit again",
                       "A plan uses its compiled verdict instead"):
            self.assertIn(phrase, flat)
        self.assertLess(flat.index("TCP 22, 80 and 443"), flat.index("Run `commit`"))
        self.assertLess(flat.index("Run `commit`"), flat.index("`nmap -Pn -p 3389,5900 10.20.20.10`"))
        for index in (3, 4):
            text = " ".join(scenario("firewall-implementation")["steps"][index]["description"].split())
            self.assertNotIn("immediately", text)
        self.assertNotRegex(text, r"(?m)^\s+nmap ")


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

    def test_run_button_mbpoll_reads_poll_once(self):
        # mbpoll usage: "-1 Poll only once only, otherwise every poll rate interval";
        # a write (data after the host) prints "Written N references." and exits.
        # The frozen runner has no Cancel for an auto-run command, so every
        # Run-button read must terminate on its own.
        for path in sorted(SCENARIOS.glob("*.yml")):
            steps = scenario(path.stem)["steps"]
            for index, step in enumerate(steps, 1):
                # Dedented step text, as the runner and extractor read it: a
                # command block is an indented line that starts with the tool.
                for line in step["description"].splitlines():
                    match = re.match(r"^\s+mbpoll (.+)$", line)
                    if not match:
                        continue
                    args = shlex.split(match.group(1), comments=True)
                    host = next(i for i, a in enumerate(args) if a.startswith("10."))
                    with self.subTest(path=path.name, step=index, command=line.strip()):
                        if args[host + 1:]:
                            self.assertNotIn("-1", args)  # a write already exits
                        else:
                            self.assertIn("-1", args)

    def test_rtac_register_prose_uses_mbpoll_reference_labels(self):
        # `mbpoll -r 1` prints [1]..[n]: reference = 0-based register address + 1.
        for name in ("hardening-configurations", "vendor-rdp-compromise"):
            text = " ".join("\n".join(s["description"] for s in scenario(name)["steps"]).split())
            with self.subTest(scenario=name):
                self.assertNotIn("`[0]", text)
                self.assertNotRegex(text, r"(?i)\b(?:holding|input) registers? [0-9]\b")
                self.assertIn("reference = 0-based register address + 1", text)
        text = " ".join(scenario("hardening-configurations")["steps"][0]["description"].split())
        self.assertIn("`[1]=1` (breaker closed), `[2]=1` (recloser closed), "
                      "`[3]=1` (auto-reclose enabled)", text)
        text = " ".join(scenario("vendor-rdp-compromise")["steps"][2]["description"].split())
        # -c 5 reads references [1]..[5]; the shot count is holding register 5 = [6].
        self.assertIn("mbpoll -a 1 -t 4 -r 1 -c 5 -1 10.30.30.20", text)
        self.assertNotIn("`[6]` (`recloser_shot_count`) is `1`", text)
        self.assertIn("Re-run with `-c 6` to also see `[6]` (`recloser_shot_count`) at `1`", text)


class FirstMatchContracts(unittest.TestCase):
    def test_knowledge_teaches_terminal_order_not_allow_wins(self):
        eu = ROOT / "lab-definitions/packages/eu-iec104-substation"
        us = ROOT / "lab-definitions/packages/us-dnp3-substation"
        text = (eu / "knowledge/eu-containd-policy-enforcement.md").read_text()
        for phrase in ("first matching enforcing terminal rule in listed order decides",
                       "monitor rules never decide", "Rule IDs do not set priority",
                       "`RATE_LIMIT` meters and `TAG` labels",
                       "`firewall.defaultAction: DENY`",
                       "The L4 source pin enforces; DPI detects and logs."):
            self.assertIn(phrase, text)
        self.assertNotIn("compiler emits `ALLOW` entries before `DENY`", text)
        text = (eu / "knowledge/eu-default-deny.md").read_text()
        self.assertIn("An earlier matching DENY also takes precedence over a later ALLOW", text)
        text = (us / "knowledge/default-deny.md").read_text()
        self.assertIn("first matching enforcing terminal rule decides", text)
        self.assertIn("Hardened removes or narrows broad allows", text)
        self.assertNotIn("broad ALLOW rules over the top", text)

    def test_scenario_and_curriculum_both_teach_concrete_placement(self):
        for package, path in (("us-dnp3-substation", SCENARIOS / "firewall-implementation.yml"),
                              ("eu-iec104-substation", ROOT / "lab-definitions/packages/"
                               "eu-iec104-substation/scenarios/eu-firewall-implementation.yml")):
            base = ROOT / "lab-definitions/packages" / package
            texts = [path.read_text(), (base / "curriculum/implementation.yml").read_text()]
            for text in texts:
                self.assertIn("first matching enforcing terminal rule decides", text)
                self.assertIn("monitor rules never decide", text)
                self.assertIn("Review configured order and shadow warnings before commit", text)
                if package.startswith("us-"):
                    self.assertIn("Keep RTAC field exceptions before `deny-ot-to-field-direct`", text)
                    self.assertIn("Rule IDs do not set priority", text)
                else:
                    for phrase in ("CC allow before `deny-unauthorized-iec104`",
                                   "Modbus allows before `deny-unauthorized-modbus`",
                                   "CC ingest before `deny-unauthorized-historian-ingest`",
                                   "engineering web before `deny-unauthorized-plc-web`",
                                   "If recreating the CC allow, insert or move it before its deny",
                                   "Review configured order and shadow warnings before commit"):
                        self.assertIn(phrase, text)

    def test_planner_selection_order_does_not_set_priority(self):
        for path in (SCENARIOS / "remediation-planning.yml",
                     ROOT / "lab-definitions/packages/eu-iec104-substation/"
                     "scenarios/eu-remediation-planning.yml"):
            self.assertIn("Selection order does not set rule order; compilation uses authored slots.",
                          path.read_text())


class EstablishedConnectionContracts(unittest.TestCase):
    def test_knowledge_distinguishes_commit_enforcement_from_retained_state(self):
        eu = ROOT / "lab-definitions/packages/eu-iec104-substation/knowledge"
        for article in ("established-sessions", "telecontrol-deep-dive", "default-deny",
                        "segmentation-approaches", "port-source-boundary",
                        "tool-nmap-probes", "containd-policy-enforcement",
                        "change-resource-plan", "policy-comparison"):
            text = (eu / f"eu-{article}.md").read_text()
            with self.subTest(article=article):
                self.assertIn("re-evaluates established connections on their next packet", text)
                self.assertIn("Sessions may still show the conntrack entry", text)
                self.assertIn("an entry is not proof that traffic passes", text)
        text = (eu / "eu-established-sessions.md").read_text()
        for phrase in ("Session revocation is optional",
                       "not needed to enforce a committed DENY",
                       "checks handshake admission and inspection from SYN"):
            self.assertIn(phrase, text)
        text = (eu / "eu-containd-policy-enforcement.md").read_text()
        self.assertIn("blob/v0.1.37/docs/mkdocs/policy-model.md"
                      "#established-connections-and-policy-changes", text)
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", text)
        text = (ROOT / "lab-definitions/packages/us-dnp3-substation/"
                       "knowledge/default-deny.md").read_text()
        self.assertIn("only admission under the current policy can preserve access", text)
        self.assertNotIn("an established session can preserve access", text)

    def test_f2_loss_needs_commit_not_session_deletion(self):
        eu = ROOT / "lab-definitions/packages/eu-iec104-substation"
        for relative in ("scenarios/eu-firewall-implementation.yml",
                         "curriculum/implementation.yml"):
            text = (eu / relative).read_text()
            with self.subTest(path=relative):
                self.assertIn("Traffic no longer admitted stops without session deletion", text)
                self.assertIn("Sessions may retain the conntrack entry until timeout", text)
                self.assertIn("Observe loss after commit", text)
                self.assertNotIn("instructor must cut", text)
        text = (eu / "scenarios/eu-hardening-configurations.yml").read_text()
        self.assertIn("Post-commit delivery on an existing connection tests the current policy", text)
        self.assertIn("separate fresh-link check proves handshake admission", text)


class ChangeManagementContracts(unittest.TestCase):
    def test_configuration_rollback_requires_candidate_review_commit_and_verification(self):
        text = (ROOT / "lab-definitions/packages/us-dnp3-substation/knowledge/"
                       "change-management-firewall-rules.md").read_text()
        rollback = text.split("4. **Rollback plan.**", 1)[1].split("5. **Maintenance window.**", 1)[0]
        sequence = ("import the old config", "review the candidate diff",
                    "commit", "verify the running hash and operations")
        positions = [rollback.index(step) for step in sequence]
        self.assertEqual(positions, sorted(positions))
        for phrase in ("only stages the shared candidate", "/api/v1/config/diff",
                       "/api/v1/config/commit", "management reservation",
                       "interface device bindings", "required connectivity and device operations"):
            self.assertIn(phrase, rollback)
        for phrase in ("reads running, not the candidate", "committed, tested policy",
                       "not an uncommitted proposal", "not a binary or stored-schema rollback",
                       "pre-upgrade firewall `/data`"):
            self.assertIn(phrase, text)
        self.assertNotIn("is the proposed policy", text)


if __name__ == "__main__":
    unittest.main()
