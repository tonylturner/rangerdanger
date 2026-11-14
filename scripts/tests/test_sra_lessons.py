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

    def test_course_is_six_manual_activities_in_four_modules(self):
        modules = self.package["course"]["modules"]
        self.assertEqual(len(modules), 4)
        activities = [a for m in modules for a in m["activities"]]
        self.assertEqual([a["label"] for a in activities], ["1.1", "2.1", "2.2", "4.1", "5.1", "5.2"])
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
                source = "kali-1" if "-p 8080 " in command else "vendor-jump-1"
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
                state = "open" if row["policy"] == "weak" else "filtered"
                output = "\n".join(f"{p}/tcp {state} test" for p in ports)
                is_deny = oracle["intent"] == "deny"
                self.assertTrue(extract.verdict(oracle, 0, output, "", control=is_deny), extract.key(row))
                for wrong in ("closed", "open" if state == "filtered" else "filtered"):
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
        self.assertIn("does not expose runningRevision", baseline)
        self.assertIn("inconclusive", baseline)
        self.assertIn("/containd/api/v1/firewall/counters", baseline)
        for token in ("snapshotVersion", "since", "deny-vendor-to-ot",
                      "deny-vendor-to-field", "deny-enterprise-to-sra",
                      "vendor-to-sra-identity", "vendor-to-sra-broker"):
            self.assertIn(token, baseline)
        final = lesson["steps"][-1]["description"]
        for token in ("Tester name/signature/UTC", "operator name/signature/UTC",
                      "change-owner name/signature/UTC", "residual-risk owner",
                      "inconclusive/untested", "absent 3.1"):
            self.assertIn(token, final)
        body = "\n".join(s["description"] for s in lesson["steps"])
        self.assertIn("not logins or users", body)
        self.assertIn("not yet available", " ".join(body.split()))


if __name__ == "__main__":
    unittest.main()
