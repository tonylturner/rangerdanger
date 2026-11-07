"""Section 1 (baseline assessment and requirements) text contracts."""

import re
import unittest

from scripts.tests.test_lab_content_contracts import reference_action, scenario


class BaselineAssessmentContracts(unittest.TestCase):
    def test_vendor_observation_depends_on_capture_window(self):
        doc = scenario("baseline-assessment")
        findings = next(step for step in doc["steps"] if step["id"] == "findings-what-you-observed")
        self.assertIn("NOT_OBSERVED", findings["description"])
        text = findings["description"]
        decision = next(line for line in text.splitlines() if "id=vendor-to-ot " in line)
        self.assertNotIn("correct=", decision)
        self.assertIn("depends on whether the capture window overlaps", text)

    def test_observation_key_does_not_invent_writes(self):
        doc = scenario("baseline-assessment")
        text = doc["steps"][5]["description"]
        self.assertRegex(text, r"id=unauth-modbus-writes [^\n]*correct=NOT_OBSERVED")
        self.assertIn("NOT_OBSERVED in the prescribed read-only capture", text)
        self.assertNotIn("eng-ws mbpoll write commands", text)
        self.assertIn("Absence does not prove denial.", text)
        self.assertNotIn(":::hint What an analyst would conclude", text)
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
                columns = row.split("|")
                status = columns[-3].lower()
                self.assertTrue("preserve" in status or "blocked" in status)
                self.assertTrue(columns[-2].strip())
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
            self.assertIn("Blocked by reference", row)
        requirements = scenario("segmentation-requirements")["steps"][2]["description"]
        self.assertNotIn("all five critical conduits", requirements)
        self.assertIn("A/E autonomous and F/G intra-zone", requirements)
        self.assertIn("in this version", requirements)
        self.assertNotIn("deliberately retired", text)

    def test_first_capture_teaches_prompt_transition(self):
        text = scenario("baseline-assessment")["steps"][2]["description"]
        self.assertIn("configuration CLI", text)
        self.assertIn("Type `exit` to leave it", text)
        self.assertLess(text.index("Type `exit`"), text.index("tcpdump -i"))
        self.assertNotRegex(text, r"containd#|`(?:shell|bash)`")
        self.assertIn("containd cli", text)
        self.assertIn("| Prompt |", text)

    def test_capture_scope_does_not_hide_management(self):
        text = scenario("baseline-assessment")["steps"][2]["description"]
        self.assertIn("This filter excludes loopback only.", text)
        self.assertIn("10.99.99.0/24", text)
        self.assertIn("An ingress packet alone does not prove forwarding.", text)
        self.assertIn("'not host 127.0.0.1'", text)
        self.assertNotIn("no management UI traffic", text)

    def test_map_task_uses_policy_rows_not_missing_edges(self):
        text = scenario("baseline-assessment")["steps"][0]["description"]
        self.assertIn("Open Segmentation Policy", text)
        self.assertIn("Policy rows show configured permission", " ".join(text.split()))
        self.assertNotIn("what did the tooltip show", text)
        for decision in ("zones-located", "ent-to-field-protocols", "ot-to-field-protocols"):
            self.assertIn(f"id={decision}", text)


if __name__ == "__main__":
    unittest.main()
