"""Post-merge contract: requires the SRA package from the package lane."""
import unittest
from scripts.lib import simulator_api_isolation as g3


class SRAShippedIsolationTests(unittest.TestCase):
    def test_shipped_sra_catalogue(self):
        cat = g3.load_inventory("us-sra-substation")
        self.assertEqual(cat["sources"], ["kali", "vendor_jump"])
        self.assertEqual(len(cat["destinations"]), 37)
