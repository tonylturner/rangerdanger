"""Offline Windows readiness parity, including terminal and superseded applies."""
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
$script:rows = @($env:OBSERVATIONS | ConvertFrom-Json)
$script:reads = 0
function docker {
    $row = $script:rows[[Math]::Min($script:reads, $script:rows.Count-1)]
    $script:reads++
    $global:LASTEXITCODE = 0
    $row | ConvertTo-Json -Depth 20 -Compress
    '200'
}
try {
    Wait-ContaindProtection ($env:RECEIPT | ConvertFrom-Json) ([double]$env:BUDGET)
    Write-Output "ready reads=$script:reads"
} catch { Write-Output $_; exit 1 }
'''


def row(state, apply="ours", installed="ours"):
    return {"state": state, "applyId": apply, "installedApplyId": installed,
            "components": [{"reason": "nflog diagnostic"}]}


@unittest.skipUnless(shutil.which("pwsh"), "pwsh unavailable")
class WindowsReadinessTests(unittest.TestCase):
    def run_case(self, rows, receipt=None, budget=3):
        return subprocess.run(
            ["pwsh", "-NoProfile", "-Command", PS], text=True, capture_output=True, timeout=15,
            env={**os.environ, "REPO": str(ROOT), "RECEIPT": json.dumps(receipt or {"applyId": "ours"}),
                 "OBSERVATIONS": json.dumps(rows), "BUDGET": str(budget)})

    def test_ready_receipt_needs_no_get(self):
        result = self.run_case([], {"applyId": "ours", "protection": row("protecting")})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("reads=0", result.stdout)

    def test_pending_then_protecting(self):
        result = self.run_case([row("pending", installed="old"),
                                row("pending", installed="old"), row("protecting")])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("reads=3", result.stdout)

    def test_terminal_states_fail_with_component_reasons(self):
        for state in ("degraded", "failed", "disabled"):
            with self.subTest(state=state):
                result = self.run_case([row(state)])
                self.assertEqual(result.returncode, 1)
                self.assertIn("nflog diagnostic", result.stdout)

    def test_superseded(self):
        result = self.run_case([row("pending", installed="old"), row("protecting", "new", "new")])
        self.assertEqual(result.returncode, 1)
        self.assertIn("superseded", result.stdout)

    def test_timeout_last_observation(self):
        result = self.run_case([row("protecting", installed="wrong")],
                               {"applyId": "ours", "protection": row("pending", installed="wrong")}, budget=0.1)
        self.assertEqual(result.returncode, 1)
        self.assertIn("timeout", result.stdout)
        self.assertIn("wrong", result.stdout)

    def test_missing_identity(self):
        result = self.run_case([], {"status": "ok"})
        self.assertEqual(result.returncode, 1)
        self.assertIn("no applyId", result.stdout)


if __name__ == "__main__":
    unittest.main()
