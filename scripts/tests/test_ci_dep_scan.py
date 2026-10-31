"""Dependency scans must cover the complete first-party release inventory."""
import json
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


class DependencyScanTests(unittest.TestCase):
    def test_scan_matrix_matches_inventory_without_duplicates(self):
        inventory = json.loads((ROOT / ".github/release-images.json").read_text())
        scan = yaml.safe_load((ROOT / ".github/workflows/dep-scan.yml").read_text())
        images = scan["jobs"]["scan"]["strategy"]["matrix"]["image"]
        self.assertEqual(len(images), len(set(images)))
        self.assertEqual(set(images), {image["image"] for image in inventory["images"]})
