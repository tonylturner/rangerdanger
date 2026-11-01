# Backend routes already wait for readiness. Consume only their own receipt.
function Assert-ContaindProtection($Response) {
    $receipt = $Response.containd
    if (-not $receipt -or $receipt.applyId -isnot [string] -or -not $receipt.applyId -or
        $receipt.protection -isnot [string] -or
        $receipt.protection -cne 'protecting') {
        throw "backend apply is not protecting: $($Response | ConvertTo-Json -Depth 20 -Compress)"
    }
}
