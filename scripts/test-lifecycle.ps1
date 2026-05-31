<#
.SYNOPSIS
End-to-end setup -> execute -> teardown lifecycle validator for Windows.

.DESCRIPTION
Runs the REAL setup.ps1 and uninstaller against the full stack, then
asserts each phase independently and prints a single PASS/FAIL. This is the
Windows counterpart of scripts/test-lifecycle.sh. Use on a TEST machine --
it installs the full stack and then removes it.

It catches what a bare setup.ps1 does not: OpenPLC's readiness is checked
explicitly, and teardown is verified to leave the host clean.

x86_64 Windows runs OpenPLC natively (no emulation; the binfmt path does
not apply). DPI labs (2.3) need the custom WSL2 kernel setup.ps1 installs.

.PARAMETER Yes
Skip the confirmation prompt.

.PARAMETER Reinstall
Also re-install after teardown (idempotency check).

.PARAMETER NoTeardown
Leave the stack up after the assertions instead of uninstalling.

.PARAMETER FromTarballs
Offline / SSD install: directory containing images-<arch>.tar.

.EXAMPLE
.\scripts\test-lifecycle.ps1
.\scripts\test-lifecycle.ps1 -FromTarballs D:\WORKSHOP_SSD
.\scripts\test-lifecycle.ps1 -Yes -Reinstall

.NOTES
ASCII-only, BOM-free (see setup.ps1). Exit: 0 all pass, 1 a failure,
2 wrong environment.
#>
[CmdletBinding()]
param(
    [switch]$Yes,
    [switch]$Reinstall,
    [switch]$NoTeardown,
    [string]$FromTarballs
)

$ErrorActionPreference = "Continue"

$script:Pass = 0
$script:Fail = 0
function Ok($m)    { Write-Host "  PASS $m" -ForegroundColor Green; $script:Pass++ }
function Bad($m)   { Write-Host "  FAIL $m" -ForegroundColor Red;   $script:Fail++ }
function Info($m)  { Write-Host "[*] $m" -ForegroundColor Yellow }
function Phase($m) { Write-Host ""; Write-Host "== $m ==" -ForegroundColor Cyan }

$RepoRoot    = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RepoRoot
$ComposeFile = Join-Path $RepoRoot "docker-compose.release.yml"
$EnvFile     = Join-Path $RepoRoot ".env"

Phase "RangerDanger lifecycle test (Windows)"
Info "Repo: $RepoRoot   install: $(if ($FromTarballs) { "offline ($FromTarballs)" } else { 'online' })"
if (-not (Test-Path $ComposeFile)) { Bad "run from a checkout - $ComposeFile missing"; exit 2 }
& docker compose version *> $null
if ($LASTEXITCODE -ne 0) { Bad "docker compose v2 not found"; exit 2 }

function HttpOk($url) {
    try {
        $r = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10 -ErrorAction Stop
        return ($r.StatusCode -eq 200)
    } catch { return $false }
}

function RunSetup {
    if ($FromTarballs) { & .\setup.ps1 -FromTarballs $FromTarballs }
    else               { & .\setup.ps1 }
    # setup.ps1 die()s with exit 1; success falls through. The real verdict
    # is the health asserts below -- this is a best-effort signal only.
    return ($LASTEXITCODE -eq 0 -or $null -eq $LASTEXITCODE)
}

function AssertUp {
    Phase "Assert stack is up + healthy"
    if (HttpOk "http://localhost:8088/api/health")          { Ok "backend /api/health" } else { Bad "backend /api/health" }
    if (HttpOk "http://localhost:8088/")                    { Ok "web UI (8088)" }       else { Bad "web UI (8088)" }
    if (HttpOk "http://localhost:9080/")                    { Ok "containd UI (9080)" }  else { Bad "containd UI (9080)" }
    if (HttpOk "http://localhost:8088/api/firewall/health") { Ok "containd firewall health" } else { Bad "containd firewall health" }

    # Every compose service should be in state 'running'.
    $notRunning = @(& docker compose -f $ComposeFile ps -a --format '{{.Service}} {{.State}}' 2>$null |
        Where-Object { $_ -and ($_ -notmatch ' running$') })
    if ($notRunning.Count -eq 0) { Ok "all compose services running" } else { Bad "not running: $($notRunning -join ', ')" }

    # OpenPLC explicitly -- setup's probe is non-fatal.
    $st = (& docker inspect -f '{{.State.Status}}' rangerdanger-openplc 2>$null)
    if ($st -eq 'running') { Ok "OpenPLC running (native on x86_64)" }
    else { Bad "OpenPLC NOT running (state=$st) - check 'docker logs rangerdanger-openplc'" }
}

function AssertExecute {
    Phase "Assert workshop-critical execution"
    foreach ($cfg in 'weak','improved') {
        try {
            $r = Invoke-RestMethod -Method Post -Uri "http://localhost:8088/api/firewall/apply" `
                -ContentType 'application/json' -Body "{`"config`":`"$cfg`"}" -TimeoutSec 15 -ErrorAction Stop
            # A 200 carrying warnings = containd committed the policy but the
            # kernel rejected the ruleset. On Windows this is the missing
            # CONFIG_NFT_QUEUE case: the "queue num" DPI rules fail and, nft
            # being atomic, the whole hardened ruleset rolls back. The apply
            # still returns 200, so without inspecting the body the harness
            # would green-light a stack where the hardened policy silently does
            # not enforce. Fail loudly with the fix instead.
            if ($cfg -eq 'improved' -and $r.warnings) {
                Bad "firewall apply (improved) returned warnings - hardened policy NOT enforcing (missing CONFIG_NFT_QUEUE; run .\scripts\install-wsl-kernel.ps1)"
            } else {
                Ok "firewall apply ($cfg)"
            }
        } catch { Bad "firewall apply ($cfg) - Lab 2.2/2.3/2.4 would not work" }
    }
    try {
        $resp = Invoke-RestMethod -Method Post -Uri "http://localhost:8088/api/workshop/reset" -TimeoutSec 15 -ErrorAction Stop
        if ($resp.success -eq $true) { Ok "workshop reset" } else { Bad "workshop reset (success != true)" }
    } catch { Bad "workshop reset (request failed)" }
}

function AssertTeardown {
    Phase "Teardown - uninstall + assert clean"
    & .\scripts\uninstall-rangerdanger.ps1 -Yes
    if ($LASTEXITCODE -eq 0 -or $null -eq $LASTEXITCODE) { Ok "uninstall exited 0" } else { Bad "uninstall exited $LASTEXITCODE" }

    $left = @(& docker ps -a --format '{{.Names}}' --filter "name=rangerdanger-" 2>$null | Where-Object { $_ })
    if ($left.Count -eq 0) { Ok "no rangerdanger containers remain" } else { Bad "$($left.Count) container(s) remain" }

    $vols = @(& docker volume ls --format '{{.Name}}' 2>$null | Where-Object { $_ -like 'rangerdanger*' })
    if ($vols.Count -eq 0) { Ok "no rangerdanger volumes remain" } else { Bad "$($vols.Count) volume(s) remain" }

    if (-not (Test-Path $EnvFile)) { Ok ".env removed" } else { Bad ".env still present" }
    Info "WSL2 kernel: confirm the uninstaller reverted it (it runs 'wsl --shutdown')."
}

# --- confirm (installs + removes the full stack) --------------------
if (-not $Yes) {
    Info "This runs setup.ps1 (full stack) then uninstalls it. Intended for a test host."
    $a = Read-Host "Proceed? [y/N]"
    if ($a -notmatch '^(y|yes)$') { Info "aborted, no changes made."; exit 0 }
}

Phase "Setup (.\setup.ps1)"
if (RunSetup) { Ok "setup.ps1 completed" } else { Bad "setup.ps1 exited non-zero" }
AssertUp
AssertExecute

if ($NoTeardown) {
    Info "Leaving stack up (-NoTeardown). Tear down later: .\scripts\uninstall-rangerdanger.ps1 -Yes"
} else {
    AssertTeardown
    if ($Reinstall) {
        Phase "Re-install (idempotency)"
        if (RunSetup) { Ok "re-install completed" } else { Bad "re-install exited non-zero" }
        AssertUp
        AssertTeardown
    }
}

Phase "Result"
if ($script:Fail -eq 0) {
    Write-Host "  ALL $($script:Pass) CHECKS PASSED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  $($script:Pass) passed, $($script:Fail) FAILED" -ForegroundColor Red
    exit 1
}
