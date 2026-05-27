# Security: known issues

Findings from `govulncheck` that the project is aware of but has not
acted on, with rationale. Source of truth for the `Go vulnerability
scan` hard-gate job in `.github/workflows/ci.yml`: a triage entry
here plus appending the GOID to the workflow's `ALLOWED` env var is
the contract for accepting a new finding.

The triage cadence is per-release. If you tag a new version, sweep
this file against `gh run view <latest-ci-run> --log-failed` and
either remove resolved entries or add new ones.

## Open

### docker/docker - `GO-2026-4887` and `GO-2026-4883`

- **Module**: `github.com/docker/docker@v27.5.1+incompatible`
- **Affects**: backend Docker SDK calls (container exec, lifecycle,
  image inspection)
- **Upstream fix**: `Fixed in: N/A` - no patched docker SDK release at
  time of writing
- **Mitigation**: lab-only deployment is loopback-bound (A3); the
  Docker socket mount is in the always-trusted backend container; no
  untrusted input reaches the affected SDK paths
- **Action**: monitor https://github.com/moby/moby for a release
  containing the fix; bump when available. Until then, the
  `docker/docker` direct dependency in `backend/go.mod` stays pinned
  at the current version.

## Resolved by direct dependency bumps (2026-05-27)

A vuln-db refresh on 2026-05-26/27 surfaced 6 new findings, all in
`golang.org/x/net` (5 in `net/html`, 1 in `net/idna`), all fixed in
v0.55.0. Resolved by one `go get golang.org/x/net@v0.55.0` in
`backend/`; no transitive bumps required.

GO IDs cleared by this bump:

- `GO-2026-5025` (net/html XSS via namespaced foreign content)
- `GO-2026-5026` (net/idna ASCII-only Punycode label decode)
- `GO-2026-5027` (net/html XSS via foreign-content element handling)
- `GO-2026-5028` (net/html DoS on pathological HTML parse)
- `GO-2026-5029` (net/html XSS via DOCTYPE character references)
- `GO-2026-5030` (net/html XSS via duplicate attributes)

Practical exposure: rangerdanger does NOT call `golang.org/x/net/html`
parse/Render on attacker-controlled input — html templating is in
the Next.js frontend, not the Go backend; backend HTML is limited to
a couple of static error pages. x/net/idna is reachable transitively
via HTTP routing but the affected ToASCII/ToUnicode paths aren't on
any code path that handles untrusted hostnames. Still bumped to keep
govulncheck green.

## Resolved by direct dependency bumps (2026-05-22)

A vuln-db refresh on 2026-05-22 surfaced 13 new findings, all in
`golang.org/x/crypto`'s SSH stack (`ssh`, `ssh/agent`,
`ssh/knownhosts`), all fixed in v0.52.0. Resolved by one
`go get golang.org/x/crypto@v0.52.0` in `backend/`; transitive
bumps of x/net (v0.53.0 → v0.54.0), x/sys (v0.43.0 → v0.45.0),
and x/text (v0.36.0 → v0.37.0) came along automatically and
introduced no new findings.

GO IDs cleared by this bump:

- `GO-2026-5005` (ssh/agent key constraints not enforced)
- `GO-2026-5006` (ssh/agent constraints dropped when forwarding)
- `GO-2026-5013` (ssh byte-arithmetic underflow / panic)
- `GO-2026-5014` (ssh certificate-restriction bypass)
- `GO-2026-5015` (ssh server panic during CheckHostKey/Authenticate)
- `GO-2026-5016` (ssh memory-leak DoS on rejected channels)
- `GO-2026-5017` (ssh client → server deadlock on unexpected responses)
- `GO-2026-5018` (ssh pathological RSA/DSA DoS)
- `GO-2026-5019` (ssh FIDO/U2F physical-interaction bypass)
- `GO-2026-5020` (ssh infinite loop on large channel writes)
- `GO-2026-5021` (ssh/knownhosts @revoked status unenforced)
- `GO-2026-5023` (ssh VerifiedPublicKeyCallback permissions skip)
- `GO-2026-5033` (ssh/agent client panic on pathological input)

Practical exposure: rangerdanger does NOT terminate SSH on the
backend; the only SSH path is `ssh -p 2222 containd@localhost`
which terminates inside the firewall container, not in the backend
Go process. x/crypto/ssh is reachable via transitive imports only
(docker/docker → ssh helpers). Still bumped to keep govulncheck
green.

Note: the existing x/net direct pin (added 2026-05-07 for
GO-2026-4918) rolls forward to v0.54.0 with this bump. The fix
is still present (semver patch-rollforward), and the rationale
remains the same as before.

## Resolved by direct dependency bumps (2026-05-07)

When the `govulncheck` job flipped to a hard gate, three new
findings surfaced that hadn't appeared in the prior advisory runs
(vuln database refresh between scans). All three were upstream-fixed
and resolved by `go get`:

- **`GO-2025-4233`, `GO-2025-4017`** - `quic-go` v0.54.0 → v0.57.0.
  Was transitive via `gin-gonic/gin` → `quic-go/http3`; direct pin
  in `backend/go.mod`'s `require` block keeps the patched version
  even if gin lags. Practical exposure under our loopback-bound
  deployment was zero (we serve HTTP/1.1 + HTTP/2 only).
- **`GO-2025-4134`, `GO-2025-4135`** - `golang.org/x/crypto`
  v0.44.0 → v0.50.0.

## Resolved by Go toolchain bump (2026-05-07)

The Go toolchain pin was bumped twice this release cycle:

1. **`1.24.3 → 1.24.13`** cleared 6 stdlib findings whose `Fixed in`
   was on the 1.24.x line: `GO-2026-4341` (net/url), `GO-2026-4340`
   (tls), `GO-2026-4337` (tls), `GO-2025-4175` (x509), `GO-2025-4155`
   (x509), `GO-2025-4014` (tar).

2. **`1.24.13 → 1.25.9`** cleared the remaining 7 stdlib findings
   whose `Fixed in` required the 1.25.x line:
   - `GO-2026-4947` (x509 chain build) - Fixed in 1.25.9
   - `GO-2026-4946` (x509 policy validation) - Fixed in 1.25.9
   - `GO-2026-4870` (TLS 1.3 KeyUpdate DoS) - Fixed in 1.25.9
   - `GO-2026-4869` (archive/tar sparse alloc) - Fixed in 1.25.9
   - `GO-2026-4865` (html/template XSS) - Fixed in 1.25.9
   - `GO-2026-4602` (os Root escape) - Fixed in 1.25.8
   - `GO-2026-4601` (net/url IPv6 parse) - Fixed in 1.25.8

3. **`1.25.9 → 1.25.10`** cleared 8 new stdlib findings that
   surfaced when the vuln-db refreshed on 2026-05-07. All 8 have
   fixes on both 1.25.x and 1.26.x; we stayed on the 1.25 line:
   - `GO-2026-4986` (net/mail consumeComment quadratic) - 1.25.10
   - `GO-2026-4982` (html/template meta URL escape XSS) - 1.25.10
   - `GO-2026-4981` (net long CNAME crash) - 1.25.10
   - `GO-2026-4980` (html/template escaper bypass XSS) - 1.25.10
   - `GO-2026-4977` (net/mail consumePhrase quadratic) - 1.25.10
   - `GO-2026-4976` (httputil ReverseProxy query forwarding) - 1.25.10
   - `GO-2026-4971` (net Dial NUL byte panic on Windows) - 1.25.10
   - `GO-2026-4918` (http2 SETTINGS_MAX_FRAME_SIZE infinite loop) -
     stdlib 1.25.10 + `golang.org/x/net@v0.53.0`. The x/net bump was
     applied as a direct `require` in `backend/go.mod` so the
     transitive pin is honored even if a future update of an
     intermediate dep tries to pull in an older x/net.

The toolchain directive in each module's `go.mod` controls the
version CI's `actions/setup-go` installs (`go-version-file:
<module>/go.mod` reads it). `backend/go.mod`, `services/go.mod`,
and `dnp3go/go.mod` all pin `toolchain go1.25.10`. The `go 1.24.0`
directive (minimum language version) is left alone so the modules
remain buildable by anyone on Go 1.24+ as a consumer.

Dockerfile bases also bumped:
`Dockerfile.backend` → `golang:1.25` (was `1.24`)
`services/Dockerfile`, `Dockerfile.kali`, `Dockerfile.eng-ws` →
`golang:1.25-alpine` (was `1.24-alpine`).

## Adding a new exception

The hard-gate `govulncheck` job allowlists OSV IDs via the
`ALLOWED` env var in `.github/workflows/ci.yml`. To accept a new
finding:

1. Add a triage entry to the **Open** section above with module,
   affected paths, upstream fix status, mitigation, and action.
2. Append the GOID to `ALLOWED` in `.github/workflows/ci.yml`.
3. Add a `### Security` note to `CHANGELOG.md` under `[Unreleased]`.

Same PR for all three - the entry, the workflow change, and the
changelog note travel together so the acceptance is reviewable.
