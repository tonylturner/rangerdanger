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

A successful stage contains the four core files and a version marker,
plus (when published) the Windows WSL2 kernel asset. The amd64 and arm64
image archives are written one after the other, not as an atomic pair:
if the second stage or final verification fails, earlier files may remain
in the output directory. Distribute only after the helper reports success.

| File | What it is | Size |
|---|---|---|
| `images-amd64.tar` | All Docker images for Intel/AMD64 hosts, saved together | ~6 GB |
| `images-arm64.tar` | All Docker images for Apple Silicon / arm64 hosts. `openplc` is cross-included as the amd64 image (Rosetta 2 on macOS); also bundles `tonistiigi/binfmt` so arm64 **Linux** hosts can register amd64 emulation offline. | ~6 GB |
| `rangerdanger.tgz` | The repo at the staged commit | ~1-2 MB |
| `README.md` | Auto-generated per-stage instructions for the student | ~1 KB |
| `.version` | Plain-text version marker (`vX.Y.Z` or `latest`) | <1 KB |
| `rangerdanger-wsl2-kernel` + `.sha256` | Custom WSL2 kernel for Windows ICS DPI labs - only present when staging from a tagged release whose `build-wsl-kernel.yml` workflow has produced the asset. `setup.ps1 -FromTarballs` picks it up automatically. | ~14 MB |

When the kernel asset is published, staging requires a valid checksum that
matches the downloaded kernel. If the checksum cannot be fetched, is invalid,
or does not match, staging fails and removes both kernel files instead of
writing a bundle that could install an unverified kernel. Re-run the stage
helper, or place both files in the output directory by hand.

Both `images-*.tar` cover the image references of the release Compose
files (the platform's and every range package's), with
different architecture-specific binaries. The arm64 archive additionally
contains `tonistiigi/binfmt` and cross-includes the amd64-only `openplc`
image because no arm64 manifest exists for it.
`setup.sh --from-tarballs` auto-detects host arch and loads the matching
archive.

## Initial stage

On a machine with internet, from the repo root:

```sh
./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.1.17
```

The positional version selects the first-party image tags through Compose
interpolation even if the repo's `.env` contains another `VERSION`; `.version`
records that same value. The helper aborts if Compose still resolves a
different first-party tag. It also aborts when a manifest cannot be read;
the sole architecture exception is a readable OpenPLC manifest with no
arm64 entry, which is cross-included from amd64. Before reporting success,
it checks each Docker archive's saved tags against every enumerated image
and checks that first-party tags match `.version`. Containd remains on
its independent mutable `latest` tag. The staging helper requires `python3`
for this archive-manifest verification.

Runtime: 25-45 minutes on a fast connection. Pulls every release
image once per architecture, then `docker save`s each set into the
arch-specific tarball.

The architecture archives are written in sequence (amd64, then arm64),
so a failure may leave the first archive behind; output is not atomic.
Do not distribute files unless the script reaches its successful final
summary. A manifest-resolution, pull, save, or final archive-verification
error aborts the run with the image or archive named.

The `rangerdanger.tgz` is `git archive HEAD`, so whatever's committed
in the working tree at stage time is what students get.

## Student first-run install

Documented short form in [`quickstart.md`](quickstart.md#path-c---offline--ssd-workshops).
Long form for the operator:

```sh
# from the SSD (offline):
tar xzf /Volumes/WORKSHOP_SSD/rangerdanger.tgz -C ~
# or with internet: git clone https://github.com/tonylturner/rangerdanger ~/rangerdanger
cd ~/rangerdanger
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

Or on Windows:

```powershell
.\setup.ps1 -FromTarballs D:\WORKSHOP_SSD
```

What `setup.sh --from-tarballs` does, in order:

1. **Pre-flight checks** - Docker reachable, Compose v2, arch
   recognized, and ports `8088 / 9080 / 9443 / 2222` free (or held by
   this install's own containers, on a re-run). Disk and
   memory readings are advisory: setup warns below 30 GB free on the
   checkout filesystem and below 7 whole GiB of reported memory (8 GB
   recommended). Linux-native Docker can fall back to host RAM. It does
   not measure Docker's storage volume or check macOS host RAM.
   `--check-only` runs just this stage and exits.
2. **`.env`** - writes `VERSION` (from the SSD's `.version`) and
   `RANGERDANGER_ROOT` (the install directory) to `.env`.
3. **Migration** - an older single-project install (Compose project
   `rangerdanger` owning `rangerdanger_mgmt_net`) is taken down by its
   Compose label first. A `rangerdanger_mgmt_net` owned by anything
   else stops setup with an error.
4. **`docker load`** the matching `images-<arch>.tar` every time.
   Docker deduplicates existing layers by content hash. The archive
   carries the images of the platform and of every range package.
5. **Platform** - `docker compose -p rangerdanger-platform ... -f
   docker-compose.release.yml up -d --wait --pull never` starts the
   backend, frontend and proxy. `--pull never` overrides the release
   file's pull policy, so a slow/blocked GHCR can't ruin the day.
6. **Range** - once `/api/health` answers, `POST /api/range` asks the
   backend to start the range (containd, the zones, the devices), the
   same request the UI's package toggle sends. The backend always runs
   ranges with `--pull never`. Setup waits until `GET /api/range`
   reports `ready`; a failed range start is fatal and prints the
   backend's error.
7. **Readiness checks** - failed firewall API-health, policy-apply, or
   workshop-reset checks are fatal. DPI degradation and a non-running
   OpenPLC only warn, so the final banner can still print when those
   checks fail.

Successful tail looks like:

```
[+] Workshop-readiness: OpenPLC (protection-logic lab)...
[+] OpenPLC container is running

RangerDanger is up
──────────────────
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
- nginx config (`proxy/nginx.conf`, a package's `nginx.routes.conf`)
- Setup script changes
- containd policy JSONs (`lab-definitions/firewall/*.json`)

Distribution: ship the new `rangerdanger.tgz` only. ~1 MB. USB,
AirDrop, Slack, anything. Student replaces their repo and restarts:

```sh
cd ~/rangerdanger
./scripts/dev-down.sh                                 # the range, then the platform
tar xzf /Volumes/WORKSHOP_SSD/rangerdanger.tgz -C ~   # overwrites ~/rangerdanger in place
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD      # loads the image archive again
```

`setup.sh` runs `docker load` on every import. Docker deduplicates
layers that are already present.

### Pattern 2: image rebuild change (Dockerfile / Go / TS / sim code)

Anything that lands in a first-party image:

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

containd intentionally floats on its own mutable `latest` tag; its release
version is independent of RangerDanger's. The delta helper does not record
the digest included by a previous full stage, so it cannot detect whether
containd has drifted since that stage. To include the current `latest` image
in a delta, force it with:

```sh
./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.1.17 v0.1.16 v0.1.17 \
  --include-upstream --include containd
```

Before distributing, record the digest you intend to ship with
`docker buildx imagetools inspect --format '{{.Manifest.Digest}}' ghcr.io/tonylturner/containd:latest`.
After staging, compare it with
`docker image inspect ghcr.io/tonylturner/containd:latest --format '{{json .RepoDigests}}'`.
If the approved digest is not present locally, do not distribute the delta;
the helper does not pin containd's mutable tag to a prior-stage digest.

## Delta staging

`stage-ssd-delta.sh <output-dir> <since-version> <new-version>`

- Compares remote digests of every first-party image at `<since>`
  vs `<new>` and saves only the ones that differ.
- Requires each `<new>` first-party image to resolve in the registry;
  local-only images are not a fallback.
- Always includes a fresh `rangerdanger.tgz` (since the repo
  archive is tiny anyway).
- Runs with stock macOS Bash 3.2 and Compose `config --images`; it
  requires `python3` for registry-manifest parsing and exact
  image-to-service mapping.
- Compares the union of images the platform release file and every
  package's `compose.release.yml` name.
- Writes a `DELTA-README.md` whose image table lists, for every changed
  image, its package/service membership (the platform or the package
  that uses it). Its apply recipe loads changed tags, retags unchanged
  images from `<since>` to `<new>`, snapshots the complete existing repo
  (including `.env` and local edits), selects `<new>`, then restarts the
  lab offline: the platform with `--pull never`, then the recorded range
  package through the backend (`POST /api/range`). Rollback restores
  that snapshot and reuses the retained old image tags. The recipe needs
  `curl` and `python3` on the student's machine.

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

Saving 2 changed image(s) per arch...
  delta-amd64.tar  (~230 MB)
  delta-arm64.tar  (~225 MB)
  rangerdanger.tgz (~1 MB)
  rangerdanger-wsl2-kernel + .sha256 (~14 MB; tagged releases only)
  DELTA-README.md
```

Distribution: the per-arch `delta-*.tar` files plus `rangerdanger.tgz`.

The delta helper includes the kernel pair only after verifying its checksum.
If the checksum is unavailable, invalid, or mismatched, staging fails rather
than writing a delta that could install an unverified kernel. Re-run the delta
helper, or place both kernel files in the output directory by hand.

Size depends entirely on how many image digests differ between the two
releases, and that example is the best case. A tagged release rebuilds
every first-party image, so every digest changes and a plain
release-to-release delta is close to a full bundle:
`v0.1.31 -> v0.1.32` reported 15 changed, 0 unchanged, and wrote 8.1 GB.
`--include <image>` is additive: it force-adds a named image whose digest
compared as unchanged. It does not exclude changed images or make a delta
smaller; a delta is small only when few images differ. Either name form
works (`gps-sim` or `rangerdanger-gps-sim`), and an entry that matches no
candidate image stops the run rather than being dropped silently.

### Student-side delta apply

Follow the generated `DELTA-README.md`: it contains the exact changed
image-to-service table and the apply commands for the staged versions.
Before stopping anything, the recipe reads `VERSION` from the install's
`.env` and requires it to match the delta's `<since-version>`. A missing
version or any other version is refused without changing the install; if
the install is already at `<new-version>`, the recipe says the delta looks
already applied and nothing was changed. This also permits safely
re-applying a delta after an interrupted attempt that left the install at
`<since-version>`.

After that precondition passes, the recipe stops the lab (the range,
then the platform, each by its Compose project label from an empty
directory, verifying that no container or network is left), then saves the complete existing `~/rangerdanger` tree beside the
install as
`../rangerdanger.before-<new-version>.tar.gz`. The snapshot includes
`.env`, Compose files, lab definitions, policy files, local edits, and all
of `./data/` (including captures, Kali home, and simulator state), but not
Docker images. It also captures any other files present in the install
tree. Snapshot size and creation time grow with lab state; allow enough
free disk space for a compressed copy of the full tree. The snapshot is
not overwritten if the same delta is applied again. If snapshot creation
or its archive check fails, the recipe stops with the lab down and
points to its `Rollback` section to bring the unchanged install back.
If stopping and verifying both projects fails (a container or network
of either is left), it stops before any snapshot or repo change; some
services may already be stopped.

The recipe then extracts the repo and loads the changed-image archive for
the host architecture. For every unchanged first-party image it emits a
`docker image tag <old-ref> <new-ref>` command, so Compose can find every
image at the selected new version while offline. It then updates `VERSION`
in `.env` and finishes by restarting the lab offline.

For ARM64 Linux, OpenPLC uses amd64 emulation. A delta with changed images
includes `tonistiigi/binfmt` in `delta-arm64.tar`; a repo-only delta creates
no image archives and cannot provide that image. Ensure
`tonistiigi/binfmt:qemu-v10.2.1` is already present before applying a
repo-only delta offline if OpenPLC may need to restart after a host reboot.

Do not restart only the changed services: first select the new image tags,
then restart the whole lab so every service resolves against the same
release and updated repo files: the platform with `--pull never`, then the
range through the backend (`POST /api/range`). The generated
`DELTA-README.md` gives the exact commands.

For offline rollback, the generated `Rollback` section first stops the
lab, then removes the updated repo and restores the
complete saved repo tree (including `.env`, local edits, and `./data/`)
before starting the complete lab with the old tags. It needs no network
or second bundle. Docker images are not in the repo snapshot, so keep the
snapshot and old image tags until the rollback window closes. Images behind
mutable tags overwritten by the delta are parked as `:before-<new version>`;
deleting those parked tags forfeits rollback for those images.

## Recovery scenarios

### "I think the SSD is corrupt / partial"

Re-stage. `stage-ssd.sh` fails on unresolved manifests, pull/save errors,
or an archive that does not contain every enumerated image tag. The two
architecture archives are written in sequence, so a failed run can leave
one behind; distribute only after the final success message. A rerun may
be layer-cache-warm, but mutable upstream tags such as containd `latest`
can resolve to different content.

### "A student's import failed mid-load"

`docker load` is mostly atomic per-layer. Partial loads shouldn't
break anything - the student can re-run `setup.sh --from-tarballs`
and it'll pick up where it left off. If state's still weird:

```sh
./scripts/uninstall-rangerdanger.sh --yes --purge   # the lab, .env and its release/dev images
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

### "Containd password got changed somehow"

The v0.1.6 lab-mode lock prevents this on containd >= v0.1.22, but
older images allowed it. `/api/workshop/reset` wipes containd's
`users.db` so the canonical `containd / containd` is restored on
next firewall restart. Or manually:

```sh
docker stop rangerdanger-firewall
rm -f data/firewall/users.db data/firewall/users.db-*
curl -fsS -X POST -H 'Content-Type: application/json' -d '{}' http://localhost:8088/api/range
```

The `POST` restarts the range (offline-safe: the backend never pulls)
and resets the firewall policy to the package default.

### "The lab YAML I edited mid-workshop isn't showing up"

Lab YAML is bind-mounted into the backend container at
`/lab-definitions:ro`, and the backend loads scenarios at startup.
Restart the backend to pick up edits:

```sh
docker restart rangerdanger-backend
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
The new RangerDanger image refs must already exist in the registry.
Build and publish them under the new release tag first, then run
`stage-ssd-delta.sh` against your last shipped tag (e.g. `v0.1.7`) and
that registry tag. The helper has no local-image fallback; it names any
new image it cannot resolve.

**Q: Students get OpenPLC errors on an ARM host. Why?**
`openplc` is amd64-only and needs amd64 emulation. On Apple Silicon,
use a Docker Desktop backend with working amd64 emulation enabled
(Rosetta is a Docker Desktop setting); `setup.sh` does not enable or
verify it. On **arm64 Linux**, `setup.sh` can register a
`qemu-x86_64` handler via `tonistiigi/binfmt`. A native arm64 OpenPLC
image is not available.

**Q: Can students share a single SSD?**
Yes for the load - `docker load` is read-only on the tarball. Eject
politely between users. For a workshop, having ~3 SSDs with the same
content lets people pass them around without crowding.

**Q: How do I check what's actually in the SSD bundle?**
```sh
tar -xOf /Volumes/WORKSHOP_SSD/images-amd64.tar manifest.json |
  python3 -c 'import json,sys; [print(tag) for image in json.load(sys.stdin) for tag in (image.get("RepoTags") or [])]'
```
The staging helper runs this kind of tag check for both architecture
archives before it reports success. Or load the archive on a scratch host
and inspect the local tags:
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
