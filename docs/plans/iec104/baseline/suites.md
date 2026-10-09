# Unit suites at the plan commit

Run on 2026-10-09 at `f516a5f` (v0.1.34 code plus plan docs), macOS
arm64, local `go1.26.0` toolchain, Node `v26.3.1`. Commands are the
AGENTS validation block (`-race -count=1` everywhere).

| Suite | Result |
|---|---|
| gofmt (tracked files) | clean |
| backend: vet, test, build | pass, 7 packages with tests |
| services: vet, test, build | pass, 9 packages with tests |
| dnp3go: vet, test, build | pass, 1 package with tests |
| frontend: lint, vitest, build | pass, 12 files / 117 tests |
| `docker compose config -q` (source and release) | pass |

Packages with tests:

- `github.com/tturner/rangerdanger/backend/internal/config`
- `github.com/tturner/rangerdanger/backend/internal/containd`
- `github.com/tturner/rangerdanger/backend/internal/db`
- `github.com/tturner/rangerdanger/backend/internal/labs`
- `github.com/tturner/rangerdanger/backend/internal/models`
- `github.com/tturner/rangerdanger/backend/internal/orchestrator`
- `github.com/tturner/rangerdanger/backend/internal/server`
- `github.com/tonylturner/dnp3go`
- `github.com/tturner/rangerdanger/services/capbank-sim`
- `github.com/tturner/rangerdanger/services/dnp3wire`
- `github.com/tturner/rangerdanger/services/gps-sim`
- `github.com/tturner/rangerdanger/services/historian-sim`
- `github.com/tturner/rangerdanger/services/recloser-sim`
- `github.com/tturner/rangerdanger/services/regulator-sim`
- `github.com/tturner/rangerdanger/services/relay-sim`
- `github.com/tturner/rangerdanger/services/rtac-sim`
- `github.com/tturner/rangerdanger/services/shared`
