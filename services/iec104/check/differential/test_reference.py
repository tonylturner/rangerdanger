# SPDX-License-Identifier: Apache-2.0
"""Check sealed metadata against the pinned first-party Docker recipe only."""

import json
from pathlib import Path
import subprocess
import unittest

HERE = Path(__file__).resolve().parent


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


if __name__ == "__main__":
    unittest.main()
