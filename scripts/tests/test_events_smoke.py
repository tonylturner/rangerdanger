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
        $n.Name -in @('ConvertTo-EventEpochNs', 'Get-FreshKaliDenyCount', 'Get-FreshDpiRequestCount')
}, $true)) { Invoke-Expression $f.Extent.Text }
try {
    $events = $env:FIXTURE | ConvertFrom-Json
    Get-FreshKaliDenyCount $events ([decimal]$env:BOUNDARY)
} catch { Write-Error $_; exit 1 }
"""

DPI_PS = r"""
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    "$env:REPO/scripts/events-smoke.ps1", [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
foreach ($f in $ast.FindAll({
    param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $n.Name -in @('ConvertTo-EventEpochNs', 'Get-FreshDpiRequestCount')
}, $true)) { Invoke-Expression $f.Extent.Text }
try {
    $events = $env:FIXTURE | ConvertFrom-Json
    Get-FreshDpiRequestCount $events ([decimal]$env:BOUNDARY) $env:PROTO `
        ([int]$env:FUNCTION_CODE) $env:SRC_IP $env:DST_IP ([int]$env:DST_PORT)
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


def dpi_event(proto="modbus", function_code=8, timestamp="2026-10-25T12:00:00.123456790Z", **changes):
    value = {
        "proto": proto, "kind": "request", "timestamp": timestamp,
        "attributes": {"ics": True, "function_code": function_code},
        "srcIp": "10.30.30.20", "srcPort": 49152,
        "dstIp": "10.40.40.20", "dstPort": 502 if proto == "modbus" else 20000,
        "transport": "tcp",
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

    def check_dpi_counts(self, events, expected, proto="modbus", function_code=8, dst_port=502):
        fixture = json.dumps(events)
        source = (ROOT / "scripts/events-smoke.sh").read_text()
        function = re.search(r"(?ms)^count_dpi_requests\(\) \{.*?^\}", source)[0]
        bash = subprocess.run(
            ["bash", "-c", 'set -o pipefail\nengine_events() { printf "%s" "$FIXTURE"; }\n'
             + function
             + '\ncount_dpi_requests "$PROTO" "$FUNCTION_CODE" "$SRC_IP" "$DST_IP" "$DST_PORT" "$BOUNDARY"'],
            env=dict(os.environ, FIXTURE=fixture, PROTO=proto, FUNCTION_CODE=str(function_code),
                     SRC_IP="10.30.30.20", DST_IP="10.40.40.20", DST_PORT=str(dst_port), BOUNDARY=BOUNDARY),
            capture_output=True, text=True, timeout=10,
        )
        ps = powershell(
            DPI_PS, FIXTURE=fixture, BOUNDARY=BOUNDARY, PROTO=proto,
            FUNCTION_CODE=str(function_code), SRC_IP="10.30.30.20",
            DST_IP="10.40.40.20", DST_PORT=str(dst_port),
        )
        for name, result in (("bash", bash), ("powershell", ps)):
            with self.subTest(implementation=name):
                if expected is None:
                    self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
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

    def test_dpi_assertion_matches_fresh_tuple_protocol_and_function_code(self):
        self.check_dpi_counts([dpi_event()], 1)
        self.check_dpi_counts([dpi_event(timestamp="2026-10-24T12:00:00Z")], 0)
        for changes in (
            {"proto": "dnp3"}, {"attributes": {"ics": True, "function_code": 3}},
            {"kind": "response"}, {"srcIp": "10.30.30.21"},
            {"dstIp": "10.40.40.21"}, {"dstPort": 503},
            {"srcPort": 0}, {"transport": "udp"},
            {"attributes": {"ics": False, "function_code": 8}},
        ):
            with self.subTest(changes=changes):
                self.check_dpi_counts([dpi_event(**changes)], 0)
        self.check_dpi_counts([dpi_event(proto="dnp3", function_code=5)], 1,
                              proto="dnp3", function_code=5, dst_port=20000)
        self.check_dpi_counts([dpi_event(timestamp="invalid")], None)

    def test_dpi_gates_no_longer_inspect_or_flush_block_flows(self):
        for name in ("sh", "ps1"):
            source = (ROOT / f"scripts/events-smoke.{name}").read_text()
            with self.subTest(name=name):
                self.assertNotIn("block_flows", source)
                self.assertIn("internal/events?limit=500", source)
                self.assertIn("date -u +%s%N", source)
                if name == "ps1":
                    self.assertIn("Get-FreshDpiRequestCount", source)
                else:
                    self.assertIn("count_dpi_requests", source)

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

    def test_shell_skip_totals_and_required_skip_failure(self):
        source = (ROOT / "scripts/events-smoke.sh").read_text()
        skip_function = re.search(r"(?ms)^skip\(\) \{.*?^\}", source)[0]
        summary = source[source.index('\nnote "summary"') + 1:]
        for required, expected in (("optional", 0), ("required", 1)):
            with self.subTest(required=required):
                fixture = (
                    "fail=0 passed=0 skipped=0 required_skipped=0 total=0\n"
                    "note() { :; }\n" + skip_function +
                    f'\nskip "DPI" "unavailable" "{required}"\n' + summary
                )
                result = subprocess.run(
                    ["bash", "-c", fixture], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
                self.assertIn("passed: 0 / 1", result.stdout)
                self.assertIn("skipped: 1", result.stdout)
                self.assertIn(f"required skipped: {int(required == 'required')}", result.stdout)

    def test_powershell_skip_totals_and_required_skip_failure(self):
        script = r"""
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$path = "$env:REPO/scripts/events-smoke.ps1"
$source = [IO.File]::ReadAllText($path)
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors | Out-String) }
$skipAst = $ast.Find({
    param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Skip'
}, $true)
Invoke-Expression $skipAst.Extent.Text
$script:fail = 0; $script:passed = 0; $script:skipped = 0
$script:requiredSkipped = 0; $script:total = 0
function Note($message) { Write-Host $message }
Skip 'DPI' 'unavailable' ([bool]::Parse($env:REQUIRED))
$summaryAt = $source.LastIndexOf('# --- Summary')
Invoke-Expression $source.Substring($summaryAt)
"""
        for required, expected in ((False, 0), (True, 1)):
            with self.subTest(required=required):
                result = powershell(script, REQUIRED=str(required).lower())
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
                self.assertIn("passed: 0 / 1", result.stdout)
                self.assertIn("skipped: 1", result.stdout)
                self.assertIn(f"required skipped: {int(required)}", result.stdout)
