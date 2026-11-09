"""Release headers, workshop notes and errata/update guidance contracts."""

import re
import unittest

import yaml

from scripts.tests.curriculum import read_curriculum
from scripts.tests.test_lab_content_contracts import ROOT, SCENARIOS, scenario

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
        'that trips the recloser and disables auto-reclose. The remote trip stays open until '
        'someone closes it; disabling auto-reclose removes automatic restoration after a '
        "fault. You'll run the attack under the weak baseline (it "
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
        'applied plan and Hardened, and PCAP evidence from the firewall. This is the validation '
        'step required before any segmentation change goes to production.\n'
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
            "firewall-implementation": {
                4: ("**Apply Your Plan**", "without replacing it", "Deferred rules retain"),
                7: ("13 unauthorized TCP rows", "Connection refused means", "prevention gap", "running hash"),
                8: ("does not support shell redirection", "CLI JSON-file edit/import exercise is deferred", "action IDs", "policy hash")},
            "hardening-configurations": {
                3: ("Apply Your Plan", "without replacing it", "**Block enterprise → field direct access**"),
                4: ("With it deferred, the command still lands", "Detection selections do not close it", "device evidence"),
                5: ("re-test timestamps", "do not clear history", "Restoration is common to every plan")},
            "vendor-rdp-compromise": {
                4: ("Apply Your Plan", "without replacing it", "| Restrict enterprise → vendor remote access |"),
                5: ("Deferred means reachable", "Restore auto-reclose", "A refused connection counts as reached")},
            "validation-evidence": {
                0: ("Do not replace a partial plan", "A saved browser plan is not an applied plan"),
                1: ("Validate Exercise does not replace your test evidence",)},
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
        for phrase in ("not DNP3 function-code rejection",
                       "These two management ports stay allowed for every plan"):
            self.assertIn(phrase, report)
        apply = " ".join(scenario("hardening-configurations")["steps"][3]["description"].split())
        self.assertIn("**Pin field-device access to RTAC source IP** owns non-RTAC OT→field attempts, "
                      "not Kali's enterprise path", apply)
        self.assertIn("Neither **Apply DNP3 ICS DPI - detect and log Direct Operate writes** nor "
                      "**Apply Modbus ICS DPI - provide function-code visibility** changes these verdicts", apply)

    def test_export_recipes_print_and_copy_without_fictitious_files(self):
        for name in ("firewall-implementation", "validation-evidence"):
            doc = scenario(name)
            text = doc["description"] + "\n" + "\n".join(s["description"] for s in doc["steps"])
            self.assertNotRegex(text, r"export config\s*>")
            self.assertNotIn("cat /tmp/student-policy.json", text)
            self.assertIn("Copy that redacted running JSON to your evidence document",
                          " ".join(text.split()))
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
                       "quickstart.md#upgrading-to-v023", "workshop-ssd.md#delta-staging"):
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
        quickstart = " ".join((ROOT / "docs/quickstart.md").read_text().split())
        for phrase in ("git fetch origin tag v0.2.3", "git checkout --detach v0.2.3",
                       "./setup.sh --version v0.2.3 --package us-dnp3-substation",
                       "v0.2.2 → v0.2.3", "does not require a firewall-data migration",
                       "A direct upgrade from v0.2.0 or older data",
                       "eu-iec104-substation", "PowerShell", "v0.2.2 Windows fix was validated",
                       "workshop reset", "DELTA-README.md", "carries only the images this release rebuilt",
                       "v0.2.3 kernel asset only if the probe finds them missing"):
            self.assertIn(phrase, quickstart)
        ssd = " ".join((ROOT / "docs/workshop-ssd.md").read_text().split())
        for phrase in ("./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.3",
                       "./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.3 v0.2.2 v0.2.3",
                       "v0.1.x SSD baseline lacks it", "package Compose metadata",
                       "only the images this release rebuilt", "promotes images whose inputs are unchanged",
                       "v0.2.0 → v0.2.3", "does not require a containd migration backup"):
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


if __name__ == "__main__":
    unittest.main()
