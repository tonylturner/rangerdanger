# Windows PowerShell 5.1: exact IDs, ordinal order, UTF-8 without BOM and LF.
function Get-AvailablePackages([string]$Root) {
    $ids = [string[]]@(Get-ChildItem -LiteralPath (Join-Path $Root 'lab-definitions/packages') -Directory |
        Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'package.yml') } |
        ForEach-Object { $_.Name })
    [Array]::Sort($ids, [StringComparer]::Ordinal)
    $ids
}

function ConvertTo-PackageSelection([string]$Root, [string[]]$Values) {
    $available = @(Get-AvailablePackages $Root)
    $set = New-Object 'Collections.Generic.SortedSet[string]' ([StringComparer]::Ordinal)
    foreach ($value in $Values) {
        foreach ($id in $value.Split(',')) {
            if ($id -cnotmatch '^[a-z0-9-]+$' -or $id -cnotin $available) { throw "Invalid or unknown package '$id'." }
            $null = $set.Add($id)
        }
    }
    if (-not $set.Count) { throw 'Select at least one package.' }
    @($set)
}

function Read-InstalledPackages([string]$Root) {
    $file = Join-Path $Root 'data/installed-packages'
    $fix = "Invalid or missing $file. Repair the record, or remove it and run .\setup.ps1 -Packages <ids>."
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw $fix }
    $text = [Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($file))
    if ($text -cnotmatch '^rangerdanger-installed-packages-v1\n([a-z0-9-]+\n)+\z') { throw $fix }
    $ids = @($text.Split("`n") | Select-Object -Skip 1 | Where-Object { $_ })
    try { $normalized = @(ConvertTo-PackageSelection $Root $ids) } catch { throw $fix }
    if (($ids -join "`n") -cne ($normalized -join "`n")) { throw $fix }
    $ids
}

function Write-InstalledPackages([string]$Root, [string[]]$Ids) {
    $file = Join-Path $Root 'data/installed-packages'
    $null = New-Item -ItemType Directory -Path (Split-Path -Parent $file) -Force
    $tmp = "$file.tmp.$([Guid]::NewGuid().ToString('N'))"
    try {
        [IO.File]::WriteAllText($tmp, "rangerdanger-installed-packages-v1`n" + ($Ids -join "`n") + "`n", (New-Object Text.UTF8Encoding $false))
        if (Test-Path -LiteralPath $file) { [IO.File]::Replace($tmp, $file, [NullString]::Value) }
        else { [IO.File]::Move($tmp, $file) }
    } finally {
        if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Force }
    }
}

function Get-InstalledEnvValue([string]$Root, [string]$Key) {
    $file = Join-Path $Root '.env'
    $values = @([IO.File]::ReadAllLines($file) | Where-Object { $_.StartsWith("$Key=") } | ForEach-Object { $_.Substring($Key.Length + 1) })
    if ($values.Count -ne 1 -or -not $values[0]) { throw "Missing or ambiguous $Key in $file; run setup first." }
    $values[0]
}
