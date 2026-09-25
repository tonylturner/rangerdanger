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
  and pins `toolchain go1.26.7`, which `go` downloads automatically);
  the `services/` and `dnp3go/` modules declare `go 1.24.0` as their
  minimum but pin the same `go1.26.7` toolchain.
- **Node.js 20+** and **npm** for the frontend.

Bring up the stack - either invocation works:

```sh
./scripts/dev-up.sh           # build-from-source path; passes extra args through
docker compose up -d --build  # equivalent if you prefer the raw command
```

`dev-up.sh` is a thin wrapper around `docker compose up --build -d`
that resolves the compose file relative to the script (so it works
from any cwd) and forwards extra args. To stop:

```sh
./scripts/dev-down.sh         # → docker compose down
```

Both scripts target `docker-compose.yml` (the source-build file).
For the release-image path see [`docs/quickstart.md`](docs/quickstart.md).

Open http://localhost:8088 - the UI is the entry point.

## Repository layout

| Path | Purpose |
|------|---------|
| `backend/` | Go HTTP API (Gin, GORM, SQLite) |
| `frontend/` | Next.js + TypeScript UI |
| `services/` | Field device simulators (relay, recloser, regulator, rtac, opendss, capbank, historian, gps) |
| `dnp3go/` | Standalone Go DNP3 library, vendored as its own module |
| `lab-definitions/` | YAML lab topologies, exercises, firewall configs |
| `docs/` | Architecture, API spec, workshop guides, lab-authoring guide |
| `scripts/` | Dev helpers (`dev-up.sh`, `dev-down.sh`, `seed-labs.sh`) |
| `.github/workflows/` | CI |

## Running tests

The same commands CI runs (`.github/workflows/ci.yml`):

```sh
# gofmt gate (tracked files only, so local Go caches do not trip it)
test -z "$(git ls-files '*.go' | xargs gofmt -l)"

# Backend. -count=1 matters: firewall_config_test reads the policy JSONs
# at runtime and Go's test cache does not see them.
(cd backend && go vet ./... && go test -race -count=1 ./... && go build ./cmd/server)

# Services (simulators)
(cd services && go vet ./... && go test -race ./... && go build ./...)

# DNP3 library
(cd dnp3go && go vet ./... && go test -race ./... && go build ./...)

# Frontend
(cd frontend && npm ci && npm run lint && npm test && npm run build)

# Compose validation (dev and release)
docker compose config -q
docker compose -f docker-compose.release.yml config -q

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

Three layered smoke gates protect the lab against regressions. Run
them after any change that touches lab content, the firewall
dataplane, the policy YAMLs, or the simulator images. Each requires
the compose stack to be up:

```sh
docker compose up -d --build

# 1. Inventory + boot. Lab YAML count, scenario IDs, sim health.
./scripts/smoke-test.sh

# 2. Firewall traffic enforcement matrix. Applies weak then improved
#    via the lab API and probes positive + negative flows from inside
#    each container. Catches dataplane regressions.
./scripts/firewall-smoke.sh

# 3. Lab-doc rot. Every CMD_TOOL_RE-matching command in every lab
#    YAML run via docker exec from the right container under the
#    right policy. Catches typo'd IPs, missing tools, wrong source
#    containers.
./scripts/lab-commands-smoke.sh

# Single scenario instead of all:
./scripts/lab-commands-smoke.sh baseline-assessment
./scripts/firewall-smoke.sh weak       # or "improved", or "both"
```

CI runs all three on every PR and push to main
(`.github/workflows/smoke.yml`). Locally is faster because the
images are already cached.

## Networking invariants

The lab's teaching value depends on the topology being exactly as
documented. Treat the following as frozen; a PR that changes any of
them needs to say so explicitly and must pass `scripts/firewall-smoke.sh`.

- Six Docker networks with fixed subnets and static IPs:
  `mgmt_net` 10.99.99.0/24, `enterprise_net` 10.10.10.0/24,
  `vendor_net` 10.20.20.0/24, `ot_ops_net` 10.30.30.0/24,
  `field_net` 10.40.40.0/24, `physics_net` 10.50.50.0/24. Node IPs are
  listed in [`docs/architecture.md`](docs/architecture.md) and
  referenced by absolute IP inside lab YAML commands.
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
  authentication; containd, OpenPLC and the vendor-jump / eng-ws desktops
  use baked-in lab credentials
  ([`docs/lab-credentials.md`](docs/lab-credentials.md)).
  [`SECURITY.md`](SECURITY.md) explains why.
- containd zone names `wan` / `dmz` / `lan1` / `lan2` / `lan3` and the
  canned policies in `lab-definitions/firewall/` are what the
  validators and firewall smoke gate assert against.

This covers the compose files, network settings, port bindings,
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
`:::decision`, `:::findings-panel`), the localStorage model that
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
