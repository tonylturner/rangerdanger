<#
.SYNOPSIS
Install a custom WSL2 kernel that has CONFIG_NFT_QUEUE=y so the
RangerDanger ICS DPI dataplane works on Windows + Docker Desktop.

.DESCRIPTION
Microsoft's stock WSL2 kernel does not enable CONFIG_NFT_QUEUE, which
means nft rules containing "queue num <N>" silently fail to load and
the lab's ICS DPI enforcement falls flat. This script:

  1. Detects whether the running Docker Desktop is using the WSL2
     backend (versus Hyper-V or no Docker).
  2. Probes whether the current WSL2 kernel can apply a minimal
     queue rule. If yes, exits as a no-op.
  3. Downloads the prebuilt rangerdanger kernel from the matching
     GitHub release (asset: rangerdanger-wsl2-kernel +
     rangerdanger-wsl2-kernel.sha256). Verifies sha256 before install.
  4. Merges a kernel= pointer into the [wsl2] section of
     %USERPROFILE%\.wslconfig, preserving every other line/section
     and writing a .wslconfig.bak alongside.
  5. Prompts the user once, then runs wsl --shutdown and polls until
     Docker Desktop's daemon reconnects.
  6. Re-probes the kernel feature. Reports.

.PARAMETER Test
Probe-only. Exits 0 if the running WSL2 kernel can apply queue rules,
non-zero if it cannot. Useful for setup.ps1 to decide whether to call
install mode.

.PARAMETER Restore
Undo: restores .wslconfig.bak, deletes our installed kernel binary,
and runs wsl --shutdown so WSL2 picks up the change. Idempotent if no
prior install detected.

.PARAMETER DryRun
Walk through the install path (download, sha256, merge .wslconfig)
but do NOT actually write .wslconfig or run wsl --shutdown. Prints
what would happen.

.PARAMETER Yes
Skip the confirmation prompt before wsl --shutdown.

.PARAMETER Force
Install even if the probe says we are already fine, OR even if
.wslconfig already has a kernel= pointer that is not ours (back it
up to .wslconfig.bak.foreign, then overwrite).

.PARAMETER KernelPath
Use this local file instead of downloading. Skips the network step
and assumes the binary is correct (no sha256 verify unless -ExpectedSha256
is also set). Useful for offline / SSD installs.

.PARAMETER KernelUrl
Explicit download URL. Overrides the per-release computed URL.

.PARAMETER ExpectedSha256
Explicit sha256 to verify the downloaded kernel against. Required
when using -KernelUrl with a non-GitHub-release source (no auto sha256
file to fetch).

.PARAMETER ReleaseTag
RangerDanger release to download the kernel from. Default: 'latest'
(GitHub's latest-release endpoint). For pinned installs set to e.g.
v0.1.17.

.PARAMETER OwnerRepo
GitHub owner/repo of the rangerdanger fork. Default: tonylturner/rangerdanger.

.EXAMPLE
.\scripts\install-wsl-kernel.ps1 -Test
.\scripts\install-wsl-kernel.ps1
.\scripts\install-wsl-kernel.ps1 -Yes
.\scripts\install-wsl-kernel.ps1 -KernelPath D:\WORKSHOP_SSD\rangerdanger-wsl2-kernel
.\scripts\install-wsl-kernel.ps1 -Restore

.NOTES
ASCII-only, BOM-free. See setup.ps1 / wsl-kernel/README.md for the
encoding rationale and supply-chain story.

Exit codes:
  0  = success / no-op (kernel already good)
  1  = install attempted but the post-install probe still fails
  2  = ran on a non-Windows / non-WSL2 host (skipped, no error)
  10 = user declined the wsl --shutdown prompt
  11 = .wslconfig already has a foreign kernel= and -Force not set
  12 = download or sha256 verify failed
  13 = stack-side probe (nft queue test) could not run at all
#>

[CmdletBinding()]
param(
    [switch]$Test,
    [switch]$Restore,
    [switch]$DryRun,
    [switch]$Yes,
    [switch]$Force,
    [string]$KernelPath = "",
    [string]$KernelUrl = "",
    [string]$ExpectedSha256 = "",
    [string]$ReleaseTag = "latest",
    [string]$OwnerRepo = "tonylturner/rangerdanger"
)

$ErrorActionPreference = "Stop"

# --- Output helpers (mirror setup.ps1's color scheme) -------------------
function Say($m)    { Write-Host "[+] $m" -ForegroundColor Green }
function Warn($m)   { Write-Host "[!] $m" -ForegroundColor Yellow }
function Die($code, $m) { Write-Host "[x] $m" -ForegroundColor Red; exit $code }
function Banner($m) {
    Write-Host ""
    Write-Host $m -ForegroundColor Cyan
    Write-Host ('-' * $m.Length) -ForegroundColor Cyan
    Write-Host ""
}

$Managed = @{
    KernelDir = Join-Path $env:LOCALAPPDATA "rangerdanger\wsl-kernel"
    KernelFile = "rangerdanger-wsl2-kernel"
    Sha256File = "rangerdanger-wsl2-kernel.sha256"
    WslConfig  = Join-Path $env:USERPROFILE ".wslconfig"
}
$Managed.KernelTarget   = Join-Path $Managed.KernelDir $Managed.KernelFile
$Managed.WslConfigBak   = "$($Managed.WslConfig).bak"
$Managed.WslConfigBakFg = "$($Managed.WslConfig).bak.foreign"
# Forward-slash form is what we write into .wslconfig (PS path quoting
# in .wslconfig is fragile; forward slashes work and are what every
# example online uses).
$Managed.WslConfigKernelValue = ($Managed.KernelTarget -replace '\\','/')

# --- Pre-flight: are we even on Windows + WSL2? ------------------------
function Test-WindowsWsl2Backend {
    if ($env:OS -ne 'Windows_NT') {
        return [pscustomobject]@{ Supported=$false; Reason='not running on Windows' }
    }
    $dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
    if (-not $dockerCmd) {
        return [pscustomobject]@{ Supported=$false; Reason='docker CLI not on PATH' }
    }
    # Probe docker info. Apply the same NativeCommandError dance as
    # setup.ps1 -- docker on WSL2 often writes blkio warnings to stderr.
    $kernel = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker info --format '{{.KernelVersion}}' 2>$null
    }
    if (-not $kernel) {
        return [pscustomobject]@{ Supported=$false; Reason='docker daemon not reachable' }
    }
    if ($kernel -notmatch 'WSL2') {
        return [pscustomobject]@{ Supported=$false; Reason="docker is using a non-WSL2 backend (kernel: $kernel)" }
    }
    return [pscustomobject]@{ Supported=$true; Kernel=$kernel }
}

# --- The actual kernel feature probe -----------------------------------
# Tries to apply a minimal nft rule containing `queue num <N>`. If the
# current kernel has CONFIG_NFT_QUEUE compiled in, this succeeds; if
# not, nft reports "Could not process rule: No such file or directory"
# pointing at the queue token. We prefer running the probe in the
# already-pulled rangerdanger-firewall container if it exists (no
# extra pull). Otherwise we spin up a tiny one-shot Alpine container.
function Test-NftQueueSupported {
    $script = "nft 'add table inet rdprobe; add chain inet rdprobe c { type filter hook output priority 0; }; add rule inet rdprobe c queue num 999'"

    # Try the firewall container first if it already exists -- saves a
    # pull on hosts that already ran setup.ps1.
    $useFirewall = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker inspect -f '{{.State.Running}}' rangerdanger-firewall 2>$null
    }
    if ($useFirewall -eq 'true') {
        $r = & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker exec rangerdanger-firewall sh -c "$script 2>&1" 2>$null
        }
        $rc = $LASTEXITCODE
        # Cleanup attempt -- ignore failure (rule may not have been added).
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker exec rangerdanger-firewall sh -c "nft delete table inet rdprobe 2>/dev/null" *>$null
        }
        if ($rc -eq 0 -and "$r" -notmatch 'No such file or directory') {
            return [pscustomobject]@{ Supported=$true; ProbeSource='rangerdanger-firewall'; Detail="$r" }
        } else {
            return [pscustomobject]@{ Supported=$false; ProbeSource='rangerdanger-firewall'; Detail="$r" }
        }
    }

    # Fallback: tiny one-shot Alpine container with nftables.
    $img = "alpine:3.20"
    Say "Probing kernel via a one-shot $img container (no rangerdanger stack required)..."
    $r = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker run --rm --cap-add NET_ADMIN $img sh -c `
            "apk add --quiet --no-progress nftables 2>/dev/null && $script 2>&1" 2>$null
    }
    $rc = $LASTEXITCODE
    if ($rc -eq 0 -and "$r" -notmatch 'No such file or directory') {
        return [pscustomobject]@{ Supported=$true; ProbeSource='alpine'; Detail="$r" }
    } else {
        return [pscustomobject]@{ Supported=$false; ProbeSource='alpine'; Detail="$r" }
    }
}

# --- INI-aware merge for .wslconfig ------------------------------------
# Reads .wslconfig (or starts fresh if missing), and either updates or
# inserts kernel=<path> inside the [wsl2] section. Preserves every
# other line, comment, and section. Returns the proposed new content
# so the caller can decide whether to write it.
function Get-WslConfigMerged($newKernel) {
    $original = if (Test-Path $Managed.WslConfig) {
        Get-Content $Managed.WslConfig -Raw
    } else { "" }

    # Normalize trailing newline so our edits are predictable.
    if ($original -and -not $original.EndsWith("`n")) { $original += "`n" }

    $lines = if ($original) { $original -split "`r?`n" } else { @() }

    # Find [wsl2] section bounds.
    $wsl2Start = -1; $wsl2End = $lines.Count
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match '^\s*\[wsl2\]\s*$') { $wsl2Start = $i; break }
    }
    if ($wsl2Start -ge 0) {
        for ($j = $wsl2Start + 1; $j -lt $lines.Count; $j++) {
            if ($lines[$j] -match '^\s*\[.+\]\s*$') { $wsl2End = $j; break }
        }
    }

    $existingKernel = $null
    $kernelLineIdx = -1
    if ($wsl2Start -ge 0) {
        for ($k = $wsl2Start + 1; $k -lt $wsl2End; $k++) {
            if ($lines[$k] -match '^\s*kernel\s*=\s*(.+?)\s*$') {
                $existingKernel = $Matches[1].Trim()
                $kernelLineIdx = $k
                break
            }
        }
    }

    $output = New-Object System.Collections.Generic.List[string]
    $newLine = "kernel=$newKernel"

    if ($wsl2Start -ge 0) {
        # Replace or insert kernel= inside the existing [wsl2] section.
        for ($i = 0; $i -lt $lines.Count; $i++) {
            if ($i -eq $kernelLineIdx) {
                $output.Add($newLine) | Out-Null
            } elseif ($i -eq $wsl2End -and $kernelLineIdx -lt 0) {
                # Insert just before the next section header.
                $output.Add($newLine) | Out-Null
                $output.Add($lines[$i]) | Out-Null
            } else {
                $output.Add($lines[$i]) | Out-Null
            }
        }
        if ($kernelLineIdx -lt 0 -and $wsl2End -eq $lines.Count) {
            # [wsl2] was the last section and had no kernel= line.
            $output.Add($newLine) | Out-Null
        }
    } else {
        # No [wsl2] section -- append.
        foreach ($l in $lines) { $output.Add($l) | Out-Null }
        if ($output.Count -gt 0 -and $output[$output.Count - 1] -ne "") {
            $output.Add("") | Out-Null
        }
        $output.Add("[wsl2]") | Out-Null
        $output.Add($newLine) | Out-Null
    }

    [pscustomobject]@{
        ExistingKernel = $existingKernel
        Original       = $original
        Merged         = ($output -join "`n").TrimEnd("`n") + "`n"
    }
}

# --- Download + sha256 verify (network-touching) -----------------------
function Get-ReleaseAssetUrl($owner, $tag, $asset) {
    if ($tag -eq 'latest') {
        "https://github.com/$owner/releases/latest/download/$asset"
    } else {
        "https://github.com/$owner/releases/download/$tag/$asset"
    }
}

function Invoke-DownloadVerified($url, $sha256Url, $expectedSha256, $outPath) {
    $parent = Split-Path -Parent $outPath
    if (-not (Test-Path $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }

    Say "Downloading kernel: $url"
    try {
        Invoke-WebRequest -Uri $url -OutFile $outPath -UseBasicParsing -ErrorAction Stop
    } catch {
        Die 12 "Download failed: $_"
    }
    $size = [math]::Round((Get-Item $outPath).Length / 1MB, 2)
    Say "Downloaded $size MB to $outPath"

    if (-not $expectedSha256 -and $sha256Url) {
        Say "Fetching sha256 from $sha256Url"
        # GitHub serves .sha256 release assets with Content-Type:
        # application/octet-stream, so Invoke-WebRequest's .Content in
        # PS 5.1 returns Byte[], NOT a string. Splitting a Byte[] on
        # '\s+' yields the first byte (the ASCII code, e.g. 54 for '6')
        # rather than the first whitespace-delimited token, and the
        # subsequent sha compare fails with a misleading "expected: 54"
        # error. Download to a temp file and read as text -- avoids
        # the byte/string dispatch entirely and matches what we already
        # do for the kernel binary itself.
        $shaTemp = [System.IO.Path]::GetTempFileName()
        try {
            Invoke-WebRequest -Uri $sha256Url -OutFile $shaTemp -UseBasicParsing -ErrorAction Stop
            $shaRaw = Get-Content $shaTemp -Raw
        } catch {
            Die 12 "sha256 fetch failed: $_"
        } finally {
            Remove-Item $shaTemp -Force -ErrorAction SilentlyContinue
        }
        # File format: <sha256>  <filename>
        $expectedSha256 = (($shaRaw -split '\s+', 2)[0]).Trim().ToLower()
    }
    if (-not $expectedSha256) {
        Warn "No expected sha256 supplied AND no .sha256 sidecar file -- skipping verification."
        Warn "This is supported but not recommended. Use -ExpectedSha256 to lock down the binary."
        return
    }

    $actual = (Get-FileHash -Algorithm SHA256 -Path $outPath).Hash.ToLower()
    if ($actual -ne $expectedSha256.ToLower()) {
        Remove-Item $outPath -Force -ErrorAction SilentlyContinue
        Die 12 "sha256 mismatch:`n  expected: $expectedSha256`n  actual:   $actual`n  Refusing to install."
    }
    Say "sha256 verified: $actual"
}

# --- Restart sequence --------------------------------------------------
function Invoke-WslShutdown {
    Say "Running wsl --shutdown (stops all WSL2 distros + the Docker Desktop VM)..."
    & wsl.exe --shutdown
    if ($LASTEXITCODE -ne 0) { Warn "wsl --shutdown exited $LASTEXITCODE (continuing anyway)" }
}

function Wait-DockerReconnect($maxSecs = 120) {
    Say "Waiting up to ${maxSecs}s for Docker Desktop to reconnect..."
    for ($i = 0; $i -lt $maxSecs; $i++) {
        $k = & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker info --format '{{.KernelVersion}}' 2>$null
        }
        if ($k) {
            Say "Docker reachable again. Kernel now: $k"
            return $k
        }
        Start-Sleep -Seconds 1
        if (($i -gt 0) -and ($i % 10 -eq 0)) { Write-Host "  ...still waiting (${i}s)..." }
    }
    Warn "Docker did not reconnect within ${maxSecs}s. You may need to launch Docker Desktop manually."
    return $null
}

# --- Mode: -Test --------------------------------------------------------
if ($Test) {
    Banner "WSL2 kernel feature probe"
    $env_ = Test-WindowsWsl2Backend
    if (-not $env_.Supported) {
        Say "Skipping: $($env_.Reason)"
        exit 2
    }
    Say "Docker kernel: $($env_.Kernel)"
    $probe = Test-NftQueueSupported
    if ($probe.Supported) {
        Say "nft queue rule applies cleanly via $($probe.ProbeSource) -- no install needed."
        exit 0
    } else {
        Warn "nft queue rule failed via $($probe.ProbeSource):"
        Warn "$($probe.Detail)"
        Warn "Install path needed: .\scripts\install-wsl-kernel.ps1"
        exit 1
    }
}

# --- Mode: -Restore ----------------------------------------------------
if ($Restore) {
    Banner "Restoring stock WSL2 kernel"
    if (Test-Path $Managed.WslConfigBak) {
        Copy-Item -Path $Managed.WslConfigBak -Destination $Managed.WslConfig -Force
        Say "Restored $($Managed.WslConfig) from $($Managed.WslConfigBak)"
    } elseif (Test-Path $Managed.WslConfig) {
        # No .bak (file was created by us into an empty parent dir).
        # Strip just our kernel= line, then check whether anything
        # meaningful is left -- if all that remains is the [wsl2]
        # section header (which we also added) and whitespace, delete
        # the file entirely so we leave $env:USERPROFILE\.wslconfig
        # exactly as the user would have it on a fresh Windows.
        $current = Get-Content $Managed.WslConfig -Raw
        $stripped = $current -replace "(?m)^kernel\s*=\s*$([regex]::Escape($Managed.WslConfigKernelValue))\s*\r?\n?", ""
        if ($stripped -eq $current) {
            Warn "No managed kernel= entry found in $($Managed.WslConfig); nothing to remove."
        } else {
            # Test whether the residual contains any non-empty,
            # non-comment, non-section-header line. If not, the file
            # had nothing in it except our addition.
            $meaningful = $stripped -split "`r?`n" | Where-Object {
                $t = $_.Trim()
                $t -ne "" -and -not $t.StartsWith("#") -and -not $t.StartsWith(";") -and -not ($t -match '^\[.+\]$')
            }
            if (-not $meaningful) {
                Remove-Item $Managed.WslConfig -Force
                Say "Removed $($Managed.WslConfig) (no other config in it)"
            } else {
                Set-Content -Path $Managed.WslConfig -Value $stripped -NoNewline -Encoding ascii
                Say "Removed managed kernel= line from $($Managed.WslConfig); preserved your other [wsl2] keys"
            }
        }
    } else {
        Warn "No .wslconfig present; nothing to restore."
    }
    if (Test-Path $Managed.KernelTarget) {
        Remove-Item $Managed.KernelTarget -Force
        Say "Removed installed kernel: $($Managed.KernelTarget)"
    }
    if (-not $DryRun) {
        Invoke-WslShutdown
        Wait-DockerReconnect 60 | Out-Null
    }
    exit 0
}

# --- Default mode: install ---------------------------------------------
Banner "RangerDanger WSL2 kernel install"

$env_ = Test-WindowsWsl2Backend
if (-not $env_.Supported) {
    Say "Skipping kernel install: $($env_.Reason)"
    exit 2
}
Say "Docker kernel: $($env_.Kernel)"

if (-not $Force) {
    $probe = Test-NftQueueSupported
    if ($probe.Supported) {
        Say "nft queue rule already applies via $($probe.ProbeSource) -- nothing to do."
        Say "(Use -Force to install anyway.)"
        exit 0
    }
    Warn "nft queue rule fails on the current kernel. Proceeding with install."
}

# Acquire the kernel binary.
$kernelSrc = ""
if ($KernelPath) {
    if (-not (Test-Path $KernelPath)) { Die 12 "-KernelPath does not exist: $KernelPath" }
    $kernelSrc = (Resolve-Path $KernelPath).Path
    Say "Using local kernel: $kernelSrc"
    if ($ExpectedSha256) {
        $actual = (Get-FileHash -Algorithm SHA256 -Path $kernelSrc).Hash.ToLower()
        if ($actual -ne $ExpectedSha256.ToLower()) {
            Die 12 "sha256 mismatch on local file:`n  expected: $ExpectedSha256`n  actual:   $actual"
        }
        Say "sha256 verified: $actual"
    }
    # Stage into managed location.
    if (-not (Test-Path $Managed.KernelDir)) { New-Item -ItemType Directory -Path $Managed.KernelDir -Force | Out-Null }
    if ($kernelSrc -ne $Managed.KernelTarget) {
        Copy-Item -Path $kernelSrc -Destination $Managed.KernelTarget -Force
        Say "Staged kernel to $($Managed.KernelTarget)"
    }
} else {
    $url = if ($KernelUrl) { $KernelUrl } else { Get-ReleaseAssetUrl $OwnerRepo $ReleaseTag $Managed.KernelFile }
    $shaUrl = if ($KernelUrl) { "" } else { Get-ReleaseAssetUrl $OwnerRepo $ReleaseTag $Managed.Sha256File }
    Invoke-DownloadVerified $url $shaUrl $ExpectedSha256 $Managed.KernelTarget
}

# Plan the .wslconfig merge.
$merge = Get-WslConfigMerged $Managed.WslConfigKernelValue
if ($merge.ExistingKernel -and $merge.ExistingKernel -ne $Managed.WslConfigKernelValue) {
    if (-not $Force) {
        Warn "$($Managed.WslConfig) already has a kernel= pointing at:"
        Warn "  $($merge.ExistingKernel)"
        Warn "Refusing to overwrite a foreign kernel pointer. Re-run with -Force to back it up to:"
        Warn "  $($Managed.WslConfigBakFg)"
        Warn "and replace it with ours."
        exit 11
    }
    Copy-Item -Path $Managed.WslConfig -Destination $Managed.WslConfigBakFg -Force
    Say "Backed up foreign kernel= configuration to $($Managed.WslConfigBakFg)"
} elseif ($merge.ExistingKernel -eq $Managed.WslConfigKernelValue) {
    Say ".wslconfig already points at our managed kernel."
}

# Confirm with the user unless -Yes.
Banner "About to:"
Write-Host "  1. Write $($Managed.WslConfig) (backup at $($Managed.WslConfigBak))"
Write-Host "     Setting kernel = $($Managed.WslConfigKernelValue)"
Write-Host "  2. Run 'wsl --shutdown' (will stop the Docker Desktop VM and any other WSL2 distros you have running)"
Write-Host "  3. Wait for Docker Desktop to reconnect"
Write-Host "  4. Re-probe the nft queue rule to confirm the fix worked"
Write-Host ""
if ($DryRun) {
    Say "DRY RUN: would write .wslconfig, then run 'wsl --shutdown', then poll. No changes made."
    Write-Host ""
    Write-Host "--- merged .wslconfig that WOULD be written ---"
    Write-Host $merge.Merged
    Write-Host "--- end ---"
    exit 0
}
if (-not $Yes) {
    $ans = Read-Host "Continue? [y/N]"
    if ($ans -notmatch '^(y|yes)$') {
        Say "Aborted by user. No changes made."
        exit 10
    }
}

# Write .wslconfig (with backup).
if (Test-Path $Managed.WslConfig) {
    Copy-Item -Path $Managed.WslConfig -Destination $Managed.WslConfigBak -Force
    Say "Backed up existing $($Managed.WslConfig) to $($Managed.WslConfigBak)"
}
Set-Content -Path $Managed.WslConfig -Value $merge.Merged -NoNewline -Encoding ascii
Say "Wrote $($Managed.WslConfig)"

# Restart.
Invoke-WslShutdown
$newKernel = Wait-DockerReconnect 180
if (-not $newKernel) {
    Warn "Docker did not reconnect in time. Please launch Docker Desktop manually and re-run -Test."
    exit 1
}

# Re-probe.
Banner "Post-install verification"
$probe2 = Test-NftQueueSupported
if ($probe2.Supported) {
    Banner "Success"
    Say "Custom kernel installed and nft queue rule now applies."
    Say "Docker kernel: $newKernel"
    exit 0
} else {
    Warn "Post-install probe still fails:"
    Warn "$($probe2.Detail)"
    Warn "Possible causes:"
    Warn "  - .wslconfig path quoting (check $($Managed.WslConfig) -- WSL2 needs Windows-style paths, but forward slashes are usually fine)"
    Warn "  - The downloaded kernel does not actually have CONFIG_NFT_QUEUE=y (check sha256 against the release page)"
    Warn "  - Docker Desktop is still using a stale VM (try restarting Docker Desktop manually)"
    Warn "Run with -Restore to undo, or inspect with -Test."
    exit 1
}
