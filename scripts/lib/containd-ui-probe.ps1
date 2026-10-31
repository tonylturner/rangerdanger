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

function Get-ContaindOnDemandChunks($Chunks) {
    # The served webpack runtime, black-box (current bundle):
    #   r.p="/containd/_next/",r.u=e=>"static/chunks/"+(3888===e?"e868780c":e)+"."+({747:"5cee...",...})[e]+".js"
    # Ids not named in the optional ===-chain use the id as the name. Any other
    # shape fails rather than skipping chunks. Returns chunk URLs in map order.
    $webpackChunks = '(?<![\w$])([A-Za-z_$][\w$]*)\.u=([A-Za-z_$][\w$]*)=>"static/chunks/"\+(\2|\((?:\d+===\2\?"[\w-]+":)+\2\))\+"\."\+\((\{(?:\d+:"[\w-]+"(?:,\d+:"[\w-]+")*)?\})\)\[\2\]\+"\.js"'
    $chunkName = '(\d+)===[A-Za-z_$][\w$]*\?"([\w-]+)"'
    $chunkHash = '(\d+):"([\w-]+)"'
    $publicPath = '(?<![\w$])([A-Za-z_$][\w$]*)\.p="([^"]*)"'
    $runtimes = New-Object 'System.Collections.Generic.List[object]'
    foreach ($entry in $Chunks.GetEnumerator()) {
        foreach ($match in [regex]::Matches([string]$entry.Value, $webpackChunks)) {
            $runtimes.Add([pscustomobject]@{ Path = $entry.Key; Body = [string]$entry.Value; Match = $match })
        }
    }
    if ($runtimes.Count -ne 1) {
        $where = if ($runtimes.Count) { $runtimes[0].Path } else { 'served bundle' }
        throw "Cannot verify on-demand chunks in $where`: unsupported or ambiguous Next.js chunk-map shape ($($runtimes.Count) webpack maps; expected exactly 1); inspect the live bundle before changing this oracle"
    }
    $runtime = $runtimes[0]
    $public = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    foreach ($found in [regex]::Matches($runtime.Body, $publicPath)) {
        if ($found.Groups[1].Value -ceq $runtime.Match.Groups[1].Value) { $null = $public.Add($found.Groups[2].Value) }
    }
    if ($public.Count -ne 1) { throw "$($runtime.Path): webpack runtime does not set exactly one public path" }
    $prefix = "$(@($public)[0])static/chunks/"
    $names = @{}
    foreach ($found in [regex]::Matches($runtime.Match.Groups[3].Value, $chunkName)) {
        if (-not $names.ContainsKey($found.Groups[1].Value)) { $names[$found.Groups[1].Value] = $found.Groups[2].Value }
    }
    foreach ($found in [regex]::Matches($runtime.Match.Groups[4].Value, $chunkHash)) {
        $chunk = $found.Groups[1].Value
        $name = if ($names.ContainsKey($chunk)) { $names[$chunk] } else { $chunk }
        "$prefix$name.$($found.Groups[2].Value).js"
    }
}

function Get-ContaindLinkRoutes([string]$Body) {
    # Root-relative link targets: href:"/identities/" in a chunk, href="/identities/" in HTML,
    # then the same link inside an inline React Server Components payload, where quotes are escaped.
    $linkRoute = 'href[:=](["''])/(?!containd[/?#"''])([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"'']*)?\1'
    $flightLink = '\\"href\\":\\"/(?!containd[/?#\\])([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"\\]*)?\\"'
    foreach ($match in [regex]::Matches($Body, $linkRoute)) {
        [pscustomobject]@{ Route = $match.Groups[2].Value; Literal = $match.Value; Escaped = $false }
    }
    foreach ($match in [regex]::Matches($Body, $flightLink)) {
        [pscustomobject]@{ Route = $match.Groups[1].Value; Literal = $match.Value; Escaped = $true }
    }
}

function Confirm-ContaindLinksRewritten($Documents, [string]$Kind) {
    # In the proxied UI every route link must carry /containd/; a bare one leaves the UI.
    # Root-relative links that are not routes: assets and the API.
    $linkExempt = @('_next', 'api')
    foreach ($entry in $Documents.GetEnumerator()) {
        $link = Get-ContaindLinkRoutes ([string]$entry.Value) |
            Where-Object { $_.Route.Split('/')[0] -cnotin $linkExempt } | Select-Object -First 1
        if ($link) { throw "$($entry.Key): $Kind has a route link the proxy did not rewrite: $($link.Literal)" }
    }
}

function Get-ContaindRoutePages($Chunks) {
    # App-router page chunks (app/<route>/page-*.js) load only from their own route's HTML.
    # Routes come from the bundle's path literals: proxy-prefixed ones ("/containd/alerts/",
    # "/containd/login?reason=") and root-relative link targets (href:"/identities/").
    # A root-relative link is also a proxy defect: the route list's sub_filter missed it.
    # Returns /containd/<route>/ paths in ordinal order.
    $containdRoute = '(["''])/containd/([A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*)/?(?:[?#][^"'']*)?\1'
    # Not app-router pages: assets, the API, a double prefix, and containd's documentation,
    # which is a separate static site whose scripts live outside /_next/static/.
    $notRoutes = @('_next', 'api', 'containd', 'docs')
    $routes = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    foreach ($body in $Chunks.Values) {
        $named = @(foreach ($match in [regex]::Matches([string]$body, $containdRoute)) { $match.Groups[2].Value }) +
            @(Get-ContaindLinkRoutes ([string]$body) | Where-Object { -not $_.Escaped } | ForEach-Object { $_.Route })
        foreach ($route in $named) {
            if ($route.Split('/')[0] -cnotin $notRoutes) { $null = $routes.Add("/containd/$route/") }
        }
    }
    $sorted = [string[]]@($routes)
    [Array]::Sort($sorted, [StringComparer]::Ordinal)
    $sorted
}

function Confirm-ContaindCliUpgrade(
    [string]$Origin, [string]$Ticket = '', [string]$Token = '',
    [string]$QueryName = '', [int]$ExpectedStatus = 101
) {
    # A raw HTTP/1.1 handshake exposes refusal status on both PS 5.1 and 7.
    # Close immediately after headers: no CLI commands or websocket frame reads.
    $url = [uri]$Origin
    $authority = [regex]::Match($Origin, '^[^:]+://([^/]+)$').Groups[1].Value
    $path = '/containd/api/v1/cli/ws'
    $target = $path
    if ($Ticket) {
        $target += '?ticket=' + [uri]::EscapeDataString($Ticket)
    } elseif ($QueryName -and $Token) {
        $target += '?' + $QueryName + '=' + [uri]::EscapeDataString($Token)
    }
    if ($Token -match '[\r\n]') { throw "${path}: invalid token header" }
    $client = New-Object Net.Sockets.TcpClient
    $stream = $null
    try {
        $connect = $client.ConnectAsync($url.DnsSafeHost, $url.Port)
        if (-not $connect.Wait(15000)) { throw "${path}: connection timeout" }
        $null = $connect.GetAwaiter().GetResult()
        $stream = $client.GetStream()
        $stream.ReadTimeout = 15000
        $stream.WriteTimeout = 15000
        if ($url.Scheme -eq 'https') {
            $stream = New-Object Net.Security.SslStream $stream, $false
            $tls = $stream.AuthenticateAsClientAsync($url.DnsSafeHost)
            if (-not $tls.Wait(15000)) { throw "${path}: TLS timeout" }
            $null = $tls.GetAwaiter().GetResult()
        }
        $key = [Convert]::ToBase64String([Guid]::NewGuid().ToByteArray())
        $authHeader = if ($Token -and -not $QueryName) { "Authorization: Bearer $Token`r`n" } else { '' }
        $request = "GET $target HTTP/1.1`r`nHost: $authority`r`nOrigin: $Origin`r`n$authHeader" +
            "Connection: Upgrade`r`nUpgrade: websocket`r`nSec-WebSocket-Version: 13`r`nSec-WebSocket-Key: $key`r`n`r`n"
        $bytes = [Text.Encoding]::ASCII.GetBytes($request)
        $stream.Write($bytes, 0, $bytes.Length)
        $header = New-Object Text.StringBuilder
        $timer = [Diagnostics.Stopwatch]::StartNew()
        while (-not $header.ToString().EndsWith("`r`n`r`n", [StringComparison]::Ordinal)) {
            $remaining = 15000 - [int]$timer.ElapsedMilliseconds
            if ($remaining -le 0) { throw "${path}: upgrade timeout" }
            $stream.ReadTimeout = $remaining
            $byte = $stream.ReadByte()
            if ($byte -lt 0) { throw "${path}: incomplete upgrade response" }
            $null = $header.Append([char]$byte)
            if ($header.Length -gt 16384) { throw "${path}: oversized upgrade response" }
        }
        $lines = $header.ToString() -split "`r`n"
        if ($lines[0] -notmatch "^HTTP/1\.1 $ExpectedStatus(?: |$)") {
            throw "${path}: expected HTTP $ExpectedStatus, got $($lines[0])"
        }
        if ($ExpectedStatus -ne 101) {
            Write-Host "PASS ${path}: rejected with HTTP $ExpectedStatus"
            return
        }
        $headers = @{}
        foreach ($line in $lines[1..($lines.Length - 1)]) {
            if ($line.Contains(':')) {
                $pair = $line -split ':', 2
                $headers[$pair[0].ToLowerInvariant()] = $pair[1].Trim()
            }
        }
        $sha = [Security.Cryptography.SHA1]::Create()
        try {
            $accept = [Convert]::ToBase64String($sha.ComputeHash(
                [Text.Encoding]::ASCII.GetBytes($key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11')))
        } finally { $sha.Dispose() }
        $connections = @(([string]$headers['connection'] -split ',') | ForEach-Object { $_.Trim() })
        if ($headers['upgrade'] -ine 'websocket' -or 'Upgrade' -notin $connections -or
            $headers['sec-websocket-accept'] -cne $accept) {
            throw "${path}: invalid websocket upgrade headers"
        }
    } finally {
        if ($stream) { $stream.Dispose() }
        $client.Close()
    }
    Write-Host "PASS ${path}: websocket 101 with a fresh ticket and browser Origin $Origin"
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
    function Get-ProbeRequest(
        [string]$Path, [string]$Data = '', [string]$Token = '',
        [string]$Method = '', [object]$WebSession = $null
    ) {
        $headers = @{ 'Accept-Encoding' = 'identity' }
        if ($Token) { $headers.Authorization = "Bearer $Token" }
        $selectedSession = if ($WebSession) { $WebSession } else { $session }
        $request = @{
            Uri = "$Origin$Path"; Headers = $headers; WebSession = $selectedSession
            TimeoutSec = 15; UseBasicParsing = $true; MaximumRedirection = 0; ErrorAction = 'Stop'
        }
        if ($Method) { $request.Method = $Method }
        elseif ($Data) { $request.Method = 'POST' }
        if ($Data) { $request.Body = $Data; $request.ContentType = 'application/json' }
        $request
    }
    function Get-ProbeBody(
        [string]$Path, [string[]]$Types, [string]$Data = '', [string]$Token = '',
        [string]$Method = '', [object]$WebSession = $null
    ) {
        $request = Get-ProbeRequest $Path $Data $Token $Method $WebSession
        $response = Invoke-WebRequest @request
        $type = ([string]$response.Headers['Content-Type'] -split ';', 2)[0].Trim().ToLowerInvariant()
        if ($response.StatusCode -ne 200 -or $type -notin $Types) {
            throw "${Path}: expected 200 $Types, got $($response.StatusCode) $type"
        }
        # The Python oracle requires UTF-8, regardless of the declared charset.
        $utf8 = New-Object Text.UTF8Encoding $false, $true
        $utf8.GetString($response.RawContentStream.ToArray())
    }

    function Get-ProbeChunkPath([string]$Source, [string]$Kind) {
        $url = New-Object uri (New-Object uri "$Origin/containd/"), $Source
        # Uri.Authority canonicalizes case/default ports; urlsplit's netloc
        # comparison in the reference probe deliberately does not.
        $scriptAuthority = [regex]::Match($url.OriginalString, '^[^:]+://([^/?#]+)').Groups[1].Value
        if ($url.Scheme -cne $base.Scheme -or $scriptAuthority -cne $authority -or
            -not $url.AbsolutePath.StartsWith('/containd/_next/static/', [StringComparison]::Ordinal) -or
            -not $url.AbsolutePath.EndsWith('.js', [StringComparison]::Ordinal)) {
            throw "Unexpected ${Kind}: $Source"
        }
        $url.PathAndQuery
    }

    function Get-ProbePageChunks([string]$Path) {
        $html = Get-ProbeBody $Path @('text/html')
        $pages[$Path] = $html
        foreach ($source in @(Get-ContaindScriptSources $html)) { Get-ProbeChunkPath $source 'embedded script source' }
    }
    function Get-OrdinalSorted($Items) {
        # Sort-Object is culture-aware; the Python oracle sorts by code point.
        $sorted = [string[]]@($Items)
        [Array]::Sort($sorted, [StringComparer]::Ordinal)
        $sorted
    }

    $scripts = @('application/javascript', 'text/javascript')
    $maxRoutePages = 200
    $pages = New-Object Collections.Specialized.OrderedDictionary ([StringComparer]::Ordinal)
    $paths = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    foreach ($path in @(Get-ProbePageChunks '/containd/')) { $null = $paths.Add($path) }
    if (-not $paths.Count) { throw 'containd page contains no JavaScript chunks' }
    $chunks = New-Object Collections.Specialized.OrderedDictionary ([StringComparer]::Ordinal)
    foreach ($path in @(Get-OrdinalSorted $paths)) { $chunks[$path] = Get-ProbeBody $path $scripts }
    $onDemand = 0
    foreach ($source in @(Get-ContaindOnDemandChunks $chunks)) {
        $path = Get-ProbeChunkPath $source 'on-demand chunk'
        if (-not $chunks.Contains($path)) { $chunks[$path] = Get-ProbeBody $path $scripts; $onDemand++ }
    }
    # Each round fetches the newly named route pages, then their new chunks, which may name more.
    $visited = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
    while ($true) {
        $pending = @(Get-ContaindRoutePages $chunks | Where-Object { -not $visited.Contains($_) })
        if (-not $pending.Count) { break }
        if ($visited.Count + $pending.Count -gt $maxRoutePages) { throw "Chunks name more than $maxRoutePages route pages" }
        $found = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::Ordinal)
        foreach ($page in $pending) {
            $null = $visited.Add($page)
            foreach ($path in @(Get-ProbePageChunks $page)) { if (-not $chunks.Contains($path)) { $null = $found.Add($path) } }
        }
        foreach ($path in @(Get-OrdinalSorted $found)) { $chunks[$path] = Get-ProbeBody $path $scripts }
    }
    $autodetect = $false
    $authMe = $false
    foreach ($entry in $chunks.GetEnumerator()) {
        $path = $entry.Key
        $body = [string]$entry.Value
        if ($body.Contains('/containd/containd/')) { throw "${path}: double /containd/ prefix" }
        # containd adds its API base at runtime, so no proxy-prefixed API literal is allowed.
        $match = [regex]::Match($body, '(["''])(/containd/api/[^"'']*)\1')
        if ($match.Success) { throw "${path}: runtime-based API literal was rewritten: $($match.Groups[2].Value)" }
        # Direct pathname detection or the optional-path fallback in 0.1.33.
        # The backreference requires the fallback to test and return the same identifier.
        if ($body -cmatch '(?:window\.location\.pathname|\(\s*null\s*!=\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\?\s*\1\s*:\s*window\.location\.pathname\s*\))\.startsWith\(\s*["'']/containd/["'']\s*\)\s*\?\s*["'']/containd["'']') { $autodetect = $true }
        if ($body -cmatch '["'']/api/v1/auth/me["'']') { $authMe = $true }
    }
    Confirm-ContaindLinksRewritten $chunks 'chunk'
    if (-not $autodetect) { throw 'Chunks do not contain the /containd/ runtime API-base autodetect' }
    if (-not $authMe) { throw 'Chunks do not contain the unprefixed auth/me call' }
    Confirm-ContaindLinksRewritten $pages 'page'
    Write-Host "PASS containd UI: $($chunks.Count) chunks ($onDemand on demand), $($visited.Count) route pages, runtime API base intact, no double prefix"

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

    $ticketPath = '/containd/api/v1/cli/ws/ticket'
    function Get-ProbeCliTicket {
        $ticketSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
        $ticketJson = Get-ProbeBody $ticketPath @('application/json') '' $token 'POST' $ticketSession
        $ticket = Get-ProbeJsonProperty $ticketJson 'ticket'
        $expiresIn = Get-ProbeJsonProperty $ticketJson 'expiresIn'
        if ($ticket -isnot [string] -or -not $ticket -or $expiresIn -ne 30) {
            throw "${ticketPath}: expected an opaque ticket and expiresIn=30"
        }
        $ticket
    }
    Write-Host "PASS ${ticketPath}: authenticated ticket issuance contract (expiresIn=30)"

    Confirm-ContaindCliUpgrade $Origin '' '' '' 401
    Confirm-ContaindCliUpgrade $Origin '' $token '' 401
    Confirm-ContaindCliUpgrade $Origin '' $token 'token' 401

    $firstTicket = Get-ProbeCliTicket
    Confirm-ContaindCliUpgrade $Origin $firstTicket
    $reconnectTicket = Get-ProbeCliTicket
    Confirm-ContaindCliUpgrade $Origin $reconnectTicket
    Confirm-ContaindCliUpgrade $Origin $reconnectTicket '' '' 401

    # A missing capture must remain an API response, not fall through to the UI page.
    $apiRoutes = @('/containd/api/v1/pcap/download/rangerdanger-probe-missing.pcap')
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
