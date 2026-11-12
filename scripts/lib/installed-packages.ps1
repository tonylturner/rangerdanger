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

function Assert-RuntimePackageSelected([string]$Root, [string[]]$Ids) {
    $file = Join-Path $Root 'backend/data/range.json'
    if (-not (Test-Path -LiteralPath $file)) {
        # A dangling link is not an absent runtime record.
        if (-not (Get-Item -LiteralPath $file -Force -ErrorAction SilentlyContinue)) { return }
    }
    try {
        $utf8 = New-Object Text.UTF8Encoding $false, $true
        $text = $utf8.GetString([IO.File]::ReadAllBytes($file))
        # ConvertFrom-Json accepts comments, trailing commas and duplicate keys
        # on some hosts. Check strict JSON grammar first, including unique keys.
        # The mutable cursor is shared by recursive calls (PowerShell 5.1 too).
        $pattern = '\G(?:[ \t\r\n]+|"(?:[^"\\\x00-\x1f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false|null|[{}\[\]:,])'
        $tokens = New-Object 'Collections.Generic.List[string]'
        $end = 0
        foreach ($match in [regex]::Matches($text, $pattern)) {
            $end += $match.Length
            if ($match.Value -cnotmatch '^[ \t\r\n]+$') { $tokens.Add($match.Value) }
        }
        if ($end -ne $text.Length) { throw 'Invalid JSON token.' }
        $cursor = @{ Index = 0 }
        function Read-RuntimeJsonValue([int]$Depth) {
            if ($Depth -gt 64 -or $cursor.Index -ge $tokens.Count) { throw 'Incomplete runtime JSON.' }
            $token = $tokens[$cursor.Index++]
            if ($token -eq '{' -or $token -eq '[') {
                $close = if ($token -eq '{') { '}' } else { ']' }
                $keys = New-Object 'Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
                if ($cursor.Index -lt $tokens.Count -and $tokens[$cursor.Index] -eq $close) {
                    $cursor.Index++
                    return
                }
                while ($true) {
                    if ($token -eq '{') {
                        if ($cursor.Index -ge $tokens.Count -or -not $tokens[$cursor.Index].StartsWith('"')) { throw 'Expected JSON key.' }
                        $key = ConvertFrom-Json -InputObject $tokens[$cursor.Index++]
                        if (-not $keys.Add($key)) { throw 'Duplicate JSON key.' }
                        if ($cursor.Index -ge $tokens.Count -or $tokens[$cursor.Index++] -ne ':') { throw 'Expected JSON colon.' }
                    }
                    Read-RuntimeJsonValue ($Depth + 1)
                    if ($cursor.Index -ge $tokens.Count) { throw 'Incomplete runtime JSON.' }
                    $next = $tokens[$cursor.Index++]
                    if ($next -eq $close) { return }
                    if ($next -ne ',') { throw 'Expected JSON comma.' }
                }
            }
            if ($token -in @('}', ']', ':', ',')) { throw 'Expected JSON value.' }
        }
        Read-RuntimeJsonValue 0
        if ($cursor.Index -ne $tokens.Count -or $tokens[0] -ne '{') { throw 'Expected one runtime object.' }
        $record = ConvertFrom-Json -InputObject $text
        if ('package' -cnotin @($record.PSObject.Properties.Name) -or
            $record.package -isnot [string] -or $record.package -cnotmatch '^[a-z0-9-]+\z') {
            throw 'Missing or invalid runtime package.'
        }
        $package = $record.package
    } catch {
        throw "Invalid runtime record $file. Repair the runtime record before rerunning setup."
    }
    if ($package -cnotin $Ids) {
        throw "The range runtime record names $package, which -Packages drops. Switch the range to a selected package first (the range switcher, or POST /api/range), then re-run."
    }
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
