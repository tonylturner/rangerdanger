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

### docker/docker - `GO-2026-4887`, `GO-2026-4883`, `GO-2026-5617`, `GO-2026-5668`, `GO-2026-5746`

- **Module**: `github.com/docker/docker@v27.5.1+incompatible`
- **Affects**: backend Docker SDK calls (container exec, lifecycle,
  image inspection)
- **Upstream fix**: `Fixed in: N/A` for all five. The `docker/docker`
  module path has **no** patched release. Upstream's fix for the
  2026-08 advisories landed on the renamed `github.com/moby/moby/v2`
  module (`v2.0.0-beta.14+`), which is a different module path and a
  beta line - not a bump we can take from `docker/docker`.
- **Not reachable**: the vulnerable symbols live in
  `github.com/docker/docker/daemon` (e.g. `GO-2026-5746` is
  `Daemon.containerExtractToDir`). That package is **not in this
  project's build graph** - verified with
  `go list -deps ./... | grep docker/docker/daemon` (no match). The
  backend imports only `api/types/*` and `client`. govulncheck still
  reports these because the traces resolve to package `init`
  functions of `api/types/*`, not to any called vulnerable code.
- **Mitigation**: lab-only deployment is loopback-bound (A3); the
  Docker socket mount is in the always-trusted backend container; no
  untrusted input reaches the affected SDK paths
- **Enforced by**: `scripts/assert-unreachable-vulns.sh`, run as a step
  of the `govulncheck` job. The allowlist filter matches on GOID alone,
  so it cannot tell that "not in the build graph" has stopped being
  true. The script fails the gate if `docker/docker/daemon` ever enters
  the graph, which is the precondition this exception rests on.
- **Action**: monitor https://github.com/moby/moby for a `docker/docker`
  release containing the fix, or for the `moby/moby/v2` module to reach
  a stable release worth migrating to. Until then, the `docker/docker`
  direct dependency in `backend/go.mod` stays pinned at the current
  version.

### x/crypto openpgp - `GO-2026-5932`

- **Module**: `golang.org/x/crypto@v0.56.0`
- **Upstream fix**: `Fixed in: N/A`, and there will never be one. This
  advisory says `golang.org/x/crypto/openpgp` is unmaintained and
  unsafe by design; upstream's guidance is to migrate to
  `github.com/ProtonMail/go-crypto/openpgp`, not to await a patch.
- **Not reachable**: rangerdanger does not import `openpgp` at all -
  verified with `go list -deps ./... | grep openpgp` (no match). The
  only reason `x/crypto` is in the graph is `gin` ->
  `go-playground/validator/v10` -> `x/crypto/sha3`. There is nothing
  to migrate; the vulnerable packages are never compiled in.
- **Mitigation**: n/a - no OpenPGP code path exists in the project.
- **Enforced by**: `scripts/assert-unreachable-vulns.sh`, run as a step
  of the `govulncheck` job. "Re-check if a dependency ever starts
  pulling in `x/crypto/openpgp`" is not something a human will reliably
  remember, so the gate now checks it: the script fails the build if
  `x/crypto/openpgp` (or any subpackage) enters the build graph.
- **Action**: none while the guard passes. If it ever fires, drop
  `GO-2026-5932` from `ALLOWED` and re-triage on the real merits -
  most likely by migrating the offending import to
  `github.com/ProtonMail/go-crypto/openpgp`.

## Resolved by direct dependency bumps (2026-09-12)

A vuln-db refresh on 2026-09-02 surfaced one new `x/crypto/ssh` finding
that failed the gate on every Dependabot PR opened after that date, and
by 2026-09-12 two more had joined it. All three are fixed in
`golang.org/x/crypto` v0.56.0; resolved by one
`go get golang.org/x/crypto@v0.56.0` in `backend/`.

- `GO-2026-6303` (ssh source-address restriction not enforced for
  password / keyboard-interactive / no-client-auth / GSSAPI callbacks,
  CVE-2026-56854) - Fixed in v0.55.0
- `GO-2026-6354` (ssh DoS on deadlocked undecided channel,
  CVE-2026-56855) - Fixed in v0.56.0
- `GO-2026-6355` (ssh DoS on deadlocked established channel,
  CVE-2026-78662) - Fixed in v0.56.0

Practical exposure: same as the 2026-05-22 batch - the backend does
not terminate SSH. All three are module-level findings (govulncheck
reports no called symbol), which the gate's filter counts anyway.

**`go` directive moved.** `x/crypto` v0.56.0 declares `go 1.26.0`, so
`backend/go.mod` had to follow (1.25.0 -> 1.26.0). This is the first
time a `go` directive has moved since the "leave them alone" note in
the 2026-08-22 toolchain entry. The reasoning there was keeping the
modules buildable by a consumer on older Go, which matters for
`dnp3go` (a library) and `services/` (still at `go 1.24.0`, both
untouched) but not for `backend/` - it is an application, CI has built
it with 1.26.7 since v0.1.28, and the Docker image is `golang:1.27`.
Expect this to keep happening: the `golang.org/x/*` modules track the
two most recent Go releases, so once 1.27 shipped, 1.26 became their
floor.

Also this round, from `trivy fs` rather than govulncheck:

- **CVE-2026-39882** (`otlptracehttp` memory exhaustion) - `otel`
  v1.42.0 -> v1.44.0. Not in govulncheck's DB and not reachable (otel
  has no exporter wired), but the fix is a minor bump. v1.43.0 was the
  minimum fix and **regressed `GO-2026-5158`** (baggage header length
  cap, the finding v0.1.28 cleared with the 1.41 -> 1.42 bump); govulncheck
  reports it as introduced 1.43.0 / fixed 1.44.0. Second time this
  release cycle that landing on the minimum `Fixed in` version and
  re-scanning turned up a further hop - keep doing the re-scan.
- Frontend: `next` 15.5.24 (CVE-2026-75604 + GHSA-2xp9-vwfh-vxw4, both
  CRITICAL RCE, neither reachable - Linux container, no `next/image`),
  `sharp` override 0.35.4 (GHSA-rgj7-g3m4-5g8c libheif),
  `js-yaml` 4.3.2 (GHSA-2883-xcg3-v3hh, via eslint). `npm audit` 0.

Trivy also lists five `docker/docker` CVEs (CVE-2025-54410,
CVE-2026-33997, CVE-2026-41567, CVE-2026-41568, CVE-2026-42306) against
v27.5.1. Those are the same daemon-side family as the govulncheck
entries in **Open** above - the fixes that exist are on v28/v29
(major bumps Dependabot is configured to suppress) or `moby/moby/v2`,
and the affected code is not in our build graph. No action; the
existing triage entry and `assert-unreachable-vulns.sh` guard cover
them.

No allowlist changes this round.

## Resolved by direct dependency bumps (2026-08-22)

The repo sat idle from early June to late August, so the first
govulncheck run after the gap surfaced a backlog. Five findings were
upstream-fixed and cleared by `go get` in `backend/`:

- `GO-2026-5970` (x/text) - `golang.org/x/text` v0.37.0 -> v0.39.0
- `GO-2026-5676` (quic-go) - `github.com/quic-go/quic-go` v0.57.0 -> v0.59.1
- `GO-2026-5942` (x/net) - `golang.org/x/net` v0.55.0 -> v0.56.0
- `GO-2026-5506` (otel) - `go.opentelemetry.io/otel` v1.39.0 -> v1.41.0
- `GO-2026-5158` (otel) - surfaced *by* the v1.41.0 bump above;
  cleared by going to v1.42.0. Worth noting: bumping to the minimum
  listed `Fixed in` version was not enough here, the scan had to be
  re-run after the bump to see the next one.

`golang.org/x/crypto` rolled v0.52.0 -> v0.53.0 and `x/sys` v0.45.0 ->
v0.46.0 transitively; neither introduced new findings.

Practical exposure was low across the board - quic-go is unused (we
serve HTTP/1.1 + HTTP/2 only, same rationale as the 2026-05-07 entry),
otel is not wired to an exporter, and x/text/x/net sit behind
loopback-bound routing. Bumped anyway to keep the gate green and the
allowlist small.

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
parse/Render on attacker-controlled input - html templating is in
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

## Resolved by Go toolchain bump (2026-08-22)

**`1.25.10 -> 1.26.7`** cleared 3 stdlib findings that were failing the
gate on every open PR since roughly 2026-06-10, in all three modules:

- `GO-2026-5037` (crypto/x509 quadratic `VerifyHostname`,
  CVE-2026-27145) - Fixed in 1.25.11 / 1.26.4
- `GO-2026-5038` (mime `WordDecoder.DecodeHeader` CPU exhaustion) -
  Fixed in 1.25.11 / 1.26.4
- `GO-2026-5039` (net/textproto user input in error strings,
  CVE-2026-42507) - Fixed in 1.25.11 / 1.26.4

All three had fixes on both the 1.25.x and 1.26.x lines. **We moved to
the 1.26 line rather than taking the minimal 1.25.11 patch**, because
Go 1.27.0 has since shipped and Go only supports the two most recent
major releases - pinning 1.25.11 would have put the project on an
already-unsupported line that accrues unpatched stdlib CVEs and
re-breaks this same gate. `services/` and `dnp3go/` are fully clean
after this bump (zero findings).

The `go` directives are deliberately left alone (`backend` at
`go 1.25.0`, `services` and `dnp3go` at `go 1.24.0`) so the modules
stay buildable by a consumer on an older Go; only the `toolchain`
directive, which is what CI's `actions/setup-go` installs, moved.

Dockerfile bases bumped to match, so image builds stay hermetic
instead of downloading a toolchain mid-build (which would break the
offline SSD workflow): `Dockerfile.backend` -> `golang:1.26`;
`Dockerfile.eng-ws`, `Dockerfile.kali`, `Dockerfile.openplc`,
`Dockerfile.vendor-jump`, `services/Dockerfile` -> `golang:1.26-alpine`.

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
and `dnp3go/go.mod` all pinned `toolchain go1.25.10` at the time (since
raised to `go1.26.7`; see "Resolved by Go toolchain bump (2026-08-22)"
above). The `go 1.24.0`
directive (minimum language version) is left alone so the modules
remain buildable by anyone on Go 1.24+ as a consumer.

Dockerfile bases were also bumped at the time (`golang:1.25` /
`golang:1.25-alpine`, from `1.24`); every Go build stage has since
moved to `golang:1.27` / `golang:1.27-alpine` (v0.1.30).

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

**If the exception rests on the vulnerable package being unreachable**
(rather than on a mitigation or an accepted risk), there is a fourth
step: add the package prefix to `GUARDED` in
`scripts/assert-unreachable-vulns.sh`. The allowlist filter matches on
GOID alone and will keep passing the finding even if the package later
enters the build graph, so an unreachability claim that isn't wired
into that script is a claim nothing is checking. Raised by Codex review
on PR #91.
