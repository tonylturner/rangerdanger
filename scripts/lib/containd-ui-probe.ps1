# Embedded bundle, authenticated session and API routing conformance. Dot-source, then call
# Invoke-ContaindUiProbe. No Python, Docker, IE engine or policy changes.
# ASCII-only, BOM-free; Windows PowerShell 5.1 compatible.

function Get-ContaindScriptSources([string]$Html) {
    # Tokenize tags (including quoted '>') rather than matching script-looking
    # text inside comments, attributes, or script/style raw text.
    $tagPattern = '(?s)<!--.*?(?:-->|$)|<![^>]*>|<\?[^>]*>|<(?<close>/)?(?<tag>[a-zA-Z][\w:-]*)(?<attrs>(?:[^"''<>]|"[^"]*"|''[^'']*'')*)>'
    $attrPattern = '(?<name>[^\s/=<>"'']+)(?:\s*=\s*(?:"(?<value>[^"]*)"|''(?<value>[^'']*)''|(?<value>[^\s>]+)))?'
    $rawTag = ''
    foreach ($tag in [regex]::Matches($Html, $tagPattern)) {
        $name = $tag.Groups['tag'].Value.ToLowerInvariant()
        if (-not $name) { continue }
        $closing = $tag.Groups['close'].Success
        if ($rawTag) {
            if ($closing -and $name -eq $rawTag) { $rawTag = '' }
            continue
        }
        if ($closing) { continue }
        if ($name -eq 'script') {
            foreach ($attr in [regex]::Matches($tag.Groups['attrs'].Value, $attrPattern)) {
                if ($attr.Groups['name'].Value -ieq 'src' -and $attr.Groups['value'].Value) {
                    [Net.WebUtility]::HtmlDecode($attr.Groups['value'].Value)
                }
            }
        }
        if ($name -in @('script', 'style') -and $tag.Value -notmatch '/\s*>$') { $rawTag = $name }
    }
}

function Invoke-ContaindUiProbe([string]$Origin = 'http://127.0.0.1:8088') {
    $Origin = $Origin.TrimEnd('/')
    $base = $null
    if ($Origin -notmatch '^https?://[^\s/?#]+$' -or
        -not [uri]::TryCreate($Origin, [UriKind]::Absolute, [ref]$base) -or $base.UserInfo) {
        throw 'Supply a portal origin, e.g. http://127.0.0.1:8088'
    }
    $authority = [regex]::Match($Origin, '^[^:]+://([^/]+)$').Groups[1].Value
    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    function Get-ProbeRequest([string]$Path, [string]$Data = '', [string]$Token = '') {
        $headers = @{ 'Accept-Encoding' = 'identity' }
        if ($Token) { $headers.Authorization = "Bearer $Token" }
        $request = @{
            Uri = "$Origin$Path"; Headers = $headers; WebSession = $session
            TimeoutSec = 15; UseBasicParsing = $true; MaximumRedirection = 0; ErrorAction = 'Stop'
        }
        if ($Data) { $request.Method = 'POST'; $request.Body = $Data; $request.ContentType = 'application/json' }
        $request
    }
    function Get-ProbeBody([string]$Path, [string[]]$Types, [string]$Data = '', [string]$Token = '') {
        $request = Get-ProbeRequest $Path $Data $Token
        $response = Invoke-WebRequest @request
        $type = ([string]$response.Headers['Content-Type'] -split ';', 2)[0].Trim().ToLowerInvariant()
        if ($response.StatusCode -ne 200 -or $type -notin $Types) {
            throw "${Path}: expected 200 $Types, got $($response.StatusCode) $type"
        }
        # The Python oracle requires UTF-8, regardless of the declared charset.
        $utf8 = New-Object Text.UTF8Encoding $false, $true
        $utf8.GetString($response.RawContentStream.ToArray())
    }

    $page = Get-ProbeBody '/containd/' @('text/html')
    $paths = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    foreach ($source in @(Get-ContaindScriptSources $page)) {
        $url = New-Object uri (New-Object uri "$Origin/containd/"), $source
        # Uri.Authority canonicalizes case/default ports; urlsplit's netloc
        # comparison in the reference probe deliberately does not.
        $scriptAuthority = [regex]::Match($url.OriginalString, '^[^:]+://([^/?#]+)').Groups[1].Value
        if ($url.Scheme -cne $base.Scheme -or $scriptAuthority -cne $authority -or
            -not $url.AbsolutePath.StartsWith('/containd/_next/static/', [StringComparison]::Ordinal) -or
            -not $url.AbsolutePath.EndsWith('.js', [StringComparison]::Ordinal)) {
            throw "Unexpected embedded script source: $source"
        }
        $null = $paths.Add($url.PathAndQuery)
    }
    if (-not $paths.Count) { throw 'containd page contains no JavaScript chunks' }
    $autodetect = $false
    $authMe = $false
    foreach ($path in ($paths | Sort-Object)) {
        $body = Get-ProbeBody $path @('application/javascript', 'text/javascript')
        if ($body.Contains('/containd/containd/')) { throw "${path}: double /containd/ prefix" }
        # containd adds its API base at runtime, so no proxy-prefixed API literal is allowed.
        $match = [regex]::Match($body, '(["''])(/containd/api/[^"'']*)\1')
        if ($match.Success) { throw "${path}: runtime-based API literal was rewritten: $($match.Groups[2].Value)" }
        # Direct pathname detection or the optional-path fallback in 0.1.33.
        # The backreference requires the fallback to test and return the same identifier.
        if ($body -cmatch '(?:window\.location\.pathname|\(\s*null\s*!=\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\?\s*\1\s*:\s*window\.location\.pathname\s*\))\.startsWith\(\s*["'']/containd/["'']\s*\)\s*\?\s*["'']/containd["'']') { $autodetect = $true }
        if ($body -cmatch '["'']/api/v1/auth/me["'']') { $authMe = $true }
    }
    if (-not $autodetect) { throw 'Chunks do not contain the /containd/ runtime API-base autodetect' }
    if (-not $authMe) { throw 'Chunks do not contain the unprefixed auth/me call' }
    Write-Host "PASS containd UI: $($paths.Count) chunks, runtime API base intact, no double prefix"

    function Get-ProbeJsonProperty([string]$Json, [string]$Name) {
        # ConvertFrom-Json enumerates arrays and PS member lookup folds case;
        # neither is allowed by the Python dict/string assertions.
        if (-not $Json.TrimStart().StartsWith('{', [StringComparison]::Ordinal)) { return $null }
        $object = ConvertFrom-Json -InputObject $Json -ErrorAction Stop
        $property = $object.PSObject.Properties | Where-Object { $_.Name -ceq $Name } | Select-Object -First 1
        if ($property) { return ,$property.Value }
        return $null
    }
    $loginPath = '/containd/api/v1/auth/login'
    $loginJson = Get-ProbeBody $loginPath @('application/json') '{"username":"containd","password":"containd"}'
    $token = Get-ProbeJsonProperty $loginJson 'token'
    if ($token -isnot [string] -or -not $token) {
        throw "${loginPath}: no session token"
    }
    $mePath = '/containd/api/v1/auth/me'
    $meJson = Get-ProbeBody $mePath @('application/json') '' $token
    $username = Get-ProbeJsonProperty $meJson 'username'
    if ($username -isnot [string] -or $username -cne 'containd') {
        throw "${mePath}: JSON does not identify the logged-in lab user"
    }
    Write-Host "PASS $loginPath -> ${mePath}: 200 application/json, username=containd"

    # The paths the CLI console and PCAP download build from the runtime base. Without an
    # Upgrade header, or for a capture that does not exist, the API refuses with a 400;
    # a double-prefixed path instead gets the UI page (200 text/html).
    $apiRoutes = @('/containd/api/v1/cli/ws', '/containd/api/v1/pcap/download/rangerdanger-probe-missing.pcap')
    foreach ($route in $apiRoutes) {
        $request = Get-ProbeRequest $route '' $token
        try {
            $response = Invoke-WebRequest @request
            $type = [string]$response.Headers['Content-Type']
        } catch {
            # Windows PowerShell 5.1 throws a WebException carrying an HttpWebResponse;
            # PowerShell 7 an HttpResponseException carrying an HttpResponseMessage.
            $response = $_.Exception.Response
            if (-not $response) { throw }
            if ($response.PSObject.Properties['ContentType']) { $type = [string]$response.ContentType }
            else { $type = [string]$response.Content.Headers.ContentType }
        }
        $status = [int]$response.StatusCode
        $type = ($type -split ';', 2)[0].Trim().ToLowerInvariant()
        if ($status -ne 400 -or $type -eq 'text/html') {
            throw "${route}: expected a 400 refusal from the containd API, got $status $type"
        }
    }
    Write-Host "PASS $($apiRoutes -join ', '): 400 from the containd API, not the UI page"
}
