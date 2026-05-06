# Security Policy

## Reporting a vulnerability

If you discover a security issue in RangerDanger, please **do not** open
a public GitHub issue. Email **security@sentinel24.com** with:

- A description of the issue and where it lives in the codebase.
- Reproduction steps or a proof of concept.
- The version (`GET /api/version`) and the commit SHA you're testing
  against.
- Whether you would like to be credited in the fix.

You will receive an acknowledgement within five business days. We will
work with you on a coordinated disclosure timeline appropriate to the
severity.

## Lab security model

RangerDanger is intended as a **single-student lab environment that
runs on the student's own laptop**. The default deployment posture is:

- **No authentication on the backend API.** Every endpoint is open to
  anything that can reach the host. This is intentional for a
  single-tenant lab. Do not expose the backend (port 8088) to a network
  you do not fully trust.
- **Default credentials.** `containd/containd` (containd CLI/SSH),
  `openplc/openplc` (OpenPLC web UI), and `CONTAIND_JWT_SECRET=
  rangerdanger-dev` are baked in for the lab. They are not secrets and
  must never be reused outside the lab.
- **Self-signed TLS.** Webtop and containd ship self-signed certs that
  are regenerated on first run. Browser warnings are expected.
- **Privileged containers.** The `firewall` (containd) container runs
  with `NET_ADMIN`, `NET_RAW`, and `SYS_TIME` capabilities so it can
  manage nftables, iptables, and run tcpdump. The simulators run
  unprivileged.

If you are deploying RangerDanger in any context other than a single
student's own laptop, you are responsible for adding authentication,
TLS, and network isolation in front of it.

## Supported versions

Once `v0.1.0` ships, the latest minor release will receive security
fixes. Older minor releases will not.

## Hardening tracked for the public release

See `docs/release-plan.md` for the running list of security-related
items being addressed before the first public release, including:

- Tightening the `/api/workshop/nodes/:nodeId/exec` allowlist
  (currently passes user input through `/bin/sh -c`).
- Documenting the no-auth posture explicitly in the README quickstart.
- Pinning all upstream container images by digest.
