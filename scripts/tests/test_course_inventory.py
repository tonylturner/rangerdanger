"""Generic course command routing does not require a planning feature."""
from pathlib import Path
import shutil
import tempfile
import unittest

from scripts.lib.lab_commands_inventory import inventory, extract

ROOT = Path(__file__).resolve().parents[2]


class CourseInventoryTests(unittest.TestCase):
    def test_featureless_package_inventory(self):
        fixture = ROOT / "backend/internal/labs/testdata/course-basic"
        with tempfile.TemporaryDirectory(prefix="rd-course-") as temporary:
            root = Path(temporary)
            shutil.copytree(fixture, root / "lab-definitions")
            rows = extract(root, package_id="basic")
            self.assertEqual([row["command"] for row in rows], ["ls /sys/class/net"])
            self.assertEqual(rows[0]["node"], "fw-1")
            self.assertEqual(inventory(rows, root, "basic"), {
                "second": {"RUN": 0, "SKIP": 0},
                "first": {"RUN": 1, "SKIP": 0},
            })
