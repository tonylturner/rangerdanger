# Support

RangerDanger is open source under Apache 2.0. This document explains
where to ask questions, what to expect from maintainers, and which
channel to use for what.

## Where to ask

| If you want to… | Use |
|---|---|
| Ask a question, share an idea, or discuss workshop use | [GitHub Discussions](https://github.com/tonylturner/rangerdanger/discussions) |
| Report a bug | [Open an issue](https://github.com/tonylturner/rangerdanger/issues/new?template=bug_report.md) (use the bug template) |
| Request a feature or new exercise | [Open an issue](https://github.com/tonylturner/rangerdanger/issues/new?template=feature_request.md) (use the feature template) |
| Report a **security vulnerability** | **Do not** open a public issue. See [`SECURITY.md`](SECURITY.md) for the disclosure address. |
| Propose a code change | [Open a pull request](https://github.com/tonylturner/rangerdanger/compare). Read [`CONTRIBUTING.md`](CONTRIBUTING.md) first. |

## What to expect

This is **best-effort open-source support** maintained alongside other
work. Realistic expectations:

- **Bug reports**: triage within ~1 week. Good repro steps and the
  output of `GET /api/build` get faster attention.
- **Pull requests**: triage within ~1 week. CI must be green; lab
  content changes should include a play-through note (see
  `CONTRIBUTING.md`).
- **Discussions / questions**: best-effort. The community is welcome
  to answer first.
- **Security vulnerabilities**: acknowledged within 5 business days
  per `SECURITY.md`.

## What this isn't

- **Not a SaaS.** RangerDanger is software you run on your own laptop.
  There's no hosted version to log a ticket against.
- **No SLA.** No guaranteed response time outside the security
  channel.
- **Not a paid product.** Workshop delivery, custom exercises, and
  instructor-led training are organized through
  [DefendICS](https://defendics.org), the nonprofit under which
  RangerDanger is released. Open a Discussion or use the contact
  routing in [`SECURITY.md`](SECURITY.md) if you'd like to talk -
  but the OSS project itself stays free.

## Before opening an issue

Most "the lab won't start" issues fall into one of these:

1. **Docker isn't running** or **Compose v2 isn't installed.** Run
   `./setup.sh --check-only` (or `setup.ps1 -CheckOnly` on Windows).
2. **Ports already in use** (`8088`, `9080`, `9443`, `2222`). Same
   pre-flight catches this.
3. **Out of disk** during a 6+ GB image pull. Free up ≥30 GB.
4. **Out of memory** on Docker Desktop's allocated VM. Bump to ≥8 GB
   in Settings → Resources.
5. **You're on a network that blocks `ghcr.io`** (rare but happens
   on conference Wi-Fi or behind aggressive corporate proxies). Use
   the offline path: `./stage-ssd.sh <out>` from a machine that has
   pull access, then `setup.sh --from-tarballs <out>` on the target.

If none of those, a good bug report includes:

- The output of `./setup.sh --check-only` (or `-CheckOnly`)
- `docker compose -f docker-compose.release.yml logs <service>`
  for whichever service didn't come up
- `GET /api/build` if the API is reachable at all
- The relevant excerpt from `~/Library/Logs/Docker Desktop/log.log`
  (macOS) or `%LOCALAPPDATA%\Docker\log` (Windows) if Docker itself
  is misbehaving
