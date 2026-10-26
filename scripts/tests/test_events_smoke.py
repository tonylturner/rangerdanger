"""Fresh probe evidence in both event gates; no Docker or live range."""
import json
import os
import re
import subprocess
import unittest

from .test_setup_ps1_root import ROOT, powershell


BOUNDARY = "1792929600123456789"  # 2026-10-25T12:00:00.123456789Z
PS = r"""
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    "$env:REPO/scripts/events-smoke.ps1", [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
foreach ($f in $ast.FindAll({
    param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $n.Name -in @('ConvertTo-EventEpochNs', 'Get-FreshKaliDenyCount')
}, $true)) { Invoke-Expression $f.Extent.Text }
try {
    $events = $env:FIXTURE | ConvertFrom-Json
    Get-FreshKaliDenyCount $events ([decimal]$env:BOUNDARY)
} catch { Write-Error $_; exit 1 }
"""


def event(timestamp="2026-10-25T12:00:00.123456790Z", **changes):
    value = {
        "kind": "firewall.rule.hit",
        "timestamp": timestamp,
        "attributes": {"action": "DENY", "ruleId": "deny-enterprise-to-field"},
        "srcIp": "10.10.10.50", "dstIp": "10.40.40.20",
        "dstPort": 502, "transport": "tcp",
    }
    value.update(changes)
    return value


class EventsSmokeTests(unittest.TestCase):
    def check_counts(self, events, expected=None):
        fixture = json.dumps(events)
        source = (ROOT / "scripts/events-smoke.sh").read_text()
        function = re.search(r"(?ms)^count_kali_denies\(\) \{.*?^\}", source)[0]
        bash = subprocess.run(
            ["bash", "-c", 'set -o pipefail\nprobe_boundary="$BOUNDARY"\n'
             'engine_events() { printf "%s" "$FIXTURE"; }\n'
             + function + "\ncount_kali_denies"],
            env=dict(os.environ, FIXTURE=fixture, BOUNDARY=BOUNDARY),
            capture_output=True, text=True, timeout=10,
        )
        ps = powershell(PS, FIXTURE=fixture, BOUNDARY=BOUNDARY)
        for name, result in (("bash", bash), ("powershell", ps)):
            with self.subTest(implementation=name):
                if expected is None:
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                else:
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual(result.stdout.strip(), str(expected))

    def test_old_matching_deny_without_fresh_fails(self):
        self.check_counts([event("2026-10-24T12:00:00Z")], 0)

    def test_fresh_matching_deny_passes(self):
        self.check_counts([event()], 1)

    def test_old_match_plus_fresh_wrong_tuple_fails(self):
        self.check_counts([event("2026-10-24T12:00:00Z"),
                           event(srcIp="10.10.10.51")], 0)

    def test_exact_boundary_and_wrong_rule_action_destination_transport(self):
        for changes in (
            {"timestamp": "2026-10-25T12:00:00.123456789Z"},
            {"timestamp": "2026-10-25T12:00:00.123456788Z"},
            {"attributes": {"action": "ALLOW", "ruleId": "deny-enterprise-to-field"}},
            {"attributes": {"action": "DENY", "ruleId": "other"}},
            {"dstIp": "10.40.40.21"}, {"dstPort": 503}, {"transport": "udp"},
            {"kind": "other"},
        ):
            with self.subTest(changes=changes):
                self.check_counts([event(**changes)], 0)
        self.check_counts([], 0)
        self.check_counts([event("2026-10-25T14:00:00.123456790+02:00")], 1)

    def test_missing_or_invalid_matching_timestamp_is_an_error(self):
        for timestamp in (None, "invalid"):
            with self.subTest(timestamp=timestamp):
                self.check_counts([event(timestamp)], None)

    def test_boundary_captured_before_probe_and_passed_to_predicate(self):
        for name, boundary, call in (
            ("sh", "probe_boundary=$(docker exec rangerdanger-firewall date -u +%s%N)",
             '\' "$probe_boundary"'),
            ("ps1", "$boundaryRaw = docker exec rangerdanger-firewall date -u +%s%N",
             "Get-FreshKaliDenyCount $events $probeBoundary"),
        ):
            source = (ROOT / f"scripts/events-smoke.{name}").read_text()
            self.assertLess(source.index(boundary),
                            source.index('nc -nv -w 3 10.40.40.20 502'))
            self.assertIn(call, source)
        (ROOT / "scripts/events-smoke.ps1").read_bytes().decode("ascii")
