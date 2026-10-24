# SPDX-License-Identifier: Apache-2.0
"""Offline ownership checks for the legacy gate."""

from pathlib import Path
import importlib.util
import unittest
from unittest.mock import patch

CHECK = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("legacy_docker", CHECK / "legacy_docker.py")
legacy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(legacy)


class OwnershipTests(unittest.TestCase):
    def test_anonymous_jobs_get_owned_names_and_labels(self):
        args = legacy.prepare(["run", "--rm", "image", "echo"], "rd-test", "owner=unit")
        self.assertTrue(args[args.index("--name") + 1].startswith("rd-test-job-"))
        self.assertEqual(args[args.index("--label") + 1], "owner=unit")

    def test_explicit_names_and_build_tags_cannot_escape_prefix(self):
        for args in (["run", "--name", "other", "image"],
                     ["build", "-t", "other:test", "."]):
            with self.assertRaises(ValueError):
                legacy.prepare(args, "rd-test", "owner=unit")

    def test_preflight_refuses_existing_resources(self):
        for inventory in (([{"Names": "rd-test-rtu"}], [], []),
                          ([], [{"Name": "rd-test-net"}], []),
                          ([], [], [{"Repository": "rd-test-iec104"}])):
            with patch.object(legacy, "inventory", return_value=inventory):
                with self.assertRaisesRegex(RuntimeError, "already in use"):
                    legacy.preflight("rd-test")

    def test_cleanup_discovers_only_exact_owner(self):
        with patch.object(legacy, "checked", side_effect=["c", "", "n", "", "", "", ""]) as call:
            legacy.cleanup("rd-test", "owner=unit")
        self.assertEqual(call.call_args_list[0].args[0],
                         ["ps", "-aq", "--filter", "label=owner=unit"])
        self.assertEqual(call.call_args_list[2].args[0],
                         ["network", "ls", "-q", "--filter", "label=owner=unit"])

    def test_cleanup_failure_does_not_skip_other_resource_kinds(self):
        with patch.object(legacy, "checked", side_effect=[
            RuntimeError("container cleanup failed"), "", "", "", "",
        ]) as call:
            with self.assertRaisesRegex(RuntimeError, "cleanup failed"):
                legacy.cleanup("rd-test", "owner=unit")
        self.assertEqual(call.call_count, 5)
        self.assertEqual(call.call_args_list[-1].args[0][-1], "reference=rd-test-model:test")


if __name__ == "__main__":
    unittest.main()
