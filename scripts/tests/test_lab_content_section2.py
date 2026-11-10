"""Section 2 (implementation, hardening, validation and bonus) text contracts."""

import json
import re
import shlex
import unittest

import yaml

from scripts.tests.curriculum import read_curriculum
from scripts.tests.test_lab_content_contracts import (
    ROOT,
    SCENARIOS,
    reference_action,
    scenario,
)


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
                authored = curriculum["phases"][key].split("\n\n### " + ("Rule reference" if key == "rules" else "Expected results"))[0]
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
        for phrase in ("149h vs 140h", "9h overrun", "80h", "apply limit"):
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

    def test_eu_rtu_conduit_keys_cover_every_field_device(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        topology = yaml.safe_load((package / "topology.yml").read_text())
        field = {node["ip"] for node in topology["nodes"] if node["networks"] == ["field_net"]}
        for name in ("eu-baseline-assessment", "eu-firewall-implementation"):
            text = (package / "scenarios" / f"{name}.yml").read_text()
            match = re.search(r"\| RTU→field \| 10\.30\.30\.20→10\.40\.40\.(\d+)…(\d+) \| TCP/502 \|", text)
            with self.subTest(scenario=name):
                self.assertIsNotNone(match)
                low, high = map(int, match.groups())
                self.assertEqual({f"10.40.40.{n}" for n in range(low, high + 1)}, field)

    def test_eu_shadow_handover_and_optional_pcap_review_have_limits(self):
        package = ROOT / "lab-definitions/packages/eu-iec104-substation"
        for rel in ("scenarios/eu-validation-evidence.yml", "knowledge/eu-ics-dpi.md"):
            with self.subTest(path=rel):
                text = (package / rel).read_text()
                for phrase in ("snapshotVersion", "since", "available:false", "NFQUEUE 101",
                               "no queue bypass", "if the engine is down, the CC conduit drops",
                               "withheld", "SBO sequences, setpoint values/QL and bitstrings",
                               "not graded", "does not grade anything", "PCAP Analysis", "Generate suggestions", "Review",
                               "Stage N selected", "selected revision count", "Commit is a separate deliberate step",
                               "Configuration → Staged config", "Copy running → staged", "Save staged config",
                               "config diff shows no changes",
                               "discarding the candidate", "running policy/hash is unchanged"):
                    self.assertIn(phrase.casefold(), text.casefold())
                self.assertNotIn("Stage selected", text)
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
        for phrase in ("**Block enterprise → field direct access** selected", "connection timeout",
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
                       "Device / point", "Running policy identity / commit", "does not sign this record"):
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

    def test_validation_tcp_scope_is_narrow(self):
        text = self.flat(scenario("validation-evidence")["steps"][2]["description"])
        self.assertIn("not command authorization", text)
        overview = (ROOT / "docs/workshop-overview.md").read_text()
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", overview)
        errata = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        self.assertIn("**RD-24:**", errata)
        self.assertIn("The L4 source pin enforces; DPI detects and logs.", errata)
        self.assertNotIn("dnp3cmd ", errata)
        self.assertIn("(RD-24)", errata)


class RuleAuthoringContracts(unittest.TestCase):
    def test_hand_change_request_work_survives_dynamic_replacement_in_both_packages(self):
        for package, scenario_path in (
            ("us-dnp3-substation", SCENARIOS / "firewall-implementation.yml"),
            ("eu-iec104-substation", ROOT / "lab-definitions/packages/eu-iec104-substation/scenarios/eu-firewall-implementation.yml"),
        ):
            package_dir = ROOT / "lab-definitions/packages" / package
            curriculum = read_curriculum(package_dir)
            binding = yaml.safe_load((package_dir / "package.yml").read_text())["curriculum"]
            doc = yaml.safe_load(scenario_path.read_text())
            steps = {step["id"]: step["description"] for step in doc["steps"]}
            for step_id, phase in binding["dynamicSteps"].items():
                with self.subTest(package=package, phase=phase):
                    divider = "Rule reference" if phase == "rules" else "Expected results"
                    authored = curriculum["phases"][phase].split("\n\n### " + divider)[0]
                    self.assertEqual(authored.strip(), steps[step_id].strip())
                    self.assertNotRegex(authored, r"(?i)plan proof|report matrix|manual.*targets Hardened")
                    if phase == "rules":
                        flat = " ".join(authored.split())
                        for phrase in ("approved Lab 1.4 change request", "full Hardened reference",
                                       "same shared candidate", "Before the single commit",
                                       "diff", "show running-config", "pre-change export",
                                       "previous rules", "read back"):
                            self.assertIn(phrase, flat)
                        hint = re.search(r":::hint Optional catch-up — for less technical students or teams behind schedule\n(.*?)\n:::", authored, re.S)
                        self.assertIsNotNone(hint)
                        self.assertIn("Apply Your Plan", hint[1])
                        self.assertNotIn("Apply Your Plan", authored.replace(hint[0], ""))
            logging = steps["phase-4-enable-logging-and-visibility"]
            for phrase in ("separate from the Phase 3 rule change", "show diff", "`commit`", "Read back", "Retest operations"):
                self.assertIn(phrase, logging)
            if package.startswith("eu-"):
                positive = steps["test-positive"]
                self.assertIn("Operator-authorized operational demonstration", positive)
                self.assertIn("not\nacceptance reads", positive)
                demonstration = "\n".join(test["text"] for test in curriculum["tests"])
                self.assertIn("obtain operator approval", demonstration)
                self.assertIn("not an acceptance read", demonstration)

    def test_custom_policy_authoring_checks_remote_access_after_commit(self):
        weak = json.loads((ROOT / "lab-definitions/firewall/substation-weak.json").read_text())
        rule = next(r for r in weak["firewall"]["rules"] if r["id"] == "enterprise-to-vendor")
        self.assertTrue({"3389", "5900"} <= {p["port"] for p in rule["protocols"]})
        for port in (3389, 5900):
            self.assertEqual(reference_action("10.10.10.50", "10.20.20.10", "tcp", port), "DENY")
        text = scenario("firewall-implementation")["steps"][4]["description"]
        flat = " ".join(text.split())
        for phrase in ("**Restrict enterprise → vendor remote access** selected", "TCP 22, 80 and 443",
                       "With it deferred, RDP/3389 and VNC/5900 remain reachable",
                       "**Create rule** and **Delete** only stage edits",
                       "Expect `filtered` for the selected restriction",
                       "not for an approved deferral", "saved selection sets the expected results"):
            self.assertIn(phrase, flat)
        commit = flat.index("commit replacements and removals together")
        self.assertLess(flat.index("TCP 22, 80 and 443"), commit)
        self.assertLess(commit, flat.index("`nmap -Pn -p 3389,5900 10.20.20.10`"))
        for index in (3, 4):
            text = " ".join(scenario("firewall-implementation")["steps"][index]["description"].split())
            self.assertNotIn("immediately", text)
        self.assertNotRegex(text, r"(?m)^\s+nmap ")


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


if __name__ == "__main__":
    unittest.main()
