# Multi-arch lifecycle testing

How to exhaustively verify that **setup → execution → teardown** works
flawlessly across architectures and install paths. The goal: a student on
any supported host can install, run every lab, and cleanly remove the
stack — and re-run it months later (lasting training value).

## The matrix

Test each **platform × install-path**, asserting the full **lifecycle**.

| Platform | Online | Offline SSD | Delta | Notes |
|---|---|---|---|---|
| **arm64 Linux** (Docker Engine) | ✅ | ✅ | ✅ | The new surface. OpenPLC runs amd64 under `qemu-x86_64` emulation. Fully testable in a Multipass VM. |
| **x86_64 Linux** (Docker Engine) | ✅ | ✅ | ✅ | OpenPLC native. |
| **Apple Silicon** (Docker Desktop) | ✅ | ✅ | ✅ | OpenPLC under Rosetta. |
| **Windows** (Docker Desktop + WSL2) | ✅ | ✅ | ✅ | OpenPLC native (x86_64). DPI labs need the custom WSL2 kernel. |

**Lifecycle asserted per cell:** setup gates pass → all services running →
OpenPLC running → web/containd UIs up → firewall apply (weak+improved) +
workshop reset succeed → uninstall leaves the host clean (no containers /
volumes / `.env`, emulation/kernel reverted) → re-install works.

## Linux / macOS — automated (`test-lifecycle.sh`)

One command runs the whole lifecycle and prints PASS/FAIL. Use a **test
host or throwaway VM** — it installs the full stack, then removes it.

```sh
./scripts/test-lifecycle.sh                       # online, full lifecycle
./scripts/test-lifecycle.sh --from-tarballs <DIR> # offline / SSD path
./scripts/test-lifecycle.sh --reinstall           # also re-install after teardown
./scripts/test-lifecycle.sh --no-teardown         # leave stack up to poke at
```

It catches what a bare `./setup.sh` does not: OpenPLC's readiness probe is
non-fatal, so setup can exit 0 with OpenPLC crash-looping — the harness
asserts it explicitly, and verifies teardown is clean.

### arm64 Linux via Multipass (on an Apple Silicon Mac)

```sh
brew install --cask multipass
multipass launch 24.04 --name rd --cpus 4 --memory 12G --disk 40G
multipass mount "$PWD" rd:/home/ubuntu/rangerdanger
multipass shell rd
# --- inside the VM ---
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER" && newgrp docker
cd ~/rangerdanger
./scripts/test-arm-linux-emulation.sh --yes   # quick: emulation register/run/revert
./scripts/test-lifecycle.sh --yes             # full lifecycle (~20-40 min)
```

## Windows — `test-lifecycle.ps1` (or manual)

One command, the Windows counterpart of `test-lifecycle.sh` (elevated
PowerShell, repo root):

```powershell
.\scripts\test-lifecycle.ps1                       # online, full lifecycle
.\scripts\test-lifecycle.ps1 -FromTarballs D:\WORKSHOP_SSD
.\scripts\test-lifecycle.ps1 -Reinstall
```

Or run the phases by hand (also useful for spot-checks). x86_64 Windows
runs OpenPLC natively (no emulation; the binfmt/qemu path does not apply):

```powershell
# 1. SETUP  (online; or: .\setup.ps1 -FromTarballs D:\WORKSHOP_SSD)
.\setup.ps1

# 2. ASSERT UP
(Invoke-WebRequest http://localhost:8088/api/health -UseBasicParsing).StatusCode   # 200
(Invoke-WebRequest http://localhost:9080/            -UseBasicParsing).StatusCode   # 200 (containd UI)
docker compose -f docker-compose.release.yml ps                                    # every service "running"
docker inspect -f '{{.State.Status}}' rangerdanger-openplc                         # running

# 3. EXECUTE (workshop-critical surfaces)
Invoke-RestMethod -Method Post -Uri http://localhost:8088/api/firewall/apply -ContentType application/json -Body '{"config":"weak"}'
Invoke-RestMethod -Method Post -Uri http://localhost:8088/api/firewall/apply -ContentType application/json -Body '{"config":"improved"}'
Invoke-RestMethod -Method Post -Uri http://localhost:8088/api/workshop/reset       # expect success = True
#   Spot-check a DPI lab in the browser at http://localhost:8088/exercises (Lab 2.3 needs the WSL2 kernel).

# 4. TEARDOWN
.\scripts\uninstall-rangerdanger.ps1 -Yes
docker ps -a --filter "name=rangerdanger-"     # expect: none
Test-Path .env                                 # expect: False
#   The uninstaller also reverts the custom WSL2 kernel and runs `wsl --shutdown`.

# 5. RE-INSTALL (idempotency) — repeat step 1, then step 2.
```

**Windows-specific things to confirm:**
- Docker Desktop is on the **WSL2 backend**.
- DPI labs (2.3 / 2.3-bonus): `setup.ps1` installs a custom WSL2 kernel
  (`CONFIG_NFT_QUEUE=y`); confirm DPI events appear, and that
  `uninstall-rangerdanger.ps1` reverts the kernel (it restores
  `.wslconfig.bak` / removes our `kernel=` line, then `wsl --shutdown`).
- Loopback ports `8088 / 9080 / 9443 / 2222` free before setup.

## What still needs real hardware

The Multipass VM de-risks the bulk of the arm64-Linux logic, but a few
cells need a physical box: a real **arm64 Linux laptop** (to confirm
performance + reboot persistence via `sudo ./scripts/persist-emulation.sh`)
and **Windows** (WSL2 kernel install/revert can't be emulated on a Mac).
