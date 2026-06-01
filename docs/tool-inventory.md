# Tool inventory - what's installed where, and how to decide

The lab ships student-facing CLI tools (`mbpoll`, `dnp3poll`, `nmap`,
`tcpdump`, etc.) across multiple Dockerfiles, each tailored to the
zone-position and persona of the host. Several recent audits have
shuffled tools between images - this doc is the cross-reference so
the next author doesn't have to re-derive it from `apt-get install`
lines, and the [decision tree](#deciding-where-to-add-a-new-tool)
gives a stable answer to "this command needs to run somewhere - which
image?"

## Lab personas at a glance

| Image | Persona | Base | Zone | Lives in |
|-------|---------|------|------|----------|
| `kali` | External attacker | Kali Linux | Enterprise (10.10.10.50) | `Dockerfile.kali` |
| `eng-ws` | Engineering workstation | linuxserver/webtop ubuntu-mate | Vendor / DMZ (10.20.20.20) | `Dockerfile.eng-ws` |
| `vendor-jump` | Vendor support laptop | linuxserver/webtop ubuntu-xfce | Vendor / DMZ (10.20.20.10) | `Dockerfile.vendor-jump` |
| `corp-ws` | Office laptop | linuxserver/webtop ubuntu-mate | Enterprise (10.10.10.10) | (compose-only, no Dockerfile) |
| `openplc` | Substation automation PLC | tuttas/openplc_v3 | OT Operations (10.30.30.30) | `Dockerfile.openplc` |
| `rtac-sim` | Supervisory controller | alpine 3.21 (sim-base) | OT Operations + Field | `services/Dockerfile` (`rtac-sim` target) |
| `relay-sim`, `recloser-sim`, `regulator-sim`, `capbank-sim`, `historian-sim`, `gps-sim` | Field / OT sims | alpine 3.21 (sim-base) | Field or OT Ops | `services/Dockerfile` |
| `fuxa-hmi` | Operator HMI | frangoteam/fuxa | OT Operations (10.30.30.10) | (compose-only, no Dockerfile) |

## Tool matrix

**Inheritance note:** every sim stage in `services/Dockerfile` is
`FROM sim-base AS <sim>` (relay, recloser, regulator, capbank,
historian, gps, **and rtac**), so anything in the `sim-base` column is
also present in every per-sim image including `rtac-sim` - the matrix
only adds an explicit ✓ in the `rtac-sim` column when rtac-sim layers
on something *additional* (sshd, nginx, mbpoll built from source).
Treat `sim-base` ✓ as "every sim has this, rtac-sim included."

| Tool | kali | eng-ws | vendor-jump | openplc | rtac-sim (additions) | sim-base (all sims, incl. rtac) |
|------|:----:|:------:|:-----------:|:-------:|:--------:|:---------------------:|
| **Network basics** | | | | | | |
| `iproute2` | ✓ | | ✓ | | ✓ | ✓ |
| `iptables` | | | | | ✓ | ✓ |
| `iputils-ping` | ✓ | | | | | (busybox `ping`) |
| `traceroute` | ✓ | | | | | |
| `net-tools` (`netstat`) | ✓ | | | ✓ | | |
| `curl` | ✓ | (built-in) | (built-in) | ✓ | ✓ | (busybox `wget`) |
| `wget` | ✓ | | | | | (busybox) |
| `openssh-client` | ✓ | (built-in) | (server, ✓) | | | |
| `openssh-server` | | | ✓ | | ✓ | |
| **Capture / analysis** | | | | | | |
| `tcpdump` | ✓ | ✓ | ✓ | | | |
| `tshark` | ✓ | ✓ | | | | |
| `wireshark` (GUI) | | ✓ | | | | |
| **Port scanning** | | | | | | |
| `nmap` | ✓ | ✓ | ✓ | ✓ | | ✓ |
| `netcat` (`ncat`) | ✓ (`netcat-openbsd`) | | ✓ (`ncat`) | | | (busybox `nc`) |
| **ICS protocols** | | | | | | |
| `mbpoll` | ✓ (apt) | ✓ (apt) | ✓ (apt) | | ✓ (built-from-source) | |
| `dnp3poll` | ✓ (in-tree build) | ✓ | ✓ | ✓ | ✓ | |
| `dnp3cmd` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| `pymodbus` | | ✓ (pip) | ✓ (pip) | | | |
| **Languages / utilities** | | | | | | |
| `python3` | ✓ | ✓ | ✓ | ✓ | ✓ | |
| `python3-pip` | | ✓ | ✓ | | | |
| `bash` | (debian-base) | (built-in) | (built-in) | (debian-base) | (apk) | ✓ (sim-base) |
| **Remote access - clients** | | | | | | |
| `xfreerdp` (RDP) | ✓ (`freerdp-x11`) | | | | | |
| `xtightvncviewer` (VNC) | ✓ | | | | | |
| `sshpass` | ✓ | | | | | |
| **Remote access - servers** | | | | | | |
| `xrdp` | | | ✓ | | | |
| `x11vnc` | | | ✓ | | | |
| `nginx` | | | ✓ (`nginx-light`) | | ✓ | |
| `openssl` | | | ✓ | | ✓ | |
| **Storage** | | | | | | |
| `sqlite3` | | | | ✓ | | |
| **System** | | | | | | |
| `sudo` | | | ✓ | | | |
| `xfce4-terminal` | | | ✓ | | | |
| `shadow` (`chpasswd`) | | | | | ✓ | |
| `libmodbus` (runtime) | | | | | ✓ | |

Notes:

- **`mbpoll` on rtac-sim** is built from source against alpine's
  `libmodbus-dev` (`mbpoll-builder` stage) because it isn't in any
  alpine repo. The runtime image installs `libmodbus` to satisfy the
  dynamic link.
- **DNP3 tools** are built from the in-tree `dnp3go/` module via a
  `dnp3-builder` stage in each Dockerfile that needs them, then
  `COPY --from=dnp3-builder` into the runtime image. There is **no**
  apt/apk source for these - they're our own.
- **vendor-jump** runs the OpenSSH server (`openssh-server` package).
  The client-side `ssh` binary depends on whether
  `openssh-server`'s recommended dependency `openssh-client` resolves
  under `--no-install-recommends` (Ubuntu lists it as `Recommends`,
  not `Depends`); if you need the client guaranteed, add it explicitly
  to the apt install list in `Dockerfile.vendor-jump`.

## Deciding where to add a new tool

Use this in PR review or before opening one. The rule of thumb is
**add the tool to the lowest-privilege host that legitimately runs
the command in the lab narrative**, not "wherever's most convenient
to apt install."

```
Is the tool an attack tool (used to violate the policy)?
    → Dockerfile.kali. Always.
    Examples: nmap, mbpoll (write-coil), dnp3cmd (Direct Operate),
              xfreerdp (compromise lab), sshpass (auth-bypass demo).

Is it an operator/engineer diagnostic the student would run from
the engineering workstation?
    → Dockerfile.eng-ws.
    Examples: wireshark (GUI), tshark, mbpoll (read-only), python3
              + pymodbus for ad-hoc scripts.

Is it a vendor-laptop tool - implies the vendor support persona has
it pre-installed?
    → Dockerfile.vendor-jump.
    Examples: dnp3poll for monitoring, RDP server for the foothold,
              tcpdump for vendor-side capture.

Is it a field-side diagnostic command the lab YAMLs run from a
specific simulator?
    → services/Dockerfile, in the matching sim's stage. Prefer
      sim-base if every sim should have it; otherwise scope to the
      single sim that needs it (the rtac-sim openssh-server +
      nginx pattern).
    Examples: rtac-sim's mbpoll for "from the RTAC" Modbus poll;
              rtac-sim's sshd + nginx for vendor → OT mgmt listeners.

Does the tool only exist to make the lab UI's auto-run button
work? (i.e. lab YAML has a fence the smoke would otherwise mark
as "no source container")
    → Same answer as above - pick the persona the lab tells the
      student they're on, not a different container that happens
      to already have the tool. Smoke gates rely on `node:` matching
      the actual image inventory.
```

Three guardrails:

1. **Smoke catches the simple case.** `scripts/lab-commands-smoke.sh`
   walks every command block in every lab YAML and execs it from the
   declared `node:` container. If the tool isn't there, the smoke
   fails. CI runs this on every PR - adding a command without
   adding the tool will fail the build.

2. **Don't sprawl image size.** Webtop images (eng-ws, vendor-jump)
   already carry a desktop environment; one more apt package is
   cheap. Alpine sims are minimal - every package added to sim-base
   costs across all six images. Scope tightly.

3. **Document when you add anything.** Update this matrix in the
   same PR. The whole point of this doc is that the next person
   doesn't have to grep four Dockerfiles to answer "where is X."

## See also

- [`docs/lab-credentials.md`](lab-credentials.md) - credentials for
  services running on these images (vendor-jump RDP/VNC/SSH,
  rtac-sim SSH/HTTPS).
- [`docs/lab-authoring.md`](lab-authoring.md) - fence vocabulary +
  the `node:` field that smoke validates against this matrix.
- [`scripts/lab-commands-smoke.sh`](../scripts/lab-commands-smoke.sh)
  - the CI gate that catches drift between this matrix and what
  YAMLs actually reference.
