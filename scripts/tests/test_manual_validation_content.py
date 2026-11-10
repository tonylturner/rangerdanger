"""The workshop teaches a hand test record, not a generated acceptance result."""
import importlib.util
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
HINT = ":::hint Optional catch-up — for less technical students or teams behind schedule"
IDS = ["write-test-plan", "confirm-hardened-config-is-active", "run-source-tests",
       "read-boundary-capture", "check-operator-health", "sign-test-record"]


def lesson(name, eu):
    directory = ROOT / ("lab-definitions/packages/eu-iec104-substation/scenarios"
                        if eu else "lab-definitions/scenarios")
    return yaml.safe_load((directory / (("eu-" if eu else "") + name + ".yml")).read_text())


class ManualValidationContentTests(unittest.TestCase):
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
            self.assertTrue(command.startswith("tcpdump -i any -nn -w /data/captures/validation.pcap"))
            self.assertNotIn("port ", command)
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
                for field in ("full diff", "read back", "commit once"):
                    self.assertIn(field, core)
                self.assertNotIn("Apply Your Plan", core)
                if name == "vendor-rdp-compromise":
                    for field in ("Enterprise → vendor", "Vendor → field", "tester", "operator", "change owner"):
                        self.assertIn(field, end["description"])
