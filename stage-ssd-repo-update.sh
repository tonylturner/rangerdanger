#!/bin/bash
# Stage a repository-only SSD update; never inspect, load or download images.
# Usage: ./stage-ssd-repo-update.sh <out-dir> <tag>
# Maintainer tool for macOS/Linux; student Windows steps are included in the README.
set -euo pipefail
export COPYFILE_DISABLE=1

die() { printf 'Repo staging refused: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 2 ] || die "Usage: $0 <out-dir> <tag>"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
OUT=$1
TAG=$2
# Release tags become filenames as well as refs; forbid path/option injection.
[[ "$TAG" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "Use a release tag without slashes: $TAG"
REF="refs/tags/$TAG"
git -C "$ROOT_DIR" show-ref --verify --quiet "$REF" || die "Tag does not exist: $TAG"
git -C "$ROOT_DIR" rev-parse --verify "$REF^{commit}" >/dev/null || die "Tag must name a commit."
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
ARCHIVE="rangerdanger-$TAG.tgz"
BUNDLE="rangerdanger-$TAG.bundle"
for file in "$ARCHIVE" "$BUNDLE" SHA256SUMS REPO-UPDATE-README.md; do
    [ ! -e "$OUT/$file" ] || die "Output already exists: $OUT/$file"
done
STAGE=$(mktemp -d "$OUT/.repo-update.XXXXXX")
trap 'rm -rf "$STAGE"' EXIT

git -C "$ROOT_DIR" archive --format=tar --prefix=rangerdanger/ "$REF" | gzip > "$STAGE/$ARCHIVE"
git -C "$ROOT_DIR" bundle create "$STAGE/$BUNDLE" "$REF"
(cd "$STAGE" && shasum -a 256 "$ARCHIVE" "$BUNDLE" > SHA256SUMS)
cat > "$STAGE/REPO-UPDATE-README.md" <<EOF
# $TAG repository-only SSD update

Use only with an existing v0.1.35 installation and its cached images.
No pull, no rebuild. Keep .env and VERSION unchanged (normally VERSION=latest).
Save desktop work, captures and notes; vendor_jump will be recreated.
Back up local source edits BEFORE extraction: archive installs cannot detect
them. Tracked files are overwritten; .env, data/ and captures are kept.
Do not extract an archive over a Git clone; use the bundle path instead.
Do not run setup or stage-ssd-delta for this update.

## macOS/Linux: verify first

Set SSD to the directory containing these four update files. Stop if the
checksum check fails; do not proceed to either install path.

\`\`\`sh
SSD=/Volumes/WORKSHOP_SSD
(cd "\$SSD" && shasum -a 256 -c SHA256SUMS)
\`\`\`

On Linux, \`sha256sum -c SHA256SUMS\` is equivalent.

### Archive install (no .git)

For the existing ~/rangerdanger installation:

\`\`\`sh
tar xzf "\$SSD/$ARCHIVE" -C ~ &&
cd ~/rangerdanger &&
./scripts/apply-repo-update.sh --from-tarballs "\$SSD"
\`\`\`

The helper says it skipped the Git tracked-file guard when .git is absent.
It still checks local images and Compose before changing services.

### Git clone (offline)

From the existing clean ~/rangerdanger checkout; back up tracked edits first:

\`\`\`sh
cd ~/rangerdanger &&
git fetch "\$SSD/$BUNDLE" tag $TAG &&
git checkout --detach $TAG &&
./scripts/apply-repo-update.sh --from-tarballs "\$SSD"
\`\`\`

## Windows PowerShell 5.1+

Set the SSD path, then verify BOTH checksums. A mismatch throws; stop there.

\`\`\`powershell
\$SSD = 'D:\WORKSHOP_SSD'
Get-Content (Join-Path \$SSD 'SHA256SUMS') | ForEach-Object {
    \$Expected, \$Name = \$_ -split '  ', 2
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path \$SSD \$Name)).Hash -ne \$Expected) {
        throw "Checksum mismatch: \$Name"
    }
}
\`\`\`

Archive install (tar.exe requires Windows 10 1803+), existing
\$HOME\rangerdanger, no .git:

\`\`\`powershell
tar.exe -xzf (Join-Path \$SSD '$ARCHIVE') -C \$HOME
if (\$LASTEXITCODE -ne 0) { throw "Extraction failed; stop here" }
Set-Location -LiteralPath (Join-Path \$HOME 'rangerdanger') -ErrorAction Stop
.\scripts\apply-repo-update.ps1 -FromTarballs \$SSD
\`\`\`

Or, from the existing clean Git clone (do not extract the archive):

\`\`\`powershell
Set-Location -LiteralPath (Join-Path \$HOME 'rangerdanger') -ErrorAction Stop
git fetch (Join-Path \$SSD '$BUNDLE') tag $TAG
if (\$LASTEXITCODE -ne 0) { throw "Fetch failed; stop here" }
git checkout --detach $TAG
if (\$LASTEXITCODE -ne 0) { throw "Checkout failed; stop here" }
.\scripts\apply-repo-update.ps1 -FromTarballs \$SSD
\`\`\`

The SSD flag is required: it selects docker-compose.offline.yml, not an image
import. The helper never pulls or builds. It restarts backend and proxy,
then recreates only vendor_jump, keeping data and its anonymous /config volume.
Allow about 20 seconds; open http://localhost:8088/apps/vendor-jump/.
On Windows verify LF line endings and executable bind mounts with your
instructor; prefer a WSL ext4 checkout (see docs/quickstart.md).
EOF
for file in "$ARCHIVE" "$BUNDLE" SHA256SUMS REPO-UPDATE-README.md; do
    mv "$STAGE/$file" "$OUT/$file"
done
printf 'Staged repo-only update from %s in %s (no images).\n' "$TAG" "$OUT"
