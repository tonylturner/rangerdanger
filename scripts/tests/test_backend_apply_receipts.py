"""Real lifecycle assertions and range helpers, with all HTTP calls stubbed."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
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
    def bash(self, script, state=None, *, body=None):
        return subprocess.run(
            ["bash", "-c", script], text=True, capture_output=True, timeout=15,
            env={**os.environ, "REPO": str(ROOT), "RESPONSE": body if body is not None
                 else json.dumps(response(state), separators=(",", ":"))})

    def test_shell_and_python_receipt_verdicts_agree(self):
        fixtures = [
            ("protecting", response("protecting"), True),
            ("degraded", response("degraded"), False),
            ("missing", response("missing"), False),
            ("empty-applyId", {"containd": {"applyId": "", "protection": "protecting"}}, False),
            ("native-response", {"status": "ok", "applyId": "route-id", "protection": {
                "applyId": "route-id", "installedApplyId": "route-id", "state": "protecting",
            }}, False),
            ("nested-native-response", {"containd": {"applyId": "route-id", "protection": {
                "applyId": "route-id", "installedApplyId": "route-id", "state": "protecting",
            }}}, False),
            ("unrelated-protection", {"applyId": "route-id", "protection": "protecting"}, False),
            ("protecting-prefix", response("protecting-later"), False),
            ("pending", response("pending"), False),
            ("failed", response("failed"), False),
            ("disabled", response("disabled"), False),
            ("null-applyId", {"containd": {"applyId": None, "protection": "protecting"}}, False),
            ("numeric-applyId", {"containd": {"applyId": 123, "protection": "protecting"}}, False),
        ]
        for apply_id in ('quote"id', "back\\slash", "line\nbreak", "unicode-\u2603"):
            fixtures.append((apply_id, {"containd": {
                "applyId": apply_id, "protection": "protecting",
            }}, True))
        for name, fixture, expected in fixtures:
            with self.subTest(name=name):
                body = json.dumps(fixture, separators=(",", ":"))
                shell = self.bash(
                    '. "$REPO/scripts/lib/range.sh"\nrange_has_protection "$RESPONSE"', body=body)
                python = subprocess.run(
                    [sys.executable, str(ROOT / "scripts/lib/containd_readiness.py"),
                     "--backend-receipt"],
                    input=body, text=True, capture_output=True, timeout=15)
                verdict = 0 if expected else 1
                self.assertEqual(shell.returncode, verdict, shell.stdout + shell.stderr)
                self.assertEqual(python.returncode, verdict, python.stdout + python.stderr)

    def test_setup_receipt_paths_without_python_on_path(self):
        source = (ROOT / "setup.sh").read_text()
        start = source.index("RANGE_BODY='{}'")
        end = source.index("# ─── OpenPLC readiness", start)
        script = r'''
set -euo pipefail
if command -v python3 >/dev/null 2>&1; then exit 99; fi
. "$REPO/scripts/lib/range.sh"
say() { :; }
warn() { printf '%s\n' "$*" >&2; }
die() { printf '%s\n' "$*" >&2; exit 1; }
sleep() { SECONDS=$((SECONDS + 600)); }
API=http://fixture PACKAGE=fixture SKIP_FW_GATE=0
curl() {
    case "$*" in
        *api/range*)
            if [[ "$*" = *"-X POST"* ]]; then
                printf '{"target":"fixture","phase":"preflight"}\n202'
            else
                printf '%s' "$RESPONSE"
            fi ;;
        *api/firewall/health*) return 0 ;;
        *api/firewall/apply*)
            printf '%s\n' "$*" >> "$CALLS"
            printf '%s' "$RESPONSE" ;;
        *api/workshop/reset*)
            printf 'reset\n' >> "$CALLS"
            printf '%s' "$RESET_RESPONSE" ;;
        *) exit 98 ;;
    esac
}
''' + source[start:end]
        with tempfile.TemporaryDirectory() as tmp:
            tools = Path(tmp) / "bin"
            tools.mkdir()
            # An allowlist avoids macOS's /usr/bin/python3 developer-tools shim.
            for tool in ("sed", "grep"):
                (tools / tool).symlink_to(shutil.which(tool))
            calls = Path(tmp) / "calls"
            # Mirror the real handler's sorted top-level keys. Keep a successful
            # nested action and protecting receipt even when the reset fails.
            reset = json.dumps({
                "actions": [{"action": "Apply weak firewall baseline",
                             "success": True, "detail": "weak config applied"}],
                "containd": response("protecting")["containd"],
                "success": True,
            }, separators=(",", ":"))
            fixtures = [
                ("success", reset, True),
                ("partial-reset", reset.removesuffix('true}') + 'false}', False),
                ("string-success", reset.removesuffix('true}') + '"true"}', False),
                ("missing-success", reset.removesuffix(',"success":true}') + '}', False),
                ("truncated", reset[:-1], False),
                ("trailing-garbage", reset + "garbage", False),
                ("malformed", "not-json", False),
                ("empty", "", False),
            ]
            for name, body, expected in fixtures:
                with self.subTest(name=name):
                    calls.unlink(missing_ok=True)
                    result = subprocess.run(
                        [shutil.which("bash"), "-c", script],
                        text=True, capture_output=True, timeout=15,
                        env={**os.environ, "PATH": str(tools), "REPO": str(ROOT),
                             "CALLS": str(calls), "RESET_RESPONSE": body,
                             "RESPONSE": json.dumps(response("protecting"),
                                                    separators=(",", ":"))})
                    self.assertEqual(result.returncode, 0 if expected else 1,
                                     result.stdout + result.stderr)
                    if not expected:
                        self.assertIn("/api/workshop/reset reported non-success", result.stderr)
                    requests = calls.read_text().splitlines()
                    self.assertEqual(len(requests), 3)
                    self.assertIn('{"config":"weak"}', requests[0])
                    self.assertIn('{"config":"improved"}', requests[1])
                    self.assertEqual(requests[2], "reset")

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
