# Workshop SSD distribution

How to get RangerDanger onto N student laptops in a room with bad
Wi-Fi, and how to push patches between sessions without forcing every
student to re-import the full 6 GB image bundle.

This is the v0.2.7 package-based operator runbook. Tagged examples require
published v0.2.7 images. PowerShell setup supports the same packages on
Docker Desktop WSL2 with a physical local-drive checkout, and installs the
release's WSL2 kernel only when its feature probe finds the current kernel
missing required support. It rejects UNC,
`\\wsl$`, mapped network drives, junctions and Hyper-V; see the
[Windows requirements](quickstart.md#windows-docker-desktop-wsl2-only).
The v0.2.2 Windows fix was validated on Windows 11 (Docker Desktop 4.94,
PowerShell 5.1) with the rebuilt kernel, including online and offline installs.

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

A successful full stage contains a versioned index per architecture and one
Docker-save archive per unique image ref, plus the repo, instructions, version
marker and required Windows kernel assets. Distribute only after the helper
reports success; a failed run can leave partial files.

| File | What it is | Size |
|---|---|---|
| `image-index-amd64.txt`, `image-index-arm64.txt` | `rangerdanger-image-index-v1`; VERSION/arch headers, then tab-separated owner (`platform` or exact package id), image ref, relative archive path and SHA-256 | small |
| `images/<arch>/*.tar` | One archive per unique selected image ref; shared refs saved once. OpenPLC uses amd64 on arm64; its owners also receive binfmt on arm64. | varies with selection |
| `rangerdanger.tgz` | The repo at the staged commit | ~1-2 MB |
| `README.md` | Auto-generated per-stage instructions for the student | ~1 KB |
| `.version` | Plain-text version marker (`vX.Y.Z` or `latest`) | <1 KB |
| `rangerdanger-wsl2-kernel` + `.sha256` | Required x86_64 WSL2 kernel for Windows DPI detection and logging. The release's `build-wsl-kernel.yml` workflow must have published both assets before staging can succeed. `setup.ps1 -FromTarballs` picks it up automatically. | ~14 MB |

Full staging fails if the kernel is absent (HTTP 404) or its availability
request fails (for example a timeout or authentication error); these are
reported separately. A valid checksum must match the downloaded kernel.
If verification fails, staging removes both kernel files and refuses success.
Wait for complete release assets and re-run the stage helper; do not
distribute partial output. The kernel cannot remediate ARM64 Windows hosts;
those hosts need working queue support in their existing kernel.

Full staging defaults to every package; `--packages us-dnp3-substation`
(PowerShell `-Packages us-dnp3-substation`) narrows the staged closure.
Setup selection is independent: a superset bundle supports a subset install,
but every selected package must be present. Setup validates VERSION, architecture,
selected membership, safe relative paths and SHA-256 checksums **before loading**,
then loads only platform + selected archives. Unselected archives are never loaded.
Old monolithic bundles are rejected: re-stage with `rangerdanger-image-index-v1`.
Setup needs no Python or jq for this index.

## Initial stage

On a machine with internet, from the repo root:

```sh
./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.2.7
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

Offline additions use `./setup.sh --add-package <id> --from-tarballs <dir>` or
`.\setup.ps1 -AddPackage <id> -FromTarballs <dir>`. The bundle must match the
installed VERSION and include platform + the added package; existing packages
need not be staged again. Only the added closure is loaded, then the receipt is
unioned without a restart. Source installs cannot consume release bundles.

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

Or on Windows, after extracting the repo onto a physical local drive:

```powershell
.\setup.ps1 -FromTarballs D:\WORKSHOP_SSD
# Or select EU instead of the fresh-install US default:
# .\setup.ps1 -FromTarballs D:\WORKSHOP_SSD -Package eu-iec104-substation
```

What `setup.sh --from-tarballs` does, in order:

1. **Pre-flight checks** - Docker reachable, Compose 2.36.0+ on
   Engine API 1.49+ (Docker Desktop 4.42+), arch recognized, and ports
   `8088 / 9080 / 9443 / 2222` free (or held by this install's own
   containers, on a re-run). Disk and
   memory readings are advisory: setup warns below 30 GB free on the
   checkout filesystem and below 7 whole GiB of reported memory (8 GB
   recommended). Linux-native Docker can fall back to host RAM. It does
   not measure Docker's storage volume or check macOS host RAM.
   `--check-only` runs just this stage and exits.
2. **Validate the index, then `docker load`** only the selected closure, before
   any destructive teardown. Inspect all required images, then atomically write
   `data/installed-packages` and `data/install-mode`. Docker deduplicates layers.
3. **`.env`** - after acquisition succeeds, writes `VERSION` (from the SSD's
   `.version`) and `RANGERDANGER_ROOT` (the install directory) to `.env`.
4. **Migration** - after images are available, an older single-project
   install (`rangerdanger` owning `rangerdanger_mgmt_net`) is taken down by
   Compose label. Foreign management/delivery network ownership is rejected.
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

PowerShell follows the same acquisition-before-migration sequence and also
checks WSL2 kernel features and proves the daemon can read the checkout at
the mapped root before teardown/platform startup. Both installers keep
platform volumes; the range restart removes its anonymous desktop volumes.
Save desktop work and evidence before rerunning setup.

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

This release requires containd 0.1.41 or later, including offline
bundles. RangerDanger owns its client fixes for that contract; using an older
containd image is not a compatibility path. Verify the staged build as well
as its digest.

containd intentionally floats on its own mutable `latest` tag; its release
version is independent of RangerDanger's. The delta helper does not record
the digest included by a previous full stage, so it cannot detect whether
containd has drifted since that stage. To include the current `latest` image
in a delta, force it with:

```sh
./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.7 v0.2.6 v0.2.7 \
  --include-upstream --include containd
```

The containd floor remains 0.1.41; v0.2.7 was tested on 0.1.42. The flags
above force the current mutable `latest` into the delta when you want the
bundle to carry it, since the helper cannot detect whether containd has
drifted since the previous stage. The v0.2.6 → v0.2.7 update itself does not
require a containd migration backup.

Before distributing, record the digest you intend to ship with
`docker buildx imagetools inspect --format '{{.Manifest.Digest}}' ghcr.io/tonylturner/containd:latest`.
After staging, compare it with
`docker image inspect ghcr.io/tonylturner/containd:latest --format '{{json .RepoDigests}}'`.
Also record the saved image identity for each architecture archive (image ID,
OS, architecture and repo digests). Load each archive in turn before
inspecting the tag; the two archives can contain different platform images
under the same mutable `latest` tag:

```sh
for arch in amd64 arm64; do
  docker load -i "/Volumes/WORKSHOP_SSD/delta-v0.2.7/delta-${arch}.tar"
  docker image inspect ghcr.io/tonylturner/containd:latest \
    --format 'ID={{.Id}} OS={{.Os}} ARCH={{.Architecture}} REPO_DIGESTS={{json .RepoDigests}}'
done
```

Capture and label each output by archive. Verify each OS/architecture and
saved image identity against the intended release before distributing. If
the approved digest is not present locally, do not distribute the delta; the
helper does not pin containd's mutable tag to a prior-stage digest.

Before the first 0.1.37 start on each installed machine, stop the lab and
back up the complete firewall `/data` while quiescent. In the repo root
(`RANGERDANGER_ROOT`), preserve `data/firewall/` for US and
`data/eu-iec104-substation/firewall/` for EU, including both if installed.
Keep all databases, journals, users and captures, not just a config export.
The stored schema migrates one-way to 0.1.10, which containd 0.1.36 and earlier
cannot read.

For v0.2.0 → v0.2.7, a fresh full
storage backup is still required before updating containd. containd 0.1.41
migrates firewall `config.db` one-way; 0.1.40 cannot read the migrated store.
Rollback requires the full pre-upgrade firewall data backup and matching old
image and RangerDanger release, even if schema 0.1.10 was already in use. Follow the
[current upgrade backup guidance](quickstart.md#upgrading-to-v027), based on
containd's [update checklist](https://github.com/tonylturner/containd/blob/main/docs/mkdocs/update-policy.md#operator-update-checklist); keep the matching old image and repo
until the update is verified. The generated delta apply recipe stops the lab
and snapshots the complete installed tree, including both package stores.

## Delta staging

`stage-ssd-delta.sh <output-dir> <since-version> <new-version>`

Delta creation is instructor tooling: `stage-ssd-delta.ps1` can also produce
a bundle. **Delta apply and rollback are Bash procedures today**, including
those generated by the PowerShell stager; there is no supported native
PowerShell apply/rollback path. Do not assume running the recipe inside WSL
qualifies a native Windows installation. For Windows students, use a full
bundle with `setup.ps1 -FromTarballs` until a native delta consumer is tested.
Both generators' recipes preserve the installed `.env` daemon root
(including the translation written by `scripts/lib/windows-root.ps1` via
setup), rather than replacing it with a shell's `$PWD`. That preservation
is not Windows runtime acceptance.

- Compares remote digests of the selected first-party closure at `<since>`
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

The source release must carry the platform/package Compose metadata. A
v0.1.x SSD baseline lacks it: stage a full v0.2.7 bundle for that migration.
Later package-aware releases can use:

```sh
./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.2.7 v0.2.6 v0.2.7
```

v0.2.7 rebuilds changed first-party images; images with unchanged inputs can be
promoted from the previous release. This is not a full image refresh. The delta helper
compares registry manifest digests, not just changed repository files.
The v0.2.6 → v0.2.7 delta carries changed images plus the repository archive,
not automatically the full first-party image set. Check the helper's summary
for the actual image set and size before distributing it.
Unchanged upstream images are not automatically included;
include containd separately
with the flags in Pattern 3 if the delta should also carry the current `latest`.
Do not use the retired single-stack
repository-update helpers for package migration; use setup and full/delta staging.

Typical output (from the script's run summary):

```
Comparing <since> -> <new> across N candidate image(s)
  changed: backend, frontend
  unchanged: kali, vendor-jump, eng-ws, openplc, rtac-sim,
             relay-sim, recloser-sim, regulator-sim,
             capbank-sim, historian-sim, gps-sim,
             opendss-sim

Saving 2 changed image(s) per arch...
  delta-images/amd64/  (~230 MB)
  delta-images/arm64/  (~225 MB)
  rangerdanger.tgz (~1 MB)
  rangerdanger-wsl2-kernel + .sha256 (~14 MB; tagged releases only)
  DELTA-README.md
```

Distribution: the per-arch `delta-*.tar` files plus `rangerdanger.tgz`.

The delta helper includes the kernel pair only after verifying its checksum.
If the checksum is unavailable, invalid, or mismatched, staging fails rather
than writing a delta that could install an unverified kernel. Re-run the delta
helper, or place both kernel files in the output directory by hand.

Size depends on how many image digests actually differ between the two
releases, and that example is the best case. Releases build changed images
or promote recorded image digests for unchanged inputs. A full rebuild
can make a release-to-release delta close to a full bundle; for example,
`v0.1.31 -> v0.1.32` reported 15 changed, 0 unchanged, and wrote 8.1 GB.
`--include <image>` is additive: it force-adds a named image whose digest
compared as unchanged. It does not exclude changed images or make a delta
smaller; a delta is small only when few images differ. Either name form
works (`gps-sim` or `rangerdanger-gps-sim`), and an entry that matches no
candidate image stops the run rather than being dropped silently.

### Selected-package deltas

Changed refs use `delta-image-index-<arch>.txt` and per-ref
`delta-images/<arch>/*.tar`, with the same v1 index grammar as full bundles.

`stage-ssd-delta.sh <out> <old> <new> --since-packages <ids> --packages <ids>`
and `stage-ssd-delta.ps1 <out> <old> <new> -SincePackages <ids> -Packages <ids>`
carry exact source and target sets (defaults: every package in each release).
Only the target closure is compared; global release-record validation stays intact.
Newly selected refs absent from the source install are shipped, not assumed cached.
Apply refuses an installed-set mismatch before teardown and checks old tags,
archive checksums and disk space. Its full-tree snapshot includes
`data/installed-packages`, `data/install-mode` and `.env`; rollback restores all
three together with the repository and lab data. containd stays published `:latest`,
never locally retagged; an overwritten containd image is saved for rollback instead.
Both stagers generate Bash recipes for macOS/Linux. ROOT remains owned by
`.env`; Git Bash/WSL application to native Windows installs is not validated.
Use full bundles on Windows; there is no native PowerShell delta apply command.

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
includes `tonistiigi/binfmt` in `delta-images/arm64/`; a repo-only delta creates
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
mutable tags overwritten by the delta are parked as `:before-<new version>`
(except containd, saved as a Docker archive without retagging);
deleting those parked tags forfeits rollback for those images.

The complete repo backup must include both package firewall directories
listed above. After the containd schema 0.1.10 migration, binary rollback
requires matching pre-upgrade data **and** the old image; an older tag alone
is not enough because containd 0.1.36 and earlier cannot read the upgraded
store. Stop the lab and replace the migrated directories; never merge old
database files into migrated storage. Restore the matching older RangerDanger
release too when rolling back, then restart and verify operations. Importing
a prior policy stages configuration and does not undo the binary/schema
upgrade.

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

From the install root, select the **active package's** firewall store below.
Stop the firewall and back up its complete store before deleting credentials;
this removes local users. Keep the backup with your saved workshop evidence.
Export any policy and save other range work first: the final `POST` restarts
the range and resets the firewall policy to the package default.

```sh
FIREWALL_DATA=data/firewall                         # US
# FIREWALL_DATA=data/eu-iec104-substation/firewall   # EU: use this instead
docker stop rangerdanger-firewall &&
cp -a "$FIREWALL_DATA" "${FIREWALL_DATA}.backup-$(date +%Y%m%d%H%M%S)" &&
rm -f "$FIREWALL_DATA/users.db" "$FIREWALL_DATA/users.db-wal" \
  "$FIREWALL_DATA/users.db-shm" "$FIREWALL_DATA/users.db-journal" &&
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
Yes - the bundles are static files. Copy `delta-image-index-*.txt`,
`delta-images/`, `.version`, and `rangerdanger.tgz` together. Download the
delta bundle and its generated `DELTA-README.md`, then follow that file's
apply and rollback procedure. `--from-tarballs` consumes **full bundles**
containing `image-index-<arch>.txt` and per-image archives, not delta indexes
and `delta-images/`; do not pass a delta directory to it.

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
cat /Volumes/WORKSHOP_SSD/image-index-amd64.txt
# Inspect one archive listed in that index:
tar -xOf /Volumes/WORKSHOP_SSD/images/amd64/1.tar manifest.json |
  python3 -c 'import json,sys; [print(tag) for image in json.load(sys.stdin) for tag in (image.get("RepoTags") or [])]'
```
The staging helper runs this kind of tag check for every per-ref archive in both architectures before it reports success. Or load the archive on a scratch host
and inspect the local tags:
```sh
docker load -i /Volumes/WORKSHOP_SSD/images/amd64/1.tar
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
