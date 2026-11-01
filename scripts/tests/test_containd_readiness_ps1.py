"""Offline Windows backend-receipt parity; backend owns readiness polling."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
PS = r'''
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-readiness.ps1"
function docker { throw 'backend receipts must not poll containd' }
function Invoke-RestMethod { throw 'backend receipts must not fetch a later identity' }
try {
    Assert-ContaindProtection ($env:RESPONSE | ConvertFrom-Json)
    'protecting'
} catch { Write-Output $_.Exception.Message; exit 1 }
'''


@unittest.skipUnless(shutil.which("pwsh"), "pwsh unavailable")
class WindowsReceiptTests(unittest.TestCase):
    def run_case(self, response):
        return subprocess.run(
            ["pwsh", "-NoProfile", "-Command", PS], text=True, capture_output=True, timeout=15,
            env={**os.environ, "REPO": str(ROOT), "RESPONSE": json.dumps(response)})

    def test_route_receipt_passes_without_reads(self):
        result = self.run_case({"containd": {"applyId": "opaque-id", "protection": "protecting"}})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_degraded_and_other_states_fail(self):
        for state in ("degraded", "failed", "disabled", "pending", "unknown", "Protecting", None):
            with self.subTest(state=state):
                result = self.run_case({"containd": {"applyId": "id", "protection": state,
                                                     "warning": "logging unavailable"}})
                self.assertEqual(result.returncode, 1)
                self.assertIn("logging unavailable", result.stdout)

    def test_missing_receipt_or_identity_is_not_inferred(self):
        for response in ({}, {"applyId": "id", "protection": "protecting"},
                         {"containd": {"protection": "protecting"}},
                         {"containd": {"applyId": "", "protection": "protecting"}},
                         {"containd": {"applyId": 1, "protection": "protecting"}},
                         {"containd": {"applyId": "id", "protection": []}},
                         {"containd": {"applyId": "id", "protection": ["protecting"]}}):
            with self.subTest(response=response):
                result = self.run_case(response)
                self.assertEqual(result.returncode, 1)
                self.assertIn("not protecting", result.stdout)


if __name__ == "__main__":
    unittest.main()
