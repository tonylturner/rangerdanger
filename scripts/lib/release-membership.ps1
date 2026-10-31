# Native delta-candidates contract from release_image_plan.py.
# ASCII-only, BOM-free; Windows PowerShell 5.1. No external commands.

function Get-ReleaseField($Object, [string]$Name) {
    if ($Object -is [pscustomobject]) {
        foreach ($property in $Object.PSObject.Properties) {
            if ($property.Name -ceq $Name) { return ,$property.Value }
        }
    }
    return $null
}

function New-ReleaseSet {
    return ,(New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal))
}

function New-ReleaseMap {
    return ,(New-Object 'System.Collections.Generic.Dictionary[string,object]' ([StringComparer]::Ordinal))
}

function Get-ReleaseSorted($Values) {
    $items = [string[]]@($Values)
    [Array]::Sort($items, [StringComparer]::Ordinal)
    return ,$items
}

function Get-ReleaseRepository([string]$Reference) {
    $base = ($Reference -split '@', 2)[0]
    if ($base.LastIndexOf(':') -gt $base.LastIndexOf('/')) {
        $base = $base.Substring(0, $base.LastIndexOf(':'))
    }
    return $base
}

function Read-ReleaseMembership([string]$Json, [string]$ExpectedTag) {
    if (-not $Json.TrimStart().StartsWith('{', [StringComparison]::Ordinal)) {
        throw "Release record does not identify $ExpectedTag"
    }
    $record = ConvertFrom-Json -InputObject $Json -ErrorAction Stop
    $schema = Get-ReleaseField $record 'schema'
    $release = Get-ReleaseField $record 'release'
    if (($schema -isnot [int] -and $schema -isnot [long]) -or $schema -ne 1 -or
        $release -isnot [string] -or $release -cne $ExpectedTag) {
        throw "Release record does not identify $ExpectedTag"
    }
    $compose = Get-ReleaseField $record 'compose_files'
    $platform = Get-ReleaseField $compose 'platform'
    $packages = Get-ReleaseField $compose 'packages'
    if ($compose -isnot [pscustomobject] -or $platform -isnot [pscustomobject] -or $packages -isnot [pscustomobject]) {
        throw "Release $ExpectedTag has no platform/package membership"
    }
    $refs = New-ReleaseMap
    $owners = New-ReleaseMap
    $models = @([pscustomobject]@{Owner='platform'; Model=$platform})
    $packageNames = @($packages.PSObject.Properties | ForEach-Object { $_.Name })
    foreach ($name in (Get-ReleaseSorted $packageNames)) {
        $models += [pscustomobject]@{Owner=$name; Model=(Get-ReleaseField $packages $name)}
    }
    foreach ($entry in $models) {
        $model = $entry.Model
        $file = Get-ReleaseField $model 'file'
        $hash = Get-ReleaseField $model 'sha256'
        $services = Get-ReleaseField $model 'services'
        if ($model -isnot [pscustomobject] -or $file -isnot [string] -or $hash -isnot [string] -or
            -not [regex]::IsMatch($hash, '\A[0-9a-f]{64}\z') -or $services -isnot [pscustomobject]) {
            throw "Release $ExpectedTag has malformed $($entry.Owner) Compose metadata"
        }
        foreach ($service in $services.PSObject.Properties) {
            $reference = $service.Value
            if ($reference -isnot [string] -or -not $reference) {
                throw "Release $ExpectedTag has malformed $($entry.Owner) service membership"
            }
            $repository = Get-ReleaseRepository $reference
            if (-not $refs.ContainsKey($repository)) {
                $refs[$repository] = New-ReleaseSet
                $owners[$repository] = New-ReleaseMap
            }
            $null = $refs[$repository].Add($reference)
            if (-not $owners[$repository].ContainsKey($reference)) { $owners[$repository][$reference] = New-ReleaseSet }
            $null = $owners[$repository][$reference].Add("$($entry.Owner)/$($service.Name)")
        }
    }
    $images = Get-ReleaseField $record 'images'
    if ($images -isnot [array]) { throw "Release $ExpectedTag has no image build inventory" }
    $builds = New-ReleaseSet
    foreach ($image in $images) {
        $name = Get-ReleaseField $image 'image'
        if ($name -is [string]) { $null = $builds.Add("ghcr.io/tonylturner/$name") }
    }
    $firstParty = New-ReleaseSet
    foreach ($repository in $refs.Keys) {
        if ($repository.StartsWith('ghcr.io/tonylturner/rangerdanger-', [StringComparison]::Ordinal)) {
            $null = $firstParty.Add($repository)
        }
    }
    if (-not $firstParty.SetEquals($builds)) {
        throw "Release $ExpectedTag package/platform membership differs from the authored build inventory"
    }
    return @{Refs=$refs; Owners=$owners; FirstParty=$firstParty}
}

function Get-DeltaImageCandidates(
    [string]$PreviousJson, [string]$NewJson, [string[]]$ComposeImages,
    [string]$SinceTag, [string]$NewTag, [switch]$IncludeUpstream
) {
    $old = Read-ReleaseMembership $PreviousJson $SinceTag
    $new = Read-ReleaseMembership $NewJson $NewTag
    $local = New-ReleaseSet
    $localFirstParty = New-ReleaseSet
    foreach ($image in $ComposeImages) {
        $reference = $image.Trim()
        if (-not $reference) { continue }
        $null = $local.Add($reference)
        $repository = Get-ReleaseRepository $reference
        if ($repository.StartsWith('ghcr.io/tonylturner/rangerdanger-', [StringComparison]::Ordinal)) {
            $null = $localFirstParty.Add($repository)
        }
    }
    if (-not $localFirstParty.SetEquals($new.FirstParty)) {
        throw 'New Compose image union differs from release asset first-party membership'
    }
    $newReferences = New-ReleaseSet
    foreach ($references in $new.Refs.Values) { $newReferences.UnionWith($references) }
    if (-not $local.SetEquals($newReferences)) {
        throw 'New Compose image union differs from release asset membership'
    }
    foreach ($reference in (Get-ReleaseSorted $local)) {
        $repository = Get-ReleaseRepository $reference
        $firstParty = $repository.StartsWith('ghcr.io/tonylturner/rangerdanger-', [StringComparison]::Ordinal)
        if ($firstParty) {
            $tag = if ($reference.Contains('@')) { '' } else { ($reference -split ':')[-1] }
            if ($tag -cne $NewTag) { throw "Compose image $reference does not use release tag $NewTag" }
        }
        $oldCandidates = New-ReleaseSet
        if ($old.Refs.ContainsKey($repository)) { $oldCandidates = $old.Refs[$repository] }
        $serviceOwners = $new.Owners[$repository][$reference]
        $matching = New-ReleaseSet
        foreach ($oldReference in $oldCandidates) {
            if ($serviceOwners.Overlaps($old.Owners[$repository][$oldReference])) { $null = $matching.Add($oldReference) }
        }
        $previous = if ($matching.Count) { (Get-ReleaseSorted $matching)[0] }
            elseif ($oldCandidates.Count) { (Get-ReleaseSorted $oldCandidates)[0] } else { $reference }
        if (-not $firstParty -and -not $IncludeUpstream -and $previous -ceq $reference -and $oldCandidates.Count) { continue }
        [pscustomobject]@{
            image=$reference; since=$previous; repository=$repository
            first_party=$firstParty; old_member=($oldCandidates.Count -gt 0)
            owners=(Get-ReleaseSorted $serviceOwners)
        }
    }
}
