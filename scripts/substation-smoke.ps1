# Field-device -> RTAC -> OpenDSS -> API smoke, matching substation-smoke.sh.
# Standalone or dot-source and call Invoke-SubstationSmoke. US package only.
# ASCII-only, BOM-free; Windows PowerShell 5.1 compatible.
[CmdletBinding()]
param([string]$Api = $(if ($env:API) { $env:API } else { 'http://localhost:8088' }))

. "$PSScriptRoot/lib/docker-bounded.ps1"

function Invoke-SubstationSmoke([string]$Api = 'http://localhost:8088') {
    $sub = $Api.TrimEnd('/') + '/api/substation'
    $context = @{ Failed = $false; Paused = $false }
    $physicsContainer = 'rangerdanger-opendss-sim'
    function Assert-Substation($Condition, [string]$Pass, [string]$Failure) {
        if ($Condition) { Write-Host "  [+] $Pass" }
        else { Write-Host "  [x] $Failure"; $context.Failed = $true }
    }
    function Test-SubstationBoolean($Value, [bool]$Expected) {
        return $Value -is [bool] -and $Value -eq $Expected
    }
    function Get-SubstationState {
        Invoke-RestMethod -Uri "$sub/state" -TimeoutSec 5 -UseBasicParsing -ErrorAction Stop
    }
    function Send-SubstationCommand([string]$Device, [string]$Command, $Value = $null) {
        $body = @{ command = $Command }
        if ($null -ne $Value) { $body.value = $Value }
        $null = Invoke-RestMethod -Uri "$sub/command/$Device" -Method Post -ContentType 'application/json' `
            -Body ($body | ConvertTo-Json -Compress) -TimeoutSec 5 -UseBasicParsing -ErrorAction Stop
    }
    function Wait-Substation([scriptblock]$Predicate, [int]$Seconds) {
        for ($i = 0; $i -lt $Seconds; $i++) {
            try { if (& $Predicate (Get-SubstationState)) { return $true } } catch { }
            Start-Sleep -Seconds 1
        }
        return $false
    }
    function Invoke-PhysicsDocker([string[]]$DockerArgs) {
        $run = Invoke-DockerBounded $DockerArgs 5
        if (-not $run -or $run.TimedOut -or $run.ExitCode -ne 0) { throw "docker $DockerArgs failed or timed out" }
        return $run.StdOut
    }
    function Wait-PhysicsHealthy {
        for ($i = 0; $i -lt 30; $i++) {
            try {
                if ((Invoke-PhysicsDocker @('inspect', '-f', '{{.State.Health.Status}}', $physicsContainer)).Trim() -ceq 'healthy') {
                    return $true
                }
            } catch { }
            Start-Sleep -Seconds 1
        }
        return $false
    }
    function Resume-Physics {
        try { $null = Invoke-PhysicsDocker @('unpause', $physicsContainer); $context.Paused = $false }
        catch {
            if ($context.Paused) { Assert-Substation $false '' "Could not unpause $physicsContainer" }
        }
    }

    # Refuse another package or a range mid-switch before any mutations.
    $range = Invoke-RestMethod -Uri ($Api.TrimEnd('/') + '/api/range') -TimeoutSec 10 -UseBasicParsing -ErrorAction Stop
    if ($range.package -cne 'us-dnp3-substation' -or $range.phase -cne 'ready') {
        throw 'Range is not us-dnp3-substation ready'
    }
    Write-Host '  [+] range us-dnp3-substation ready'
    try {
        $state = Get-SubstationState
        foreach ($device in 'relay', 'recloser', 'regulator', 'capbank') {
            $present = $null -ne $state.devices.$device -and -not (Test-SubstationBoolean $state.devices.$device $false)
            Assert-Substation $present "devices.$device present" "devices.$device missing"
        }
        foreach ($command in 'clear_fault', 'unlock', 'close') { Send-SubstationCommand relay $command }
        foreach ($command in 'clear_fault', 'reset_lockout', 'enable_reclose', 'close') { Send-SubstationCommand recloser $command }
        Send-SubstationCommand regulator set_tap 0
        Send-SubstationCommand regulator set_auto
        Send-SubstationCommand capbank reset_lockout
        Start-Sleep -Seconds 5
        $state = Get-SubstationState
        Assert-Substation (Test-SubstationBoolean $state.physics.stale $false) 'physics is fresh' 'physics is stale after baseline reset'
        Assert-Substation (Test-SubstationBoolean $state.electrical.converged $true) 'electrical solve converged' 'electrical solve is not converged'
        $loadsEnergized = (Test-SubstationBoolean $state.electrical.critical_load_energized $true) -and
            (Test-SubstationBoolean $state.electrical.general_load_energized $true)
        Assert-Substation $loadsEnergized 'solver reports critical and general loads energized' `
            'solver does not report both critical and general loads energized'
        $recent = $false
        try {
            # Native JSON deserialization can return DateTime, not a string.
            $stamp = $state.electrical.solved_at
            if ($stamp -is [DateTime]) {
                $stamp = $stamp.ToUniversalTime().ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", [Globalization.CultureInfo]::InvariantCulture)
            }
            $stamp = [string]$stamp -creplace '\.[0-9]+Z$', 'Z'
            $solved = [DateTimeOffset]::ParseExact($stamp, "yyyy-MM-dd'T'HH:mm:ss'Z'",
                [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal)
            $age = ([DateTimeOffset]::UtcNow - $solved).TotalSeconds
            $recent = $age -ge -10 -and $age -le 10
        } catch { }
        Assert-Substation $recent 'electrical solve timestamp is within 10s' 'electrical solve timestamp is missing, invalid, or older than 10s'

        Send-SubstationCommand capbank set_manual
        Send-SubstationCommand capbank switch_out
        Start-Sleep -Seconds 4
        $pfOut = (Get-SubstationState).electrical.power_factor
        $capOut = (Get-SubstationState).devices.capbank.switched_in
        Send-SubstationCommand capbank switch_in
        Start-Sleep -Seconds 4
        $state = Get-SubstationState
        $pfIn = $state.electrical.power_factor
        Assert-Substation (Test-SubstationBoolean $capOut $false) 'manual switch_out holds cap out' "cap stayed in after manual switch_out (cap_out=$capOut)"
        Assert-Substation ((Test-SubstationBoolean $state.devices.capbank.switched_in $true) -and (Test-SubstationBoolean $state.electrical.capbank_switched_in $true)) `
            'manual switch_in echoes through OpenDSS' 'switch_in not reflected in device and electrical results'
        $outNumber = 0.0; $inNumber = 0.0
        $numeric = [double]::TryParse([string]$pfOut, [Globalization.NumberStyles]::Float,
            [Globalization.CultureInfo]::InvariantCulture, [ref]$outNumber) -and
            [double]::TryParse([string]$pfIn, [Globalization.NumberStyles]::Float,
            [Globalization.CultureInfo]::InvariantCulture, [ref]$inNumber)
        Assert-Substation ($numeric -and $inNumber -gt $outNumber) "switch-in raises power factor ($pfOut -> $pfIn)" `
            "power factor did not rise on switch-in ($pfOut -> $pfIn)"

        Send-SubstationCommand capbank switch_out
        Start-Sleep -Seconds 2
        Send-SubstationCommand capbank set_auto
        Assert-Substation (Wait-Substation { param($s) Test-SubstationBoolean $s.devices.capbank.switched_in $true } 35) `
            'cap AUTO re-engages to correct power factor' 'cap AUTO did not switch in within 35s'

        Send-SubstationCommand regulator set_manual
        foreach ($i in 1..3) { Send-SubstationCommand regulator lower_tap }
        Assert-Substation (Wait-Substation { param($s) $null -ne $s.electrical.regulator_tap -and $s.electrical.regulator_tap -le -2 } 10) `
            'manual tap sag registered' 'manual tap sag did not register'
        $tapLow = (Get-SubstationState).electrical.regulator_tap
        Send-SubstationCommand regulator set_auto
        Assert-Substation (Wait-Substation { param($s) $null -ne $tapLow -and $null -ne $s.electrical.regulator_tap -and $s.electrical.regulator_tap -gt $tapLow } 20) `
            "regulator AVR raises tap from $tapLow back toward setpoint" "regulator AVR did not correct the sag (tap stuck at $tapLow)"

        try {
            $null = Invoke-PhysicsDocker @('pause', $physicsContainer)
            $context.Paused = $true
        } catch { Assert-Substation $false '' "Could not pause $physicsContainer" }
        if ($context.Paused) {
            Assert-Substation (Wait-Substation { param($s) Test-SubstationBoolean $s.physics.stale $true } 15) `
                'physics becomes stale while OpenDSS is paused' 'physics did not become stale within 15s while OpenDSS was paused'
        }
        Resume-Physics
        Assert-Substation (Wait-PhysicsHealthy) 'OpenDSS container healthy again after unpause' 'OpenDSS container did not become healthy within 30s after unpause'
        Assert-Substation (Wait-Substation { param($s) Test-SubstationBoolean $s.physics.stale $false } 15) `
            'physics freshness recovers after OpenDSS resumes' 'physics did not recover within 15s after OpenDSS resumed'
    } finally {
        $wasPaused = $context.Paused
        Resume-Physics
        if ($wasPaused) {
            Assert-Substation (Wait-PhysicsHealthy) 'OpenDSS healthy during cleanup' 'OpenDSS container did not become healthy within 30s during cleanup'
        }
    }
    if ($context.Failed) { throw 'substation-smoke FAIL' }
    Write-Host '  substation-smoke PASS'
}

if ($MyInvocation.InvocationName -ne '.') {
    $ErrorActionPreference = 'Stop'
    try { Invoke-SubstationSmoke $Api; exit 0 }
    catch { Write-Host "  [x] $_"; exit 1 }
}
