# RangerDanger documentation

This is the routed landing page for everything under `docs/`. Pick the
audience that best fits what you are trying to do.

For a one-paragraph project description and quick install, see the
top-level [`README.md`](../README.md). For the day-by-day changelog
see [`CHANGELOG.md`](../CHANGELOG.md), and for forward-looking direction
see [`ROADMAP.md`](../ROADMAP.md).

## Start here

- **Install the lab on your laptop** &rarr; [`quickstart.md`](quickstart.md)
- **Run an offline / SSD workshop** &rarr; [`workshop-ssd.md`](workshop-ssd.md)
- **Understand the lab and zones** &rarr; [`architecture.md`](architecture.md)

## Student experience

Use these if you are a workshop attendee or self-learner working through
the labs.

- [`workshop-overview.md`](workshop-overview.md) - Lab-by-lab walkthrough.
  What each lab simulates, what it does not, what skills it teaches.
- [`lab-credentials.md`](lab-credentials.md) - The canonical lab
  credentials and which surfaces use them (containd UI, SSH-on-`:2222`,
  RTAC management endpoints, vendor jump host RDP/VNC, etc.).
- [`quickstart.md`](quickstart.md) - Install path, common errors,
  what to do when something does not come up.

## Instructor / workshop operator

Use these if you are running a workshop, staging an SSD, or supporting
students mid-class.

- [`workshop-ssd.md`](workshop-ssd.md) - SSD distribution operator
  runbook. Initial stage, student first-run install, mid-workshop delta
  patches, recovery scenarios.
- [`workshop-overview.md`](workshop-overview.md) - The deck alignment
  and what to emphasize per lab.
- [`quickstart.md`](quickstart.md) - Path C covers the `--from-tarballs`
  flow students run on workshop morning.
- Pre-workshop check: every student should run
  `./setup.sh --check-only` the night before to verify Docker, Compose
  v2, ports, disk, and RAM.

## Architecture and internals

Use these if you are evaluating whether to fork or contribute, or
investigating the lab's design decisions.

- [`architecture.md`](architecture.md) - Zone model, node inventory,
  service interactions, data flow. The multi-homed RTAC kernel-pinning
  compensating control is documented here.
- [`architecture-diagram.md`](architecture-diagram.md) - The Docker
  architecture view: compose stack composition, network wiring, request
  flow. Mermaid diagrams pre-rendered to SVG so the page works in any
  markdown viewer.
- [`api-spec.md`](api-spec.md) - Backend REST and WebSocket reference.
  Useful when wiring custom tools or automation.
- [`tool-inventory.md`](tool-inventory.md) - Which CLI tool lives in
  which Dockerfile by lab persona. Useful when authoring new exercises.

## Lab content

Use these if you are authoring new exercises or extending the existing
ones.

- [`lab-authoring.md`](lab-authoring.md) - How to write a workshop lab.
  YAML shape, the runner-specific fences (`:::hint`, `:::decision`,
  `:::findings-panel`, `:::plan-coverage`), the localStorage model that
  lets a lab read student selections from earlier labs, authoring
  checklist.
- The 7 shipped labs live in
  [`lab-definitions/scenarios/`](../lab-definitions/scenarios/) (YAML).
  The substation topology and firewall policies live alongside.

## Security model

Use these if you are reviewing the lab security posture or evaluating
external-access patterns.

- [`../SECURITY.md`](../SECURITY.md) - Lab security model. All
  host-exposed ports bind to `127.0.0.1`. No auth on terminals or the
  backend by design (loopback-only trust boundary). Default credentials
  are baked-in lab conveniences and explicitly called out as not
  secrets. Supported and unsupported patterns for deliberately exposing
  the stack to other machines.
- [`security-known-issues.md`](security-known-issues.md) - Triaged
  `govulncheck` and Trivy findings with rationale, allowlist contract
  for the hard-gate CI job.

## Extending RangerDanger

Use these if you want to contribute, run local development, or modify
the platform.

- [`../CONTRIBUTING.md`](../CONTRIBUTING.md) - Local dev setup, the
  test gauntlet (backend Go tests + frontend vitest + firewall traffic
  smoke + lab-commands smoke), PR conventions.
- [`../RELEASING.md`](../RELEASING.md) - Release tagging procedure,
  GHA workflow, containd image policy ("fix containd, not the pin"),
  the workshop-day determinism trade-off.

## What is not in this directory

- [`../ROADMAP.md`](../ROADMAP.md) - Forward-looking direction
  (v0.2.0 FUXA HMI screens, v0.3.0 multi-user, backlog ideas).
- [`../CHANGELOG.md`](../CHANGELOG.md) - Per-release history.
- [`../SUPPORT.md`](../SUPPORT.md) - Where to ask questions, what to
  expect from maintainers, separate channels for security versus
  commercial-workshop support.
