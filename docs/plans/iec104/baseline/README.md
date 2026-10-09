# US baseline (increment 0)

The fixture that every later increment compares the US package against.
"US gates unchanged" means: the same counts, the same skip lines, and no
new failing row.

## Source

- Release: `v0.1.34` (`b653743`), clean clone of the tag, release images
  from GHCR, on macOS (Docker Desktop), 2026-10-09.
- containd: `ghcr.io/tonylturner/containd:latest` resolved to
  `sha256:47fdae83e3a2127516747cf9bf75e2762bb4134f544be1acf40ba6c7fca180d0`
  (image created 2026-09-28). containd tracks `:latest` (decision D11), so
  record the digest of every later gate run beside its result.

## Smoke gates (verbatim logs in this directory)

| Gate | Result | Log |
|---|---|---|
| firewall | matrix passed 54 / 54 | [`firewall-smoke.log`](firewall-smoke.log) |
| lab-commands | passed 69, skipped 1 (mutating, interactive, or out-of-scope) | [`lab-commands-smoke.log`](lab-commands-smoke.log) |
| events | passed 10 / 10 | [`events-smoke.log`](events-smoke.log) |
| substation | PASS | [`substation-smoke.log`](substation-smoke.log) |
| test-suite | passed 40 / 40 (18 auto-passed), reset failures 0 | [`test-suite-smoke.log`](test-suite-smoke.log) |

The 18 auto-passed test-suite steps and the hidden skips are known
weaknesses of the gates, not of the range. They are tracked separately and
must not be "fixed" by changing these numbers on this branch.

## Unit suites

Recorded in [`suites.md`](suites.md): Go (backend, services, dnp3go) and
frontend (lint, vitest, build) at the plan commit.

## Gate runs on `iec104`

| Date | Commit | containd digest | Result |
|---|---|---|---|
| 2026-10-09 | `7d09667` (increment-0 fixes, source build, macOS) | `sha256:47fdae83…` | firewall 54/54, lab-commands 69 + 1 skipped, events 10/10, substation PASS, test-suite 40/40 (18 auto-passed). Matches baseline. |
| 2026-10-09 | `fcde077` (increment 1: packages, declared validators, step IDs, package-scoped browser state; containd flows/token/health fixes; source build, macOS) | `sha256:47fdae83…` | firewall 54/54, lab-commands 69 + 1 skipped, events 10/10, substation PASS, test-suite 40/40 (18 auto-passed). Matches baseline. |
| 2026-10-09 | `db5537e` (increment 2a: platform and range projects, package lint, range lifecycle and portal Range control; source build, macOS) | `sha256:fa56316b…` (`:latest` moved) | firewall 54/54, lab-commands 69 + 1 skipped, events 10/10, substation PASS, test-suite 40/40 (18 auto-passed). Matches baseline. US → US range round trip (generation 1 → 2, 20 s) then firewall 54/54 and events 10/10 again. Teardown left 0 containers, 0 networks, no new dangling volumes. |
