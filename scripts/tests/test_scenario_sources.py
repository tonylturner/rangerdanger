"""Offline readers share the hard-cutover consumer source contract."""
import tempfile
import unittest
from pathlib import Path

import yaml

from scripts.lib.scenario_sources import read_scenarios, scenario_files


class ScenarioSourcesTests(unittest.TestCase):
    def fixture(self, root):
        package = root / "packages/alpha"
        package.mkdir(parents=True)
        (root / "shared").mkdir()
        (root / "shared/not-the-id.yml").write_text("id: lesson\nsteps: []\n")
        return package

    def manifest(self, package, sources):
        (package / "package.yml").write_text(yaml.safe_dump({"scenarios": sources}))

    def test_imports_by_authored_id_and_consumers_are_independent(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = self.fixture(root)
            self.manifest(package, [{"dir": "../../shared", "include": ["lesson"]}])
            other = root / "packages/beta"
            other.mkdir()
            self.manifest(other, [{"dir": "../../shared"}])
            self.assertEqual(list(read_scenarios(package, root)), ["lesson"])
            self.assertEqual(read_scenarios(package, root), read_scenarios(other, root))

    def test_rejects_invalid_sources(self):
        for sources in (
            "scenarios", [], [{"dir": ""}], [{"dir": "/tmp"}],
            [{"dir": r"..\shared"}], [{"dir": "../../../outside"}],
            [{"dir": "../../shared", "include": []}],
            [{"dir": "../../shared", "include": None}],
            [{"dir": "../../shared", "include": ["missing"]}],
            [{"dir": "../../shared", "include": ["not-the-id"]}],
            [{"dir": "../../shared", "include": ["lesson", "lesson"]}],
            [{"dir": "../../shared"}, {"dir": "../../shared"}],
        ):
            with self.subTest(sources=sources), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                package = self.fixture(root)
                self.manifest(package, sources)
                with self.assertRaises(ValueError):
                    scenario_files(package, root)

    def test_confines_directory_and_file_symlinks(self):
        for file_link in (False, True):
            with self.subTest(file_link=file_link), tempfile.TemporaryDirectory() as temporary, tempfile.TemporaryDirectory() as outside:
                root = Path(temporary)
                package = self.fixture(root)
                target = Path(outside) / "outside.yml"
                target.write_text("id: outside\n")
                if file_link:
                    (root / "shared/link.yml").symlink_to(target)
                    source = "../../shared"
                else:
                    (package / "linked").symlink_to(outside, target_is_directory=True)
                    source = "linked"
                self.manifest(package, [{"dir": source}])
                with self.assertRaisesRegex(ValueError, "escapes"):
                    scenario_files(package, root)
