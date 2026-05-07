# Security: known issues

Findings from `govulncheck` that the project is aware of but has not
acted on, with rationale. Intended as the source-of-truth when the
advisory CI job (`Go vulnerability scan (advisory)` in
`.github/workflows/ci.yml`) reports findings — a triage entry here
plus a CHANGELOG note is the contract before flipping the job from
`continue-on-error: true` to a hard gate.

The triage cadence is per-release. If you tag a new version, sweep
this file against `gh run view <latest-ci-run> --log-failed` and
either remove resolved entries or add new ones.

## Open

After the Go 1.25.9 toolchain bump (commit in this batch), only
the following findings remain. Both have rationale for acceptance
under the loopback-bound deployment.

### docker/docker — `GO-2026-4887` and `GO-2026-4883`

- **Module**: `github.com/docker/docker@v27.5.1+incompatible`
- **Affects**: backend Docker SDK calls (container exec, lifecycle,
  image inspection)
- **Upstream fix**: `Fixed in: N/A` — no patched docker SDK release at
  time of writing
- **Mitigation**: lab-only deployment is loopback-bound (A3); the
  Docker socket mount is in the always-trusted backend container; no
  untrusted input reaches the affected SDK paths
- **Action**: monitor https://github.com/moby/moby for a release
  containing the fix; bump when available. Until then, the
  `docker/docker` direct dependency in `backend/go.mod` stays pinned
  at the current version.

### quic-go — `GO-2025-4233`

- **Module**: `github.com/quic-go/quic-go@v0.54.0` (transitive via
  `github.com/gin-gonic/gin` → `quic-go/quic-go/http3`)
- **Affects**: HTTP/3 QPACK header expansion DoS — only relevant if
  the backend serves HTTP/3, which it does not (it serves HTTP/1.1
  + HTTP/2 via Gin's standard server)
- **Upstream fix**: quic-go v0.57.0
- **Mitigation**: govulncheck flags this as reachable because gin
  imports the http3 package even when the http3 server isn't
  started. Practical exposure under our deployment is zero.
- **Action**: clears once `gin-gonic/gin` releases a version that
  pins quic-go v0.57.0+. Tracked by dependabot's weekly gomod update
  for `/backend`. If gin lags, we can add a `replace` directive
  forcing quic-go v0.57.0 — defer unless the dependabot patch path
  proves slow.

## Resolved by Go toolchain bump (2026-05-07)

The Go toolchain pin was bumped twice this release cycle:

1. **`1.24.3 → 1.24.13`** cleared 6 stdlib findings whose `Fixed in`
   was on the 1.24.x line: `GO-2026-4341` (net/url), `GO-2026-4340`
   (tls), `GO-2026-4337` (tls), `GO-2025-4175` (x509), `GO-2025-4155`
   (x509), `GO-2025-4014` (tar).

2. **`1.24.13 → 1.25.9`** cleared the remaining 7 stdlib findings
   whose `Fixed in` required the 1.25.x line:
   - `GO-2026-4947` (x509 chain build) — Fixed in 1.25.9
   - `GO-2026-4946` (x509 policy validation) — Fixed in 1.25.9
   - `GO-2026-4870` (TLS 1.3 KeyUpdate DoS) — Fixed in 1.25.9
   - `GO-2026-4869` (archive/tar sparse alloc) — Fixed in 1.25.9
   - `GO-2026-4865` (html/template XSS) — Fixed in 1.25.9
   - `GO-2026-4602` (os Root escape) — Fixed in 1.25.8
   - `GO-2026-4601` (net/url IPv6 parse) — Fixed in 1.25.8

The toolchain directive in each module's `go.mod` controls the
version CI's `actions/setup-go` installs (`go-version-file:
<module>/go.mod` reads it). `backend/go.mod`, `services/go.mod`,
and `dnp3go/go.mod` all pin `toolchain go1.25.9`. The `go 1.24.0`
directive (minimum language version) is left alone so the modules
remain buildable by anyone on Go 1.24+ as a consumer.

Dockerfile bases also bumped:
`Dockerfile.backend` → `golang:1.25` (was `1.24`)
`services/Dockerfile`, `Dockerfile.kali`, `Dockerfile.eng-ws` →
`golang:1.25-alpine` (was `1.24-alpine`).

## Triggering the advisory job → hard-fail flip

When this file's "Open" section reaches zero (or only contains
deliberately-accepted findings with rationale), do the flip in
`.github/workflows/ci.yml`:

```yaml
govulncheck:
  name: Go vulnerability scan
  runs-on: ubuntu-latest
  # continue-on-error: true   # remove this line
  steps:
    ...
```

CI then fails any PR that introduces a new finding. New findings get
triaged into this file before merge.

The current "Open" section has only the docker/docker (no upstream
fix) and quic-go (transitive, unused HTTP/3 path) findings. Both
have explicit acceptance rationale tied to the loopback-bound
deployment. If you're comfortable with the rationale, this is the
moment to make govulncheck a hard gate.
