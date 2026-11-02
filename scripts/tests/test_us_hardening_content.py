"""Physical-outcome wording for the US protocol-hardening exercise."""

import unittest

from scripts.tests.test_lab_content_contracts import scenario


class USHardeningContentContracts(unittest.TestCase):
    def test_headers_distinguish_remote_trip_from_fault_recovery(self):
        lab = scenario("hardening-configurations")
        for field in ("summary", "description"):
            text = " ".join(lab[field].lower().split())
            with self.subTest(field=field):
                self.assertNotIn("flicker", text)
                self.assertIn("remote trip stays open until someone closes it", text)
                self.assertIn(
                    "disabling auto-reclose removes automatic restoration after a fault",
                    text,
                )


if __name__ == "__main__":
    unittest.main()
