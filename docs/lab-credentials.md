# Lab credentials

Credentials for services that the workshop's segmentation labs
exercise. These are intentionally weak / well-known so students can
focus on the firewall and DPI lessons rather than fighting auth.

The lab is loopback-bound by default (A3 in `docs/release-plan.md`);
treat these credentials as public-knowledge defaults and **do not**
reuse them outside the lab.

## Standard lab account

The same username / password works on every lab service that
requires one. This keeps the workshop friction low — students don't
have to track per-service credentials while they're learning the
network model.

| Field    | Value          |
| -------- | -------------- |
| Username | `rangerdanger` |
| Password | `rangerdanger` |

## Where these credentials are accepted

| Container          | IP             | Service          | Port      | Notes |
| ------------------ | -------------- | ---------------- | --------- | ----- |
| vendor-jump (DMZ)  | `10.20.20.10`  | SSH              | 22        | Full shell. Workshop attacker pivots through this. |
| vendor-jump (DMZ)  | `10.20.20.10`  | HTTP             | 80        | Vendor Remote Access Portal landing page. |
| vendor-jump (DMZ)  | `10.20.20.10`  | HTTPS            | 443       | Same page over TLS (self-signed cert). |
| vendor-jump (DMZ)  | `10.20.20.10`  | RDP (xrdp)       | 3389      | Drops you into an XFCE desktop session. |
| vendor-jump (DMZ)  | `10.20.20.10`  | VNC (x11vnc)     | 5900      | Shares the running kasm desktop session. |
| rtac-sim (OT Ops)  | `10.30.30.20`  | SSH              | 22        | RTAC management shell. Improved policy keeps vendor → OT:22 open for monitoring. |
| rtac-sim (OT Ops)  | `10.30.30.20`  | HTTPS            | 443       | RTAC mgmt portal (self-signed cert). Improved policy keeps vendor → OT:443 open for monitoring. |

Containd's own management plane has separate credentials (the
`containd / containd` admin user; password change forced on first
login via the UI); see `docs/containd-admin.md` if/when added.

## How to test from the lab

These all work from the kali container under the **weak** baseline
policy (which permits enterprise → DMZ on the full vendor port set):

```sh
# from inside kali (or any wan-zone host)
ssh rangerdanger@10.20.20.10                    # password: rangerdanger
curl -k https://10.20.20.10/
xfreerdp /v:10.20.20.10 /u:rangerdanger /p:rangerdanger /cert:ignore
vncviewer 10.20.20.10::5900                     # password: rangerdanger
```

Under the **improved** policy, RDP (3389) and VNC (5900) are
explicitly blocked at the perimeter — those connections will time
out, while SSH/HTTP/HTTPS stay reachable. The `firewall-smoke.sh`
matrix encodes this contrast.

## Where the user is created

`rangerdanger` is created at container startup by:
- `scripts/vendor-jump-services.sh` on vendor-jump (linuxserver/webtop's
  `/custom-cont-init.d/` hook) — for SSH/HTTP/HTTPS/RDP/VNC.
- `scripts/rtac-mgmt-init.sh` on rtac-sim (alpine, runs as part of
  the `rtac-sim` CMD before the Go binary execs) — for SSH/HTTPS
  only.

Adding new credentialed services elsewhere should reuse the
same user/password so the matrix above stays consistent — update
the table above when you do.
