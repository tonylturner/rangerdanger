# Quickstart

Full install walkthrough for RangerDanger. The 5-line version lives
in the [README](../README.md); this doc covers the offline / SSD
path, common errors, and what to do when something breaks.

## Prerequisites

| | Workshop recommendation | More headroom |
|---|---|---|
| Docker | Docker Desktop or Engine + Compose v2 | latest |
| Host RAM | 16 GB | 32 GB |
| Docker VM RAM allocation | 8 GB | 12 GB |
| Disk | 30 GB free | 50 GB free |
| Host arch | Apple Silicon or x86_64 | - |
| Loopback ports free | 8088, 9080, 9443, 2222 | - |

These are capacity recommendations, not `setup.sh` pass thresholds.
The script warns when the checkout filesystem has less than 30 GB free;
it does not measure Docker Desktop's storage volume or disk-image
capacity. It reads Docker's reported memory and warns only below 7
whole GiB, while recommending 8 GB. On Linux-native Docker, if Docker
reports no memory, it falls back to `/proc/meminfo` host RAM. It does
not check macOS host RAM. On Docker Desktop, raise VM memory under
Settings -> Resources -> Memory. The lab idles around 4 GB across all
containers and peaks around 6-8 GB during a workshop - mostly the
three webtop containers (`corp_ws` / `vendor_jump` / `eng_workstation`) at
their 2 GB caps plus OpenPLC ramping under runtime load.

Host RAM has to cover the Docker VM allocation *plus* macOS/Windows
itself plus the student's browser plus any IDE - 8 GB host is too
tight in practice and will swap-thrash through the workshop. 16 GB
is the realistic floor.

Linux hosts running Docker Engine need Compose v2 (`docker compose`,
with a space - not `docker-compose`).

### OpenPLC on ARM64 hosts

OpenPLC is amd64-only upstream. On Apple Silicon, use a Docker Desktop
backend with working amd64 emulation enabled (Rosetta is a Docker
Desktop setting); `setup.sh` does not enable or verify it. On arm64
Linux, `setup.sh` can register a `qemu-x86_64` handler via
`tonistiigi/binfmt`, and `scripts/uninstall-rangerdanger.sh` reverts
it only if setup installed it. If registration fails, setup prints a
command to register the handler manually:

```bash
docker run --privileged --rm tonistiigi/binfmt:qemu-v10.2.1 --install amd64
```

Note: this binfmt registration is runtime-only and does not survive a
reboot. Either re-run `setup.sh` after a restart, or install a boot-time
systemd unit once for permanent persistence:

```bash
sudo ./scripts/persist-emulation.sh        # --uninstall to remove
```

The release's other services use native arm64 images where published;
OpenPLC is the stack's amd64-only first-party image and is cross-included
in the arm64 SSD archive. x86_64 Linux runs OpenPLC natively.

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

`setup.sh` checks Docker, Compose v2, architecture, and loopback ports.
Its disk and memory checks only warn: disk is measured on the checkout
filesystem (not Docker's storage volume), and low memory is reported
when Docker reports less than 7 whole GiB. On Linux-native Docker, the
memory check can fall back to host RAM. It then pulls images, starts
the stack, and runs backend and workshop-readiness checks.

To re-run only the preflight checks without installing:

```bash
./setup.sh --check-only
```

### Path B - Build from source

For developers / contributors. Builds the first-party images locally;
the first build takes several minutes (Go simulators, Kali
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

The positional version selects every first-party image tag independently
of the instructor repo's `.env`; the helper records the same value in
`.version` and verifies the saved image tags before reporting success.

This produces `images-amd64.tar`, `images-arm64.tar`,
`rangerdanger.tgz`, a `.version` marker, and an auto-generated SSD
README on the volume. Tagged releases additionally bundle
`rangerdanger-wsl2-kernel` + `rangerdanger-wsl2-kernel.sha256` so
Windows students get ICS DPI on Labs 2.3 / 2.3-bonus without a
separate kernel download.

**On the workshop laptop (student):**

```bash
tar xzf /Volumes/WORKSHOP_SSD/rangerdanger.tgz -C ~
cd ~/rangerdanger
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
# Windows: .\setup.ps1 -FromTarballs D:\WORKSHOP_SSD
```

The installer uses `docker-compose.release.yml`; `--from-tarballs`
also selects `docker-compose.offline.yml`.

`setup.sh` detects the host architecture, loads the matching
tarball with `docker load`, then starts the selected Compose stack.

Do not assume each [GitHub release](https://github.com/tonylturner/rangerdanger/releases)
has prebuilt image tarballs attached. Use an instructor-staged, verified
bundle, or verify any manually supplied assets and their `.version`
before using them offline.

## Once it's up

| | URL | Credentials |
|---|---|---|
| RangerDanger UI | http://localhost:8088 | - |
| containd Web UI | http://localhost:9080 | containd / containd |
| containd SSH | `ssh -p 2222 containd@localhost` | containd / containd |
| FUXA HMI | http://localhost:8088/apps/fuxa-hmi/ | - |
| OpenPLC | http://localhost:8088/apps/openplc/ | openplc / openplc |

Open [http://localhost:8088/exercises](http://localhost:8088/exercises)
and start with **Lab 1.2** (Baseline Traffic Analysis).

## Common errors

Compose commands below use the release and offline files explicitly, so
they also work after Path C without contacting GHCR. For an online Path A
install, drop `-f docker-compose.offline.yml` if Compose should pull an
updated image. A bare `docker compose` selects the source stack and may
rebuild from Dockerfiles; Path B source-build users should drop both
release flags. The firewall-pull example below is intentionally online
and uses only the release file.

### "the lab doesn't come up"

Most "doesn't start" issues fall into one of these:

1. **Docker isn't running** - start Docker Desktop, or
   `sudo systemctl start docker` on Linux.
2. **Compose v2 isn't installed** - Compose v1 (`docker-compose`,
   with a hyphen) is end-of-life. Install Compose v2 via Docker
   Desktop or `apt install docker-compose-plugin`.
3. **Ports already in use** (`8088`, `9080`, `9443`, `2222`).
   `./setup.sh --check-only` will tell you which.
4. **Out of disk** during a 6+ GB image pull. Leave at least 30 GB
   free for the install, and check Docker Desktop's storage-volume
   capacity separately; setup only reports free space where the
   checkout lives.
5. **Out of memory** on Docker Desktop's allocated VM. Allocate 8 GB
   or more in Settings → Resources; setup warns only below 7 whole GiB.
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
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml down
rm -f backend/data/rangerdanger.db
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
```

### "containd won't authenticate"

Stale local users.db after the default password got changed in
a prior session. Delete and restart:

```bash
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml down
rm -f data/firewall/users.db data/firewall/users.db-*
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
```

containd's lab-mode default-admin seeding (`CONTAIND_LAB_MODE=1`
in the compose file) will restore the `containd` / `containd`
admin on next boot.

### "the build is slow"

The first `docker compose build` pulls + compiles a lot. Subsequent
builds reuse the layer cache. To force a clean re-pull of just the
firewall image when containd publishes a security fix:

```bash
docker compose -f docker-compose.release.yml pull firewall
docker compose -f docker-compose.release.yml up -d firewall
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
