"""The workshop teaches a hand test record, not a generated acceptance result."""
import importlib.util
import json
import unittest
from pathlib import Path

import yaml

from scripts.tests.curriculum import read_curriculum

ROOT = Path(__file__).resolve().parents[2]
HINT = ":::hint Optional catch-up — for less technical students or teams behind schedule"
IDS = ["write-test-plan", "confirm-hardened-config-is-active", "run-source-tests",
       "read-boundary-capture", "check-operator-health", "sign-test-record"]


def lesson(name, eu):
    directory = ROOT / ("lab-definitions/packages/eu-iec104-substation/scenarios"
                        if eu else "lab-definitions/scenarios")
    return yaml.safe_load((directory / (("eu-" if eu else "") + name + ".yml")).read_text())


class ManualValidationContentTests(unittest.TestCase):
    def test_required_closures_teach_deny_not_reset_in_all_four_lessons(self):
        canonical = ('Use DENY for every required closure. A RESET rule answers with a TCP reset, '
                     'so the host test shows "Connection refused" and cannot prove the drop; '
                     'the validator reports it as blocked by RESET.')
        for eu in (False, True):
            for name in ("firewall-implementation", "validation-evidence"):
                with self.subTest(eu=eu, lesson=name):
                    descriptions = [s["description"] for s in lesson(name, eu)["steps"]]
                    self.assertTrue(any(canonical in text for text in descriptions))
            package = "eu-iec104-substation" if eu else "us-dnp3-substation"
            dynamic = read_curriculum(ROOT / "lab-definitions/packages" / package)
            self.assertIn("firewall RESET rule", dynamic["phases"]["negative"])
            decision = lesson("segmentation-requirements", eu)
            text = "\n".join(s["description"] for s in decision["steps"])
            self.assertIn("options=TIMEOUT,CONNECTION_REFUSED,OPEN correct=TIMEOUT", text)
            self.assertIn("host (nothing listening) or a firewall RESET rule", text)

    def test_change_safety_and_backup_in_both_packages(self):
        for eu in (False, True):
            implementation = lesson("firewall-implementation", eu)
            rules = next(s["description"] for s in implementation["steps"]
                         if s["id"] in ("implement-rules", "phase-3-create-minimal-required-rules"))
            backup = next(s["description"] for s in implementation["steps"]
                          if s["id"] == "export-your-policy")
            for phrase in ("commit confirmed 60", "Commit-confirmed", "confirm",
                           "previous running config at the deadline", "management",
                           "operational conduits", "rollback", "one level only",
                           "rollback targets", "do not confirm"):
                self.assertIn(phrase, " ".join(rules.split()))
            self.assertLess(rules.index("commit confirmed 60"), rules.index("    confirm"))
            self.assertLess(rules.index("Check management access"), rules.index("    confirm"))
            for phrase in ("restorable", "evidence", "import config /data/lab-2.2-policy.json",
                           "local/SSH", "not web CLI",
                           "Import stages only", "commit to activate", "confirm"):
                self.assertIn(phrase, " ".join(backup.split()))
            for name in ("hardening-configurations", "vendor-rdp-compromise"):
                steps = lesson(name, eu)["steps"]
                intro = " ".join(steps[0]["description"].split())
                for phrase in ("one level only", "no other commit came after Reset to Weak",
                               "import your Lab 2.2 backup", "catch-up"):
                    self.assertIn(phrase, intro)
                restore = next(s["description"] for s in steps
                               if s["id"] in ("apply-hardened-policy", "apply-the-hardened-policy"))
                self.assertIn("    rollback", restore)
                self.assertIn("`import config /data/lab-2.2-policy.json`", restore)
                self.assertIn("A second rollback does not go", restore)
                for step in steps:
                    self.assertNotIn("not preserve them for automatic restoration", step["description"])
                    self.assertNotIn("normal route back", step["description"])
            for name in ("remediation-planning", "validation-evidence"):
                text = " ".join(s["description"] for s in lesson(name, eu)["steps"])
                for phrase in ("confirm", "rollback", "import", "management"):
                    self.assertIn(phrase, text)
                self.assertNotIn("re-stage the previous rules", text)
                self.assertNotIn("do not assume an import command exists", text)

    def test_backup_file_procedure_is_identical_and_complete_in_both_packages(self):
        bodies = []
        for eu in (False, True):
            text = next(s["description"] for s in lesson("firewall-implementation", eu)["steps"]
                        if s["id"] == "export-your-policy")
            body = text.split("After `confirm`,", 1)[1].split("In Notes,", 1)[0].strip()
            bodies.append(body)
            for phrase in ("create backup lab-2.2 redacted CONFIRM", "show backups",
                           "Replace `<id>`", "download backup <id> /data/lab-2.2-policy.json",
                           "takes effect immediately", "lab config has no secrets",
                           "refuses to overwrite", "stored backup survive **Reset to Weak**",
                           "persistent data volume", "mode 0600"):
                self.assertIn(phrase, " ".join(body.split()))
            commands = ["export config", "create backup lab-2.2 redacted CONFIRM",
                        "show backups", "download backup <id> /data/lab-2.2-policy.json",
                        "import config /data/lab-2.2-policy.json"]
            positions = [body.index("    " + command) for command in commands]
            self.assertEqual(positions, sorted(positions))
            self.assertNotIn("save the JSON file on", body)
            self.assertNotIn("heredoc", body)
        self.assertEqual(*bodies)

    def test_deny_correlation_is_taught_in_static_dynamic_retest_and_both_capture_steps(self):
        from scripts.lib.lab_commands_inventory import extract
        for eu, package in ((False, "us-dnp3-substation"), (True, "eu-iec104-substation")):
            implementation = lesson("firewall-implementation", eu)
            retest = next(s["description"] for s in implementation["steps"]
                          if s["id"] in ("test-negative", "phase-6-validate-blocked-traffic"))
            dynamic = read_curriculum(ROOT / "lab-definitions/packages" / package)["phases"]["negative"]
            validation = lesson("validation-evidence", eu)
            for text in (retest, dynamic):
                self.assertNotIn("boundary capture", text)
                self.assertNotIn("baseline step", text)
                self.assertIn("connect timeout with a fw-1 DENY event in the same UTC window",
                              " ".join(text.split()))
            self.assertIn("fw-1 DENY event and the boundary capture",
                          validation["steps"][2]["description"])
            self.assertIn("Preserve all three references",
                          validation["steps"][3]["description"])
            for text in (retest, dynamic, validation["steps"][2]["description"],
                         validation["steps"][3]["description"]):
                for required in ("show events", "firewall.rule.hit", "DENY", "ruleId",
                                 "SRC", "DST", "UTC", "fw-1", "containd cli"):
                    self.assertIn(required, text)
                self.assertNotIn("diag reach", text)
                self.assertIn("not", text)
            rows = [r for r in extract(package_id=package) if r["command"] == "show events"]
            self.assertEqual(len(rows), 3)
            suffix = "-eu-iec104-substation" if eu else ""
            contracts = json.loads((ROOT / f"scripts/lab-commands-expectations{suffix}.json").read_text())
            for row in rows:
                key = f'{row["scenario"]}|{row["step"]}|show events|1'
                self.assertEqual((row["node"], row["prompt"], row["action"]), ("fw-1", "cli", "RUN"))
                self.assertEqual(contracts["commands"][key]["oracle"]["intent"], "read")
                self.assertIn(r"firewall\.rule\.hit", contracts["commands"][key]["oracle"]["match"])

    def test_shared_six_step_design_and_artifacts(self):
        for eu in (False, True):
            doc = lesson("validation-evidence", eu)
            with self.subTest(eu=eu):
                self.assertEqual([s["id"] for s in doc["steps"]], IDS)
                self.assertEqual([s.get("action", {}).get("type") for s in doc["steps"]],
                                 [None, "check", None, None, "check", None])
                self.assertEqual(doc["steps"][1]["action"]["expect"], {"firewall_config": "improved"})
                self.assertNotIn("firewall_config", doc["steps"][4]["action"]["expect"])
                plan, baseline, tests, capture, health, record = [s["description"] for s in doc["steps"]]
                for field in ("Source host / IP / zone", "Destination / IP", "TCP port / purpose",
                              "Expected TCP and application result", "Abort criteria", "per prohibited requirement and source zone"):
                    self.assertIn(field, plan)
                prefix = "eu-" if eu else ""
                for panel in (f"notes-panel from={prefix}remediation-planning",
                              f"notes-panel from={prefix}firewall-implementation",
                              f"findings-panel from={prefix}segmentation-requirements",
                              f"findings-panel from={prefix}firewall-implementation"):
                    self.assertIn(panel, plan)
                for field in ("Go / no-go", "Deferred Lab 1.4 actions", "Tester signature",
                              "Operator signature", "Change owner approval or rejection", "does not sign this record"):
                    self.assertIn(field, record)
                self.assertIn("not graded", health)
                self.assertNotIn("correct=", health)
                self.assertEqual(tests.count("### Prepared positive test commands"), 1)
                self.assertEqual(tests.count("### Prepared negative test commands"), 1)
                self.assertEqual(tests.count(HINT), 2)
                self.assertIn("does not grade", " ".join(health.split()))

    def test_capture_correlation_covers_both_legs_and_missing_evidence(self):
        for eu in (False, True):
            doc = lesson("validation-evidence", eu)
            baseline, capture = doc["steps"][1]["description"], doc["steps"][3]["description"]
            command = next(line.strip() for line in baseline.splitlines() if line.strip().startswith("tcpdump "))
            self.assertTrue(command.startswith("tcpdump -i any -nn -U -G 1200 -W 1 -w /data/captures/validation.pcap"))
            self.assertNotIn("port ", command)
            self.assertIn("Ctrl-C stops it early", baseline)
            self.assertIn("sll.ifindex", capture)
            self.assertNotIn(" -V", capture)
            self.assertIn("-T fields", capture)
            for field in ("10.10.10.50", "10.20.20.0/24", "10.30.30.0/24", "10.40.40.0/24"):
                self.assertIn(field, command)
            if eu:
                self.assertIn("10.60.60.0/24", command)
                self.assertIn("10.70.70.0/24", command)
            for field in ("Ctrl-C", "Linux cooked capture v2 interface index", "ip link",
                          "source-zone", "destination-zone", "No packets is not proof of denial",
                          "inconclusive", "request/response", "packet/drop statistics",
                          "outside this capture", "before any reset"):
                self.assertIn(field, capture)
            self.assertNotIn("show audit", capture)
            self.assertNotIn("show logs", capture)
            self.assertNotIn("later episode", capture)
            self.assertNotIn("pending", capture)

    def test_positives_use_real_sources_and_read_only_application_evidence(self):
        spec = importlib.util.spec_from_file_location("extract", ROOT / "scripts/lib/lab-commands-extract.py")
        extractor = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(extractor)
        for eu, package in ((False, "us-dnp3-substation"), (True, "eu-iec104-substation")):
            tests = lesson("validation-evidence", eu)["steps"][2]["description"]
            for field in ("You do not add test clients to a production RTU", "watch it work",
                          "fresh good-quality points", "Do not install tools", "No control writes",
                          "Interactive terminal" if eu else "routed source must be 10.30.30.20"):
                self.assertIn(field, tests)
            rows = [r for r in extractor.extract(ROOT, package_id=package) if r["scenario"].endswith("validation-evidence")]
            self.assertGreaterEqual(sum(r["action"] == "RUN" for r in rows), 9 if eu else 17)
            for row in rows:
                self.assertEqual(row["policy"], "hardened")
                self.assertNotRegex(row["command"], r"(?:dnp3cmd|iec104cmd .* (?:dc|sc|rc)|mbpoll .* -- )")
            if eu:
                self.assertIn("busybox pscan -cb", tests)
                self.assertIn("socket.create_connection", tests)
                self.assertIn("not Run", tests)
                self.assertNotIn("iec104cmd", tests)
                reconnect = next(r for r in rows if "api/reconnect" in r["command"])
                self.assertEqual(reconnect["node"], "cc-1")

    def test_hardening_and_bonus_end_in_hand_records(self):
        for eu in (False, True):
            for name in ("hardening-configurations", "vendor-rdp-compromise"):
                steps = lesson(name, eu)["steps"]
                end = steps[-1]
                self.assertEqual(end["id"], "validate")
                self.assertEqual(end["action"]["type"], "check")
                self.assertIn("record", end["description"])
                self.assertIn("UTC", end["description"])
                text = " ".join(s["description"] for s in steps)
                self.assertNotRegex(text, r"(?i)plan proof|validation.report|21-row matrix")
                apply = next(s for s in steps if s["id"] in ("apply-the-hardened-policy", "apply-hardened-policy"))
                core = apply["description"].split(HINT)[0]
                for field in ("full diff", "Read back", "commit confirmed <seconds>", "rollback",
                              "one level only", "no other", "import", "confirm"):
                    self.assertIn(field, core)
                self.assertTrue(core.startswith("Restore your own Lab 2.2 policy"))
                self.assertIn(HINT, apply["description"])
                self.assertNotIn("Apply Your Plan", core)
                if name == "vendor-rdp-compromise":
                    for field in ("Enterprise → vendor", "Vendor → field", "tester", "operator", "change owner"):
                        self.assertIn(field, end["description"])
