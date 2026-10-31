# SPDX-License-Identifier: Apache-2.0
"""Check sealed metadata and materialize the shared fixture before role maps."""

import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("reference_build", HERE.parent / "reference/build.py")
builder = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(builder)


class ReferenceTests(unittest.TestCase):
    def test_manifest_matches_pinned_recipe_without_opening_dependencies(self):
        metadata = json.loads((HERE.parent / "reference/baseline.json").read_text())
        recipe = subprocess.run(
            ["git", "-C", str(HERE.parents[3]), "show",
             metadata["commit"] + ":services/iec104/Dockerfile"],
            capture_output=True, text=True, check=True, timeout=10,
        ).stdout
        for key, arg in (
            ("lib60870_version", "LIB60870_VERSION"),
            ("lib60870_sha256", "LIB60870_SHA256"),
            ("cjson_version", "CJSON_VERSION"),
            ("cjson_sha256", "CJSON_SHA256"),
        ):
            self.assertIn("ARG " + arg + "=" + metadata[key] + "\n", recipe)
        self.assertIn('CFLAGS="' + metadata["cflags"] + '"', recipe)
        self.assertIn('LIBS="' + metadata["libs"] + '"', recipe)
        # The portable input is not a claim that any platform-specific image
        # was built. build.py materializes that ID alongside the evidence.
        self.assertIsNone(metadata["image_id"])


class PeerImageTests(unittest.TestCase):
    def test_cached_peer_needs_no_pull(self):
        with patch.object(builder.subprocess, "run", return_value=
                          subprocess.CompletedProcess([], 0)) as call:
            self.assertEqual(builder.ensure_peer(), builder.PEER_IMAGE)
        self.assertEqual(call.call_count, 1)
        self.assertEqual(call.call_args.args[0],
                         ["docker", "image", "inspect", builder.PEER_IMAGE])
        self.assertEqual(call.call_args.kwargs["timeout"], 15)

    def test_cache_miss_pulls_only_the_pinned_peer_with_a_deadline(self):
        with patch.object(builder.subprocess, "run", side_effect=[
                subprocess.CompletedProcess([], 1),
                subprocess.CompletedProcess([], 0)]) as call:
            self.assertEqual(builder.ensure_peer(), builder.PEER_IMAGE)
        self.assertEqual(call.call_count, 2)
        self.assertEqual(call.call_args.args[0], ["docker", "pull", builder.PEER_IMAGE])
        self.assertEqual(call.call_args.kwargs["timeout"], 300)
        self.assertTrue(call.call_args.kwargs["check"])
        self.assertRegex(builder.PEER_IMAGE, r"@sha256:[0-9a-f]{64}$")

    def test_pull_failure_or_timeout_does_not_build_or_publish_roles(self):
        for failure in (subprocess.CalledProcessError(9, ["docker", "pull"]),
                        subprocess.TimeoutExpired(["docker", "pull"], 300)):
            with self.subTest(failure=type(failure).__name__), \
                    tempfile.TemporaryDirectory() as directory:
                out = Path(directory) / "evidence"
                with patch.object(builder.subprocess, "run", side_effect=[
                        subprocess.CompletedProcess([], 1), failure]), \
                        patch.object(builder, "run_owned") as build:
                    with self.assertRaises(type(failure)):
                        builder.materialize({"commit": "sealed"}, "owned:tag", out)
                build.assert_not_called()
                self.assertFalse(out.exists())


if __name__ == "__main__":
    unittest.main()
