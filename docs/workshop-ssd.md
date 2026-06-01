# Workshop SSD distribution

How to get RangerDanger onto N student laptops in a room with bad
Wi-Fi, and how to push patches between sessions without forcing every
student to re-import the full 6 GB image bundle.

This is the operator runbook. For the student-facing one-liner see
[`quickstart.md`](quickstart.md) Path C.

## Why this exists

A clean install pulls roughly 6 GB of images per laptop. With 10-30
students on conference Wi-Fi, that's anywhere from "slow" to "the
workshop has not started 90 minutes in". The SSD path moves the
network round-trip to the instructor's machine the night before:
stage once, students load from USB.

Two staging flows:

- `stage-ssd.sh` - full bundle. First-time SSD, or when a student is
  joining late and needs everything from scratch.
- `stage-ssd-delta.sh` - patch bundle. After a fix lands and you
  need to push it without re-shipping the unchanged 6 GB.

## What's on the SSD

A staged SSD always contains at least four core files, plus a
version marker and (on tagged releases) the prebuilt Windows WSL2
kernel asset:

| File | What it is | Size |
|---|---|---|
| `images-amd64.tar` | All Docker images for Intel/AMD64 hosts, saved together | ~6 GB |
| `images-arm64.tar` | All Docker images for Apple Silicon / arm64 hosts. `openplc` is cross-included as the amd64 image (Rosetta 2 on macOS); also bundles `tonistiigi/binfmt` so arm64 **Linux** hosts can register amd64 emulation offline. | ~6 GB |
| `rangerdanger.tgz` | The repo at the staged commit | ~1-2 MB |
| `README.md` | Auto-generated per-stage instructions for the student | ~1 KB |
| `.version` | Plain-text version marker (`vX.Y.Z` or `latest`) | <1 KB |
| `rangerdanger-wsl2-kernel` + `.sha256` | Custom WSL2 kernel for Windows ICS DPI labs - only present when staging from a tagged release whose `build-wsl-kernel.yml` workflow has produced the asset. `setup.ps1 -FromTarballs` picks it up automatically. | ~14 MB |

Both `images-*.tar` carry the same image *content* (different binaries
inside). `setup.sh --from-tarballs` auto-detects host arch and loads
the matching one.

## Initial stage

On a machine with internet, from the repo root:

```sh
./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.1.17
```

Runtime: 25-45 minutes on a fast connection. Pulls every release
image once per architecture, then `docker save`s each set into the
arch-specific tarball.

The script fails fast on any pull error (no partial bundles -
v0.1.6 fix). If the network blips, it dies and tells you which image
failed; re-run after fixing.

The `rangerdanger.tgz` is `git archive HEAD`, so whatever's committed
in the working tree at stage time is what students get.

## Student first-run install

Documented short form in [`quickstart.md`](quickstart.md#path-c---offline--ssd-workshops).
Long form for the operator:

```sh
git clone https://github.com/tonylturner/rangerdanger     # OR copy from SSD
cd rangerdanger
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

Or on Windows:

```powershell
.\setup.ps1 -FromTarballs D:\WORKSHOP_SSD
```

What `setup.sh --from-tarballs` does, in order:

1. **Pre-flight checks** - Docker reachable, Compose v2, arch
   recognized, disk ≥ 30 GB, RAM ≥ 8 GB, ports `8088 / 9080 / 9443
   / 2222` free. `--check-only` runs just this stage and exits, useful
   for a "is my laptop ready" pass the night before.
2. **`docker load`** the matching `images-<arch>.tar`. Docker dedups
   layers by content hash, so a re-run is cheap.
3. **`docker compose -f release.yml -f offline.yml up -d`**. The
   offline overlay sets `pull_policy: never` on every release-image
   service so a slow/blocked GHCR can't ruin the day.
4. **Health gate** - waits for `/api/health`, then runs the workshop
   readiness gate (firewall apply weak/improved + workshop reset).
   Fails loudly with diagnostics if any of those are broken.

Successful tail looks like:

```
[+] Backend reports healthy at http://localhost:8088/api/health
[+] Workshop-readiness gate: firewall health + apply + reset...
[+] Firewall apply/reset workshop gate passed
========== RangerDanger is up
```

The student's done. They open <http://localhost:8088/exercises> and
start Lab 1.2.

## Mid-workshop updates: which kind of change am I shipping?

After you stage the SSD and hand it out, every change you commit
falls into one of three categories. Figure out which before you
distribute anything - they have very different student experiences.

### Pattern 1: repo-only change (no image rebuild)

Anything that lives in the bind-mounted repo and doesn't get baked
into an image at build time:

- Lab YAML edits (`lab-definitions/scenarios/*.yml`)
- Compose file tweaks
- Documentation
- nginx config (`proxy/nginx.conf`)
- Setup script changes
- containd policy JSONs (`lab-definitions/firewall/*.json`)

Distribution: ship the new `rangerdanger.tgz` only. ~1 MB. USB,
AirDrop, Slack, anything. Student replaces their repo and restarts:

```sh
cd ~/rangerdanger
docker compose down
mkdir -p ~/rangerdanger-new
tar xzf /Volumes/WORKSHOP_SSD/rangerdanger.tgz -C ~/rangerdanger-new
cd ~/rangerdanger-new
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD     # idempotent; reuses loaded images
```

The existing Docker images stay put. No `docker load` re-run is
needed; `setup.sh` notices the images are already loaded and skips.

### Pattern 2: image rebuild change (Dockerfile / Go / TS / sim code)

Anything that lands in one of the 14 first-party images:

- Backend Go change
- Frontend Next.js change
- Sim source change (services/*-sim/*.go)
- Dockerfile change for any image
- DNP3go library change (rolls into rtac-sim, kali, eng-ws)

Distribution: build and save just the changed images, then ship them
plus the new `rangerdanger.tgz`. The full SSD is **not** invalidated
- students keep the unchanged images on disk and only load the deltas.

Use [`stage-ssd-delta.sh`](#delta-staging) below.

### Pattern 3: containd update

containd is pulled, not built locally. New containd version means a
fresh pull. Same as Pattern 2 distribution-wise, but you don't
`docker compose build` - you `docker pull ghcr.io/tonylturner/containd:latest`
on the instructor machine, then `docker save` it into the delta
bundle.

`stage-ssd-delta.sh` includes containd by default if its remote
digest has changed since your last stage.

## Delta staging

`stage-ssd-delta.sh <output-dir> <since-version> <new-version>`

- Compares remote digests of every first-party image at `<since>`
  vs `<new>` and saves only the ones that differ.
- Always includes a fresh `rangerdanger.tgz` (since the repo
  archive is tiny anyway).
- Writes a `DELTA-README.md` listing which images changed and the
  exact `docker load + docker compose up -d` commands to apply
  the delta.

Example:

```sh
./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.1.17 v0.1.16 v0.1.17
```

Typical output (from the script's run summary):

```
Comparing v0.1.16 -> v0.1.17 across N candidate image(s)
  changed: backend, frontend
  unchanged: kali, vendor-jump, eng-ws, openplc, rtac-sim,
             relay-sim, recloser-sim, regulator-sim,
             capbank-sim, historian-sim, gps-sim,
             opendss-sim
  containd: digest unchanged

Saving 2 changed image(s) per arch...
  delta-amd64.tar  (~230 MB)
  delta-arm64.tar  (~225 MB)
  rangerdanger.tgz (~1 MB)
  rangerdanger-wsl2-kernel + .sha256 (~14 MB; tagged releases only)
  DELTA-README.md
```

Distribution: the per-arch `delta-*.tar` files plus `rangerdanger.tgz`.
That's typically tens of MB to a few hundred MB instead of the full
6 GB.

### Student-side delta apply

Whatever the delta-README.md says (it's auto-generated per-stage so
the version numbers are correct), but the pattern is:

```sh
# 1. update repo
cd ~
mkdir -p rangerdanger-new
tar xzf /Volumes/WORKSHOP_SSD/delta-v0.1.17/rangerdanger.tgz -C rangerdanger-new
cd rangerdanger-new

# 2. load only the changed images
docker load -i /Volumes/WORKSHOP_SSD/delta-v0.1.17/delta-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar

# 3. restart only the affected services
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d backend frontend
```

The `docker load` for an unchanged-layer image is fast - Docker
checks layer hashes and only writes the new top layer.

If a student is uncertain which services need restart, `docker
compose up -d` (no service list) is safe - Docker compose only
restarts containers whose image digest changed.

## Recovery scenarios

### "I think the SSD is corrupt / partial"

Re-stage. `stage-ssd.sh` is idempotent and fails fast on errors;
re-running with the same args reproduces the bundle. Image pulls are
layer-cached so it's fast on the second run.

### "A student's import failed mid-load"

`docker load` is mostly atomic per-layer. Partial loads shouldn't
break anything - the student can re-run `setup.sh --from-tarballs`
and it'll pick up where it left off. If state's still weird:

```sh
docker compose down -v
docker system prune -a              # nukes all images, frees disk
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

### "Containd password got changed somehow"

The v0.1.6 lab-mode lock prevents this on containd >= v0.1.22, but
older images allowed it. `/api/workshop/reset` wipes containd's
`users.db` so the canonical `containd / containd` is restored on
next firewall restart. Or manually:

```sh
docker compose down
rm -f data/firewall/users.db data/firewall/users.db-*
docker compose up -d
```

### "The lab YAML I edited mid-workshop isn't showing up"

Lab YAML is bind-mounted into the backend container at
`/lab-definitions:ro`, hot-reloadable. Backend re-loads scenarios on
restart:

```sh
docker compose restart backend
```

If a YAML edit landed in `rangerdanger.tgz` and the student already
extracted an old version, they need the new tgz too - extract,
restart backend.

## FAQ

**Q: Can I host the delta on a web server / Slack / Drive instead of
USB?**
Yes - the bundles are static files. `delta-*.tar` and
`rangerdanger.tgz` can go anywhere students can fetch them. The
`--from-tarballs` flag wants a directory containing the right
file names, so just download to a local dir and point at it.

**Q: What if I need to ship a fix five minutes before the workshop?**
Build locally, run `stage-ssd-delta.sh` against your last shipped
tag (e.g. `v0.1.7`) and an unreleased local tag. The script doesn't
require the new version to be tagged in GHCR - it can save from
local images.

**Q: Students get OpenPLC errors on an ARM host. Why?**
`openplc` is built from `tuttas/openplc_v3`, which has no arm64
variant, so it needs amd64 emulation. macOS Apple Silicon runs it
under Rosetta 2 automatically. On **arm64 Linux** (Docker Engine, no
Rosetta), `setup.sh` registers a `qemu-x86_64` handler via
`tonistiigi/binfmt` (bundled in `images-arm64.tar`); if OpenPLC still
errors there, run `docker run --privileged --rm
tonistiigi/binfmt:qemu-v10.2.1 --install amd64` and re-run `setup.sh`.
Emulated either way - slower but functional. See `ROADMAP.md` "Known
gaps".

**Q: Can students share a single SSD?**
Yes for the load - `docker load` is read-only on the tarball. Eject
politely between users. For a workshop, having ~3 SSDs with the same
content lets people pass them around without crowding.

**Q: How do I check what's actually in the SSD bundle?**
```sh
tar -tf /Volumes/WORKSHOP_SSD/images-amd64.tar | grep -E '^[^/]+/manifest.json$' | head
```
Each top-level dir in the tar is one image. The `manifest.json`
inside lists tags. Or:
```sh
docker load -i /Volumes/WORKSHOP_SSD/images-amd64.tar
docker images --format 'table {{.Repository}}\t{{.Tag}}\t{{.Size}}' | grep rangerdanger
```
on a scratch host.

**Q: How big is a delta from version X to version Y?**
Depends entirely on which images changed. Typical patterns:
- README/docs/labs only → ~1-2 MB (just the tgz)
- One Go service rebuilt → ~30-50 MB per arch
- Frontend rebuilt → ~200 MB per arch
- Full image refresh (rare) → close to a full stage, ~6 GB per arch

The script tells you up-front before saving, so you can decide
whether USB or download is the right channel.
