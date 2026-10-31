# Security: known issues

Image findings and called symbol-level findings from `govulncheck` that
the project is aware of but has not acted on, with rationale. Go entries document
exceptions for the `govulncheck` hard-gate job in
`.github/workflows/ci.yml`; module- and package-level findings without a
function trace are not counted. Accepting a new Go finding requires a triage
entry here and adding its GOID to the `ALLOWED` list in
`scripts/govulncheck-gate.sh`.

The triage cadence is per-release. If you tag a new version, sweep
this file against `gh run view <latest-ci-run> --log-failed` and
either remove resolved entries or add new ones.

## Image triage (2026-10-30)

The v0.2.0 baseline is the 20 published `v0.2.0-rc.4` images, not
`main` or `latest`. Trivy 0.71.2, database updated 2026-10-30, was run
with `--scanners vuln --severity CRITICAL,HIGH --ignore-unfixed`.
These are installed-package findings, not proof of application reachability.
Unfixed and lower-severity findings are excluded, not accepted by this review.
Kali reports zero matching findings but Trivy warns that `kali-rolling` is
not on its Debian EOL list; that is not evidence of complete Kali OS coverage.

Workstation base refreshes address the old OS, Python/lsiopy and bundled
Docker-tool findings, including Pillow. Both OpenPLC images apply available
public Bionic updates because upstream still publishes the same old digest.
The backend refreshes its bundled Compose binary; frontend updates
the global npm CLI from its versioned, bundled release. No application
lockfile is rewritten or package removed to suppress a finding.

### Accepted upstream groups

These exceptions apply only to the trusted, single-student, loopback-bound
lab described in `SECURITY.md`. They are not production-safety claims.
Revisit each group at the next release, and remove an exception when a
compatible upstream image or bundled CLI clears it. No Trivy suppression
or CI-gate change accompanies this table.

| Image / installed group | Reason and next action |
| --- | --- |
| Backend: distroless `libssl3`, CVE-2026-84782 | The current Debian 12 distroless digest still carries 3.0.20; the listed fix is 3.0.22-1~deb12u2. Wait for a refreshed Debian 12 snapshot rather than mixing foreign libraries into the CGO runtime. |
| Backend and webtops: Compose `golang.org/x/net`, CVE-2026-78669 | Current bundled Compose still embeds v0.58.0; v0.60.0 fixes it. Refresh the complete upstream binary when available, not its embedded module metadata. |
| Frontend: global npm's `undici` | npm 11.21.1 still bundles 6.29.0; CVE-2026-109383, CVE-2026-109869 and CVE-2026-87018 require 6.29.1. These affect the installed package-manager CLI, not Next.js's application dependency tree. Follow a patched bundled npm release. |
| Webtops: `pebble` standard library; Buildx `github.com/moby/go-archive` and `golang.org/x/net` | The refreshed upstream desktop still ships these older compiled components. Follow upstream rebuilt binaries; do not replace desktop-management tools piecemeal. A desktop is a student-controlled lab host, not a trusted general-purpose workstation. |
| FUXA: application dependencies under `server/node_modules`, including its separate npm tree | The current upstream application still contains axios, body-parser, brace-expansion, form-data, ip-address, js-yaml, jsonata, multer, nodemailer, pacote, path-to-regexp, picomatch, serialize-javascript, sigstore, socket.io-parser, tar and ws findings. These are not all unreachable: the lab intentionally exposes the HMI to its student. Follow an upstream lockfile/runtime update with importer, driver and viewer acceptance; do not force major transitive replacements into its installed tree. |
| FUXA: global npm's brace-expansion, ip-address, pacote, picomatch and sigstore | The refreshed upstream image still bundles these older CLI dependencies. Preserve the image's lock-only installation contract; follow a patched base or a separately reviewed, integrity-locked CLI installation rather than an unlocked install in the application image. |
| OpenPLC US/EU: Python 2.7 Flask 1.1.1 / Werkzeug 0.16.0 | CVE-2023-30861, CVE-2023-25577 and CVE-2024-34069 need releases that no longer support the upstream Python 2.7 runtime. A supported OpenPLC runtime migration is required; public OS updates do not make Bionic or Python 2 supported. Keep this default-credential lab UI isolated. |

FUXA still has five critical occurrences: jsonata's CVE-2026-77413,
CVE-2026-77414 and CVE-2026-77415, and CVE-2026-59873 in two tar copies.
Its refreshed base removes 51 old occurrences but introduces two high
occurrences in the global npm tree: pacote's CVE-2026-9496 in a new nested
copy, and picomatch's CVE-2026-33671. Both fall under the CLI exception
above; the net count falls from 111 to 62, not to zero.

### Resolved image groups

OpenDSS's Starlette findings CVE-2025-62727, CVE-2026-48818 and
CVE-2026-54283 are fixed by upgrading to FastAPI 0.133.0 and Starlette
1.3.1 in `services/opendss-sim/requirements.txt` and validating both
simulation profiles.

## Open

### docker/docker - `GO-2026-4887`, `GO-2026-4883`

- **Module**: `github.com/docker/docker@v27.5.1+incompatible`
- **Affects**: backend Docker SDK calls (container exec, lifecycle,
  image inspection)
- **Upstream fix**: `Fixed in: N/A` for both. The `docker/docker`
  module path has **no** patched release. Upstream's fix for the
  2026-08 advisories landed on the renamed `github.com/moby/moby/v2`
  module (`v2.0.0-beta.14+`), which is a different module path and a
  beta line - not a bump we can take from `docker/docker`.
- **Reachability**: `govulncheck` reports symbol-level call traces for
  both findings in the backend. These are not accepted on an
  unreachable-package rationale.
- **Mitigation**: lab-only deployment is loopback-bound (A3); the
  Docker socket mount is in the always-trusted backend container; no
  untrusted input reaches the affected SDK paths.
- **Action**: monitor https://github.com/moby/moby for a `docker/docker`
  release containing the fix, or for the `moby/moby/v2` module to reach
  a stable release worth migrating to. Until then, the `docker/docker`
  direct dependency in `backend/go.mod` stays pinned at the current
  version.

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
not terminate SSH. All three were module-level findings (govulncheck
reported no called symbol); the previous gate overcounted them, and the
symbol-level gate now excludes them.

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
and the affected daemon code is not in our build graph. No action.

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

## Resolved by Go toolchain bump (2026-10-25)

**`1.26.9 -> 1.26.10`** clears `GO-2026-6809` in the standard-library
`crypto/x509` package (fixed in Go 1.26.10 / 1.27.3). All four Go modules
now pin `toolchain go1.26.10`; the IEC 104 service, Kali, and engineering
workstation builders use the digest-locked `golang:1.26.10-alpine` image.
The 1.26.10 Alpine multi-platform index digest is
`sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba`.

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
`ALLOWED` list in `scripts/govulncheck-gate.sh`. To accept a new
finding:

1. Add a triage entry to the **Open** section above with module,
   affected paths, upstream fix status, mitigation, and action.
2. Append the GOID to `ALLOWED` in `scripts/govulncheck-gate.sh`.
3. Add a `### Security` note to `CHANGELOG.md` under `[Unreleased]`.

Same PR for all three - the entry, the gate change, and the
changelog note travel together so the acceptance is reviewable.

**If the exception rests on the vulnerable package being unreachable**
(rather than on a mitigation or an accepted risk), there is a fourth
step: add the package prefix to `GUARDED` in
`scripts/assert-unreachable-vulns.sh`. The gate applies the allowlist
by GOID after selecting called symbol-level findings; the guard checks
that an exception's package-level unreachability rationale remains
true. Raised in review on PR #91.
