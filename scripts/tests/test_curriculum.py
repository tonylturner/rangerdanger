"""Contracts for the shared offline curriculum reader."""

from pathlib import Path
import tempfile
import unittest

import yaml

from scripts.tests.curriculum import read_curriculum


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
