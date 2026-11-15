"""Offline SRA lesson contracts; these checks never assert live acceptance."""
import json
import re
import unittest
from pathlib import Path

import yaml

from .test_lab_commands_extract import extract

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / "lab-definitions/packages/us-sra-substation"


class SRALessonTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.package = yaml.safe_load((PACKAGE / "package.yml").read_text())
        cls.lessons = {
            doc["id"]: doc for path in (PACKAGE / "scenarios").glob("*.yml")
            if (doc := yaml.safe_load(path.read_text()))
        }
        cls.rows = extract.extract(package_id=cls.package["id"])
        cls.contract = json.loads(extract.expectations_path(ROOT, cls.package["id"]).read_text())

    def lesson(self, name):
        return self.lessons["sra-" + name]

    def test_course_is_seven_manual_activities_in_five_modules(self):
        modules = self.package["course"]["modules"]
        self.assertEqual(len(modules), 5)
        activities = [a for m in modules for a in m["activities"]]
        self.assertEqual([a["label"] for a in activities], ["1.1", "2.1", "2.2", "3.1", "4.1", "5.1", "5.2"])
        self.assertEqual({a["scenario"] for a in activities}, set(self.lessons))
        self.assertEqual(len(activities), len(self.lessons))
        self.assertGreater(self.package["revision"], 1)
        self.assertNotIn("features", self.package["course"])
        node_map = yaml.safe_load((PACKAGE / "nodes.yml").read_text())["nodeMap"]
        for activity in activities:
            self.assertEqual(set(activity), {"scenario", "label", "validate"})
            self.assertIs(activity["validate"], False)
            lesson = self.lessons[activity["scenario"]]
            self.assertNotIn("validator", lesson)
            self.assertEqual(node_map[lesson["id"]]["primary"], "vendor-jump-1")
            self.assertEqual(set(lesson["nodes"]), {
                node_map[lesson["id"]]["primary"], *node_map[lesson["id"]]["others"],
            })
            for step in lesson["steps"]:
                self.assertIn(step["expected_config"], ("weak", "improved"))
                if "action" in step:
                    self.assertEqual(step["action"]["type"], "firewall")
                    self.assertEqual(step["expected_config"], step["action"]["config"])

    def test_seeds_match_authority_and_are_copy_only(self):
        realm = json.loads((ROOT / "services/sra/keycloak/realm.json").read_text())
        seeds = {
            user["username"]: json.loads(next(
                cred for cred in user["credentials"] if cred["type"] == "otp"
            )["secretData"])["value"]
            for user in realm["users"]
        }
        body = "\n".join(s["description"] for s in self.lesson("history-playback")["steps"])
        for username in ("vendor", "auditor", "sra-admin"):
            self.assertIn(f"`{username}`", body)
            self.assertIn("oathtool --totp -b " + seeds[username], body)
        self.assertIn("password `rangerdanger`", body)
        for row in self.rows:
            if row["command"].startswith(("oathtool ", "whoami")):
                self.assertEqual((row["node"], row["action"]), ("vendor-jump-1", "SKIP"))
                self.assertTrue(self.contract["commands"][extract.key(row)]["reason"])
        broker = self.lesson("broker-login")
        self.assertIn("fresh private", " ".join(broker["steps"][1]["description"].split()))
        self.assertIn("not a per-session broker approval", " ".join(broker["steps"][-1]["description"].split()))

    def test_source_dispatch_and_policy_windows(self):
        for row in self.rows:
            command = row["command"]
            if command.startswith("nmap "):
                if row["scenario"] == "sra-no-inbound-ot":
                    source = ("kali-1" if "-p 8080 " in command or
                              row["step_id"] == "improved-source-negative" or
                              (row["step"] == 2 and row["occurrence"] == 2)
                              else "vendor-jump-1")
                else:
                    source = ("kali-1" if "-p 8080 " in command or
                              (row["scenario"] == "sra-vendor-test-record" and
                               "-p 1280 " in command and row["occurrence"] == 2)
                              else "vendor-jump-1")
                self.assertEqual(row["node"], source, extract.key(row))
            elif command.startswith("curl "):
                self.assertEqual(row["node"], "vendor-jump-1", extract.key(row))
            elif row["prompt"] == "cli":
                self.assertEqual(row["node"], "fw-1", extract.key(row))
        steps = self.lesson("close-direct-path")["steps"]
        self.assertEqual(steps[0]["action"]["config"], "weak")
        self.assertEqual(steps[2]["action"]["config"], "improved")
        weak = {r["command"] for r in self.rows
                if r["scenario"] == "sra-close-direct-path" and r["step"] == 2}
        negative = {r["command"] for r in self.rows
                    if r["scenario"] == "sra-close-direct-path" and r["step"] == 4}
        self.assertLessEqual(negative, weak)
        for name, reset_id in (("history-playback", "reset-live-session"),
                               ("regression-weak", "reset-lab-regression")):
            steps = self.lesson(name)["steps"]
            index = next(i for i, s in enumerate(steps) if s["id"] == reset_id)
            self.assertEqual(steps[0]["action"]["config"], "improved")
            reset = steps[index]
            self.assertNotIn("action", reset)
            self.assertIn("Reset Lab", reset["description"])
            self.assertIn("Weak", reset["description"])
            restore = next(i for i, s in enumerate(steps) if i > index and "action" in s)
            self.assertEqual(steps[restore]["action"]["config"], "improved")
            for step in steps[index + 1:restore]:
                self.assertEqual(step["expected_config"], "weak")
            for step in steps[restore:]:
                self.assertEqual(step["expected_config"], "improved")

    def test_run_oracles_reject_wrong_network_results(self):
        for row in self.rows:
            if row["action"] != "RUN":
                continue
            oracle = self.contract["commands"][extract.key(row)]["oracle"]
            command = row["command"]
            if command.startswith("nmap "):
                ports = re.search(r"-p ([0-9,]+)", command)[1].split(",")
                if ports == ["1280"]:
                    state = ("open" if row["policy"] == "improved" and
                             row["node"] == "vendor-jump-1" else "filtered")
                elif ports == ["8080"]:
                    state = "filtered"  # Kali is denied under both policies.
                else:
                    state = "open" if row["policy"] == "weak" else "filtered"
                output = "\n".join(f"{p}/tcp {state} test" for p in ports)
                is_deny = oracle["intent"] == "deny"
                self.assertTrue(extract.verdict(oracle, 0, output, "", control=is_deny), extract.key(row))
                for wrong in ("closed", "open" if state == "filtered" else "filtered", "unfiltered"):
                    self.assertFalse(extract.verdict(
                        oracle, 0, output.replace(state, wrong), "", control=is_deny
                    ), extract.key(row))
                if is_deny:
                    self.assertFalse(extract.verdict(oracle, 0, output, "", control=False))
                if len(ports) > 1:
                    self.assertFalse(extract.verdict(
                        oracle, 0, f"{ports[0]}/tcp {state}", "", control=is_deny
                    ))
            elif command.startswith("curl "):
                output = (
                    '{"issuer":"http://10.25.25.10:8080/realms/sra"}'
                    if "openid-configuration" in command else
                    "403\n" if "/api/tokens" in command else
                    "RTAC Management Console" if "10.30.30.20" in command else "200\n"
                )
                self.assertTrue(extract.verdict(oracle, 0, output, ""), extract.key(row))
                for rc, text in ((0, "500\n"), (0, "302\n"), (28, "")):
                    self.assertFalse(extract.verdict(oracle, rc, text, ""))
            elif command == "show protection":
                # Documented API-backed CLI shape, not a claim of a live read.
                ready = {
                    "state": "protecting", "ready": True,
                    "appliedRevision": "runtime-test", "protectingRevision": "runtime-test",
                }
                self.assertTrue(extract.verdict(oracle, 0, json.dumps(ready), ""))
                for change in ({"ready": False}, {"state": "pending"},
                               {"appliedRevision": None}, {"protectingRevision": None}):
                    self.assertFalse(extract.verdict(oracle, 0, json.dumps(ready | change), ""))

    def test_record_keeps_missing_evidence_and_human_signatures(self):
        lesson = self.lesson("vendor-test-record")
        self.assertEqual(len(lesson["steps"]), 6)
        baseline = lesson["steps"][1]["description"]
        self.assertIn("runningRevision equals", baseline)
        self.assertIn("non-null/non-empty", baseline)
        self.assertIn("record POL-1 as **failed**", baseline)
        self.assertNotIn("does not expose runningRevision", baseline)
        self.assertIn("/containd/api/v1/firewall/counters", baseline)
        for token in ("snapshotVersion", "since", "deny-vendor-to-ot",
                      "deny-vendor-to-field", "deny-enterprise-to-sra",
                      "vendor-to-sra-identity", "vendor-to-sra-broker",
                      "vendor-to-sra-controller", "ot-host-to-sra-controller",
                      "ot-host-to-sra-router"):
            self.assertIn(token, baseline)
        final = lesson["steps"][-1]["description"]
        for token in ("Tester name/signature/UTC", "operator name/signature/UTC",
                      "change-owner name/signature/UTC", "residual-risk owner",
                      "inconclusive/untested", "WORK-1", "OBS-1", "OUT-1"):
            self.assertIn(token, final)
        body = "\n".join(s["description"] for s in lesson["steps"])
        self.assertIn("not logins or users", body)
        for token in ("Dial/Bind", "same-session re-read", "10.30.30.60",
                      "10.25.25.40:1280", "10.25.25.50:3022"):
            self.assertIn(token, body)
        for lesson in self.lessons.values():
            text = " ".join(" ".join(s["description"] for s in lesson["steps"]).split())
            for stale in ("not yet available", "not included yet", "absent 3.1",
                          "remains untested until 3.1"):
                self.assertNotIn(stale, text)

        regression = self.lesson("regression-weak")
        steps = regression["steps"]
        before = steps[1]["description"]
        reset = steps[2]["description"]
        weak = steps[3]["description"]
        restored = steps[4]["description"]
        final_readback = steps[6]["description"]
        self.assertIn("running revision shown in the Execute Step result", restored)
        for description in (before, weak, final_readback):
            self.assertIn("runningRevision", description)
            self.assertIn("appliedRevision", description)
            self.assertIn("protectingRevision", description)
            self.assertIn("non-null/non-empty", description)
            self.assertIn("POL-1", description)
        self.assertIn("Containd running revision", reset)
        self.assertIn("do not reuse the earlier Execute Step revision", reset)
        self.assertIn("the Weak revision must differ", weak)
        self.assertIn("Compare the restored Execute Step", final_readback)
        for description in (steps[0]["description"], before, reset, weak,
                            restored, final_readback):
            self.assertNotIn("receipt visibility gap", description)
            self.assertNotIn("runningRevision if exposed", description)

    def test_no_inbound_proof_has_separate_controls_and_manual_observer(self):
        steps = self.lesson("no-inbound-ot")["steps"]
        self.assertEqual(steps[0]["action"]["config"], "weak")
        self.assertEqual(steps[2]["action"]["config"], "improved")
        self.assertEqual([s["expected_config"] for s in steps], ["weak"] * 2 + ["improved"] * 6)
        read = steps[3]["description"]
        for token in ("#sra-dialers", "#sra-hosts", "sra-router", "ot-host",
                      "not a Ziti workload identity", "1280/tcp open", "HOME `/config`"):
            self.assertIn(token, read)
        refusal = steps[4]["description"]
        for token in ("exit **1**", "401 Unauthorized", "Do **not** log",
                      "same session", "including their IDs", "whole listener"):
            self.assertIn(token, refusal)
        observer = [r for r in self.rows if r["command"].startswith("ziti ")]
        self.assertEqual(len(observer), 7)
        for row in observer:
            self.assertEqual((row["node"], row["action"]), ("vendor-jump-1", "SKIP"))
            self.assertTrue(self.contract["commands"][extract.key(row)]["reason"])
        command_order = [r["command"] for r in observer]
        self.assertEqual(command_order[-2:], [
            "ziti edge delete service-policy sra-dial", "ziti edge list service-policies",
        ])
        direct = steps[5]["description"]
        for token in ("whoami", "rangerdanger", "working Weak SSH control",
                      "service works now", "refused", "reached"):
            self.assertIn(token, direct)
        final = steps[-1]["description"]
        policy = json.loads((PACKAGE / "firewall/improved.json").read_text())
        rules = {r["id"]: r for r in policy["firewall"]["rules"]}
        for rule_id in ("vendor-to-sra-controller", "ot-host-to-sra-controller",
                        "ot-host-to-sra-router", "deny-vendor-to-ot"):
            self.assertIn(rule_id, final)
            self.assertIn(rule_id, rules)
        for token in ("snapshotVersion", "since", "packets/bytes, not logins",
                      "10.30.30.60", "10.25.25.40:1280", "10.25.25.50:3022",
                      "not mean all traffic into OT", "inconclusive"):
            self.assertIn(token, final)


if __name__ == "__main__":
    unittest.main()
