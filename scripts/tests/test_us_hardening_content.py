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

    def test_outage_current_is_near_zero_not_exactly_zero(self):
        lab = scenario("hardening-configurations")
        step = lab["steps"][1]
        text = " ".join(step["description"].split())
        self.assertIn("`[2]` (`downstream_voltage_v × 10`) reads `0`", text)
        self.assertIn(
            "`[4]` (`feeder_current_a × 10`) drops to about zero", text
        )
        self.assertIn("`1` means 0.1 A", text)
        self.assertNotIn("both read `0`", text)
        self.assertIn("downstream of the recloser are unenergized", text)


if __name__ == "__main__":
    unittest.main()
