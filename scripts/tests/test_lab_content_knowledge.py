"""Knowledge-article teaching contracts shared by the labs."""

import unittest

import yaml

from scripts.tests.test_lab_content_contracts import ROOT, SCENARIOS, scenario


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
            texts = [" ".join(p.read_text().split()) for p in (path, base / "curriculum/implementation.yml")]
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
            self.assertNotIn("Selection order does not set rule order; compilation uses authored slots.",
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
        self.assertIn("blob/main/docs/mkdocs/policy-model.md"
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


class AuditTeachingContracts(unittest.TestCase):
    def test_purdue_teaching_matches_package_band_labels(self):
        eu = ROOT / "lab-definitions/packages/eu-iec104-substation"
        article = (eu / "knowledge/eu-purdue-model.md").read_text()
        for topology, baseline in (
            (ROOT / "lab-definitions/substation-segmentation.yml",
             SCENARIOS / "baseline-assessment.yml"),
            (eu / "topology.yml", eu / "scenarios/eu-baseline-assessment.yml"),
        ):
            text = baseline.read_text()
            for network in yaml.safe_load(topology.read_text())["networks"]:
                label = f'{network["purdue"]} · {network["name"]}'
                with self.subTest(topology=topology, zone=network["id"]):
                    self.assertIn(label, text)
                    if topology == eu / "topology.yml":
                        self.assertIn(label, article)
                        self.assertNotIn("target_sl", network)
            self.assertIn("Lab 2.4 validates actual paths and continuity", text)
            self.assertNotIn("you'll use these labels in Lab 1.3 and 2.4", text)
        for caveat in ("teaching aid", "not a required network diagram or a security policy",
                       "no automatic mapping", "Sharing the L3.5 label does not merge"):
            self.assertIn(caveat, article)

    def test_sl_targets_do_not_certify_policy_or_dpi_filtering(self):
        paths = sorted(SCENARIOS.glob("*.yml"))
        for package in ("us-dnp3-substation", "eu-iec104-substation"):
            paths.extend((ROOT / "lab-definitions/packages" / package / "knowledge").glob("*.md"))
        removed = ("roughly SL-1", "approaches SL-3", "must enforce the higher SL",
                   "enforcing SL-3 controls", "Modbus function codes are filtered",
                   "Target SL-3 · MET", "Conduit achieves SL-", "function code filtering")
        for path in paths:
            text = " ".join(path.read_text().split())
            for claim in removed:
                with self.subTest(path=path, claim=claim):
                    self.assertNotIn(claim, text)
        design = scenario("segmentation-requirements")["steps"][0]["description"]
        criteria = scenario("segmentation-requirements")["steps"][2]["description"]
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", design)
        self.assertIn("timeout", criteria)
        self.assertIn("fresh, valid device/process state", criteria)
        self.assertNotIn(":::hint Reveal expected requirements", design)

    def test_remote_trip_and_fault_recovery_are_distinct(self):
        text = " ".join(scenario("hardening-configurations")["steps"][1]["description"].split())
        self.assertNotIn("recoverable flicker", text)
        self.assertIn("remote trip itself stays open until someone closes it", text)
        self.assertIn("even with auto-reclose enabled", text)
        self.assertIn("automatic restoration attempts after an injected fault", text)
        knowledge = (ROOT / "lab-definitions/packages/us-dnp3-substation/knowledge/reclosers.md").read_text()
        self.assertIn("Only an injected fault starts the automatic reclose sequence", knowledge)


if __name__ == "__main__":
    unittest.main()
