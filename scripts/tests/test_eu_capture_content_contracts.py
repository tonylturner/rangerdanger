"""Offline contracts for the EU capture recipes and browser handoff."""

import unittest
from pathlib import Path

import yaml


PACKAGE = Path(__file__).resolve().parents[2] / "lab-definitions/packages/eu-iec104-substation"


class EUCaptureContracts(unittest.TestCase):
    def test_optional_pcap_recipe_names_ethernet_and_browser_transfer(self):
        for rel in ("scenarios/eu-validation-evidence.yml", "knowledge/eu-ics-dpi.md"):
            with self.subTest(path=rel):
                text = (PACKAGE / rel).read_text()
                for phrase in (
                    "Ethernet classic PCAP", "tcpdump -i lan4 -nn -s 0 -U",
                    "host 10.60.60.20 and host 10.30.30.20 and tcp port 2404",
                    "http://10.99.99.21:8080/api/reconnect", "Ctrl-C",
                    "link type 276", "Upload & Analyze PCAP",
                    "<checkout>/data/eu-iec104-substation/firewall/captures/approved-command.pcap",
                    "file chooser", "out-of-band", "no demonstrated download control",
                ):
                    self.assertIn(phrase, text)

    def test_baseline_rotation_uses_float_timer_not_reconnect_timer(self):
        doc = yaml.safe_load((PACKAGE / "scenarios/eu-baseline-assessment.yml").read_text())
        text = next(s["description"] for s in doc["steps"]
                    if s["id"] == "capture-traffic-at-the-firewall")
        for phrase in ("first packet after 12 seconds", "float reports every 10 seconds",
                       "about 22 seconds", "PLC parameter reports are every 60 seconds",
                       "rotation can wait beyond the budget"):
            self.assertIn(phrase, text)
        self.assertNotIn("cyclic reports arrive every two seconds", text)
