# Quickstart

Full install walkthrough for RangerDanger. The 5-line version lives
in the [README](../README.md); this doc covers the offline / SSD
path, common errors, and what to do when something breaks.

## Prerequisites

| | Minimum | Recommended |
|---|---|---|
| Docker | Docker Desktop or Engine + Compose v2 | latest |
| Host RAM | 16 GB | 32 GB |
| Docker VM RAM allocation | 8 GB | 12 GB |
| Disk | 30 GB free | 50 GB free |
| Host arch | Apple Silicon or x86_64 | - |
| Loopback ports free | 8088, 9080, 9443, 2222 | - |

The 8 GB "Docker VM" line is what `setup.sh` checks against
`docker info --format '{{.MemTotal}}'` (Docker Desktop) or
`/proc/meminfo` (Linux-native Docker Engine). On Docker Desktop, raise
it under Settings -> Resources -> Memory. The lab idles around 4 GB
across all containers and peaks around 6-8 GB during a workshop -
mostly the three webtop containers (`corp_ws` / `vendor_jump` /
`eng_ws`) at their 2 GB caps plus OpenPLC ramping under runtime load.

Host RAM has to cover the Docker VM allocation *plus* macOS/Windows
itself plus the student's browser plus any IDE - 8 GB host is too
tight in practice and will swap-thrash through the workshop. 16 GB
is the realistic floor.

A clean Docker Desktop install on macOS or Windows usually has the
right settings out of the box. Linux hosts running Docker Engine
need Compose v2 (`docker compose`, with a space - not `docker-compose`).

### ARM64 Linux laptops (handled automatically)

One component, OpenPLC, is amd64-only upstream. macOS Apple Silicon
runs it under Rosetta via Docker Desktop; Docker Engine on arm64 Linux
has no such shim. `setup.sh` detects arm64 Linux and auto-registers a
`qemu-x86_64` emulation handler via `tonistiigi/binfmt`, and
`scripts/uninstall-rangerdanger.sh` reverts it (only if setup
installed it). The offline SSD carries the helper image too —
`stage-ssd.sh` and `stage-ssd-delta.sh` include `tonistiigi/binfmt` in
`images-arm64.tar` / `delta-arm64.tar`, so an `--from-tarballs` install
registers emulation with no network. You only need to register it by
hand if you're offline *without* a staged SSD, or you skip `setup.sh`:

```bash
docker run --privileged --rm tonistiigi/binfmt:qemu-v10.2.1 --install amd64
```

Note: this binfmt registration is runtime-only and does not survive a
reboot. Either re-run `setup.sh` after a restart, or install a boot-time
systemd unit once for permanent persistence:

```bash
sudo ./scripts/persist-emulation.sh        # --uninstall to remove
```

Everything else — the containd DPI engine and the other 13 first-party
images — runs natively on arm64; only OpenPLC needs this, and nothing
else depends on it, so the rest of the workshop works either way. x86_64
Linux needs none of this (OpenPLC is native there).

## Install paths

### Path A - Online (default)

Pulls pre-built images from GHCR. Best when bandwidth is fine.

```bash
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
./setup.sh                   # latest release
# or pin a specific version:
./setup.sh --version v0.1.17
```

PowerShell equivalent:

```powershell
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
.\setup.ps1                  # latest release
.\setup.ps1 -Version v0.1.17
```

`setup.sh` runs preflight checks (Docker reachable, Compose v2,
arch, disk, memory, loopback ports), pulls images, brings up the
stack, and waits for `/api/health` to come up.

To re-run only the preflight checks without installing:

```bash
./setup.sh --check-only
```

### Path B - Build from source

For developers / contributors. Builds all 14 first-party images
locally; the first build takes several minutes (Go simulators, Kali
trim, eng-ws, frontend bundle).

```bash
docker compose up -d --build
```

Subsequent runs reuse the layer cache and start in seconds.

### Path C - Offline / SSD (workshops)

For workshops where pulling 6+ GB per student over conference Wi-Fi
isn't realistic. The instructor stages an SSD; students load from it.

**On a machine with internet (instructor):**

```bash
./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.1.17
```

This produces `images-amd64.tar`, `images-arm64.tar`,
`rangerdanger.tgz`, a `.version` marker, and an auto-generated SSD
README on the volume. Tagged releases additionally bundle
`rangerdanger-wsl2-kernel` + `rangerdanger-wsl2-kernel.sha256` so
Windows students get ICS DPI on Labs 2.3 / 2.3-bonus without a
separate kernel download.

**On the workshop laptop (student):**

```bash
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
# Windows: .\setup.ps1 -FromTarballs D:\WORKSHOP_SSD
```

`setup.sh` detects the host architecture, loads the matching
tarball with `docker load`, then runs `docker compose up -d`.

The release artifacts (image tarballs) are also attached to each
[GitHub release](https://github.com/tonylturner/rangerdanger/releases)
if you'd rather download than stage your own SSD.

## Once it's up

| | URL | Credentials |
|---|---|---|
| RangerDanger UI | http://localhost:8088 | - |
| containd Web UI | http://localhost:9080 | containd / containd |
| containd SSH | `ssh -p 2222 containd@localhost` | containd / containd |
| FUXA HMI | http://localhost:8088/apps/fuxa-hmi/ | - |
| OpenPLC | http://localhost:8088/apps/openplc/ | - |

Open [http://localhost:8088/exercises](http://localhost:8088/exercises)
and start with **Lab 1.2** (Baseline Traffic Analysis).

## Common errors

### "the lab doesn't come up"

Most "doesn't start" issues fall into one of these:

1. **Docker isn't running** - start Docker Desktop, or
   `sudo systemctl start docker` on Linux.
2. **Compose v2 isn't installed** - Compose v1 (`docker-compose`,
   with a hyphen) is end-of-life. Install Compose v2 via Docker
   Desktop or `apt install docker-compose-plugin`.
3. **Ports already in use** (`8088`, `9080`, `9443`, `2222`).
   `./setup.sh --check-only` will tell you which.
4. **Out of disk** during a 6+ GB image pull. Free up ≥30 GB.
5. **Out of memory** on Docker Desktop's allocated VM. Bump to
   ≥8 GB in Settings → Resources.
6. **Network blocks `ghcr.io`** (rare but happens on conference
   Wi-Fi or behind aggressive corporate proxies). Use the offline
   path (Path C above).

### "the firewall (`fw-1`) terminal says command not found"

The `fw-1` in-app terminal lands directly in the **containd
appliance CLI** (you'll see the `containd# ` prompt). If you
typed a Linux command like `ls` or `tcpdump` and got "command
not found", you're in the appliance shell - type `shell` (or
`exit`) to drop to bash. Type `containd cli` from bash to come
back.

### "exercises won't load / `/api/scenarios` returns no labs"

Stale local DB after a major schema change. Delete and restart:

```bash
docker compose down
rm -f backend/data/rangerdanger.db
docker compose up -d
```

### "containd won't authenticate"

Stale local users.db after the default password got changed in
a prior session. Delete and restart:

```bash
docker compose down
rm -f data/firewall/users.db data/firewall/users.db-*
docker compose up -d
```

containd's lab-mode default-admin seeding (`CONTAIND_LAB_MODE=1`
in `docker-compose.yml`) will restore the `containd` / `containd`
admin on next boot.

### "the build is slow"

The first `docker compose build` pulls + compiles a lot. Subsequent
builds reuse the layer cache. To force a clean re-pull of just the
firewall image when containd publishes a security fix:

```bash
docker compose pull firewall
docker compose up -d firewall
```

## What a good bug report includes

If none of the above fits, file an issue against
[rangerdanger](https://github.com/tonylturner/rangerdanger/issues).
Helpful contents:

- The output of `./setup.sh --check-only` (or `-CheckOnly`)
- `docker compose -f docker-compose.release.yml logs <service>`
  for whichever service didn't come up
- `GET /api/build` if the API is reachable at all
- The relevant excerpt from `~/Library/Logs/Docker Desktop/log.log`
  (macOS) or `%LOCALAPPDATA%\Docker\log` (Windows) if Docker itself
  is misbehaving

See [`SUPPORT.md`](../SUPPORT.md) for the broader support routing.
