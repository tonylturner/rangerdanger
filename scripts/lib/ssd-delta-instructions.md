# RangerDanger - delta patch

Staged __STAGED_AT__ for upgrade from \`__SINCE__\` -> \`__NEW__\`.
Source packages: \`__SOURCE_PACKAGE_LIST__\`; target packages: \`__TARGET_PACKAGE_LIST__\`.

## Changed

| Image | Package/service membership |
|---|---|
__APPLY_TABLE__
__KERNEL_README_ROW__
__UNCHANGED_SECTION__

## Apply

Run from the student's existing \`~/rangerdanger\` directory:

\`\`\`sh
set -e
# Set this to the directory containing this delta bundle.
DELTA_DIR="/path/to/delta-__NEW__"
cd ~/rangerdanger
test -f .env || { echo "Expected .env from setup.sh; cannot preserve the prior version." >&2; exit 1; }
__RECIPE_FUNCTIONS__
# Only apply a delta to the version it was built from. Check before stopping
# services or touching the install so a wrong or repeated delta is harmless.
CURRENT_VERSION=\$(awk '/^VERSION=/ { sub(/^VERSION=/, ""); print }' .env)
if [ "\$CURRENT_VERSION" = "__NEW__" ]; then
    echo "This delta looks already applied: install VERSION is \$CURRENT_VERSION, but this delta expects __SINCE__; nothing was changed." >&2
    exit 1
fi
if [ "\$CURRENT_VERSION" != "__SINCE__" ]; then
    if [ -n "\$CURRENT_VERSION" ]; then
        echo "Install VERSION is \$CURRENT_VERSION; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    else
        echo "Install .env has no VERSION= line; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    fi
    exit 1
fi

delta_preflight || exit 1

# Stop only the two owned projects by labels before reading mutable state
# into the rollback snapshot. Teardown does not load either Compose model.
if ! compose_down_project rangerdanger || ! compose_down_project rangerdanger-platform
then
    echo "Could not stop and verify both RangerDanger projects; no snapshot or repo changes were made." >&2
    exit 1
fi

# Save the complete existing install, including .env and local lab/policy
# edits, for rollback. Keep the first snapshot if this delta is re-applied.
SNAPSHOT="../rangerdanger.before-__NEW__.tar.gz"
if [ ! -f "\$SNAPSHOT" ]; then
    tar czf "\$SNAPSHOT" -C .. rangerdanger || {
        rm -f "\$SNAPSHOT"
        echo "Could not snapshot ~/rangerdanger; refusing to apply the delta. The RangerDanger projects are stopped; restore them from the rollback procedure." >&2
        exit 1
    }
fi
tar tzf "\$SNAPSHOT" >/dev/null || {
    echo "Rollback snapshot is not a readable tar archive; refusing to apply the delta. The RangerDanger projects are stopped; restore them from the rollback procedure." >&2
    exit 1
}

# Update the repo, then load the changed images (if any).
tar xzf "\$DELTA_DIR/rangerdanger.tgz" -C ~
__APPLY_BACKUP_COMMANDS__
__APPLY_LOAD_COMMAND__

# Re-tag unchanged first-party images so every required :__NEW__ tag exists.
__APPLY_RETAG_COMMANDS__

# Select the new release while preserving other .env settings.
NEW_VERSION=__NEW__ awk '
  BEGIN { version = ENVIRON["NEW_VERSION"]; replaced = 0 }
  /^VERSION=/ {
    if (!replaced) print "VERSION=" version
    replaced = 1
    next
  }
  { print }
  END { if (!replaced) print "VERSION=" version }
' .env > .env.delta.tmp && mv .env.delta.tmp .env

write_delta_selection

# Start the platform from local images without contacting GHCR, then have
# its backend select the recorded package through the normal range API.
start_platform_and_selected_range
\`\`\`

**ARM64 Linux only:** OpenPLC needs amd64 emulation. When changed images
are included, \`delta-images/arm64/\` also ships \`__BINFMT_IMAGE__\`; if
OpenPLC isn't running after the restart (\`docker ps | grep openplc\`),
register it once with
\`docker run --privileged --rm __BINFMT_IMAGE__ --install amd64\`.
(setup.sh does this automatically on a fresh install; the registration
does not persist across a host reboot.) A repo-only delta has no image
archives, so it cannot supply the binfmt image. Make sure that image is
already present before applying a repo-only delta offline.

If \`docker load\` fails with "no space left on device", free space
without removing the prior \`__SINCE__\` image tags or parked mutable-image
tags named \`:before-__NEW__\`; removing an old or parked tag forfeits rollback
for that image. If any apply step after the stack is stopped fails, do not
try to start a partially updated tree: keep the snapshot and old tags, then
follow the \`Rollback\` section below.

## Rollback

The apply recipe saves the complete pre-upgrade \`~/rangerdanger\` tree
beside the install as \`../rangerdanger.before-__NEW__.tar.gz\`. That snapshot
includes \`data/installed-packages\`, \`data/install-mode\`, and all files and directories in the install tree: \`.env\`, Compose
files, lab definitions, policy files, local edits, and all of \`./data/\`
(including captures, Kali home, and simulator state; nothing in \`./data/\`
is excluded). Docker images are not part of it. It can be large and grows
with lab state.
The containd rollback archive is saved beside the install, never retagged.
The retained \`__SINCE__\` image tags and the parked mutable-image tags named
\`:before-__NEW__\` are reused, so rollback needs no network and no second
bundle. Keep the snapshot, retained old image tags, and parked tags until
the rollback window closes:

\`\`\`sh
set -e
cd ~/rangerdanger
__RECIPE_FUNCTIONS__
test -f "../rangerdanger.before-__NEW__.tar.gz" || {
    echo "Rollback snapshot not found beside ~/rangerdanger." >&2
    exit 1
}
tar tzf "../rangerdanger.before-__NEW__.tar.gz" >/dev/null || {
    echo "Rollback snapshot is not readable; leaving the current install untouched." >&2
    exit 1
}
compose_down_project rangerdanger
compose_down_project rangerdanger-platform
cd ..
rm -rf rangerdanger
tar xzf "rangerdanger.before-__NEW__.tar.gz"
cd rangerdanger
__ROLLBACK_RESTORE_COMMANDS__
start_platform_and_selected_range
\`\`\`
