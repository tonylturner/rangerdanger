# SPDX-License-Identifier: Apache-2.0
"""Implementation selection stays independent of the legacy live gate."""

from pathlib import Path
import importlib.util
import subprocess
import tempfile
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


class ImplementationTests(unittest.TestCase):
    def test_fixture_cache_miss_pulls_and_propagates_failure(self):
        line = next(line for line in (CHECK / "run.sh").read_text().splitlines()
                    if line.startswith('docker image inspect "$PY"'))
        for cached, pull_status, expected in ((0, 9, 0), (1, 0, 0), (1, 9, 1)):
            with self.subTest(cached=cached, pull_status=pull_status):
                with tempfile.TemporaryDirectory() as directory:
                    result = subprocess.run(
                        ["bash", "-c",
                         'PY=python:3.12-alpine; OUT="$1"; TRACE="$1/trace"\n'
                         'docker() { echo "$*" >>"$TRACE"; '
                         f'if [ "$1" = image ]; then return {cached}; fi; '
                         f'return {pull_status}; }}\n' +
                         'fail() { echo "$*" >&2; }\n' + line,
                         "test", directory],
                        capture_output=True, text=True, timeout=5,
                    )
                    calls = (Path(directory) / "trace").read_text().splitlines()
                self.assertEqual(result.returncode, expected, result.stderr)
                self.assertEqual(calls, ["image inspect python:3.12-alpine"] +
                                 (["pull python:3.12-alpine"] if cached else []))

    def test_pull_is_bounded_and_shared_image_is_not_cleaned(self):
        with patch.dict(legacy.os.environ, RD_CHECK_PREFIX="rd-test", RD_CHECK_OWNER="a" * 32):
            with patch.object(legacy.sys, "argv", ["legacy_docker.py", "pull", "python:3.12-alpine"]):
                for status in (0, 7):
                    result = subprocess.CompletedProcess([], status, "", "")
                    with patch.object(legacy, "run", return_value=result) as call:
                        self.assertEqual(legacy.main(), status)
                    call.assert_called_once_with(
                        ["docker", "pull", "python:3.12-alpine"], timeout=300, sealed=False)
        with patch.object(legacy, "checked", return_value="image-id") as call:
            legacy.cleanup("rd-test", "owner=unit")
        removed = [item.args[0][-1] for item in call.call_args_list
                   if item.args[0][:2] == ["image", "rm"]]
        self.assertEqual(removed, ["rd-test-" + suffix + ":test"
                                  for suffix in ("iec104", "iec104-base", "model")])

    def build(self, impl):
        text = (CHECK / "run.sh").read_text()
        body = text.split("build_base() {", 1)[1].split("\n}", 1)[0]
        return subprocess.run(
            ["bash", "-c", 'IMPL="$1"; ROOT=root; SVC=svc; BASEIMG=base; OUT=/dev/null\n'
             'docker() { printf "%s\\n" "$*"; }\n'
             'build_base() {' + body + '\n}\nbuild_base', "test", impl],
            capture_output=True, text=True, timeout=5,
        )

    def test_c_keeps_model_check_and_original_recipe(self):
        result = self.build("c")
        self.assertEqual(result.returncode, 0)
        self.assertIn("--target model-check", result.stdout)
        self.assertIn("-f svc/Dockerfile", result.stdout)
        self.assertNotIn("go.Dockerfile", result.stdout)

    def test_go_builds_combined_without_c_model_check(self):
        result = self.build("go")
        self.assertEqual(result.returncode, 0)
        self.assertIn("-f svc/go.Dockerfile --target combined", result.stdout)
        self.assertNotIn("model-check", result.stdout)


if __name__ == "__main__":
    unittest.main()
