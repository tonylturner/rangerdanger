"""Installer SRA branches; never run setup, Docker or a live gate."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SRAInstallerTests(unittest.TestCase):
    def test_shell_keeps_policy_and_reset_gate_for_sra(self):
        source = (ROOT / "setup.sh").read_text()
        start = source.index('    for cfg in weak improved; do')
        end = source.index('# ─── OpenPLC readiness', start)
        gate = source[start:end]
        self.assertIn('--max-time 60 -X POST http://localhost:8088/api/workshop/reset', gate)
        self.assertNotIn('reset_timeout', gate)
        self.assertNotIn('us-sra-substation', gate)
        self.assertIn('range_has_protection "$reset_resp"', gate)
        self.assertIn('"success":true}', gate)
        self.assertNotIn('SKIP', gate)

    def test_shell_openplc_is_detected_from_release_compose(self):
        source = (ROOT / "setup.sh").read_text()
        start = source.index('openplc_compose="$ROOT_DIR/lab-definitions/packages/$RANGE_PACKAGE/compose.release.yml"')
        end = source.index('# ─── done ───', start)
        diagnostic = source[start:end]
        self.assertIn('grep -qE', diagnostic)
        self.assertIn('container_name:[[:space:]]*rangerdanger-openplc', diagnostic)
        self.assertIn('rangerdanger-openplc', diagnostic)
        package_files = sorted((ROOT / "lab-definitions/packages").glob("*/compose.release.yml"))
        self.assertTrue(package_files)
        self.assertTrue(all(path.is_file() for path in package_files))
        compose_by_package = {
            path.parent.name: path.read_text() for path in package_files
        }
        self.assertNotIn("container_name: rangerdanger-openplc", compose_by_package["us-sra-substation"])
        self.assertTrue(
            any("container_name: rangerdanger-openplc" not in content for content in compose_by_package.values()),
            "a package without OpenPLC must skip the diagnostic",
        )
        after = source[end:]
        self.assertNotIn('docker_bounded 5 inspect', after)

    def test_powershell_has_same_gate_and_data_driven_diagnostic(self):
        source = (ROOT / "setup.ps1").read_text()
        start = source.index('foreach ($cfg in @("weak", "improved"))')
        end = source.index('# Like setup.sh this is diagnostic', start)
        gate = source[start:end]
        self.assertIn('-TimeoutSec 60', gate)
        self.assertNotIn('180', gate)
        self.assertNotIn('us-sra-substation', gate)
        self.assertIn('Assert-ContaindProtection $resetResult', gate)
        self.assertIn('$resetResult.success -eq $true', gate)
        start = source.index('$openPlcCompose = Join-Path $RootDir')
        end = source.index("Banner 'RangerDanger is up'", start)
        diagnostic = source[start:end]
        self.assertIn('compose.release.yml', diagnostic)
        self.assertIn('Select-String', diagnostic)
        self.assertIn('container_name:\\s*rangerdanger-openplc', diagnostic)
        self.assertNotIn('us-dnp3-substation', diagnostic)
        self.assertNotIn('eu-iec104-substation', diagnostic)
