"""Offline fail-closed authoring guard; mutations stay in temporary fixtures."""
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from .test_lab_commands_extract import ROOT, extract


class PackageGuardTests(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        shutil.copytree(ROOT / "lab-definitions", root / "lab-definitions")
        (root / "scripts").mkdir()
        for path in (ROOT / "scripts").glob("lab-commands-expectations*.json"):
            shutil.copy(path, root / "scripts" / path.name)
        return root

    def test_all_packages_and_zero_command_scenarios_are_pinned(self):
        for package_id in extract.packages():
            with self.subTest(package=package_id):
                self.assertGreater(extract.check_package(ROOT, package_id), 0)

    def test_new_command_and_empty_scenario_fail_with_author_instructions(self):
        for command in ("", "    curl http://example.invalid"):
            with self.subTest(command=command), tempfile.TemporaryDirectory() as directory:
                root = self.fixture(directory)
                new = root / "lab-definitions/packages/eu-iec104-substation/scenarios/new.yml"
                new.write_text("id: new\nnodes: [kali-1]\nsteps:\n"
                               "- id: read\n  title: Read\n  description: |\n    From Kali:\n    " + command + "\n")
                result = subprocess.run(
                    [sys.executable, str(ROOT / "scripts/lib/lab-commands-extract.py"),
                     "--root", str(root), "--guard"],
                    text=True, capture_output=True, timeout=15)
                self.assertEqual(result.returncode, 1)
                self.assertIn("include every scenario (even zero-command scenarios)", result.stderr)
                self.assertIn("RUN needs a reviewed oracle, SKIP needs a reviewed reason", result.stderr)
                self.assertIn("--repin --package <package>", result.stderr)

    def test_changed_command_or_missing_contract_fails(self):
        rows = extract.extract()
        data = json.loads(extract.EXPECTATIONS.read_text())
        rows[0]["command"] += " changed"
        with self.assertRaisesRegex(ValueError, "command inventory changed"):
            extract.validate_inventory(rows, data)

    def test_missing_review_or_stale_dispatch_is_rejected(self):
        for field, value, message in (
                ("container", "wrong", "container changed"),
                ("oracle", None, "missing reviewed oracle"),
                ("oracle", {"intent": "read", "match": "x", "typo": True}, "unknown oracle")):
            rows = extract.extract()
            data = json.loads(extract.EXPECTATIONS.read_text())
            data["commands"][extract.key(rows[0])][field] = value
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, message):
                extract.validate_inventory(rows, data)

    def test_shared_grammar_covers_nested_sources_and_ignores_decisions(self):
        from lab_commands_inventory import description_commands
        text = ("From the host:\n    curl localhost\n"
                "From cc-1:\n:::hint More\n    wget reconnect\n:::\n"
                ":::decision id=x\n    nmap not-a-command\n:::\n"
                ":::technical\nOn the RTU:\n    iec104cmd station gi\n:::\n")
        rows = description_commands(text)
        self.assertEqual([(r[0], r[2]) for r in rows], [
            ("curl localhost", "host"), ("wget reconnect", "cc-1"),
            ("iec104cmd station gi", "rtu-1")])


if __name__ == "__main__":
    unittest.main()
