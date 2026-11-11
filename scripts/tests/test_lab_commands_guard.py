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
        # Backup listings and EU export are reads; file-writing mutations stay SKIP.
        minimums = {"us-dnp3-substation": 87, "eu-iec104-substation": 47}
        for package_id in extract.packages():
            with self.subTest(package=package_id):
                data = json.loads(extract.expectations_path(ROOT, package_id).read_text())
                self.assertEqual(extract.check_package(ROOT, package_id), data["minimum_executed"])
                self.assertEqual(data["minimum_executed"], minimums[package_id])

    def test_explicitly_pinned_all_prose_package_is_valid(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.fixture(directory)
            package_id = "eu-iec104-substation"
            scenarios = root / "lab-definitions/packages" / package_id / "scenarios"
            counts = {}
            for path in scenarios.glob("*.yml"):
                doc = extract.yaml.safe_load(path.read_text())
                for step in doc["steps"]:
                    step["description"] = "Prose only."
                path.write_text(extract.yaml.safe_dump(doc))
                counts[doc["id"]] = {"RUN": 0, "SKIP": 0}
            extract.expectations_path(root, package_id).write_text(json.dumps({
                "schema": 1, "inventory": counts, "minimum_executed": 0, "commands": {}}))
            self.assertEqual(extract.check_package(root, package_id), 0)

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

    def test_change_safety_commands_never_run_against_shared_policy(self):
        for package in ("us-dnp3-substation", "eu-iec104-substation"):
            rows = extract.extract(package_id=package)
            data = json.loads(extract.expectations_path(ROOT, package).read_text())
            mutations = [r for r in rows if r["command"].split()[0]
                         in ("commit", "confirm", "rollback", "import", "create", "download")]
            self.assertEqual({r["command"].split()[0] for r in mutations},
                             {"commit", "confirm", "rollback", "import", "create", "download"})
            for row in mutations:
                with self.subTest(package=package, command=row["command"]):
                    self.assertEqual((row["action"], row["node"], row["prompt"]),
                                     ("SKIP", "fw-1", "cli"))
                    contract = data["commands"][extract.key(row)]
                    self.assertTrue(contract["reason"])
                    contract["action"] = "RUN"
                    with self.assertRaisesRegex(ValueError, "action changed"):
                        extract.validate_inventory(rows, data, package_id=package)
                    contract["action"] = "SKIP"

    def test_backup_commands_use_cli_and_never_write_files_during_smoke(self):
        for package in ("us-dnp3-substation", "eu-iec104-substation"):
            rows = [r for r in extract.extract(package_id=package)
                    if r["command"] in ("create backup lab-2.2 redacted CONFIRM",
                                        "show backups",
                                        "download backup <id> /data/lab-2.2-policy.json")]
            self.assertEqual([(r["command"].split()[0], r["action"]) for r in rows],
                             [("create", "SKIP"), ("show", "RUN"), ("download", "SKIP")])
            self.assertTrue(all((r["node"], r["prompt"]) == ("fw-1", "cli") for r in rows))

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
                ":::hint Optional catch-up — for less technical students or teams behind schedule\nOn the RTU:\n    iec104cmd station gi\n:::\n")
        rows = description_commands(text)
        self.assertEqual([(r[0], r[2]) for r in rows], [
            ("curl localhost", "host"), ("wget reconnect", "cc-1"),
            ("iec104cmd station gi", "rtu-1")])


if __name__ == "__main__":
    unittest.main()
