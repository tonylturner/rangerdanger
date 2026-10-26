"""Offline containd cutover guidance contracts."""
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class ContaindReleaseDocsTests(unittest.TestCase):
    def test_upgrade_guides_require_matching_full_storage_and_image(self):
        for path in ("RELEASING.md", "docs/quickstart.md", "docs/workshop-ssd.md"):
            text = " ".join((ROOT / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("requires containd 0.1.33 or later", "0.1.7",
                               "0.1.32 cannot read", "data/firewall/",
                               "data/eu-iec104-substation/firewall/",
                               "pre-upgrade", "stop the lab", "older tag",
                               "RangerDanger owns"):
                    self.assertIn(phrase.lower(), text.lower())

    def test_api_health_distinguishes_liveness_and_operational_diagnostics(self):
        text = (ROOT / "docs/api-spec.md").read_text()
        for phrase in ("liveness subset", "not forwarding readiness",
                       "ipv4Forwarding", "managementBinding.state",
                       "/api/v1/system/management-reservation",
                       "bootFault", "recovery", "listenerFaults",
                       "not included in `/api/firewall/health`"):
            self.assertIn(phrase, text)
        self.assertNotIn("as containd sends it", text)

    def test_pcap_docs_name_current_client_limitation_not_missing_zone_support(self):
        text = " ".join((ROOT / "docs/api-spec.md").read_text().split())
        for phrase in ("US backend submits no native interface/zone selection",
                       "EU deliberately uses `tcpdump`",
                       "not missing containd zone support", "single port/range"):
            self.assertIn(phrase, text)

    def test_event_article_does_not_attribute_retained_rows_to_current_policy(self):
        text = (ROOT / "lab-definitions/packages/us-dnp3-substation/knowledge/"
                       "live-dpi-events-strip.md").read_text()
        for phrase in ("no age expiry", "not process restarts", "timestamp, tuple and rule ID",
                       "Old matching DENYs are not proof",
                       "The L4 source pin enforces; DPI detects and logs."):
            self.assertIn(phrase, text)
