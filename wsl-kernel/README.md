# RangerDanger WSL2 kernel

This directory contains the only thing rangerdanger contributes to the
custom WSL2 kernel it ships for Windows students: a small Kconfig
overlay (`config-overlay`) that enables `CONFIG_NFT_QUEUE=y` plus two
related netfilter options on top of Microsoft's stock WSL2 defconfig.

We do not patch, fork, or otherwise modify the Linux kernel source.

## Why we ship a kernel at all

Microsoft's standard WSL2 kernel
(`5.15.x-microsoft-standard-WSL2` / `6.x-microsoft-standard-WSL2`) is
built without `CONFIG_NFT_QUEUE=y`. Without that module, any nft
rule containing `queue num <N>` fails to load with the misleading
error `Could not process rule: No such file or directory` -- even
though the userspace half of NFQUEUE (the netlink side) is fully
working.

RangerDanger's containd dataplane uses NFQUEUE to route Modbus and
DNP3 packets to a userspace ICS-DPI engine that enforces a
function-code allowlist (Lab 2.3 / 2.3-bonus / events-smoke gate 3).
Without the kernel-side `queue` verdict, those rules silently fail to
apply, and the "improved" policy ends up enforcing only its L4
subset. Workshop students get a broken Lab 2.3 with no diagnosable
error.

Rather than redesign the lab around what one specific Linux kernel
flavor happens to omit, we ship a build of the same Microsoft kernel
tree with that one config flag flipped.

## What's in the build

The `config-overlay` file is the entire diff vs. Microsoft's stock
defconfig:

```
CONFIG_NFT_QUEUE=y
CONFIG_NETFILTER_NETLINK_QUEUE_CT=y
CONFIG_NETFILTER_XT_TARGET_NFQUEUE=y
```

The build process (see `.github/workflows/build-wsl-kernel.yml`):

1. Checks out
   [`microsoft/WSL2-Linux-Kernel`](https://github.com/microsoft/WSL2-Linux-Kernel)
   at a pinned tag (workflow input `kernel_tag`).
2. Starts from `Microsoft/config-wsl` (Microsoft's stock defconfig).
3. Appends the three lines above.
4. Runs `make olddefconfig` to resolve dependencies.
5. Runs `make bzImage` to produce `arch/x86/boot/bzImage`.
6. Computes the SHA-256 of the resulting binary.
7. Publishes both the kernel and the SHA-256 as assets attached to
   the rangerdanger GitHub release that triggered the build.

There are no patches, no module loads, no out-of-tree code. The
build log is public; the diff is the three lines above.

## Reproducing the build locally

You need Ubuntu (22.04 or newer) or any Linux with a recent gcc, in
WSL2 or otherwise.

```sh
# Pick the same kernel ref as the rangerdanger release.
KERNEL_TAG=linux-msft-wsl-5.15.153.1

# Install build deps.
sudo apt-get update
sudo apt-get install -y build-essential libssl-dev libelf-dev \
    flex bison bc dwarves cpio kmod python3 git

# Clone Microsoft's WSL2 kernel source.
git clone --depth=1 --branch="$KERNEL_TAG" \
    https://github.com/microsoft/WSL2-Linux-Kernel.git kernel
cd kernel

# Apply the rangerdanger overlay.
cp Microsoft/config-wsl .config
cat /path/to/rangerdanger/wsl-kernel/config-overlay >> .config
make olddefconfig

# Normalize timestamps + uname strings to match what CI does, so a
# successful local build produces a byte-identical bzImage to the
# one published on the release page. SOURCE_DATE_EPOCH is the
# upstream tag's commit time; KBUILD_BUILD_* is hardcoded.
export SOURCE_DATE_EPOCH=$(git log -1 --format=%ct HEAD)
export KBUILD_BUILD_TIMESTAMP=$(date -u -d "@$SOURCE_DATE_EPOCH" \
    +"%a %b %e %H:%M:%S %Z %Y")
export KBUILD_BUILD_HOST=rangerdanger-ci
export KBUILD_BUILD_USER=rangerdanger

# Build.
make -j"$(nproc)" bzImage

# The kernel is at arch/x86/boot/bzImage. Compare its sha256 to the
# value published alongside the rangerdanger release:
sha256sum arch/x86/boot/bzImage
```

The build is deterministic given the pinned `KERNEL_TAG`, the
overlay file, and the toolchain. With the `SOURCE_DATE_EPOCH` +
`KBUILD_BUILD_*` env vars set as above, the same kernel tag +
overlay should produce a **byte-identical** bzImage to the one
published on the release page -- across CI runs and across local
rebuilds on a different host. The remaining failure mode for sha256
mismatch is a different gcc / binutils / glibc / dwarves version
than the ubuntu-latest CI runner had on the day we shipped the
release; if your tools match, the bytes will match.

## Verifying the binary you downloaded

`scripts/install-wsl-kernel.ps1` does this automatically before
installing, but you can do it manually:

```powershell
$expected = "<sha256 from the release asset's .sha256 file>"
$actual = (Get-FileHash -Algorithm SHA256 -Path .\rangerdanger-wsl2-kernel).Hash
if ($expected -eq $actual) { "ok" } else { "MISMATCH" }
```

## Opting out

`setup.ps1 -SkipKernelFix` skips the kernel-install probe entirely,
and `scripts/install-wsl-kernel.ps1 -Restore` removes our `kernel=`
line from `~/.wslconfig` (restoring the `.bak` we wrote when we
installed). The lab still runs without the kernel; you just lose ICS
DPI enforcement for Labs 2.3 and 2.3-bonus.

## Tradeoffs we accepted

- **Supply-chain surface.** Workshop students run a Linux kernel
  built by our CI, not by Microsoft. We mitigate with: a tiny
  publicly-auditable diff (this directory), reproducible builds via
  the public workflow log, sha256 verification on install, opt-out
  via `-SkipKernelFix`. Microsoft's kernel source remains unmodified;
  we only change one Kconfig file.

- **Kernel update cadence.** Microsoft updates the WSL2 kernel as
  Windows updates ship. Our build is pinned to a specific Microsoft
  tag, so if WSL2's host-side moves faster than our pin, students
  may run a kernel older than their stock WSL2 client would have
  given them. We bump the pin per workshop release. To check the
  delta, compare `kernel_tag` in `.github/workflows/build-wsl-kernel.yml`
  to `uname -r` after a Docker Desktop update.

- **Not portable to Hyper-V backend.** This kernel is for the WSL2
  backend only. Docker Desktop's Hyper-V backend uses a different
  (LinuxKit) VM image we cannot replace from the host. If `setup.ps1`
  detects the Hyper-V backend it skips this step and warns; either
  switch backends or run the lab without ICS DPI on that host.

- **Not relevant on macOS / Linux.** Docker on macOS already uses a
  LinuxKit VM kernel that ships with `CONFIG_NFT_QUEUE=y`; Docker on
  Linux uses the host kernel and any mainstream distro has it.
  `setup.ps1` and `setup.sh` only invoke `install-wsl-kernel.ps1` on
  Windows.
