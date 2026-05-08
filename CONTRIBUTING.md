# Contributing to RangerDanger

Thanks for your interest. RangerDanger is an OT/ICS cyber range and a
lab platform for ICS network segmentation training. The project lives
at https://github.com/tonylturner/rangerdanger.

## Development environment

You will need:

- **Docker Desktop** ≥ 4.30 (or equivalent) with **≥ 8 GB RAM** and
  **≥ 30 GB free disk** allocated. The compose stack pulls roughly
  6–8 GB on a first run.
- **Go 1.24+** for backend, services, and the `dnp3go` library.
- **Node.js 20+** and **npm** for the frontend.

Bring up the stack:

```sh
docker compose up -d --build
```

Open http://localhost:8088 — the UI is the entry point.

## Repository layout

| Path | Purpose |
|------|---------|
| `backend/` | Go HTTP API (Gin, GORM, SQLite) |
| `frontend/` | Next.js + TypeScript UI |
| `services/` | Field device simulators (relay, recloser, regulator, rtac, opendss, capbank, historian, gps) |
| `dnp3go/` | Standalone Go DNP3 library, vendored as its own module |
| `lab-definitions/` | YAML lab topologies, exercises, firewall configs |
| `docs/` | Architecture, API spec, release plan |
| `scripts/` | Dev helpers (`dev-up.sh`, `dev-down.sh`, `seed-labs.sh`) |
| `.github/workflows/` | CI |

## Running tests

The same commands CI runs:

```sh
# Backend
(cd backend && go vet ./... && go test -race ./... && go build ./cmd/server)

# Services (simulators)
(cd services && go vet ./... && go test -race ./...)

# DNP3 library
(cd dnp3go && go vet ./... && go test -race ./...)

# Frontend
(cd frontend && npm ci && npm run lint && npm run build)

# Compose validation
docker compose config -q
```

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

## Code style

- **Go**: standard `gofmt` / `goimports`. Comment exported identifiers
  per the godoc convention (`// FunctionName ...`).
- **TypeScript**: `npm run lint` enforces `next/core-web-vitals`. Avoid
  `any` types.
- **Comments**: write the *why*, not the *what*. Don't explain code
  that's already obvious from naming.

## Pull requests

1. Branch off `main`.
2. Keep PRs focused — one concern per PR is much easier to review than
   a sprawling change.
3. Update `CHANGELOG.md` under `[Unreleased]` if your change is
   user-visible.
4. CI must be green.
5. If you touch lab content, run through the affected exercise(s) end
   to end before opening the PR.

## Authoring labs

If you're writing a new lab or reshaping an existing one, see
[`docs/lab-authoring.md`](docs/lab-authoring.md) — covers the YAML
shape the runner expects, the description-body fences (`:::hint`,
`:::decision`, `:::findings-panel`), the localStorage model that
lets a lab read what the student did in earlier labs, and the
authoring checklist (run the smoke tests before opening a PR).

## What kinds of changes are most welcome

- New ICS protocol simulators or DPI rules in containd.
- Additional exercises in `lab-definitions/scenarios/`.
- Frontend polish on the network console, exercise runner, or HMI.
- Documentation improvements — the lab is most useful when the docs
  match the code.
- Tests, especially for `backend/internal/server/`,
  `services/rtac-sim`, and `dnp3go/`.

## Reporting bugs and requesting features

Open a GitHub issue. For security issues, follow `SECURITY.md` instead.
