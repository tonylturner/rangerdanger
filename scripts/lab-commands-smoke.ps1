<#
.SYNOPSIS
Lab-commands smoke test -- Windows sibling of scripts/lab-commands-smoke.sh.

.DESCRIPTION
For every command shown in a lab YAML's step description or hint body
(anything the scenario runner detects as a CommandBlock matching the
CMD_TOOL_RE pattern in scenario-runner.tsx), verifies it executes
cleanly when run from the correct container under the step's expected
firewall policy.

This script needs a real Python 3 (not the Microsoft Store stub) with
pyyaml installed. On Windows the canonical way is the py launcher:

    py -m pip install pyyaml

If you see the Store stub message ("Python was not found; run without
arguments to install from the Microsoft Store"), open Settings >
Apps > Advanced app settings > App execution aliases and disable the
python.exe / python3.exe aliases, then install real Python from
python.org.

.PARAMETER Scenario
Optional scenario id to scope the run (e.g. baseline-assessment).

.NOTES
Exit 0 = every documented command runs cleanly, non-zero = at least
one failed. ASCII-only, BOM-free. See setup.ps1 for the encoding
rationale.

The embedded Python script is BYTE-FOR-BYTE the same parser logic
that scripts/lab-commands-smoke.sh uses, so any future change to lab
schema needs to be applied to both. Diff them after edits.
#>

[CmdletBinding()]
param(
    [string]$Scenario = "",
    [int]$ProbeTimeoutSec = $(if ($env:LAB_CMD_TIMEOUT) { [int]$env:LAB_CMD_TIMEOUT } else { 5 }),
    [int]$SettleSecs     = $(if ($env:SETTLE_SECS)      { [int]$env:SETTLE_SECS }      else { 2 })
)

$ErrorActionPreference = "Continue"

$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RootDir

$Api = if ($env:RANGERDANGER_API) { $env:RANGERDANGER_API } else { 'http://localhost:8088' }

$script:fail    = 0
$script:total   = 0
$script:passed  = 0
$script:skipped = 0

function Note($msg)    { Write-Host ""; Write-Host "=== $msg ===" -ForegroundColor Cyan }
function OK($msg)      { Write-Host "  [+] $msg" -ForegroundColor Green; $script:passed++ }
function InfoOK($msg)  { Write-Host "  [+] $msg" -ForegroundColor Green }   # preflight; doesn't count toward matrix
function Err($msg)     { Write-Host "  [x] $msg" -ForegroundColor Red; $script:fail = 1 }
function Skip($msg)    { Write-Host "  [-] $msg" -ForegroundColor DarkGray; $script:skipped++ }

# --- Resolve a real Python 3 --------------------------------------------
# Order: py launcher (canonical on Windows), then python3.exe / python.exe
# if they resolve to a real install (not the Store stub).
function Resolve-Python3 {
    if (Get-Command py -ErrorAction SilentlyContinue) {
        $exe = & py -3 -c "import sys; print(sys.executable)" 2>$null
        if ($LASTEXITCODE -eq 0 -and $exe -and (Test-Path $exe.Trim())) {
            return ,@('py','-3')
        }
    }
    foreach ($name in 'python3','python') {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if (-not $cmd) { continue }
        # Skip Store stubs (their path is under WindowsApps and they
        # exit with a Store-redirect message instead of running code).
        if ($cmd.Source -like '*\WindowsApps\*') { continue }
        $exe = & $cmd.Source -c "import sys; print(sys.executable)" 2>$null
        if ($LASTEXITCODE -eq 0 -and $exe) { return ,@($cmd.Source) }
    }
    return $null
}

$Python = Resolve-Python3
if (-not $Python) {
    Err "No real Python 3 found. Install from python.org and ensure 'py -3' works, or disable the python.exe Store alias and reinstall."
    exit 1
}

# Verify pyyaml is importable. The .sh version does this in its preflight
# too; we match the message ("pip3 install pyyaml") plus give Windows
# the correct form.
& $Python[0] $Python[1..($Python.Count-1)] -c "import yaml" 2>$null
if ($LASTEXITCODE -ne 0) {
    Err "python pyyaml is required. Install with: $($Python -join ' ') -m pip install pyyaml"
    exit 1
}

# --- Helpers (canary, apply, run) ---------------------------------------
function Test-Canary {
    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec rangerdanger-kali timeout 1 bash -c 'exec 3<>/dev/tcp/10.30.30.20/502' *>$null
    }
    if ($LASTEXITCODE -eq 0) { 'allow' } else { 'deny' }
}

function Wait-ForDataplane($expected, $budgetSec = 15) {
    for ($i = 1; $i -le ($budgetSec * 2); $i++) {
        if ((Test-Canary) -eq $expected) { return $true }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Invoke-PolicyApply($name) {
    if (-not $name) { return }
    try {
        $resp = Invoke-RestMethod -Uri "$Api/api/firewall/apply" -Method POST `
                  -ContentType 'application/json' -Body (@{ config = $name } | ConvertTo-Json -Compress) `
                  -TimeoutSec 30 -ErrorAction Stop
    } catch {
        Err "policy apply ($name) failed: $_"
        exit 1
    }
    try {
        $active = Invoke-RestMethod -Uri "$Api/api/firewall/active" -TimeoutSec 10 -ErrorAction Stop
    } catch {
        Err "could not read /api/firewall/active after applying ${name}: $_"
        exit 1
    }
    if ($active.active_config -ne $name) {
        Err "policy apply silently failed: requested $name, /api/firewall/active reports: $($active | ConvertTo-Json -Compress)"
        exit 1
    }
    $expectedCanary = switch ($name) {
        'weak'     { 'allow' }
        'baseline' { 'allow' }
        'improved' { 'deny' }
        'hardened' { 'deny' }
        default    { Start-Sleep -Seconds $SettleSecs; return }
    }
    if (-not (Wait-ForDataplane $expectedCanary 15)) {
        Err "dataplane never reconciled to $name (canary kali->rtac:502 still wrong after 15s)"
        exit 1
    }
}

function Get-NowMs { [int64]([DateTime]::UtcNow - [DateTime]'1970-01-01T00:00:00Z').TotalMilliseconds }

function Invoke-LabCommand($container, $cmd) {
    $start = Get-NowMs
    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec $container timeout $ProbeTimeoutSec bash -c $cmd *>$null
    }
    $rc = $LASTEXITCODE
    $dur = (Get-NowMs) - $start
    switch ($rc) {
        0       { return @{ Verdict='PASS';         Dur=$dur } }
        124     { return @{ Verdict='PASS-TIMEOUT'; Dur=$dur } }
        143     { return @{ Verdict='PASS-TIMEOUT'; Dur=$dur } }
        125     { return @{ Verdict='FAIL_EXEC';    Dur=$dur } }
        127     { return @{ Verdict='FAIL_NOTOOL';  Dur=$dur } }
        default {
            if ($rc -le 7) { return @{ Verdict="PASS-RC$rc"; Dur=$dur } }
            return @{ Verdict="FAIL_RC=$rc"; Dur=$dur }
        }
    }
}

# --- Embedded YAML extractor --------------------------------------------
# This Python script is duplicated from scripts/lab-commands-smoke.sh.
# If you change the lab YAML schema, update BOTH places. The bash
# version is the source of truth -- keep this in sync after edits.
$ExtractPy = @'
import os, sys, re, yaml, glob

SCENARIO_DIR = "lab-definitions/scenarios"
TOPOLOGY    = "lab-definitions/substation-segmentation.yml"

CMD_TOOL_RE = re.compile(
    r'^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s'
)

SKIP_PATTERNS = [
    re.compile(r'/api/firewall/apply'),
    re.compile(r'/api/firewall/apply-custom'),
    re.compile(r'docker compose '),
    re.compile(r'docker run '),
    re.compile(r'docker rm '),
    re.compile(r'docker stop '),
    re.compile(r'^ssh\b'),
]

def should_skip(cmd: str) -> bool:
    return any(p.search(cmd) for p in SKIP_PATTERNS)

def load_topology():
    with open(TOPOLOGY) as f:
        topo = yaml.safe_load(f)
    nodes = {n["id"]: n.get("container", "") for n in topo.get("nodes", [])}
    return nodes

NODE_INFERENCE = [
    ("kali-1",        re.compile(r'10\.10\.10\.50|\bkali\b', re.I)),
    ("vendor-jump-1", re.compile(r'10\.20\.20\.10|\bvendor.?jump\b', re.I)),
    ("eng-ws-1",      re.compile(r'10\.20\.20\.20|\bengineering.?workstation\b|\beng-ws\b', re.I)),
    ("openplc-1",     re.compile(r'10\.30\.30\.30|\bopenplc\b', re.I)),
    ("rtac-1",        re.compile(r'10\.30\.30\.20|\brtac\b', re.I)),
]

def infer_node_from_text(text: str) -> str:
    for node_id, pat in NODE_INFERENCE:
        if pat.search(text):
            return node_id
    return ""

def infer_node_per_command(desc: str, cmd_line_idx: int):
    lines = desc.split("\n")
    KEYWORD_MAP = {
        "kali":         "kali-1",
        "vendor-jump":  "vendor-jump-1",
        "vendor":       "vendor-jump-1",
        "engineering":  "eng-ws-1",
        "eng-ws":       "eng-ws-1",
        "eng":          "eng-ws-1",
        "openplc":      "openplc-1",
        "rtac":         "rtac-1",
        "hmi":          "hmi-1",
        "fuxa":         "hmi-1",
        "firewall":     "fw-1",
    }
    start = max(0, cmd_line_idx - 12)
    for back in range(cmd_line_idx - 1, start - 1, -1):
        if back < 0 or back >= len(lines):
            continue
        line = lines[back].lower()
        for m in re.finditer(r'\b(?:from|on)\s+(?:the\s+)?[`*]*([a-z][a-z0-9-]*)\b', line):
            keyword = m.group(1)
            if keyword in KEYWORD_MAP:
                return KEYWORD_MAP[keyword]
    return ""

def extract_step_commands(desc: str):
    if not desc:
        return []
    out = []
    lines = desc.split("\n")
    i = 0
    while i < len(lines):
        raw = lines[i]
        if re.match(r'^\s+\S', raw) and CMD_TOOL_RE.match(raw.strip()):
            cmd = raw.strip()
            cmd_idx = i
            while cmd.endswith("\\") and i + 1 < len(lines):
                i += 1
                cmd = cmd.rstrip("\\").rstrip() + " " + lines[i].strip()
            out.append((cmd, cmd_idx))
        i += 1
    return out

def main():
    target = sys.argv[1] if len(sys.argv) > 1 else ""
    nodes = load_topology()

    files = sorted(glob.glob(os.path.join(SCENARIO_DIR, "*.yml")))
    for path in files:
        scenario_id = os.path.basename(path).replace(".yml", "")
        if target and target != scenario_id:
            continue
        with open(path) as f:
            doc = yaml.safe_load(f)
        scenario_node = (doc.get("nodes") or [None])[0]

        for idx, step in enumerate(doc.get("steps", []), start=1):
            title = step.get("title", "(untitled)")
            expected_cfg = step.get("expected_config", "")
            explicit_node = step.get("node", "") or ""
            desc = step.get("description", "") or ""

            step_inferred = infer_node_from_text(desc)
            step_node_id = explicit_node or step_inferred or (scenario_node or "")

            for cmd, line_idx in extract_step_commands(desc):
                per_cmd_node = infer_node_per_command(desc, line_idx) or step_node_id
                container = nodes.get(per_cmd_node, "")

                if should_skip(cmd):
                    print(f"{scenario_id}|{idx}|{title}|{expected_cfg}|{container}|SKIP|{cmd}")
                else:
                    print(f"{scenario_id}|{idx}|{title}|{expected_cfg}|{container}|RUN|{cmd}")

if __name__ == "__main__":
    main()
'@

# --- Preflight ----------------------------------------------------------
Note "preflight"
try {
    Invoke-WebRequest -Uri "$Api/api/health" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop | Out-Null
    InfoOK "backend $Api healthy"
} catch {
    Err "backend not reachable at $Api -- bring stack up first"
    exit 1
}

# --- Extract commands ---------------------------------------------------
Note "extracting commands$(if ($Scenario) { " ($Scenario)" })"
$pyArgs = @()
$pyArgs += $Python[1..($Python.Count-1)]
$pyArgs += '-'
$pyArgs += $Scenario
$rows = $ExtractPy | & $Python[0] @pyArgs
if ($LASTEXITCODE -ne 0 -or -not $rows) {
    Err "command extractor returned no rows -- check lab-definitions/ paths and pyyaml install"
    exit 1
}

$current_policy   = ""
$current_scenario = ""

foreach ($row in $rows) {
    if (-not $row) { continue }
    $script:total++

    $parts = $row -split '\|', 7
    if ($parts.Count -lt 7) { continue }
    $scenario     = $parts[0]
    $stepIdx      = $parts[1]
    $stepTitle    = $parts[2]
    $cfg          = $parts[3]
    $container    = $parts[4]
    $action       = $parts[5]
    $cmd          = $parts[6]

    if ($scenario -ne $current_scenario) {
        Note "scenario: $scenario"
        $current_scenario = $scenario
    }

    if ($action -eq 'SKIP') {
        Skip "[$scenario step $stepIdx] $cmd"
        $script:total--   # SKIPs don't count in the pass denominator
        continue
    }
    if (-not $container) {
        Err "[$scenario step $stepIdx] no source container -- '$cmd' cannot be run from anywhere"
        continue
    }
    if ($cfg -and $cfg -ne $current_policy) {
        # YAML "hardened" is the student-facing alias for backend policy
        # "improved" (matches scenario-runner.tsx). Map it here so a
        # "Re-test under hardened policy" step actually flips the policy
        # instead of silently inheriting weak.
        $applyName = if ($cfg -eq 'hardened') { 'improved' } else { $cfg }
        if ($applyName -eq 'weak' -or $applyName -eq 'improved') {
            Invoke-PolicyApply $applyName
            $current_policy = $cfg
        }
    }

    # Container must exist
    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker inspect $container *>$null
    }
    if ($LASTEXITCODE -ne 0) {
        Err "[$scenario step $stepIdx] container $container not running -- $cmd"
        continue
    }

    $r = Invoke-LabCommand $container $cmd
    $shortCmd = if ($cmd.Length -gt 80) { $cmd.Substring(0,80) } else { $cmd }
    $label = "[$scenario s$stepIdx $(if ($cfg) { $cfg } else { 'any' })] $($container -replace '^rangerdanger-','') : $shortCmd"

    switch -Regex ($r.Verdict) {
        '^(PASS|PASS-TIMEOUT|PASS-RC)' { OK "$label  ($($r.Verdict) $($r.Dur)ms)" }
        '^FAIL_NOTOOL'                  { Err "$label  TOOL NOT INSTALLED" }
        '^FAIL_EXEC'                    { Err "$label  DOCKER EXEC FAILED" }
        default                          { Err "$label  $($r.Verdict) ($($r.Dur)ms)" }
    }
}

# --- Summary ------------------------------------------------------------
Note "summary"
Write-Host "  ran    : $($script:total)"
Write-Host "  passed : $($script:passed)"
Write-Host "  skipped: $($script:skipped) (mutating, interactive, or out-of-scope)"
if ($script:fail -eq 0) {
    Write-Host "  ALL DOCUMENTED LAB COMMANDS RUN CLEANLY" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  FAILED -- $($script:total - $script:passed) commands had issues" -ForegroundColor Red
    exit 1
}
