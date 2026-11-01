"""The Windows workflow must assert a CheckOnly end state, not its banner."""
import unittest

import yaml

from .test_setup_ps1_root import ROOT, powershell


class CheckOnlyWorkflowTests(unittest.TestCase):
    def test_changed_windows_entrypoints_are_ascii_bom_free_and_parse(self):
        for path in ("setup.ps1", "stage-ssd.ps1", "stage-ssd-delta.ps1",
                     "scripts/install-wsl-kernel.ps1"):
            with self.subTest(path=path):
                data = (ROOT / path).read_bytes()
                self.assertTrue(data.isascii())
                self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
                result = powershell("""
$t=$null; $e=$null
[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:FILE", [ref]$t, [ref]$e) | Out-Null
if ($e.Count) { throw ($e | Out-String) }
""", FILE=path)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_workflow_accepts_only_completed_preflight_or_explicit_daemon_refusal(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/smoke-windows.yml").read_text())
        step = next(step for step in workflow["jobs"]["lint"]["steps"]
                    if step.get("name") == "setup.ps1 -CheckOnly end-state check")
        source = step["run"]
        function = source[:source.index("$output =")]
        success = ("Pre-flight checks\nPre-flight passed -- laptop is ready\n"
                   "[+] Root mapping and WSL2 kernel features will be verified "
                   "after image acquisition during install.\n")
        cases = (
            ("Pre-flight checks\n", 0, False),
            ("Pre-flight checks\n[x] unrelated runtime failure\n", 1, False),
            (success, 0, True),
            (success, 1, False),
            ("Pre-flight passed -- laptop is ready\n", 0, False),
            ("[x] Docker failed (1): info --format {{json .}}\nfixture stderr\n", 1, True),
            ("[x] Docker did not answer within 30s: info --format {{json .}}\n", 1, True),
            ("[x] Windows setup supports only Docker Desktop with the WSL2 backend "
             "and a repo on a local drive letter. Hyper-V is not supported.\n", 1, True),
            ("[x] Docker failed (1): compose version --short\n", 1, False),
            ("[x] Docker failed (1): info --format {{json .}}\n", 0, False),
            ("Pre-flight checks\n[x] Required loopback port is held\n", 1, False),
        )
        for text, code, accepted in cases:
            with self.subTest(text=text, code=code):
                result = powershell(function + """
$failed=$false
try { Assert-CheckOnlyOutcome $env:TEXT ([int]$env:CODE) }
catch { $failed=$true }
if ($failed -eq ($env:ACCEPT -eq '1')) { throw 'unexpected CheckOnly verdict' }
""", TEXT=text, CODE=str(code), ACCEPT=str(int(accepted)))
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("$code = $LASTEXITCODE", source)
        self.assertIn("Assert-CheckOnlyOutcome $joined $code", source)
