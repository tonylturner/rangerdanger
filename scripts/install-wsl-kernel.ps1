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
1 if it cannot, 2 if this is not Windows + WSL2, and 13 if no probe
could run (no running firewall container, no local firewall image, and
the Alpine fallback could not fetch nftables -- usually no network).
setup.ps1 uses it to decide whether to call install mode.

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
and assumes the binary is correct when -ExpectedSha256 is omitted.
Downloaded kernels always require a valid sha256 from -ExpectedSha256
or the release sidecar; otherwise installation fails. Useful for a
deliberately selected local build from wsl-kernel/.

.PARAMETER KernelUrl
Explicit download URL. Overrides the per-release computed URL.

.PARAMETER ExpectedSha256
Explicit sha256 to verify the downloaded kernel against. A downloaded
kernel must have a valid hash here or in its release sidecar; otherwise
installation fails with exit code 12. Required when using -KernelUrl
with a non-GitHub-release source (no auto sha256 file to fetch).

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
  13 = the nft queue probe could not run at all, so kernel support is
       unknown (see -Test). Install mode exits 13 before changing anything
       when the pre-install probe cannot run, and after installing when
       the post-install probe cannot run.
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

# --- Bounded docker CLI calls -------------------------------------------
# After `wsl --shutdown`, Docker Desktop (seen on 4.34.3) can fail to bring
# its VM back ("running wsl-bootstrap: exit status 1") and sit on a
# Restart/Quit error dialog. In that state docker CLI calls do not fail --
# they block forever, so a bare `& docker ...` hangs the caller no matter
# what wait budget it prints. Every docker call in this script therefore
# runs as a child process that is killed after $TimeoutSec. stderr is
# captured (blkio warnings on WSL2) but never surfaces as a
# NativeCommandError.

# ProcessStartInfo.Arguments is a single command line (Windows PowerShell
# 5.1 has no ArgumentList), so quote each argument by the
# CommandLineToArgvW rules the docker CLI parses it with.
function Join-NativeArguments([string[]]$ArgList) {
    ($ArgList | ForEach-Object {
        if ($_ -ne '' -and $_ -notmatch '[\s"]') { $_ }
        else { '"' + (($_ -replace '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"' }
    }) -join ' '
}

# Returns $null when the docker CLI is not on PATH; otherwise ExitCode,
# StdOut, StdErr and TimedOut (ExitCode -1 when the call was killed).
function Invoke-DockerBounded([string[]]$DockerArgs, [int]$TimeoutSec) {
    $dockerCmd = Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $dockerCmd) { return $null }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $dockerCmd.Source
    $psi.Arguments = Join-NativeArguments $DockerArgs
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    $p = [System.Diagnostics.Process]::Start($psi)
    $stdout = $p.StandardOutput.ReadToEndAsync()
    $stderr = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($TimeoutSec * 1000)) {
        try { $p.Kill() } catch { }
        return [pscustomobject]@{ ExitCode=-1; StdOut=''; StdErr=''; TimedOut=$true }
    }
    [pscustomobject]@{ ExitCode=$p.ExitCode; StdOut=$stdout.Result; StdErr=$stderr.Result; TimedOut=$false }
}

# Trimmed `docker info --format` output, or $null on timeout / non-zero
# exit / empty output.
function Get-DockerInfoBounded([string]$Format, [int]$TimeoutSec = 10) {
    $r = Invoke-DockerBounded @('info', '--format', $Format) $TimeoutSec
    if (-not $r -or $r.TimedOut -or $r.ExitCode -ne 0) { return $null }
    $text = $r.StdOut.Trim()
    if ($text) { return $text }
    return $null
}

# --- Pre-flight: are we even on Windows + WSL2? ------------------------
function Test-WindowsWsl2Backend {
    if ($env:OS -ne 'Windows_NT') {
        return [pscustomobject]@{ Supported=$false; Reason='not running on Windows' }
    }
    $dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
    if (-not $dockerCmd) {
        return [pscustomobject]@{ Supported=$false; Reason='docker CLI not on PATH' }
    }
    $kernel = Get-DockerInfoBounded '{{.KernelVersion}}' 30
    if (-not $kernel) {
        return [pscustomobject]@{ Supported=$false; Reason='docker daemon not reachable (if Docker Desktop shows an error dialog, click Restart)' }
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
# pointing at the queue token. Probe sources, in order:
#   1. the running rangerdanger-firewall container (lab already up);
#   2. a one-shot container from the local containd firewall image with
#      no network -- the SSD bundle carries this image, so an offline
#      laptop can probe once setup.ps1 -FromTarballs has loaded it;
#   3. only when that image is absent, a one-shot Alpine container that
#      installs nftables (needs network for the image and apk).
# Result is Supported, Missing (nft ran and the kernel rejected the rule)
# or Unknown (no probe could run, or it ran without NET_ADMIN). Unknown is
# never reported as Missing.

# Keep in sync with the firewall service image in docker-compose.release.yml.
$FirewallImage = 'ghcr.io/tonylturner/containd:latest'
$AlpineImage   = 'alpine:3.20'
# The trailing rdprobe-rc marker proves nft itself ran; without it the
# probe container could not start or had no nft. The table is deleted in
# the same shell so the queue rule exists only for that instant.
$NftProbeScript = 'command -v nft >/dev/null 2>&1 || { echo rdprobe-nonft; exit 90; }; ' +
    'nft ''add table inet rdprobe; add chain inet rdprobe c { type filter hook output priority 0; }; add rule inet rdprobe c queue num 999'' 2>&1; ' +
    'rc=$?; nft delete table inet rdprobe >/dev/null 2>&1; echo rdprobe-rc=$rc'

function ConvertTo-NftProbeResult($Run, [string]$Source) {
    $result = 'Unknown'
    if (-not $Run) {
        $detail = 'docker CLI not on PATH'
    } elseif ($Run.TimedOut) {
        $detail = 'docker did not answer in time'
    } else {
        $out = ("$($Run.StdOut)`n$($Run.StdErr)").Trim()
        if ($out -match 'rdprobe-rc=(\d+)') {
            $rc = [int]$Matches[1]
            $detail = ($out -replace 'rdprobe-rc=\d+', '').Trim()
            if ($rc -eq 0 -and $detail -notmatch 'No such file or directory') {
                $result = 'Supported'
            } elseif ($detail -notmatch 'Operation not permitted') {
                # Permission errors mean the probe lacked NET_ADMIN, not
                # that the kernel lacks the feature.
                $result = 'Missing'
            }
        } else {
            $detail = "probe could not run (docker exit $($Run.ExitCode)): $out"
        }
    }
    [pscustomobject]@{ Result=$result; ProbeSource=$Source; Detail=$detail }
}

function Test-NftQueueSupported {
    $state = Invoke-DockerBounded @('inspect', '-f', '{{.State.Running}}', 'rangerdanger-firewall') 15
    if ($state -and $state.ExitCode -eq 0 -and $state.StdOut.Trim() -eq 'true') {
        $run = Invoke-DockerBounded @('exec', 'rangerdanger-firewall', 'sh', '-c', $NftProbeScript) 60
        $probe = ConvertTo-NftProbeResult $run 'rangerdanger-firewall'
        if ($probe.Result -ne 'Unknown') { return $probe }
        Warn "Probe in the running rangerdanger-firewall container failed: $($probe.Detail)"
    }

    $image = Invoke-DockerBounded @('image', 'inspect', '--format', '{{.Id}}', $FirewallImage) 15
    if ($image -and $image.ExitCode -eq 0) {
        Say "Probing kernel via a one-shot $FirewallImage container (no network needed)..."
        $run = Invoke-DockerBounded @('run', '--rm', '--network', 'none', '--cap-add', 'NET_ADMIN',
            '--entrypoint', 'sh', $FirewallImage, '-c', $NftProbeScript) 120
        return ConvertTo-NftProbeResult $run $FirewallImage
    }

    Say "Probing kernel via a one-shot $AlpineImage container (needs network for the image and nftables)..."
    $alpineScript = 'apk add --quiet --no-progress nftables >/dev/null 2>&1 || { echo rdprobe-noapk; exit 91; }; ' + $NftProbeScript
    $run = Invoke-DockerBounded @('run', '--rm', '--cap-add', 'NET_ADMIN', $AlpineImage, 'sh', '-c', $alpineScript) 180
    return ConvertTo-NftProbeResult $run 'alpine'
}

# Shared wording for an Unknown probe result.
function Write-ProbeUnknown($probe) {
    Warn "Could not probe the WSL2 kernel via $($probe.ProbeSource): $($probe.Detail)"
    if ($probe.ProbeSource -eq 'alpine') {
        Warn "$FirewallImage is not loaded, and the Alpine fallback needs network"
        Warn "to fetch its image and nftables."
    }
    Warn "CONFIG_NFT_QUEUE support is UNKNOWN, not missing."
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

    # Function-scoped: Windows PowerShell 5.1 draws a per-chunk progress
    # bar for Invoke-WebRequest -OutFile that can stall this download for
    # minutes. The assignment is restored when the function returns.
    $ProgressPreference = 'SilentlyContinue'
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
        Remove-Item $outPath -Force -ErrorAction SilentlyContinue
        Die 12 "No expected sha256 supplied and no .sha256 sidecar hash was available. Refusing to install an unverified kernel."
    }
    if ($expectedSha256 -notmatch '^[0-9a-fA-F]{64}$') {
        Remove-Item $outPath -Force -ErrorAction SilentlyContinue
        Die 12 "The expected sha256 is missing or invalid. Refusing to install an unverified kernel."
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

# Budget is wall-clock: each probe is bounded (Get-DockerInfoBounded), so a
# wedged Docker Desktop can no longer stretch "up to N s" into forever. The
# budget is generous because recovery may need the user to click Restart.
function Wait-DockerReconnect($maxSecs = 120) {
    Say "Waiting up to ${maxSecs}s for Docker Desktop to reconnect..."
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $hinted = $false
    $nextNote = 30
    while ($sw.Elapsed.TotalSeconds -lt $maxSecs) {
        $k = Get-DockerInfoBounded '{{.KernelVersion}}' 10
        if ($k) {
            Say "Docker reachable again. Kernel now: $k"
            return $k
        }
        $t = [int]$sw.Elapsed.TotalSeconds
        if ((-not $hinted) -and ($t -ge 45)) {
            Warn "Docker Desktop has not come back yet. After 'wsl --shutdown' it can show an"
            Warn "error dialog: 'A WSL distro Docker Desktop relies on has exited unexpectedly'."
            Warn "If you see it, click Restart. No dialog? Quit Docker Desktop from the tray"
            Warn "icon and start it again. This script keeps waiting and continues by itself."
            $hinted = $true
        }
        if ($t -ge $nextNote) {
            Write-Host "  ...still waiting (${t}s)..."
            $nextNote += 30
        }
        Start-Sleep -Seconds 2
    }
    Warn "Docker did not reconnect within ${maxSecs}s."
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
    switch ($probe.Result) {
        'Supported' {
            Say "nft queue rule applies cleanly via $($probe.ProbeSource) -- no install needed."
            exit 0
        }
        'Missing' {
            Warn "nft queue rule failed via $($probe.ProbeSource):"
            Warn "$($probe.Detail)"
            Warn "Install path needed: .\scripts\install-wsl-kernel.ps1"
            exit 1
        }
        default {
            Write-ProbeUnknown $probe
            Warn "Load or pull the lab images (setup.ps1 does both), then re-run -Test."
            exit 13
        }
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
        Wait-DockerReconnect 300 | Out-Null
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
    if ($probe.Result -eq 'Supported') {
        Say "nft queue rule already applies via $($probe.ProbeSource) -- nothing to do."
        Say "(Use -Force to install anyway.)"
        exit 0
    }
    if ($probe.Result -eq 'Unknown') {
        Write-ProbeUnknown $probe
        Warn "Nothing was changed. Re-run after the lab images are loaded or pulled,"
        Warn "or pass -Force to install without probing."
        exit 13
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
    } else {
        # The one legitimate unverified case: a kernel the operator built from
        # wsl-kernel/ and selected by hand. Every downloaded kernel, and every
        # kernel a staged SSD bundle supplies, carries a checksum or fails.
        Warn "No -ExpectedSha256 for $kernelSrc -- installing it without verification."
        Warn "Only do this for a kernel you built yourself from wsl-kernel/."
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
$newKernel = Wait-DockerReconnect 600
if (-not $newKernel) {
    Warn "The kernel is installed, but Docker Desktop did not come back in time."
    Warn "Restart Docker Desktop, wait for it to show 'Engine running', then re-run"
    Warn ".\setup.ps1 (it detects the installed kernel and carries on from here)."
    exit 1
}

# Re-probe.
Banner "Post-install verification"
$probe2 = Test-NftQueueSupported
if ($probe2.Result -eq 'Supported') {
    Banner "Success"
    Say "Custom kernel installed and nft queue rule now applies."
    Say "Docker kernel: $newKernel"
    exit 0
} elseif ($probe2.Result -eq 'Unknown') {
    Write-ProbeUnknown $probe2
    Warn "The kernel is installed (Docker kernel: $newKernel) but could not be verified."
    Warn "Re-run with -Test once the lab images are loaded or pulled."
    exit 13
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
