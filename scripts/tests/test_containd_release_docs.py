"""Offline containd cutover guidance contracts."""
from pathlib import Path
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]


class ContaindReleaseDocsTests(unittest.TestCase):
    def test_current_install_and_staging_commands_select_v026(self):
        for path in ("README.md", "docs/quickstart.md", "docs/workshop-ssd.md"):
            text = (ROOT / path).read_text()
            with self.subTest(path=path):
                self.assertIn("v0.2.6", text)
                self.assertIn("./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.6", text)
                self.assertNotIn("./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.5", text)
                self.assertNotRegex(text, r"(?:--version|-Version) v0\.2\.0\b")
                self.assertNotIn("./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.0", text)
        for path in ("README.md", "docs/quickstart.md"):
            text = (ROOT / path).read_text()
            with self.subTest(path=path):
                self.assertIn("./setup.sh --version v0.2.6", text)
                self.assertIn(r".\setup.ps1 -Version v0.2.6", text)
                self.assertNotIn("./setup.sh --version v0.2.5", text)
                self.assertNotIn(r".\setup.ps1 -Version v0.2.5", text)
        for path in ("README.md", "docs/v0.1.36-hotfix.md"):
            self.assertIn("quickstart.md#upgrading-to-v026", (ROOT / path).read_text())

    def test_v026_upgrade_from_v025_has_no_migration_but_older_backup_still_applies(self):
        text = " ".join((ROOT / "docs/quickstart.md").read_text().split())
        current, history = text.split("## Upgrading to v0.2.0", 1)
        for phrase in ("## Upgrading to v0.2.6", "v0.2.5 → v0.2.6",
                       "does not require a firewall-data migration",
                       "same no-migration guidance applies to v0.2.4, v0.2.3, v0.2.2 and v0.2.1",
                       "git fetch origin tag v0.2.6", "git checkout --detach v0.2.6",
                       "A direct upgrade from v0.2.0 or older data not yet migrated",
                       "requires containd 0.1.41 or later", "containd:latest",
                       "tested on 0.1.42",
                       "**Back up when upgrading from v0.2.0.**",
                       "complete persistent `/data`", "migrates firewall `config.db`",
                       "full pre-upgrade firewall data backup",
                       "main/docs/mkdocs/update-policy.md",
                       "main/docs/mkdocs/config-format.md",
                       "./scripts/dev-down.sh", r".\scripts\dev-down.ps1",
                       "Changed first-party images are rebuilt for v0.2.6",
                       "images with unchanged inputs can be promoted from the previous release",
                       "This is not a full image refresh",
                       "v0.2.5 → v0.2.6 SSD delta carries changed images plus the repository archive",
                       "compares registry manifest digests, not just changed repository files",
                       "check its summary for the actual image set and size"):
            self.assertIn(phrase, current)
        self.assertNotIn("[rebuild-all]", current)
        self.assertNotIn("All 20 first-party images are rebuilt", current)
        self.assertNotIn("carries the full first-party image set", current)
        for phrase in ("v0.1.x", "rebuilt all 20 first-party images",
                       "minimum was 0.1.37", "one-way to 0.1.10",
                       "matching complete pre-upgrade storage"):
            self.assertIn(phrase, history)
        self.assertNotIn("once v0.2.0 is published", text)
        self.assertNotIn("git checkout --detach v0.2.0", text)
        self.assertNotIn("the tag carries `[rebuild-all]`", text)

    def test_v026_upgrade_keeps_matching_browser_progress(self):
        text = " ".join((ROOT / "docs/quickstart.md").read_text().split())
        patch, _ = text.split("**Upgrading from v0.2.3 or older:**", 1)
        for phrase in ("US curriculum revision 4 and EU revision 8 are unchanged from v0.2.5",
                       "same browser and portal address",
                       "v0.2.6 reuses saved browser records",
                       "lab progress, notes, decisions and plans",
                       "Progress is stored by package, revision and exercise ID",
                       "Completed/skipped marks are kept only for step IDs still present",
                       "new steps start unmarked",
                       "no release-version or content-fingerprint reset",
                       "changed wording does not clear completion marks",
                       "Saved decisions are shown only when their values remain valid options",
                       "not fresh evidence that the reset range passes its checks"):
            self.assertIn(phrase, patch)
        self.assertNotIn("fresh browser state", patch)
        self.assertNotIn("write a new Lab 1.4 change request", patch)
        self.assertNotIn("API callers must also update", patch)
        readme = " ".join((ROOT / "README.md").read_text().split())
        for phrase in ("US revision 4 and EU revision 8 are unchanged from v0.2.5",
                       "v0.2.6 reuses saved browser records",
                       "same browser and portal address",
                       "Completion marks follow matching step IDs, not changed wording"):
            self.assertIn(phrase, readme)
        self.assertNotIn("start fresh browser progress", readme)

    def test_v024_upgrade_warns_about_revision_scoped_state_and_api_cutover(self):
        text = " ".join((ROOT / "docs/quickstart.md").read_text().split())
        _, older = text.split("**Upgrading from v0.2.3 or older:**", 1)
        for phrase in ("v0.2.4 introduced US revision 4 and EU revision 8",
                       "fresh browser state",
                       "remain in browser storage but are not loaded or migrated",
                       "write a new Lab 1.4 change request",
                       "API callers must also update for the v0.2.4 cutover",
                       "`GET /api/scenarios/:id/validate` to `POST /api/scenarios/:id/validate`",
                       "optional `changeRequest`",
                       "`POST /api/firewall/validation-report` endpoint is removed"):
            self.assertIn(phrase, older)

    def test_v026_delta_examples_describe_changed_images_and_optional_containd(self):
        text = (ROOT / "docs/workshop-ssd.md").read_text()
        with_containd = (
            "./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.6 v0.2.5 v0.2.6 \\\n"
            "  --include-upstream --include containd"
        )
        self.assertEqual(text.count(with_containd), 1)
        self.assertEqual(text.count(
            "./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.6 v0.2.5 v0.2.6"
        ), 2)
        self.assertIn("v0.2.0 → v0.2.6, a fresh full\nstorage backup is still required", text)
        for phrase in ("v0.2.6 rebuilds changed first-party images",
                       "promoted from the previous release",
                       "This is not a full image refresh",
                       "compares registry manifest digests",
                       "v0.2.5 → v0.2.6 delta carries changed images plus the repository archive",
                       "not automatically the full first-party image set",
                       "for the actual image set and size before distributing it",
                       "Unchanged upstream images are not automatically included"):
            self.assertIn(phrase, text)
        self.assertNotIn("[rebuild-all]", text)
        self.assertNotIn("rebuilds all 20 first-party images", text)
        self.assertNotIn("carries the full\nfirst-party image set", text)
        self.assertNotIn("This is a future-version example", text)

    def test_local_cli_guidance_uses_exit_and_separates_candidate_status(self):
        for path in ("docs/quickstart.md", "docs/api-spec.md"):
            text = " ".join((ROOT / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("configuration CLI", "`exit`", "`containd cli`",
                               "`config`", "`config*`", "`config?`",
                               "Linux commands act immediately"):
                    self.assertIn(phrase, text)
                self.assertNotRegex(text, r"containd#|Type `shell`|type `shell`")
                self.assertIn("prompt reads `containd(config)#`", text)
        text = (ROOT / "docs/v0.1.36-hotfix.md").read_text()
        self.assertIn("Current containd 0.1.41:", text)
        self.assertIn("rule edits stage the candidate", text)

    def test_lab_guidance_separates_running_proof_and_validation_surfaces(self):
        us = ROOT / "lab-definitions/scenarios"
        access = " ".join((us / "firewall-implementation.yml").read_text().split())
        self.assertIn("prompt reads `containd(config)#`", access)
        for phrase in ("`configure` is also a supported CLI entry point",
                       "`wizard` belongs to SSH-appliance guided setup",
                       "both reference policies already use DENY",
                       "**Review changes**", "**Advanced**"):
            self.assertIn(phrase, access)
        proof = " ".join((us / "validation-evidence.yml").read_text().split())
        self.assertIn("Configuration → Running config shows committed policy", proof)
        self.assertIn("Default row describe the candidate", proof)
        eu = ROOT / "lab-definitions/packages/eu-iec104-substation"
        text = (eu / "scenarios/eu-firewall-implementation.yml").read_text()
        self.assertIn("It does not test running traffic", text)
        self.assertIn("JSON **Validate** only checks editor text without staging", text)
        for relative in ("knowledge/eu-ics-dpi.md", "scenarios/eu-validation-evidence.yml"):
            text = (eu / relative).read_text()
            with self.subTest(path=relative):
                for phrase in ("Collect → Generate → Review → Stage",
                               "**Inspect suggestion**", "**Checks on saved evidence**",
                               "**Discard receipt** does not undo staged rules",
                               "verify the candidate diff is actually empty"):
                    self.assertIn(phrase, text)

    def test_overviews_state_floor_and_one_way_storage_migration(self):
        for path in ("README.md", "docs/architecture.md"):
            text = " ".join((ROOT / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("requires containd 0.1.41 or later", "stop the lab",
                               "back up", "one-way", "0.1.10",
                               "containd 0.1.36 cannot read"):
                    self.assertIn(phrase, text)
        text = (ROOT / "docs/architecture.md").read_text()
        self.assertIn("`CONTAIND_IMAGE`", text)

    def test_source_and_release_use_published_containd_without_builds(self):
        image = "ghcr.io/tonylturner/containd:latest"
        for package in ("us-dnp3-substation", "eu-iec104-substation"):
            directory = ROOT / "lab-definitions/packages" / package
            for mode in ("source", "release"):
                with self.subTest(package=package, mode=mode):
                    model = yaml.safe_load((directory / f"compose.{mode}.yml").read_text())
                    firewall = model["services"]["firewall"]
                    self.assertNotIn("build", firewall)
                    self.assertEqual(firewall["image"],
                                     "${CONTAIND_IMAGE:-" + image + "}"
                                     if mode == "source" else image)
        text = " ".join((ROOT / "RELEASING.md").read_text().split())
        self.assertIn("Source mode also pulls the published", text)
        self.assertIn("Record digests as evidence, not Compose pins", text)

    def test_source_acquisition_refreshes_published_images_and_ci_accepts_image(self):
        source = (ROOT / "setup.sh").read_text()
        start = source.index('    source)\n        banner "Building images from source"')
        end = source.index("esac", start)
        self.assertIn('with_pull_retry range_compose "$id" pull --ignore-buildable',
                      source[start:end])
        powershell = (ROOT / "scripts/lib/setup-package.ps1").read_text()
        self.assertIn("Invoke-SetupPull $model @('--ignore-buildable')", powershell)
        workflow = yaml.safe_load((ROOT / ".github/workflows/smoke.yml").read_text())
        # PyYAML's YAML 1.1 loader reads the GitHub Actions 'on' key as True.
        inputs = workflow[True]["workflow_dispatch"]["inputs"]
        self.assertEqual(set(inputs), {"containd_image"})
        self.assertFalse(inputs["containd_image"]["required"])
        self.assertEqual(workflow["env"]["CONTAIND_IMAGE"], "${{ inputs.containd_image }}")

    def test_floor_and_new_store_migration_are_documented(self):
        self.assertIn('const MinimumVersion = "0.1.41"',
                      (ROOT / "backend/internal/containd/version.go").read_text())
        for path in ("README.md", "RELEASING.md", "docs/quickstart.md",
                     "docs/architecture.md", "docs/workshop-ssd.md", "docs/api-spec.md"):
            with self.subTest(path=path):
                text = " ".join((ROOT / path).read_text().split())
                for phrase in ("0.1.41", "0.1.40 cannot read", "one-way", "pre-upgrade"):
                    self.assertIn(phrase, text)

    def test_upgrade_guides_require_matching_full_storage_and_image(self):
        for path in ("RELEASING.md", "docs/quickstart.md", "docs/workshop-ssd.md"):
            text = " ".join((ROOT / path).read_text().split())
            with self.subTest(path=path):
                for phrase in ("data/firewall/",
                               "data/eu-iec104-substation/firewall/",
                               "pre-upgrade", "stop the lab", "older tag",
                               "RangerDanger owns"):
                    self.assertIn(phrase.lower(), text.lower())
                self.assertRegex(text, r"requires containd 0\.1\.41 or later")
                self.assertRegex(text, r"schema.{0,60}0\.1\.10")
                self.assertRegex(text, r"0\.1\.36 and earlier cannot read")
        workshop = " ".join((ROOT / "docs/workshop-ssd.md").read_text().split()).lower()
        for phrase in ("requires containd 0.1.41 or later", "schema migrates one-way to 0.1.10",
                       "0.1.36 and earlier cannot read", "--include-upstream --include containd",
                       "record the saved image identity", "matching pre-upgrade data",
                       "for arch in amd64 arm64", "docker load -i",
                       "delta-${arch}.tar", "id={{.id}} os={{.os}} arch={{.architecture}}",
                       "repo_digests={{json .repodigests}}"):
            self.assertIn(phrase, workshop)

    def test_api_health_distinguishes_liveness_and_operational_diagnostics(self):
        text = (ROOT / "docs/api-spec.md").read_text()
        self.assertIn("containd 0.1.41 minimum", text)
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
                               "The expanded drawer shows **Event history is truncated; this is not a complete record.** when `historyTruncated` is true",
                               "**DPI engine error:** with the error details when `engineError` is present",
                               "preserve the response when completeness matters"):
                    self.assertIn(phrase, text)
                self.assertNotIn("does not display that metadata", text)
                self.assertNotIn("without an on-screen warning", text)
        text = (ROOT / "lab-definitions/packages/us-dnp3-substation/knowledge/"
                       "live-dpi-events-strip.md").read_text()
        for phrase in ("timestamp, tuple and rule ID",
                       "Old matching DENYs are not proof",
                       "The L4 source pin enforces; DPI detects and logs."):
            self.assertIn(phrase, text)

    def test_apply_readiness_uses_committed_revision(self):
        text = " ".join((ROOT / "docs/api-spec.md").read_text().split())
        for phrase in ("ready == true", "protectingRevision == commit.runningRevision",
                       "desiredRevision", "required subsystem", "config`/`render",
                       "HTTP `502`", "HTTP `503`", "`degraded` is never success",
                       "No failure causes a recommit", "informational `applyId`"):
            self.assertIn(phrase, text)
        for obsolete in ("installedApplyId", "applied-with-warning", "backend apply succeeded"):
            self.assertNotIn(obsolete, text)

    def test_policy_docs_require_candidate_commit_without_nic_copy(self):
        text = " ".join((ROOT / "docs/api-spec.md").read_text().split())
        for phrase in ("POST /api/v1/config/candidate", "POST /api/v1/config/commit",
                       "both must succeed", "no legacy `/config/import` fallback",
                       "not copy interface `device` fields", "running configuration",
                       "seed readiness", "adoption after a backend restart",
                       "0.1.41 prerelease"):
            self.assertIn(phrase, text)
