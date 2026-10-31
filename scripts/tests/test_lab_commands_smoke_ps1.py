"""PowerShell lab-command totals with fake command and container results."""
import json
import unittest

from .test_setup_ps1_root import powershell


HARNESS = r"""
$ErrorActionPreference = 'Stop'
$path = "$env:REPO/scripts/lab-commands-smoke.ps1"
$source = [IO.File]::ReadAllText($path)
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
foreach ($name in @('Note', 'OK', 'Err', 'Skip')) {
    $functionAst = $ast.Find({
        param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and
            $n.Name -eq $name
    }, $true)
    Invoke-Expression $functionAst.Extent.Text
}
$loop = $ast.Find({
    param($n) $n -is [Management.Automation.Language.ForEachStatementAst] -and
        $n.Extent.Text -match '(?i)^foreach \(\$row in \$rows\)'
}, $true)
if (-not $loop) { throw 'lab command loop not found' }
$script:fail = 0; $script:total = 0; $script:passed = 0
$script:skipped = 0; $script:executed = 0
$current_policy = ''; $current_scenario = ''
$rows = @($env:ROWS | ConvertFrom-Json)
$minimum = @($rows | Where-Object {
    $parts = $_ -split '\|', 7
    $parts.Count -ge 7 -and $parts[5] -eq 'RUN'
}).Count
function Invoke-PolicyApply($name) {}
function docker {
    $global:LASTEXITCODE = if ($env:MODE -eq 'missing-container') { 1 } else { 0 }
}
function Invoke-LabCommand($container, $cmd) {
    $verdict = if ($env:MODE -eq 'command-fail') { 'FAIL_TEST' } else { 'PASS' }
    [pscustomobject]@{Verdict=$verdict; Dur=1}
}
Invoke-Expression $loop.Extent.Text
$summaryAt = $source.LastIndexOf('# --- Summary')
if ($summaryAt -lt 0) { throw 'summary block not found' }
Invoke-Expression $source.Substring($summaryAt)
"""


class LabCommandPowerShellTotals(unittest.TestCase):
    def run_script(self, rows, mode="ok"):
        return powershell(HARNESS, ROWS=json.dumps(rows), MODE=mode)

    @staticmethod
    def rows():
        return [
            "lab|1|read|weak|rangerdanger-kali|RUN|read command",
            "lab|2|mutate||rangerdanger-kali|SKIP|mutating command",
        ]

    def test_skip_is_in_total_and_success_does_not_claim_every_command_ran(self):
        result = self.run_script(self.rows())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("total  : 2", result.stdout)
        self.assertIn("executed: 1", result.stdout)
        self.assertIn("passed : 1", result.stdout)
        self.assertIn("skipped: 1", result.stdout)
        self.assertIn("ALL RUNNABLE LAB COMMANDS RAN CLEANLY", result.stdout)
        self.assertNotIn("ALL DOCUMENTED LAB COMMANDS RUN CLEANLY", result.stdout)

    def test_missing_container_cannot_be_hidden_by_a_skip(self):
        result = self.run_script(self.rows(), mode="missing-container")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("container rangerdanger-kali not running", result.stdout)
        self.assertIn("executed: 0", result.stdout)
        self.assertIn("skipped: 1", result.stdout)
        self.assertIn("unverified: 1", result.stdout)

    def test_run_failure_is_counted_separately_from_skips(self):
        result = self.run_script(self.rows(), mode="command-fail")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("failed : 1", result.stdout)
        self.assertIn("skipped: 1", result.stdout)
