<#
.SYNOPSIS
Thin Windows client for the backend validation report.

.DESCRIPTION
Requests the report for the policy currently running in the ready range.
An applied plan is judged against its plan proof and compared with Hardened
acceptance. This client never applies a policy or runs probes itself.

.PARAMETER Out
Write the report markdown to this file. Default: stdout.

.EXAMPLE
.\scripts\validation-report.ps1
.\scripts\validation-report.ps1 -Out report.md

.NOTES
summary.result determines the exit code and reflects Hardened acceptance,
not plan proof. Exit 0 means PASS; non-zero means the report failed or could
not be retrieved. ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [string]$Out = ""
)

$ErrorActionPreference = "Continue"

$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RootDir

$Api = if ($env:RANGERDANGER_API) { $env:RANGERDANGER_API } else { 'http://localhost:8088' }

function Write-Status([string]$Message) {
    [Console]::Error.WriteLine("validation-report: $Message")
}

# Keep the shared range helper while accepting any selected ready package.
. (Join-Path $PSScriptRoot 'lib\range.ps1')
$range = Get-RangeStatus $Api
if (-not $range -or $range.phase -ne 'ready' -or
    [string]::IsNullOrWhiteSpace([string]$range.package)) {
    Write-Status "range is not ready -- GET $Api/api/range: $(Format-RangeStatus $range)"
    exit 1
}

Write-Status "requesting validation report from $Api/api/firewall/validation-report"
try {
    $report = Invoke-RestMethod -Uri "$Api/api/firewall/validation-report" `
        -Method POST -TimeoutSec 180 -ErrorAction Stop
} catch {
    Write-Status "report request failed: $_"
    exit 1
}

if (-not $report -or
    $report.markdown -isnot [string] -or
    [string]::IsNullOrEmpty($report.markdown) -or
    $report.summary.result -notin @('PASS', 'FAIL')) {
    Write-Status "invalid validation-report response: expected summary.result PASS/FAIL and non-empty markdown"
    exit 1
}

$markdown = [string]$report.markdown
$result = [string]$report.summary.result
if ($Out) {
    try {
        [System.IO.File]::WriteAllText(
            $Out, $markdown, [System.Text.UTF8Encoding]::new($false))
    } catch {
        Write-Status "could not write report to ${Out}: $_"
        exit 1
    }
    Write-Status "report written to $Out"
} else {
    [Console]::Out.Write($markdown)
}

if ($result -ne 'PASS') {
    Write-Status "backend report result: $result"
    exit 1
}
exit 0
