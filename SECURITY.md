# Security Policy

## Reporting a vulnerability

If you discover a security issue in RangerDanger, please **do not** open
a public GitHub issue. Email **security@sentinel24.com** with:

- A description of the issue and where it lives in the codebase.
- Reproduction steps or a proof of concept.
- The version (`GET /api/build`) and the commit SHA you're testing
  against.
- Whether you would like to be credited in the fix.

You will receive an acknowledgement within five business days. We will
work with you on a coordinated disclosure timeline appropriate to the
severity.

## Lab security model

RangerDanger is intended as a **single-student lab environment that
runs on the student's own laptop**. The default deployment posture is:

- **Bound to localhost (127.0.0.1) only.** All four host-exposed ports
  (`8088` UI / `9080` containd UI / `9443` containd HTTPS / `2222`
  containd SSH) are pinned to the loopback interface in
  `docker-compose.yml`. The lab is not reachable from the network.
- **No authentication on the backend API.** Every endpoint is open to
  anything that can reach the host. This is intentional given the
  loopback binding above and the single-tenant design.
- **`/api/workshop/nodes/:nodeId/exec` and the WebSocket terminals**
  let lab users run shell commands inside lab containers. The
  command-allowlist on the exec endpoint is a UI guardrail (it
  prevents accidental destructive commands from auto-run buttons),
  not a security boundary - the WebSocket terminal next to it
  provides equivalent shell access. Both are safe under the
  loopback-binding posture.
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

## Exposing the lab beyond localhost

The default loopback binding is the right answer for a single-student
laptop. There are legitimate cases where you need another machine to
reach the lab - most commonly:

- **Demoing the stack to an instructor** without screen-share.
- **Connecting from a phone or tablet** on the same network for the
  HMI view.
- **Running headless** on a dedicated lab box and using a workstation
  to interact with the UI.

Pick whichever option fits the situation; safest first.

### Option A - SSH local-forward (recommended)

Leaves the lab loopback-bound. The remote machine reaches it through
an authenticated SSH tunnel. From the **other** machine:

```sh
ssh -L 8088:127.0.0.1:8088 \
    -L 9080:127.0.0.1:9080 \
    -L 9443:127.0.0.1:9443 \
    -L 2222:127.0.0.1:2222 \
    you@lab-laptop
```

Then open `http://localhost:8088` on the remote machine. SSH handles
the auth, the lab stays loopback-bound on the host, no compose edit
required.

### Option B - Tailscale / WireGuard / equivalent overlay

Install a mesh-VPN client on both machines, expose the lab on the
overlay interface only:

```yaml
proxy:
  ports:
    - "100.x.y.z:8088:8080"   # the laptop's tailscale IP
```

Same effect as Option A but persistent and works for multiple clients
without re-running `ssh`. Don't use a public WireGuard endpoint for
this - point at the overlay's private address space.

### Option C - Bind to a specific LAN interface (not recommended)

Edit `docker-compose.yml` to bind to your laptop's LAN IP instead of
loopback:

```yaml
proxy:
  ports:
    - "192.168.1.42:8088:8080"   # only your home LAN can reach it
```

Better than `0.0.0.0` (which would expose to *every* interface
including any future-attached network), but still puts the
unauthenticated lab in front of every device on the bound network.
Only do this on a network you fully trust and have no reason to
distrust later (i.e. not conference wifi, hotel wifi, coffee shop).

### Option D - `0.0.0.0` (don't)

Removing the `127.0.0.1:` prefix entirely makes the lab reachable
from every interface on the host. **Don't.** If you find yourself
wanting this, you almost certainly want Option A or B instead.

### Reverting

Once you're done with the external access:

```sh
git checkout -- docker-compose.yml
docker compose up -d   # picks up the loopback binding again
```

## Supported versions

The latest minor release receives security fixes; older minor
releases do not.

## Known issues + ongoing hardening

Documented exceptions live in
[`docs/security-known-issues.md`](docs/security-known-issues.md).
Open security-relevant work is tracked in
[`docs/tasks.md`](docs/tasks.md).
