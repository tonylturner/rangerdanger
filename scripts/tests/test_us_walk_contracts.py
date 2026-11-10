"""Offline contracts for the US manual cutover and policy-preview teaching."""
import json
import re
import unittest

from .test_lab_commands_extract import ROOT, extract
from .test_lab_content_contracts import scenario


def policy(name):
    path = ROOT / f"lab-definitions/firewall/substation-{name}.json"
    return json.loads(path.read_text())["firewall"]


class USWalkContracts(unittest.TestCase):
    def test_manual_cutover_commits_operational_replacements_with_deletions(self):
        steps = scenario("firewall-implementation")["steps"]
        phase2, phase3 = steps[3]["description"], steps[4]["description"]
        rows = [r for r in extract.extract(ROOT)
                if r["scenario"] == "firewall-implementation" and r["step"] in (4, 5)]
        deletes = [r for r in rows if r["command"].startswith("delete firewall rule ")]
        self.assertEqual(len(deletes), 5)
        self.assertTrue(all(r["step"] == 4 for r in deletes))
        self.assertEqual([r["step"] for r in rows if r["command"] == "commit"], [5])
        self.assertNotIn("mbpoll ", phase2)
        self.assertIn("Do not commit yet.", phase2)
        self.assertIn("same shared candidate", phase2)
        self.assertIn("ot-to-field-broad", phase2)
        self.assertIn("replacements and removals together", phase3)
        self.assertLess(phase3.index("Before the single commit"), phase3.index("\n    commit"))
        self.assertLess(phase3.index("\n    commit"), phase3.index("mbpoll "))

        rules = policy("improved")["rules"]
        by_id = {r["id"]: r for r in rules}
        order = [r["id"] for r in rules]
        for suffix, port in (("modbus", "502"), ("dnp3", "20000"), ("http", "8080")):
            name = "rtac-to-field-" + suffix
            self.assertIn(name, phase3)
            rule = by_id[name]
            self.assertEqual(rule["sources"], ["10.30.30.20/32"])
            self.assertEqual(rule["protocols"], [{"name": "tcp", "port": port}])
            self.assertEqual(rule["action"], "ALLOW")
            self.assertLess(order.index(name), order.index("deny-ot-to-field-direct"))
        for name in ("ot-intrazone-ops", "gps-to-field-ntp"):
            self.assertIn(name, phase3)
            self.assertEqual(by_id[name]["action"], "ALLOW")
        self.assertIn("reserved management access", phase3)

        # Policy shortcuts remain available, but only in labelled catch-up hints.
        for description in (phase2, phase3):
            hint = re.search(r":::hint Optional catch-up — for less technical students or teams behind schedule\n(.*?)\n:::", description, re.S)[1]
            self.assertIn("Apply Your Plan", hint)
            self.assertIn("replaces typing rules", hint)
            self.assertIn("does not replace your own observations, operator check or sign-off", hint)
            self.assertNotRegex(description, r":::guided|:::technical|:::track-picker")

    def test_preview_answer_counts_action_changes_not_description_changes(self):
        # usCompare supplies the rows; compareRuleSets aggregates enforcing-rule
        # actions per pair, and segmentation-view highlights action inequality
        # only (not the backend's description-sensitive Change field).
        recipe = (ROOT / "backend/internal/server/us_recipe.go").read_text()
        pairs_text = recipe.split("pairs: []zonePair{", 1)[1].split("\n\t},", 1)[0]
        pairs = re.findall(r'\{"(\w+)", "(\w+)"\}', pairs_text)
        weak, improved = policy("weak"), policy("improved")

        def actions(config, pair):
            return {r["action"] for r in config["rules"]
                    if (r.get("ics") or {}).get("mode") != "monitor"
                    and pair[0] in r.get("sourceZones", [])
                    and pair[1] in r.get("destZones", [])}

        def label(config, pair):
            values = actions(config, pair)
            return ("MIXED" if len(values) > 1 else next(iter(values))) if values else config["defaultAction"]

        self.assertEqual(len(pairs), 14)
        changed = [p for p in pairs if label(weak, p) != label(improved, p)]
        self.assertEqual(len(changed), 6)
        reverse = pairs[8:]
        self.assertEqual(len(reverse), 6)
        for pair in reverse:
            self.assertFalse(actions(weak, pair))
            self.assertFalse(actions(improved, pair))
            self.assertEqual(label(improved, pair), "DENY")
        renderer = (ROOT / "frontend/components/segmentation-view.tsx").read_text()
        self.assertIn("action !== activeAction", renderer)
        text = scenario("segmentation-requirements")["steps"][0]["description"]
        self.assertIn("correct=ABOUT_HALF", text)
        self.assertIn("Six of the fourteen rows", text)
        self.assertIn("remain implicit", text)
        self.assertNotIn("rows become explicit DENY", text)

    def test_lab_event_label_and_outage_precision_match_the_product(self):
        text = scenario("validation-evidence")["steps"]
        text = "\n".join(step["description"] for step in text)
        self.assertIn("**Live Firewall Events**", text)
        self.assertNotIn("**Live DPI Events**", text)
        renderer = (ROOT / "frontend/components/segmentation-view.tsx").read_text()
        self.assertIn("Live Firewall Events", renderer)
        text = "\n".join(s["description"] for s in scenario("vendor-rdp-compromise")["steps"])
        self.assertIn("approximately zero", text)
        self.assertIn("current register `1` means 0.1 A", text)
        self.assertNotIn("both read `0`", text)


if __name__ == "__main__":
    unittest.main()
