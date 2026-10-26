# Contributing to RangerDanger

Thanks for your interest. RangerDanger is an OT/ICS cyber range and a
lab platform for ICS network segmentation training. The project lives
at https://github.com/tonylturner/rangerdanger.

## Development environment

You will need:

- **Docker Desktop** ≥ 4.30 (or equivalent) with **≥ 8 GB RAM** and
  **≥ 30 GB free disk** allocated. The compose stack pulls roughly
  6–8 GB on a first run.
- **Go 1.26+** for the backend (`backend/go.mod` declares `go 1.26.0`
  and pins `toolchain go1.26.10`, which `go` downloads automatically);
  the `services/` and `dnp3go/` modules declare `go 1.24.0` as their
  minimum but pin the same `go1.26.10` toolchain. `iec104go/` also pins
  `go1.26.10`.
- **Node.js 20+** and **npm** for the frontend.

The lab runs as two Compose projects. The **platform**
(`rangerdanger-platform`: backend, frontend, proxy) comes from the root
`docker-compose.yml` (source) or `docker-compose.release.yml` (release)
and stays up. The **range** (`rangerdanger`: containd, the zones, the
devices, the webtops) comes from
`lab-definitions/packages/<id>/compose.{source,release}.yml`; only the
backend starts and stops it, through `POST /api/range`, which is also
what the UI's package toggle sends. One range runs at a time.

Packages are `us-dnp3-substation` (US DNP3, the fresh-install default) and
`eu-iec104-substation` (EU IEC 104). Each owns its manifest, Compose twins,
proxy routes and curriculum. Select EU with
`./setup.sh --from-source --package eu-iec104-substation`, or
`.\setup.ps1 -FromSource -Package eu-iec104-substation` on Windows.
Windows setup requires Docker Desktop WSL2 and a physical local-drive
checkout; UNC/`\\wsl$`, mapped drives, junctions and Hyper-V are rejected.
See [Windows checks](docs/quickstart.md#windows-docker-desktop-wsl2-only);
support is not a claim of completed real-hardware acceptance.

Bring it up from source:

```sh
./scripts/dev-up.sh           # = ./setup.sh --from-source --skip-firewall-gate
```

It builds the platform and every package's images, starts the
platform, then asks the backend for the range and waits until
`GET /api/range` reports `ready`. Re-run it after a change: it rebuilds
and restarts the range. Extra arguments go to `setup.sh` (for example
`--package <id>` for the first-start package). To stop:

```sh
./scripts/dev-down.sh         # the range, then the platform
```

`dev-down.sh` tears each project down by its Compose label only, run
from an empty directory: the range with
`docker compose -p rangerdanger down -v --remove-orphans` (range models
have no named volumes, so `-v` removes only the webtops' anonymous
`/config` volumes), the platform without `-v`. It fails unless no
container or network of either project, and no volume a range
container mounted, is left. For the release-image path see
[`docs/quickstart.md`](docs/quickstart.md).

Open http://localhost:8088 - the UI is the entry point.

## Repository layout

| Path | Purpose |
|------|---------|
| `backend/` | Go HTTP API (Gin, GORM, SQLite) |
| `frontend/` | Next.js + TypeScript UI |
| `services/` | US and EU device simulators, IEC 104 service, and OpenDSS physics |
| `dnp3go/` | Standalone Go DNP3 library, vendored as its own module |
| `iec104go/` | Standalone Go IEC 104 library |
| `lab-definitions/` | YAML lab topologies, exercises, firewall configs |
| `docs/` | Architecture, API spec, workshop guides, lab-authoring guide |
| `lab-definitions/packages/` | Range packages: `package.yml`, `curriculum/`, `manifest.json`, Compose twins, proxy routes and package-specific content |
| `scripts/` | Dev helpers (`dev-up.sh`, `dev-down.sh`, `seed-labs.sh`) |
| `.github/workflows/` | CI |

## Running tests

The same commands CI runs (`.github/workflows/ci.yml`):

```sh
# gofmt gate (tracked files only, so local Go caches do not trip it)
test -z "$(git ls-files '*.go' | xargs gofmt -l)"

# Backend. -count=1 matters: firewall_config_test reads the policy JSONs
# at runtime and Go's test cache does not see them. The backend builds
# only on unix: it ships as a Linux container and has no native Windows
# build (on Windows, run these in WSL).
(cd backend && go vet ./... && go test -race -count=1 ./... && go build ./cmd/server)

# Services (simulators)
(cd services && go vet ./... && go test -race ./... && go build ./...)

# DNP3 library
(cd dnp3go && go vet ./... && go test -race ./... && go build ./...)

# Frontend
(cd frontend && npm ci && npm run lint && npm test && npm run build)

# Compose validation: platform files, then every package's range files,
# with the checkout as project directory and RANGERDANGER_ROOT
export RANGERDANGER_ROOT="$PWD"
for f in docker-compose.yml docker-compose.release.yml; do
  docker compose -p rangerdanger-platform --project-directory "$PWD" -f "$f" config -q
done
for f in lab-definitions/packages/*/compose.source.yml lab-definitions/packages/*/compose.release.yml; do
  docker compose -p rangerdanger --project-directory "$PWD" -f "$f" config -q
done

# Vulnerability scan (go install golang.org/x/vuln/cmd/govulncheck@latest first)
./scripts/assert-unreachable-vulns.sh
./scripts/govulncheck-gate.sh
```

`govulncheck` findings outside the script's `ALLOWED` list fail. A new
finding needs a triage entry in
[`docs/security-known-issues.md`](docs/security-known-issues.md) plus the
`ALLOWED` list in `scripts/govulncheck-gate.sh`. `npx tsc --noEmit` in `frontend/`
is a faster type-check than waiting for `next build`.

## End-to-end smoke gates

Four layered smoke gates protect the lab against regressions. Run
them after any change that touches lab content, the firewall
dataplane, the policy YAMLs, or the simulator images. Each requires
the lab to be up with the US range (`us-dnp3-substation`) ready, and
each checks `GET /api/range` for that before it asserts anything:

```sh
./scripts/dev-up.sh

# 1. Inventory + boot. Lab YAML count, scenario IDs, sim health.
#    It runs dev-up itself; --keep leaves the lab up for the gates
#    below, without it this script runs dev-down when it finishes.
./scripts/smoke-test.sh --keep

# 2. Firewall traffic enforcement matrix. Applies weak then improved
#    via the lab API and probes positive + negative flows from inside
#    each container. Catches dataplane regressions.
./scripts/firewall-smoke.sh

# 3. Lab-doc rot. Every CMD_TOOL_RE-matching command in every lab
#    YAML run via docker exec from the right container under the
#    right policy. Catches typo'd IPs, missing tools, wrong source
#    containers.
./scripts/lab-commands-smoke.sh

# 4. Workshop test-suite (CI gate 6). Runs every scenario and checks step
#    outcomes plus state resets; this resets the lab state.
./scripts/test-suite-smoke.sh

# Single scenario instead of all:
./scripts/lab-commands-smoke.sh baseline-assessment
./scripts/firewall-smoke.sh weak       # or "improved", or "both"
```

CI runs the firewall, lab-command, and workshop test-suite gates on every
PR and push to main, alongside boot/inventory/simulator-health checks,
events + ICS DPI, and substation physics
(`.github/workflows/smoke.yml`). Locally is faster because the images are
already cached.

## Networking invariants

The lab's teaching value depends on the topology being exactly as
documented. Treat the following as frozen; a PR that changes any of
them needs to say so explicitly and must pass `scripts/firewall-smoke.sh`.

- The US teaching/control topology has six networks with fixed IPAM:
  `mgmt_net` 10.99.99.0/24, `enterprise_net` 10.10.10.0/24,
  `vendor_net` 10.20.20.0/24, `ot_ops_net` 10.30.30.0/24,
  `field_net` 10.40.40.0/24, `physics_net` 10.50.50.0/24. Node IPs are
  listed in [`docs/architecture.md`](docs/architecture.md) and
  referenced by absolute IP inside lab YAML commands. EU adds Control Centre
  (10.60.60.0/24) and Operations DMZ (10.70.70.0/24) and uses station/process
  in place of OT-Ops/physics; the package manifests are authoritative.
- The platform also owns three internal desktop delivery bridges:
  `desk_corp_net` 10.98.1.0/28, `desk_vendor_net` 10.98.2.0/28,
  `desk_eng_net` 10.98.3.0/28 (Engine names `rangerdanger_desk_corp`,
  `rangerdanger_desk_vendor`, `rangerdanger_desk_eng`). Each has gateway `.1`,
  proxy `.2` and at most one desktop `.3`. Desktops have no management leg;
  the proxy keeps management plus all three delivery legs. Range models
  reference delivery networks externally; no runtime network connect.
  They have no containd zone, host egress or backend endpoint. nginx denies
  delivery peers (`10.98.0.0/16`) before all locations, including redirects.
  Verify portal HTTP/WebSocket delivery and management isolation under weak
  and improved policies in both packages after any network change.
- The `firewall` container (containd) is multi-homed as `.2` on every
  zone and is the only path between zones. The Go sims (via their
  `services/Dockerfile` CMD), `kali`, `corp_ws`, `vendor_jump`,
  `eng_workstation`, `openplc`, and `fuxa_hmi` run `set-gateway.sh` at
  start to default-route through it, using the service's `GATEWAY`
  environment variable.
- `rtac_sim` is intentionally four-homed. `scripts/rtac-harden.sh` plus
  the compose `sysctls` disable forwarding, drop FORWARD, and replace
  the connected field route with one via the firewall. The hardened
  policy source-pins the RTAC as 10.30.30.20 because of this.
- `physics_net` is deliberately not firewalled. `mgmt_net` is the
  out-of-band control plane for backend, frontend, proxy, and containd.
- All host ports bind to loopback only: 8088 (portal), 9080 / 9443 /
  2222 (containd). The backend API and the WebSocket terminals have no
  authentication; containd, OpenPLC and the vendor-jump / eng-ws desktops
  use baked-in lab credentials
  ([`docs/lab-credentials.md`](docs/lab-credentials.md)).
  [`SECURITY.md`](SECURITY.md) explains why.
- containd zone names `wan` / `dmz` / `lan1` / `lan2` / `lan3` and the
  canned policies in `lab-definitions/firewall/` are what the
  validators and firewall smoke gate assert against.

This covers the compose files (the platform files and every package's
range files), network settings, port bindings,
sysctls, gateway and hardening scripts, and the policy JSONs.

## Code style

- **Go**: standard `gofmt` / `goimports`. Comment exported identifiers
  per the godoc convention (`// FunctionName ...`).
- **TypeScript**: `npm run lint` enforces `next/core-web-vitals`. Avoid
  `any` types.
- **Comments**: write the *why*, not the *what*. Don't explain code
  that's already obvious from naming.

## Pull requests

1. Branch off `main`.
2. Keep PRs focused - one concern per PR is much easier to review than
   a sprawling change.
3. Update `CHANGELOG.md` under `[Unreleased]` if your change is
   user-visible.
4. CI must be green.
5. If you touch lab content, run through the affected exercise(s) end
   to end before opening the PR.

## Authoring labs

If you're writing a new lab or reshaping an existing one, see
[`docs/lab-authoring.md`](docs/lab-authoring.md) - covers the YAML
shape the runner expects, the description-body fences (`:::hint`,
`:::decision`, `:::findings-panel`), the browser storage model that
lets a lab read what the student did in earlier labs, and the
authoring checklist (run the smoke tests before opening a PR).

## What kinds of changes are most welcome

- New ICS protocol simulators or DPI rules in containd.
- Additional exercises in `lab-definitions/scenarios/`.
- Frontend polish on the network console, exercise runner, or HMI.
- Documentation improvements - the lab is most useful when the docs
  match the code.
- Tests, especially for `backend/internal/server/`,
  `services/rtac-sim`, and `dnp3go/`.

## Reporting bugs and requesting features

Open a GitHub issue. For security issues, follow `SECURITY.md` instead.
