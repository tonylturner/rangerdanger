# AGENTS.md

Conventions for anyone (human or agent) working in this repository.
Facts here are verified against the tree; if you find one wrong, fix it
in the same change. Longer prose lives in `CONTRIBUTING.md`, `docs/`,
and the per-directory READMEs; this file is the short, authoritative
index.

## What this is

RangerDanger is a Docker Compose cyber range that teaches OT network
segmentation on a simulated electric distribution substation. A Go/Gin
backend orchestrates the containers and talks to the containd NGFW; a
Next.js portal gives students a topology map, in-browser terminals,
YAML-driven labs with live validators, and a process view backed by an
OpenDSS power-flow solve. See `README.md` and `docs/architecture.md`.

## Layout

| Path | What lives there |
|---|---|
| `backend/` | Go 1.26 API (Gin, GORM/SQLite, Docker SDK, containd REST client). Route registration in `internal/server/server.go`; handlers are split across `labs.go`, `topology.go`, `scenarios.go`, `ui_proxy.go` and the other `internal/server/*.go` files. |
| `frontend/` | Next.js 15 app router, React 18, TypeScript, Tailwind, vitest. |
| `services/` | One Go module: relay/recloser/regulator/capbank/rtac sims speak HTTP + Modbus TCP + DNP3 TCP over shared state; historian and gps are HTTP + Modbus only. `opendss-sim/` is Python/FastAPI. |
| `dnp3go/` | Standalone zero-dependency DNP3 library, consumed via `replace`. |
| `lab-definitions/` | Topology template, the 7 lab YAMLs, and the two canned containd policies. |
| `docker-compose.yml` | Build-from-source stack. `docker-compose.release.yml` uses GHCR images; `docker-compose.offline.yml` is the SSD overlay. |
| `scripts/` | Host helpers (`.sh` with `.ps1` twins where they run on Windows) and container-internal scripts (`rtac-harden.sh`, `set-gateway.sh`, entrypoints). |
| `setup.sh` / `stage-ssd*.sh` | Workshop install and offline SSD staging (PowerShell twins alongside). |

## Networking invariants (do not change without an explicit request)

The lab's teaching value depends on this topology being exactly as
documented. Treat the following as frozen unless the person asking
names the change.

- Six Docker networks with fixed subnets and static IPs:
  `mgmt_net` 10.99.99.0/24, `enterprise_net` 10.10.10.0/24,
  `vendor_net` 10.20.20.0/24, `ot_ops_net` 10.30.30.0/24,
  `field_net` 10.40.40.0/24, `physics_net` 10.50.50.0/24. Node IPs are
  listed in `docs/architecture.md` and referenced by absolute IP inside
  lab YAML commands.
- The `firewall` container (containd) is multi-homed as `.2` on every
  zone and is the only path between zones. The Go sims (via their
  `services/Dockerfile` CMD), `kali`, `corp_ws`, `vendor_jump` and
  `eng_workstation` run `set-gateway.sh` at start to default-route
  through it. `fuxa_hmi` and `openplc` receive `GATEWAY` as an env var
  but do not run the script; treat them as known exceptions, not a
  pattern to copy.
- `rtac_sim` is intentionally four-homed. `scripts/rtac-harden.sh` plus
  the compose `sysctls` disable forwarding, drop FORWARD, and replace
  the connected field route with one via the firewall. The hardened
  policy source-pins the RTAC as 10.30.30.20 because of this.
- `physics_net` is deliberately not firewalled. `mgmt_net` is the
  out-of-band control plane for backend, frontend, proxy, and containd.
- All host ports bind to loopback only: 8088 (portal), 9080 / 9443 /
  2222 (containd). The backend API and the WebSocket terminals have no
  authentication; containd, OpenPLC and the vendor-jump / eng-ws desktops use
  baked-in lab credentials (`docs/lab-credentials.md`). `SECURITY.md` explains why.
- containd zone names `wan` / `dmz` / `lan1` / `lan2` / `lan3` and the
  canned policies in `lab-definitions/firewall/` are what the
  validators and firewall smoke gate assert against.

Changes to compose files, network settings, port bindings, sysctls,
gateway or hardening scripts, or the policy JSONs need the requester to
ask for them by name, and must pass `scripts/firewall-smoke.sh`.

## Validation

Run what CI runs (`.github/workflows/ci.yml`) before claiming done:

```sh
test -z "$(git ls-files '*.go' | xargs gofmt -l)"   # the CI gofmt gate; tracked files only, so local Go caches do not trip it
(cd backend  && go vet ./... && go test -race -count=1 ./... && go build ./cmd/server)
(cd services && go vet ./... && go test -race ./... && go build ./...)
(cd dnp3go   && go vet ./... && go test -race ./... && go build ./...)
(cd frontend && npm ci && npm run lint && npm test && npm run build)
docker compose config -q
# Hard gate: vulnerability scan (go install golang.org/x/vuln/cmd/govulncheck@latest first)
./scripts/assert-unreachable-vulns.sh
for d in backend services dnp3go; do (cd "$d" && govulncheck ./...); done
```

`govulncheck` findings outside the `ALLOWED` list in the workflow fail
CI; a new finding needs a triage entry in `docs/security-known-issues.md`
plus the `ALLOWED` list in the workflow. Two extra checks are worth running locally even though `ci.yml`
does not: `npx tsc --noEmit` in `frontend/` (faster than waiting for
`next build` to type-check) and
`docker compose -f docker-compose.release.yml config -q` (which
`smoke.yml` validates).

`-count=1` on the backend matters: `firewall_config_test` reads the
policy JSONs at runtime and Go's test cache does not see them.

With the stack up (`./scripts/dev-up.sh`), the three smoke gates in
`CONTRIBUTING.md` (`smoke-test.sh`, `firewall-smoke.sh`,
`lab-commands-smoke.sh`) cover boot, dataplane, and lab-command rot.

## Conventions

- Go: `gofmt`, godoc comments on exported identifiers. TypeScript: no
  `any`; `npm run lint` is `next/core-web-vitals`.
- Comments explain why, not what.
- Lab content is YAML in `lab-definitions/scenarios/`; the shape,
  `expected_config` values, and the `hardened` alias caveat are in
  `lab-definitions/README.md` and `docs/lab-authoring.md`.
- Adding a sim: follow the checklist in `services/README.md` (compose
  service in both compose files, catalog entry, release matrix row).
- Branch off `main`, one concern per PR, CI green. Branch names in use:
  `fix/…`, `chore/…`, `docs/…`. Add a line under `[Unreleased]` in
  `CHANGELOG.md` for anything user-visible.
- Releases: `RELEASING.md`. Tags `v*` publish multi-arch images to GHCR.
