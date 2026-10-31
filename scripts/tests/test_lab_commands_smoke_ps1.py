"""PowerShell propagates the shared worker's semantic verdicts, not tool totals."""
import json
import sys
import unittest

from .test_setup_ps1_root import powershell


HARNESS = r"""
$path = "$env:REPO/scripts/lab-commands-smoke.ps1"
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$functionAst = $ast.Find({
    param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $n.Name -eq 'Invoke-LabCommandWorker'
}, $true)
Invoke-Expression $functionAst.Extent.Text
$PythonExe = $env:PYTHON
$PythonPrefix = @()
$code = Invoke-LabCommandWorker @('-c', $env:PROGRAM)
exit $code
"""

PROGRAM = r"""
import sys, os, json
sys.path.insert(0, os.path.join(os.environ['REPO'], 'scripts/lib'))
from lab_commands_smoke import gate
from lab_commands_eu import semantic
f = json.loads(os.environ['FIXTURE'])
passed = gate.verdict(f['oracle'], f['rc'], f['out'], f['err'],
                      control=f.get('control', False), frames=f.get('frames'))
passed = passed and semantic(f.get('semantic'), f['out'])
print('stdout receipt retained')
print('stderr receipt retained', file=sys.stderr)
print('PASS' if passed else 'FAIL')
sys.exit(0 if passed else 1)
"""


class LabCommandPowerShellSemantics(unittest.TestCase):
    def verdict(self, oracle, rc, out='', err='', **kwargs):
        fixture = dict(oracle=oracle, rc=rc, out=out, err=err, **kwargs)
        return powershell(HARNESS, PYTHON=sys.executable, PROGRAM=PROGRAM,
                          FIXTURE=json.dumps(fixture))

    def test_read_requires_semantic_output_and_both_streams_survive(self):
        oracle = {"intent": "read", "match": r"\[0\]: 1"}
        result = self.verdict(oracle, 0, '[0]: 1')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('stdout receipt retained', result.stdout)
        self.assertIn('stderr receipt retained', result.stderr)
        for rc, out, err in ((0, '', ''), (1, '[0]: 1', ''), (7, '', ''),
                             (124, '', ''), (143, '', ''), (0, '[0]: 1', 'Illegal function')):
            with self.subTest(rc=rc, out=out):
                self.assertEqual(self.verdict(oracle, rc, out, err).returncode, 1)

    def test_denial_requires_weak_positive_control_and_filtered_not_closed(self):
        oracle = {"intent": "deny", "match": "2404/tcp +filtered", "nmap": True}
        self.assertEqual(self.verdict(oracle, 0, '2404/tcp filtered', control=True).returncode, 0)
        self.assertEqual(self.verdict(oracle, 0, '2404/tcp filtered').returncode, 1)
        self.assertEqual(self.verdict(oracle, 0, '2404/tcp closed', control=True).returncode, 1)
        self.assertEqual(self.verdict(oracle, 127, err='command not found', control=True).returncode, 1)

    def test_gi_and_capture_are_not_exit_only_success(self):
        oracle = {"intent": "read", "match": ".+"}
        good = '{"event":"result","op":"gi","status":"ok"}'
        self.assertEqual(self.verdict(oracle, 0, good, semantic='gi').returncode, 0)
        self.assertEqual(self.verdict(oracle, 0, '{"event":"connected"}', semantic='gi').returncode, 1)
        capture = {"intent": "capture"}
        self.assertEqual(self.verdict(capture, 0, frames=0).returncode, 1)
        self.assertEqual(self.verdict(capture, 0, frames=3).returncode, 0)


if __name__ == '__main__':
    unittest.main()
