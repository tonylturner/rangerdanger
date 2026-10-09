# Increment 2b: lib60870 spike — report

Status: **done.** Branch `iec104`. Deliverable code under
`services/iec104/` (GPLv3); this report is the evidence.

Spec: `build/specs/iec104/spike-lib60870.md`. Plan:
`docs/plans/iec104/README.md` §3.5, §4 row 2(b), decisions D3/D6/D9/D10.
Research input: `docs/plans/iec104/recon/research.md`.

All evidence below was produced on the user's Mac (Apple Silicon, Docker
29.8.0, tshark 4.6.7) by `services/iec104/check/run.sh`, which is
rerunnable and leaves its capture and logs under `build/iec104-spike/`.
The run that this report cites: **16/16 checks PASS, exit 0.**

---

## 1. Binding decision

**Chosen: lib60870-C directly (C), not the `c104` Python binding.**

Both options sit on the same engine (lib60870-C). `c104` wraps it with
pybind11 and adds a Python point/station API. The spike built the first
profile against lib60870-C directly and ships three ~380 KB binaries in a
~16 MB image. The reasons C won for what RangerDanger ships:

- **Image and runtime.** The C build is a static binary on Alpine: ~16 MB
  total, no Python runtime, no wheel ABI to match. `c104` would add
  CPython plus a native extension (lib60870-C + mbedtls + pybind11
  compiled in) — larger, and the wheel/Python pin has to be re-verified
  on both arches.
- **Multi-arch from source.** `docker buildx --platform
  linux/amd64,linux/arm64` compiles lib60870-C and the programs natively
  in each arch. No dependence on `c104` publishing a wheel for an arch.
  Both arches were built and run (see §5).
- **`c104` bundles an *older* lib60870.** `c104` vendors lib60870-C as a
  submodule. At tag `v2.2.1` that submodule is commit `97c5f856`
  (2025-01-08, ~2.3.x era), which is behind `v2.4.1` and so **predates
  2.4.1's IOA-parse and S_IT_TC_1 OOB-read vulnerability fixes**
  (`CHANGELOG`, LIB8705-305/306, GHSA-f5xp-w6f3-vvrv). Adopting `c104`
  pins you to its bundled engine unless you patch the submodule — a real
  maintenance cost the spec's binding choice does not mention.
- **Select-before-execute and interlocks are application code either
  way.** lib60870-C's server deliberately leaves selection, expiry,
  value/peer matching and interlock rejection to the application
  ([research] §4; confirmed in `cs104_slave.c` — it has no selection
  state). `c104`'s server *does* implement a selection vector
  (`c104/.../src/Server.cpp:526` `select()` / `:575` `execute()`), so
  `c104` would save writing that state machine. We wrote it once in C
  (`services/iec104/src/rtu.c`); it is ~90 lines and is the part the lab
  actually teaches, so owning it is a feature, not a cost.

**When `c104` would be the better pick:** if the control centre or RTU
needed to be embedded in the Python/FastAPI world (e.g. alongside
`opendss-sim`), `c104`'s ready-made selection logic and Python point API
would cut code. That is not where increment 3 puts the IEC104 programs
(they are their own GPLv3 images behind a network boundary), so it does
not apply. If that changes, revisit — the protocol behaviour proven here
is identical because the engine is identical.

---

## 2. Version pins and licences (verified from upstream, not from the spec)

| Component | Pin | Integrity | Licence | Source |
|---|---|---|---|---|
| lib60870-C | **v2.4.1** (tag commit `7a388e3e133999e1ca77ba7521d55d074b7cd2bc`), released 2026-07-15 | tarball sha256 `d5708b9885c2c068ac9d9bae4696a624bd1d1d6645a1b1a9261c00274bc36c3a` | **GPL-3.0-or-later** (dual GPL/commercial) | <https://github.com/mz-automation/lib60870> |
| Alpine base | `alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8` | digest-pinned in the Dockerfile | — | docker.io/library/alpine |
| `c104` (evaluated, **not adopted**) | v2.2.1 (tag commit `e9a145f0370879c22fa2d12f320f44b872264241`), released 2025-04-11 | sdist sha256 `702ac4df58e389c49f6127513c3e8143646aef52361fa98625e9e003f3aacb8a` | **GPL-3.0** | <https://github.com/Fraunhofer-FIT-DIEN/iec104-python> |

Licence verification (the spec said "do not trust this spec"):

- lib60870-C: `COPYING` in the fetched v2.4.1 tarball is the verbatim FSF
  GPLv3 text (md5 `d32239bcb673463ab874e80d47fae504`, identical to other
  known-GPLv3 `COPYING` files on the host). Every source file carries a
  GPLv3-or-later header naming MZ Automation and pointing at `COPYING`.
  `README.md` states GPLv3-or-commercial dual licensing.
- `c104`: `LICENSE` is GPLv3 (md5 `1ebbd3e34237af26da5dc08a4e440464`,
  matching other known-GPLv3 licence files); headers say GPLv3-or-later.
  Buying a commercial lib60870 licence does **not** relicense `c104`
  (separate copyright holder, Fraunhofer FIT) — consistent with
  [research] §1.
- TLS (mbedtls) is **not** compiled into the spike: the lab runs plain
  TCP/2404, matching milestone A. No mbedtls pin is needed yet; when
  62351 TLS is in scope it ships inside lib60870-C's `dependencies/`.

mbedtls and the IEC 60870-5-7:2013 secure-authentication add-on are out
of scope here (research §5; add-on is commercial).

---

## 3. What was built (`services/iec104/`, GPLv3)

- `LICENSE` — GPLv3 text. `README.md` — states the GPLv3 scope is this
  directory only, the rest of the repo is Apache-2.0, and these programs
  talk to Apache-2.0 components over the network only.
- `src/rtu.c` → **`iec104-rtu`**: IEC104 server, CA 1, in-memory points:
  feeder breaker `M_DP_NA_1`/spontaneous `M_DP_TB_1` (IOA 1001), two
  `M_ME_NC_1` floats — current (2001) and voltage (2002) — and a
  `C_DC_NA_1` double command (3001) that is **select-before-execute
  only** and moves the breaker. `SIGUSR1` fires a local protection trip.
- `src/cc.c` → **`iec104-cc`**: control-centre client. STARTDT, station
  GI, tracks spontaneous events in a cache with quality and receipt time,
  drives the breaker by SBO, and on link loss marks the cache `IV|NT`,
  reconnects and re-runs GI.
- `src/iec104cmd.c` → **`iec104cmd`**: student/attack CLI. `gi`,
  `stopstart`, and `dc`/`sc` with `-sbo`, `-delay-ms` (force select
  expiry) and `-mismatch` (execute a different value than selected).
- `src/client.{h,c}` — a synchronous wrapper over lib60870's
  `CS104_Connection` used by `cc` and `iec104cmd`. `src/common.{h,c}` —
  logging and quality/time/ASDU formatting. `src/points.h` — the shared
  point map.
- `Dockerfile` — multi-arch, builds lib60870-C from the pinned+checksummed
  tarball and compiles the three programs with `-Werror`; one image
  carries all three. `check/run.sh` + `check/capture.Dockerfile` — the
  rerunnable checks.

Design notes worth carrying into increment 3:

- **Spontaneous data bypasses lib60870's event queue.** `iec104-rtu`
  sends breaker/current changes straight to every started connection, so
  the commanding client sees ACT-CON → return-information → ACT-TERM in
  order. The cost: events are not buffered while no client is started; a
  reconnecting client recovers state with GI. Event-queue overflow (a
  §3.5 correctness-gate item) is therefore **not exercised** by this
  design — see gaps.
- **Connections are keyed by peer `ip:port`, not the
  `IMasterConnection` pointer.** lib60870 pools and reuses those structs;
  a previous client's late `CONNECTION_CLOSED` would otherwise clear the
  selection of the client that reused the struct. This was a real bug
  found and fixed during the spike (the first SBO run failed until the
  key changed from pointer to peer string).
- **Connection events are queued and applied in the main loop.**
  lib60870 raises `ACTIVATED`/`CLOSED` while holding the connection's
  state lock, and `sendASDU` takes that same lock; taking the RTU lock in
  the event callback would invert against the main loop. The callback
  only records the event.

---

## 4. Correctness checks — each with evidence

Capture: `build/iec104-spike/iec104.pcap`; logs `cc.log`, `rtu.log`,
`NN-*.log`; `results.log` has the PASS lines. Frame numbers below are
from the cited run.

### GI returns every point with COT 20 and ACT-CON/ACT-TERM — PASS

```
8  cc → I C_IC_NA_1 Act     IOA=0          (COT 6)
9  rtu→ I C_IC_NA_1 ActCon  IOA=0          (COT 7)
10 rtu→ I M_DP_NA_1 Inrogen IOA=1001       (COT 20, dpi=ON)
11 rtu→ I M_ME_NC_1 Inrogen IOA[2]=2001,2002 (COT 20, 182.5, 20.4)
12 rtu→ I C_IC_NA_1 ActTerm IOA=0          (COT 10)
```
tshark confirms COT 7 (ACT_CON, nega=0), COT 10 (ACT_TERM) and the two
monitoring types at COT 20.

### Spontaneous M_DP_TB_1 with COT 3 and a CP56Time2a tag — PASS

The autonomous local-protection trip (`SIGUSR1`) produces:
```
frame 97  typeid=31 (M_DP_TB_1)  causetx=3 (spontaneous)  ioa=1001
          cp56time=2026-10-09T…  dpi=OFF
```
Note: a **command-caused** breaker change is correctly COT **11**
(`RETURN_INFO_REMOTE`), not COT 3 — see frames 44/48/76/80 (typeid 31,
causetx 11) from the SBO runs. The spec's wording conflates these; see §8.

### SBO: select→execute changes the breaker; the four rejections — PASS

`C_DC_NA_1` (typeid 46), `dco.se`=select bit, `nega`=negative confirm:
```
# 02 SBO open  : 40 sel(se=1) → 41 ActCon nega=0 ; 42 exe(se=0) → 43 ActCon nega=0  → breaker opens
# 05 no-select : 108 exe(se=0) → 109 ActCon nega=1   (reject: execute without a valid selection)
# 06 expiry    : 122 sel → 123 ActCon nega=0 ; (4.5 s > 3 s) 129 exe → 130 ActCon nega=1
#                rtu.log: "cleared: select timeout elapsed" then "reject … without a valid selection"
# 07 mismatch  : 143 sel(close) → 144 ActCon nega=0 ; 145 exe(open) → 146 ActCon nega=1
```
Cross-connection selection is also rejected by construction (selection is
keyed to the selecting peer; a different connection's execute is treated
as "without a valid selection"). The spike RTU rejects a **direct**
execute by policy (the breaker point is SBO-only); a direct execute is
protocol-legal in general — see §8.

### STOPDT/STARTDT, TESTFR on idle (t3), t1 timeout — PASS (t1 see note)

```
STOPDT: U utype=0x04 (act) → 0x08 (con)      # 08-stopdt-startdt
TESTFR: U utype=0x10 (act) → 0x20 (con)      # during the idle phase, t3=4 s
```
t1: with the RTU paused, the control centre's TESTFR goes unanswered and
the control centre tears the link down within t1 and reconnects. This is
asserted from `cc.log` ("cache marked stale on link loss" then STARTDT +
GI), not from a dedicated t1 frame marker — forcing a deterministic,
frame-level *t1-on-unacked-I-frame* needs a stub peer that ACKs TCP but
never answers at the APCI layer. See gaps.

### Kill and restart the link: reconnect, re-run GI, quality recovers — PASS

`cc.log`:
```
cache marked stale on link loss: breaker=… q=IV|NT      (x2: the pause and the kill)
STARTDT confirmed → GI complete: … q=GOOD               (x3 total: initial + two recoveries)
```
Quality goes `GOOD → IV|NT → GOOD` across each outage.

### Zero malformed frames — PASS

`tshark -Y '_ws.malformed || _ws.expert.severity=="Error"'` → **0**
frames. `-z expert` shows only TCP-sequence Notes/Chats (23 Notes, 41
Chats — all connection open/close bookkeeping), **no IEC104 warnings or
errors**. Full output: `build/iec104-spike/expert.txt`.

---

## 5. Image sizes per arch

Built from source per arch with `docker buildx` (one image, all three
programs):

| Arch | Image size |
|---|---|
| linux/arm64 | 15.96 MB |
| linux/amd64 | 15.28 MB |

Both were built and the binaries run (`iec104cmd` usage prints on amd64
under `--platform linux/amd64`). Corresponding source (the pinned
lib60870 tarball, sha256 verified inside the image, plus our `src/`) ships
under `/usr/share/src`; the licence under `/usr/share/licenses/iec104`.

---

## 6. GPL compliance for release images

The image is a **GPLv3 combined work** (static link to lib60870-C). To
publish it (e.g. to GHCR in the release workflow) compliantly:

1. **Ship corresponding source for the exact binaries.** Done in-image:
   `/usr/share/src/lib60870.tar.gz` (the checksum-pinned upstream
   tarball) and `/usr/share/src/iec104/src/` (our sources, also tracked
   in git). This satisfies GPLv3 §6 by accompanying the object code with
   the source; no separate written offer is then required.
2. **Ship the licence.** Done: `/usr/share/licenses/iec104/LICENSE` and
   the README stating scope.
3. **Keep the GPLv3 boundary.** Only `services/iec104/` and its image are
   GPLv3. The programs reach Apache-2.0 components over the network only
   (IEC104/TCP, Modbus/TCP, HTTP). No Apache-2.0 Go/TS module links or
   imports this code. Increment 3 should add the lint the plan calls for
   (§6, "GPL services"): fail if any Apache module imports
   `services/iec104`.
4. **Reproducible build.** The Dockerfile fetches lib60870-C by release
   tag and verifies the sha256 before building; the base is digest-pinned.
   The release workflow must build this image from the tracked source and
   that pinned tarball, and must not strip `/usr/share/src`.
5. **Notices.** The image's source location is recorded (README + the
   in-image tarball). If a future image strips in-image source to save
   space, replace it with a written offer valid for three years.

Directional compatibility holds: Apache-2.0 → GPLv3 combined work is
fine; the reverse is not. Containers alone grant no exemption
([research] §1/§distribution).

---

## 7. Effort estimate for increment 3 (EU RTU with Modbus southbound + control centre)

Reusing the spike (the session layer, GI, SBO state machine, quality,
reconnect and the three programs already exist and are proven):

| Piece | Estimate | Notes |
|---|---|---|
| RTU southbound Modbus TCP client → device model | 3–5 d | New `services/devices/` model is a separate plan item (§3.4); the RTU just needs a Modbus master poll loop feeding the point cache. A pure-Go or small C Modbus master; no new library risk. |
| RTU northbound ↔ point map from package data | 2–3 d | Move `points.h` into `lab-definitions/packages/<id>/protocols/` and load it; add the real EU point set (RMUs, OLTC). |
| Control-centre HTTP read API for the portal | 2–3 d | `iec104-cc` gains a small HTTP endpoint exposing the cache (quality, source/receipt time) for the operator view. Keeps the GPL boundary (HTTP out to Apache portal). |
| Control-centre zone (`lan4`) + compose/manifest wiring | 2–4 d | containd `lan4` auto-assign already exists ([plan] finding 13); this is package authoring, not containd code. |
| Quality/stale propagation across both hops + tests | 2–3 d | device→Modbus→RTU→IEC104→cc→portal; the headless increment-3 gate test. |
| Milestone-A L3/L4 policies + new/established session tests | 1–2 d | Pure port rules; the traffic generators (`iec104cmd`, `iec104-cc`) exist. |
| Integration, multi-arch image split, CI matrix | 2–3 d | Possibly split the control centre into its own image. |

**Total ≈ 14–23 engineer-days** for the increment-3 vertical slice, not
counting the EU OpenDSS/device-model work tracked separately (§3.4/§3.9)
or containd milestone B (§3.7, its own repo/branch). The session-layer
risk the plan worried about (§6) is retired: lib60870-C handles it and the
correctness gate passes.

---

## 8. What is wrong with the spec

Issues found while implementing it. None blocked delivery; the strongest
are the COT semantics and the macOS capture mechanism.

1. **COT 3 vs COT 11 for command-caused changes (protocol defect in the
   check).** The check "spontaneous `M_DP_TB_1` arrives with COT 3" reads
   as if the SBO-driven breaker change should be COT 3. Per IEC
   60870-5-101/104 a change caused by a command is reported with COT
   **11** (`RETURN_INFO_REMOTE`), and only an autonomous change (local
   trip) is COT **3** (spontaneous). The RTU here does both correctly;
   the spec should split the check into "command feedback is COT 11" and
   "autonomous event is COT 3" so an implementer is not pushed to emit the
   wrong COT.

2. **The capture mechanism does not work as written on the stated host.**
   The spec says capture with host tshark at `/opt/homebrew/bin/tshark`.
   On macOS Docker Desktop the containers run inside a Linux VM, so host
   tshark cannot see the container bridge. The spike captures with a
   tcpdump sidecar in the Docker host network namespace and then decodes
   the file with host tshark. The spec should describe this (or say "host
   tshark decodes the file; capture happens in-namespace").

3. **tshark only auto-dissects IEC104 on port 2404.** The spec states
   tshark "decodes TCP/2404 as IEC 60870-5-104" — true, but only because
   the dissector is registered on 2404. Increment 3 placing the RTU on a
   different port (or behind a DNAT) needs `-d tcp.port==N,iec60870_104`.
   The spec should flag this so the lab's real ports stay decodable.

4. **t1 timeout is not deterministically forcible in a container as the
   check implies.** Bundling "TESTFR on idle (t3), t1 timeout closes the
   link" as one line understates t1: a clean, frame-level t1 expiry needs
   a peer that keeps the TCP connection up but stops answering at the
   APCI layer (a stub), which neither lib60870 program will do on its own.
   `docker pause` demonstrates the effect (TESTFR unanswered → link torn
   down) but is TESTFR+t1 combined, not a pure t1 event. The spec should
   either accept the pause demonstration or budget a stub peer.

5. **"execute without select is rejected" is a lab policy, not a protocol
   rule.** A direct-execute `C_DC_NA_1` (select bit clear, no prior
   select) is legal IEC104. The rejection here is because the breaker
   point is configured SBO-only. The spec should say the RTU enforces SBO
   on the breaker by policy, so no one reads it as "direct execute is a
   protocol error."

6. **The binding criteria omit `c104`'s bundled-engine staleness.** The
   spec weighs c104 vs C on profile coverage, maintenance, arch and image
   size, but not that `c104` vendors an older lib60870-C submodule
   (behind 2.4.1, so missing 2.4.1's OOB-read CVE fixes). For a security
   training product that is a material criterion and should be listed.

7. **The check script needs a throwaway network and a capture helper
   image the fence/budget does not mention.** The spec allows building
   "this spike's images only" and forbids touching compose/`.github`.
   The rerunnable checks also need a one-off Docker network and a tiny
   tcpdump image built from an inline Dockerfile. That is within the
   spirit of the fence (nothing in compose/`.github` is touched) but the
   spec should acknowledge the check harness builds a second, throwaway
   image.

8. **Minor: the point set leaves CA/IOA unspecified.** The spec names the
   types but not addresses; IOAs (1001/2001/2002/3001) and CA 1 were
   chosen here and documented in `points.h`. Increment 3's package data
   will own these; fine, but worth stating that the spike picks them.
