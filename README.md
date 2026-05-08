# RangerDanger

**An OT/ICS cyber range for hands-on network segmentation training.**

[![CI](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml/badge.svg)](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tonylturner/rangerdanger?include_prereleases&sort=semver)](https://github.com/tonylturner/rangerdanger/releases)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go](https://img.shields.io/github/go-mod/go-version/tonylturner/rangerdanger?filename=backend%2Fgo.mod&logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-14-000000?logo=next.js&logoColor=white)](https://nextjs.org)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/compose)
[![containd](https://img.shields.io/badge/containd-NGFW-f97316)](https://github.com/tonylturner/containd)

A simulated electric distribution substation in Docker — multi-zone networks, field devices speaking Modbus and DNP3, a multi-homed RTAC, and a real DPI-capable firewall ([containd](https://github.com/tonylturner/containd)) at the edge. Students work through 7 labs aligned to the DefendICS workshop: identify required flows, design segmentation, plan under a labor budget, build the policy, stress-test it against attacks, and validate with PCAP evidence.

> **Lab-only.** All host ports bind to loopback by default. No auth on terminals or the backend. Default credentials are baked in. Do not connect to production. See [`SECURITY.md`](SECURITY.md) for the full model + safe external-access patterns.

## Quick start

**Prereqs:** Docker Desktop or Docker Engine + Compose v2 · 8 GB RAM (16 recommended) · 30 GB free disk · Apple Silicon or x86_64 · loopback ports `8088 / 9080 / 9443 / 2222` free.

**macOS / Linux:**

```bash
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
./setup.sh
```

**Windows (PowerShell):**

```powershell
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
.\setup.ps1
```

**Build from source instead** (developers):

```bash
docker compose up -d --build
```

**Offline / SSD** (for workshops where bandwidth is constrained):

```bash
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

Use `./stage-ssd.sh /Volumes/WORKSHOP_SSD vX.Y.Z` to populate the SSD, or grab the per-release tarballs from [GitHub releases](https://github.com/tonylturner/rangerdanger/releases).

Once the stack is up:

| | URL | Credentials |
|---|---|---|
| **RangerDanger UI** | http://localhost:8088 | — |
| containd Web UI | http://localhost:9080 | containd / containd |
| containd SSH | `ssh -p 2222 containd@localhost` | containd / containd |
| FUXA HMI | http://localhost:8088/apps/fuxa-hmi/ | — |
| OpenPLC | http://localhost:8088/apps/openplc/ | — |

Open [http://localhost:8088/exercises](http://localhost:8088/exercises) and start with **Lab 1.2**.

## What's in the lab

### Network zones

All cross-zone traffic transits the containd firewall.

| Zone | Subnet | Hosts |
|------|--------|-------|
| Enterprise | 10.10.10.0/24 | corp-ws, kali |
| Vendor / DMZ | 10.20.20.0/24 | vendor-jump, eng-ws |
| OT Operations | 10.30.30.0/24 | fuxa-hmi, rtac, openplc, historian, gps |
| Field Devices | 10.40.40.0/24 | relay, recloser, regulator, capbank |
| Physics | 10.50.50.0/24 | OpenDSS feeder physics (not firewalled) |

### Labs

Aligned to the DefendICS OT Network Segmentation workshop agenda.

| Lab | Title | Time | Focus |
|-----|-------|------|-------|
| 1.2 | Baseline Traffic Analysis | 15 min | Capture + analyze cross-zone flows; identify critical conduits |
| 1.3 | Segmentation Requirements & Policy Design | 15 min | Translate findings into a zone-pair rule grid + DPI policy |
| 1.4 | Remediation Planning Under Constraint | 15 min | Choose what to fix under a finite labor + per-role budget |
| 2.2 | Firewall Policy Implementation | 30 min | Build a least-privilege containd policy from your plan |
| 2.3 | Protocol-Hardened Configurations | 15 min | Stress-test against Modbus override + DNP3 injection (DPI lesson) |
| 2.3-bonus | Vendor Remote Access Compromise *(optional)* | 15 min | RDP/VNC pivot from enterprise → vendor → field |
| 2.4 | Testing & Validation | 15 min | Holistic positive/negative tests + PCAP evidence + reflection |

Each lab includes inline terminals, auto-run command buttons, per-exercise notes, and backend validators that check substation state, capture files, and firewall policy.

## Why it's different

Most OT training labs fall back to iptables and pretend. RangerDanger uses [containd](https://github.com/tonylturner/containd) — a purpose-built NGFW with ICS deep-packet inspection — so students see and control Modbus function codes, DNP3 Direct Operate commands, and traffic flows at a level that matches what modern OT-aware firewalls actually do.

| Typical OT lab | RangerDanger |
|----------------|--------------|
| iptables or OPNsense | containd NGFW with ICS DPI (Modbus, DNP3, CIP) |
| Separate tools for capture, IDS, SCADA | Unified web UI with embedded DPI visibility |
| Static, manually-provisioned containers | Dynamic compose orchestration per lab |
| Notion workbooks or PDFs | Interactive runner with inline terminals + auto-validation |
| Real VMs per lab seat | Lightweight containers — runs on a laptop |

## Architecture

A Next.js dashboard fronts a Go + Gin backend that orchestrates Docker containers, talks to containd over REST, and runs the scenario validators. Field-device simulators implement Modbus TCP + DNP3 TCP + REST against shared state, with OpenDSS providing feeder physics. Full breakdown — zone model, node inventory, service interactions, data flow — in [`docs/architecture.md`](docs/architecture.md).

```
Browser → Nginx proxy → { Backend (Go), Frontend (Next.js), containd NGFW (Web UI + SSH) }
                              │
                              └── Docker SDK → simulators across 5 zone networks
```

## Documentation

- [`docs/quickstart.md`](docs/quickstart.md) — Full install walkthrough including offline/SSD path, common errors, and where to look when something breaks
- [`docs/architecture.md`](docs/architecture.md) — Zone model, node inventory, service interactions, data flow
- [`docs/workshop-overview.md`](docs/workshop-overview.md) — Lab-by-lab walkthrough with what's simulated and what isn't
- [`docs/lab-authoring.md`](docs/lab-authoring.md) — How to write a workshop lab: YAML shape, fences, decisions, findings panels
- [`docs/api-spec.md`](docs/api-spec.md) — REST + WebSocket reference
- [`docs/security-known-issues.md`](docs/security-known-issues.md) — Triaged govulncheck/Trivy findings with rationale
- [`docs/tasks.md`](docs/tasks.md) — Active prioritized backlog
- [`SECURITY.md`](SECURITY.md) — Lab security model, external-access patterns, vuln reporting
- [`SUPPORT.md`](SUPPORT.md) — Where to ask questions and what to expect
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Local dev setup, tests, PR conventions
- [`ROADMAP.md`](ROADMAP.md) — Planned v0.2.0 + v0.3.0 + backlog
- [`CHANGELOG.md`](CHANGELOG.md) — Per-release notes

## Related projects

- [containd](https://github.com/tonylturner/containd) — The ICS-aware NGFW at the heart of the lab
- [`dnp3go/`](dnp3go/) — Zero-dependency Go DNP3 library used by the field-device simulators (standalone module vendored in this repo)

## License

Apache License 2.0. See [LICENSE](LICENSE).
