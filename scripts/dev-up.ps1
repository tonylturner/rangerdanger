<#
.SYNOPSIS
RangerDanger dev-up -- Windows sibling of scripts/dev-up.sh.

.DESCRIPTION
Windows sibling of dev-up.sh: setup -FromSource -SkipFirewallGate.
Builds the platform and every package, then starts the range through the
backend. Named options pass to setup, including -Package. Skips the
workshop apply/reset gate so dev bring-up preserves policy.

.NOTES
Keep this file ASCII-only and BOM-free. Windows PowerShell 5.1 reads
PowerShell scripts as CP1252 absent a BOM, which mangles non-ASCII
characters (em-dash, box-drawing) and produces parser errors that
mention lines hundreds of lines away from the actual source. See
setup.ps1.

(Avoid lines that start with a literal dot followed by a word inside
comment-based help -- the parser treats those as help-keyword lines,
which silently breaks the whole help block for the script.)
#>

[CmdletBinding()]
param(
    [string]$Version,
    [string]$Package,
    [switch]$CheckOnly,
    [switch]$SkipKernelFix
)

$ErrorActionPreference = "Stop"

& (Join-Path (Split-Path -Parent $PSScriptRoot) 'setup.ps1') -FromSource -SkipFirewallGate @PSBoundParameters
exit $LASTEXITCODE
