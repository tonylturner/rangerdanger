"""Offline readers share the hard-cutover single-directory contract."""
import tempfile
import unittest
from pathlib import Path

import yaml

from scripts.lib.scenario_sources import read_scenarios, scenario_files


class ScenarioSourcesTests(unittest.TestCase):
    def fixture(self, root):
        package = root / "packages/alpha"
        package.mkdir(parents=True)
        (package / "scenarios").mkdir()
        (package / "scenarios/not-the-id.yml").write_text("id: lesson\nsteps: []\n")
        return package

    def manifest(self, package, directory):
        (package / "package.yml").write_text(yaml.safe_dump({"scenarios": directory}))

    def test_package_directories_allow_the_same_authored_id(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = self.fixture(root)
            self.manifest(package, "scenarios")
            other = root / "packages/beta"
            (other / "scenarios").mkdir(parents=True)
            (other / "scenarios/lesson.yml").write_text("id: lesson\nsteps: []\n")
            self.manifest(other, "scenarios")
            self.assertEqual(list(read_scenarios(package, root)), ["lesson"])
            self.assertEqual(read_scenarios(package, root), read_scenarios(other, root))
            self.assertNotEqual(scenario_files(package, root), scenario_files(other, root))

    def test_rejects_invalid_directories(self):
        for directory in (
            [], [{"dir": "scenarios"}], {"dir": "scenarios"}, None, "", " ",
            "/tmp", "C:/temp", r"..\scenarios", "../../../outside",
            "scenarios/not-the-id.yml",
        ):
            with self.subTest(directory=directory), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                package = self.fixture(root)
                self.manifest(package, directory)
                with self.assertRaises(ValueError):
                    scenario_files(package, root)

    def test_rejects_duplicate_ids_and_non_regular_files(self):
        for duplicate in (False, True):
            with self.subTest(duplicate=duplicate), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                package = self.fixture(root)
                self.manifest(package, "scenarios")
                path = package / "scenarios/other.yml"
                if duplicate:
                    path.write_text("id: lesson\n")
                else:
                    path.mkdir()
                with self.assertRaisesRegex(ValueError, "duplicate|regular"):
                    scenario_files(package, root)

    def test_allows_confined_symlinks_and_literal_directory_names(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = self.fixture(root)
            (package / "scenarios").rename(package / "lessons[1]")
            (package / "linked").symlink_to("lessons[1]", target_is_directory=True)
            lesson = package / "lessons[1]/not-the-id.yml"
            lesson.rename(package / "lesson.yml")
            lesson.symlink_to("../lesson.yml")
            self.manifest(package, "linked")
            self.assertEqual(list(read_scenarios(package, root)), ["lesson"])

    def test_confines_directory_and_file_symlinks(self):
        for file_link in (False, True):
            with self.subTest(file_link=file_link), tempfile.TemporaryDirectory() as temporary, tempfile.TemporaryDirectory() as outside:
                root = Path(temporary)
                package = self.fixture(root)
                target = Path(outside) / "outside.yml"
                target.write_text("id: outside\n")
                if file_link:
                    (package / "scenarios/link.yml").symlink_to(target)
                    source = "scenarios"
                else:
                    (package / "linked").symlink_to(outside, target_is_directory=True)
                    source = "linked"
                self.manifest(package, source)
                with self.assertRaisesRegex(ValueError, "escapes"):
                    scenario_files(package, root)
