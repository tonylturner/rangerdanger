"""Real lifecycle assertions and range helpers, with all HTTP calls stubbed."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

from .test_setup_ps1_root import powershell

ROOT = Path(__file__).resolve().parents[2]


def response(state):
    result = {"success": True, "package": "fixture", "phase": "ready"}
    if state != "missing":
        result["containd"] = {"applyId": "route-id", "protection": state,
                              "warning": "receipt-diagnostic"}
    return result


class BackendReceiptConsumers(unittest.TestCase):
    def bash(self, script, state):
        return subprocess.run(
            ["bash", "-c", script], text=True, capture_output=True, timeout=15,
            env={**os.environ, "REPO": str(ROOT), "RESPONSE": json.dumps(response(state))})

    def test_shell_range_ready_consumes_same_status(self):
        script = r'''
. "$REPO/scripts/lib/range.sh"
range_status() { printf '%s' "$RESPONSE"; }
range_is_ready fixture
'''
        for state in ("protecting", "degraded", "pending", "failed", "missing"):
            with self.subTest(state=state):
                result = self.bash(script, state)
                self.assertEqual(result.returncode, 0 if state == "protecting" else 1,
                                 result.stdout + result.stderr)

    def test_shell_lifecycle_apply_and_reset_require_receipts(self):
        source = (ROOT / "scripts/test-lifecycle.sh").read_text()
        function = source[source.index("assert_execute() {"):source.index("assert_teardown() {")]
        script = r'''
. "$REPO/scripts/lib/range.sh"
PASS=0 FAIL=0
phase() { :; }
ok() { PASS=$((PASS + 1)); }
no() { FAIL=$((FAIL + 1)); }
curl() { printf '%s' "$RESPONSE"; }
''' + function + '\nassert_execute\nprintf "%s %s" "$PASS" "$FAIL"\n'
        for state in ("protecting", "degraded", "pending", "failed", "missing"):
            with self.subTest(state=state):
                result = self.bash(script, state)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, "3 0" if state == "protecting" else "0 3")

    @unittest.skipUnless(shutil.which("pwsh"), "pwsh unavailable")
    def test_windows_range_ready_consumes_same_status(self):
        for state in ("protecting", "degraded", "pending", "failed", "missing"):
            with self.subTest(state=state):
                result = powershell(r'''
. "$env:REPO/scripts/lib/range.ps1"
if (Test-RangeReady ($env:RESPONSE | ConvertFrom-Json) fixture) { exit 0 } else { exit 1 }
''', RESPONSE=json.dumps(response(state)))
                self.assertEqual(result.returncode, 0 if state == "protecting" else 1,
                                 result.stdout + result.stderr)

    @unittest.skipUnless(shutil.which("pwsh"), "pwsh unavailable")
    def test_windows_lifecycle_apply_and_reset_require_receipts(self):
        for state in ("protecting", "degraded", "pending", "failed", "missing"):
            with self.subTest(state=state):
                result = powershell(r'''
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/containd-readiness.ps1"
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/scripts/test-lifecycle.ps1", [ref]$tokens, [ref]$errors)
$f = $ast.Find({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'AssertExecute'}, $true)
Invoke-Expression $f.Extent.Text
$script:passed = 0; $script:failed = 0
function Phase { }
function Ok { $script:passed++ }
function Bad { $script:failed++ }
function Invoke-RestMethod($Uri, $Method, $ContentType, $Body, $TimeoutSec, $ErrorAction) {
    if ($Method -ne 'Post' -or $TimeoutSec -ne 60) { throw 'unexpected request' }
    $env:RESPONSE | ConvertFrom-Json
}
AssertExecute
"$script:passed $script:failed"
''', RESPONSE=json.dumps(response(state)))
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(result.stdout.strip(), "3 0" if state == "protecting" else "0 3")
