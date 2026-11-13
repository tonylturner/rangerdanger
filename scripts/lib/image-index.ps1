# Windows PowerShell 5.1, LF/no BOM index; no Python dependency.
function Read-ImageIndex([string]$Directory, [string]$Architecture, [string]$Version,
    [string[]]$Selected, [string[]]$Required) {
    $Directory = (Get-Item -LiteralPath $Directory -ErrorAction Stop).FullName
    $index = Join-Path $Directory "image-index-$Architecture.txt"
    if (-not (Test-Path -LiteralPath $index -PathType Leaf)) {
        throw "Re-stage with rangerdanger-image-index-v1; monolithic bundles are not supported ($index missing)."
    }
    if ([IO.File]::ReadAllText((Join-Path $Directory '.version')).TrimEnd("`n") -cne $Version) { throw 'Bundle VERSION mismatch.' }
    $text = [Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($index))
    if (-not $text.EndsWith("`n") -or $text.Contains("`r") -or $text.Contains([char]0)) { throw 'Malformed image index.' }
    $lines = $text.Split("`n")
    if ($lines[0] -cne 'rangerdanger-image-index-v1') { throw 'Bundle requires rangerdanger-image-index-v1.' }
    if ($lines[1] -cne "version`t$Version") { throw 'Index VERSION mismatch.' }
    if ($lines[2] -cne "arch`t$Architecture") { throw 'Index architecture mismatch.' }
    $byImage = New-Object 'Collections.Generic.Dictionary[string,string]' ([StringComparer]::Ordinal)
    $byPath = New-Object 'Collections.Generic.Dictionary[string,string]' ([StringComparer]::Ordinal)
    $found = New-Object 'Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    $actual = New-Object 'Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    $archives = New-Object 'Collections.Generic.SortedSet[string]' ([StringComparer]::Ordinal)
    foreach ($line in ($lines | Select-Object -Skip 3 | Select-Object -SkipLast 1)) {
        $row = $line.Split("`t")
        if ($row.Count -ne 4) { throw 'Malformed image index row.' }
        $owner, $image, $path, $hash = $row
        if ($owner -cnotmatch '^[a-z0-9-]+$' -or $image -match '\s' -or -not $image -or $hash -cnotmatch '^[0-9a-f]{64}$') { throw 'Invalid image index row.' }
        if (-not $path -or $path -match '^[\/]|[\\:]') { throw "Unsafe index path: $path" }
        $cursor = $Directory
        foreach ($part in $path.Split('/')) {
            if ($part -in @('', '.', '..')) { throw "Unsafe index path: $path" }
            $cursor = Join-Path $cursor $part
            if ((Test-Path -LiteralPath $cursor) -and ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Symlink in index path: $path" }
        }
        if (($byImage.ContainsKey($image) -and $byImage[$image] -cne "$path`t$hash") -or
            ($byPath.ContainsKey($path) -and $byPath[$path] -cne "$image`t$hash")) { throw 'Conflicting image index mapping.' }
        $byImage[$image] = "$path`t$hash"
        $byPath[$path] = "$image`t$hash"
        if ($owner -ceq 'platform' -or $owner -cin $Selected) {
            $null = $found.Add($owner)
            if (-not $archives.Contains($cursor) -and (-not (Test-Path -LiteralPath $cursor -PathType Leaf) -or (Get-FileHash -LiteralPath $cursor -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash)) { throw "Index checksum mismatch or missing archive: $path" }
            $null = $actual.Add($image)
            $null = $archives.Add($cursor)
        }
    }
    foreach ($owner in @('platform') + $Selected) { if (-not $found.Contains($owner)) { throw "Selected package missing from bundle: $owner" } }
    $null = $actual.Remove('tonistiigi/binfmt:qemu-v10.2.1')
    if (-not $actual.SetEquals([string[]]$Required)) { throw 'Indexed selected image closure differs from Compose.' }
    @($archives)
}
