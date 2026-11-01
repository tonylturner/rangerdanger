# Strict post-commit evidence readiness; Windows twin of containd_readiness.py.
function Wait-ContaindProtection($Receipt, [double]$TimeoutSec = 30) {
    $budget = [Math]::Min(30, $TimeoutSec)
    $applyId = $Receipt.applyId
    if ($applyId -isnot [string] -or -not $applyId) { throw "commit response has no applyId: $($Receipt | ConvertTo-Json -Depth 20 -Compress)" }
    $encode = { param($bytes) [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+','-').Replace('/','_') }
    $secret = if ($env:CONTAIND_JWT_SECRET) { $env:CONTAIND_JWT_SECRET } else { 'rangerdanger-dev' }
    $header = & $encode ([Text.Encoding]::UTF8.GetBytes('{"alg":"HS256","typ":"JWT"}'))
    $payload = & $encode ([Text.Encoding]::UTF8.GetBytes((@{
        sub='smoke'; role='admin'; exp=[DateTimeOffset]::UtcNow.ToUnixTimeSeconds()+600
    } | ConvertTo-Json -Compress)))
    $message = "$header.$payload"
    $hmac = [Security.Cryptography.HMACSHA256]::new([Text.Encoding]::UTF8.GetBytes($secret))
    try { $signature = & $encode ($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($message))) }
    finally { $hmac.Dispose() }
    $token = "$message.$signature"
    $clock = [Diagnostics.Stopwatch]::StartNew()
    $last = $Receipt.protection
    $seen = $false
    $previousInstalled = $null
    while ($true) {
        if ($clock.Elapsed.TotalSeconds -gt $budget) {
            throw "readiness timeout for ${applyId}: last observation $($last | ConvertTo-Json -Depth 20 -Compress)"
        }
        if ($null -ne $last) {
            if (($last.applyId -and $last.applyId -cne $applyId -and $seen) -or
                ($last.installedApplyId -and $last.installedApplyId -cne $applyId -and
                 $previousInstalled -and $last.installedApplyId -cne $previousInstalled)) {
                throw "apply $applyId superseded: $($last | ConvertTo-Json -Depth 20 -Compress)"
            }
            $seen = $seen -or $last.applyId -ceq $applyId -or $last.installedApplyId -ceq $applyId
            if ($last.installedApplyId) { $previousInstalled = $last.installedApplyId }
            if ($last.state -cin @('degraded','failed','disabled')) {
                throw "apply $applyId not protecting: $($last | ConvertTo-Json -Depth 20 -Compress)"
            }
            if ($last.applyId -ceq $applyId -and $last.installedApplyId -ceq $applyId -and $last.state -ceq 'protecting') { return }
        }
        if ($clock.Elapsed.TotalSeconds -ge $budget) {
            throw "readiness timeout for ${applyId}: last observation $($last | ConvertTo-Json -Depth 20 -Compress)"
        }
        if ($null -ne $last) { Start-Sleep -Milliseconds ([Math]::Min(500, [Math]::Max(0, ($budget*1000)-$clock.Elapsed.TotalMilliseconds))) }
        $remaining = $budget-$clock.Elapsed.TotalSeconds
        if ($remaining -le 0) { continue }
        $raw = @(docker exec rangerdanger-firewall curl -sS --max-time ($remaining.ToString([Globalization.CultureInfo]::InvariantCulture)) `
            -w '\n%{http_code}' -H "Authorization: Bearer $token" `
            http://127.0.0.1:8080/api/v1/dataplane/protection)
        if ($LASTEXITCODE -ne 0) { throw "protection read transport failure: $LASTEXITCODE" }
        $text = $raw -join "`n"
        $body, $code = $text.Substring(0, $text.LastIndexOf("`n")), $text.Substring($text.LastIndexOf("`n")+1)
        if ($code -cne '200') { throw "protection read HTTP ${code}: $body" }
        $last = $body | ConvertFrom-Json -ErrorAction Stop
        if ($null -eq $last -or $last -is [array]) { throw "invalid protection response: $body" }
    }
}
