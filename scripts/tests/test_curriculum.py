"""Contracts for the shared offline curriculum reader."""

from pathlib import Path
import copy
import tempfile
import unittest

import yaml

from scripts.tests.curriculum import read_curriculum, validate_budget_adjustments


class CurriculumReaderTests(unittest.TestCase):
    def read_fixture(self, files, directory="curriculum"):
        with tempfile.TemporaryDirectory(prefix="rd-curriculum-") as temporary:
            root = Path(temporary)
            (root / "package.yml").write_text(yaml.safe_dump({"curriculum": {"dir": directory}}))
            (root / "curriculum").mkdir()
            for name, text in files.items():
                (root / "curriculum" / name).write_text(text)
            return read_curriculum(root)

    def test_disjoint_sections_preserve_scalars(self):
        self.assertEqual(self.read_fixture({
            "catalog.yml": "schema: 1\n", "plan.yml": 'plan: "${ip:cc-1}"\n',
        }), {"schema": 1, "plan": "${ip:cc-1}"})

    def test_rejects_invalid_sections(self):
        for files in (
            {}, {"notes.yml": "schema: 1\n"},
            {"catalog.yml": "schema: 1\n", "notices.yml": "retired: true\n"},
            {"catalog.yml": "schema: 1\n", "plan.yml": "schema: 1\n"},
            {"catalog.yml": "schema: 1\nschema: 1\n"},
            {"catalog.yml": "schema: 1\n---\nschema: 1\n"},
            {"catalog.yml": "null\n"}, {"catalog.yml": "[]\n"},
            {"catalog.yml": "1: true\n"},
        ):
            with self.subTest(files=files), self.assertRaises((ValueError, yaml.YAMLError)):
                self.read_fixture(files)

    def test_rejects_non_sibling_directory(self):
        with self.assertRaisesRegex(ValueError, "Invalid"):
            self.read_fixture({}, "../curriculum")


class BudgetAdjustmentTests(unittest.TestCase):
    def model(self):
        return {"teams": [{"name": "OT Engineer", "capacity_hours": 120}],
                "catalog": [{"id": "dpi"}], "readiness": [{"id": "ot"}],
                "budgetAdjustments": [{"source": "ot", "verdict": "STRETCHED",
                    "teamCaps": {"OT Engineer": 60},
                    "effort": [{"actions": ["dpi"], "multiplier": 1.5, "extraHours": 20}]}]}

    def test_valid_and_invalid_adjustments(self):
        model = self.model()
        validate_budget_adjustments(model)
        for field, value in (("source", "foreign"), ("verdict", ""),
                             ("teamCaps", None), ("teamCaps", {"OT Engineer": -1}),
                             ("teamCaps", {"OT Engineer": 121}), ("effort", None)):
            changed = copy.deepcopy(model)
            changed["budgetAdjustments"][0][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                validate_budget_adjustments(changed)
        for field, value in (("actions", ["foreign"]), ("actions", ["dpi", "dpi"]),
                             ("multiplier", float("nan")), ("multiplier", 0),
                             ("extraHours", -1)):
            changed = copy.deepcopy(model)
            changed["budgetAdjustments"][0]["effort"][0][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                validate_budget_adjustments(changed)

    def test_both_package_tables(self):
        root = Path(__file__).resolve().parents[2] / "lab-definitions/packages"
        for package in ("us-dnp3-substation", "eu-iec104-substation"):
            with self.subTest(package=package):
                model = read_curriculum(root / package)
                self.assertEqual(len(model["budgetAdjustments"]), 5)
                self.assertEqual(model["budgetAdjustments"][0]["teamCaps"], {"OT Engineer": 60})
                self.assertEqual(model["budgetAdjustments"][1]["teamCaps"], {"OT Engineer": 0})
                self.assertEqual(model["budgetAdjustments"][2]["teamCaps"], {"Firewall Admin": 80})
                self.assertEqual(model["budgetAdjustments"][3]["teamCaps"], {"Firewall Admin": 40})
