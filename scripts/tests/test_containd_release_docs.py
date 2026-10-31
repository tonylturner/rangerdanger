"""Offline containd cutover guidance contracts."""
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]


class ContaindReleaseDocsTests(unittest.TestCase):
    def test_upgrade_guides_require_matching_full_storage_and_image(self):
        for path in ("RELEASING.md", "docs/quickstart.md", "docs/workshop-ssd.md"):
            text = " ".join((ROOT / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("data/firewall/",
                               "data/eu-iec104-substation/firewall/",
                               "pre-upgrade", "stop the lab", "older tag",
                               "RangerDanger owns"):
                    self.assertIn(phrase.lower(), text.lower())
                self.assertRegex(text, r"requires containd 0\.1\.36 or later")
                self.assertRegex(text, r"schema.{0,60}0\.1\.9")
                self.assertRegex(text, r"0\.1\.35 and earlier cannot read")
        workshop = " ".join((ROOT / "docs/workshop-ssd.md").read_text().split()).lower()
        for phrase in ("requires containd 0.1.36 or later", "schema migrates one-way to 0.1.9",
                       "0.1.35 and earlier cannot read", "--include-upstream --include containd",
                       "record the saved image identity", "matching pre-upgrade data",
                       "for arch in amd64 arm64", "docker load -i",
                       "delta-${arch}.tar", "id={{.id}} os={{.os}} arch={{.architecture}}",
                       "repo_digests={{json .repodigests}}"):
            self.assertIn(phrase, workshop)

    def test_api_health_distinguishes_liveness_and_operational_diagnostics(self):
        text = (ROOT / "docs/api-spec.md").read_text()
        self.assertIn("containd 0.1.36 minimum", text)
        for phrase in ("liveness and diagnostics", "not forwarding readiness",
                       "ipv4Forwarding", "managementBinding.state",
                       "/api/v1/system/management-reservation",
                       "bootFault", "recovery", "listenerFaults",
                       "managementReservation", "managementReservationError"):
            self.assertIn(phrase, text)
        self.assertNotIn("not included in `/api/firewall/health`", text)
        self.assertNotIn("as containd sends it", text)

    def test_pcap_docs_describe_native_us_and_package_specific_eu_capture(self):
        text = " ".join((ROOT / "docs/api-spec.md").read_text().split())
        for phrase in ("US uses native containd capture", "authored topology",
                       "US capture startup never falls back to tcpdump",
                       "EU deliberately uses `tcpdump`",
                       "single port/range", "four-port OR",
                       "`422`", "`502`", "`503`"):
            self.assertIn(phrase, text)
        self.assertNotIn("US backend submits no native interface/zone selection", text)
        self.assertNotIn("falls back to `tcpdump -i any`", text)

    def test_event_articles_distinguish_retention_from_the_live_window(self):
        for path in (
            "us-dnp3-substation/knowledge/live-dpi-events-strip.md",
            "eu-iec104-substation/knowledge/eu-live-firewall-events.md",
        ):
            text = " ".join((ROOT / "lab-definitions/packages" / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("five minutes ago", "generation's start",
                               "last policy change", "5,000", "50 rows",
                               "no age expiry", "not process restarts",
                               "`since`", "`historyTruncated`", "`engineError`",
                               "does not display that metadata"):
                    self.assertIn(phrase, text)
        text = (ROOT / "lab-definitions/packages/us-dnp3-substation/knowledge/"
                       "live-dpi-events-strip.md").read_text()
        for phrase in ("timestamp, tuple and rule ID",
                       "Old matching DENYs are not proof",
                       "The L4 source pin enforces; DPI detects and logs."):
            self.assertIn(phrase, text)

    def test_policy_docs_require_candidate_commit_and_preserve_nic_bindings(self):
        text = " ".join((ROOT / "docs/api-spec.md").read_text().split())
        for phrase in ("POST /api/v1/config/candidate", "POST /api/v1/config/commit",
                       "both must succeed", "no legacy `/config/import` fallback",
                       "NIC `device` bindings", "running configuration",
                       "seed readiness", "adoption after a backend restart",
                       "0.1.36 prerelease"):
            self.assertIn(phrase, text)
