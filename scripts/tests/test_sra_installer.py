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
        self.assertIn('reset_timeout=60', gate)
        self.assertIn('[ "$RANGE_PACKAGE" != "us-sra-substation" ] || reset_timeout=180', gate)
        self.assertIn('--max-time "$reset_timeout" -X POST http://localhost:8088/api/workshop/reset', gate)
        self.assertIn('range_has_protection "$reset_resp"', gate)
        self.assertIn('"success":true}', gate)
        self.assertNotIn('SKIP', gate)

    def test_shell_openplc_is_limited_to_packages_that_keep_it(self):
        source = (ROOT / "setup.sh").read_text()
        start = source.index('if [ "$RANGE_PACKAGE" = "us-dnp3-substation" ] ||')
        end = source.index('fi # packages with OpenPLC', start)
        diagnostic = source[start:end]
        self.assertIn('[ "$RANGE_PACKAGE" = "eu-iec104-substation" ]; then', diagnostic)
        self.assertIn('rangerdanger-openplc', diagnostic)
        self.assertNotIn('us-sra-substation', diagnostic)
        after = source[end:]
        self.assertNotIn('docker_bounded 5 inspect', after)

    def test_powershell_has_same_gate_and_diagnostic_scope(self):
        source = (ROOT / "setup.ps1").read_text()
        start = source.index('foreach ($cfg in @("weak", "improved"))')
        end = source.index('# OpenPLC is used', start)
        gate = source[start:end]
        self.assertIn("$resetTimeout = if ($RangePackage -eq 'us-sra-substation') { 180 } else { 60 }", gate)
        self.assertIn('-TimeoutSec $resetTimeout', gate)
        self.assertIn('Assert-ContaindProtection $resetResult', gate)
        self.assertIn('$resetResult.success -eq $true', gate)
        start = source.index("if ($RangePackage -in @('us-dnp3-substation', 'eu-iec104-substation'))")
        end = source.index('} # packages with OpenPLC', start)
        self.assertIn("'rangerdanger-openplc'", source[start:end])
