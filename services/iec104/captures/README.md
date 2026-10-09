# IEC 60870-5-104 reference captures

These packet captures are Apache-2.0 test data; see `LICENSE` and the
repository-root `LICENSE` for the complete license text. The programs in
`services/iec104/` remain GPLv3. These files contain only protocol traffic,
not lib60870 code or binaries.

## Regenerate

From the repository root, run:

```sh
services/iec104/captures/regenerate.sh
```

The script runs `check/run.sh` in a temporary directory, splits the
combined TCP/2404 capture by the harness's unique `iec104cmd` source
addresses, writes the four pcaps below, and updates `SHA256SUMS`. Each
`iec104cmd` invocation has a distinct TCP stream. All control-centre
streams are included in both the startup and session captures: reconnects
create separate TCP streams, and whole-stream extraction preserves both
startup and session evidence. Those two outputs therefore overlap and
neither is a time-window-only slice.

| Capture | Contents | Source stream(s) from `check/run.sh` |
|---|---|---|
| `startup-gi-monitor.pcap` | Startup handshake/GI; types 3, 13 and 31; spontaneous COT 3; S-frames and TESTFR | Control centre `.20` (startup GI, spontaneous changes and idle TESTFR); `01-gi` `.35` |
| `sbo-double-command.pcap` | Type 46 select/execute/termination; execute without select; expired select; mismatched execute | `02-sbo-open` `.30`; `03-sbo-close` `.36`; `05-exec-no-select` `.31`; `06-select-expiry` `.32`; `07-value-mismatch` `.33` |
| `session-management.pcap` | STOPDT/STARTDT; TESTFR; unanswered TESTFR/t1 timeout; link loss, RTU restart, reconnect and GI | Control centre `.20` (idle, pause/restart and reconnect); `08-stopdt-startdt` `.34` |
| `edge-addressing.pcap` | CA 1 GI returns IOAs 65535, 65536 and 16777215; CA 2 GI returns IOA 4001; unassigned command IOA 0 receives COT 47; GI command IOA 0 | `01-gi` `.35`; `04-gi-ca2` `.37`; `09-ioa-zero-unassigned` `.38` |

`SHA256SUMS` contains a SHA256 digest for each pcap.

## Public references (not bundled)

- Wireshark [SampleCaptures](https://wiki.wireshark.org/SampleCaptures#iec-60870-5-104):
  `iec104.pcap` adds direct-operate types 45–51 (our type 46 traffic is
  SBO) and multi-APDU TCP segments; `IEC104_SQ.pcapng` adds SQ=1 (16
  objects) and multiple APDUs per packet, neither exercised by our
  captures. Attachment provenance/licence is unconfirmed:
  reference only, never vendor. Downloads: `https://wiki.wireshark.org/uploads/__moin_import__/attachments/SampleCaptures/iec104.pcap`
  (SHA256 `a78aa971adc51e54413a865937f1799ef57118d397cef57ccd93a358ed5b85d6`) and `https://wiki.wireshark.org/uploads/__moin_import__/attachments/SampleCaptures/IEC104_SQ.pcapng`
  (SHA256 `f855a11326f7aa4f719b1fbb65e5f8dfe3d9d194185a8f5faf5b5dc3cb831227`).
- Peter Maynard [dataset-v1](https://figshare.com/articles/dataset/dataset-v1_pcap/6133457)
  (38,337,304 B, CC BY 4.0) adds COT 42 MITM and clock-sync Type 103
  traffic, plus multi-APDU segments. Download `https://ndownloader.figshare.com/files/11064965`
  (SHA256 `663c73fd9a957070f88d6bc47cac0dc859610d045f4516e6ffae099c7c6f84da`); reference/fetch only, not bundled.

containd tests may fetch these only in an opt-in job, never in default
unit tests. Do not download or vendor them as part of capture regeneration.

## Stack and configuration

The endpoint image uses lib60870-C v2.4.1 (source tarball SHA256
`d5708b9885c2c068ac9d9bae4696a624bd1d1d6645a1b1a9261c00274bc36c3a`) on
Alpine 3.22 (base image digest
`sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8`).
The application layer uses COT width 2, CA width 2, and IOA width 3.
The RTU advertises `k=12,w=8,t0=10,t1=15,t2=10,t3=20`; the harness
control centre overrides `t1=3,t3=4` and uses lib60870 defaults for its
other connection parameters. The RTU select timeout is 3000 ms.

Both endpoints use TCP/2404. The RTU is `10.204.104.10`; the control
centre is `10.204.104.20`. `iec104cmd` runs use `.30` through
`.38` as listed in the table.

The generator uses the host's `tshark` and `editcap` executables and
rejects a version mismatch. Their version is not pinned by `check/run.sh`
or the Dockerfile; the old spec's claim that the harness already pins
them is incorrect.

## Point map

The RTU answers station GI with the monitoring points for the requested
common address. IOA 3001 is a command point, not GI telemetry.

| CA | IOA | Type | Meaning / value |
|---:|---:|---|---|
| 1 | 1001 | `M_DP_NA_1` / `M_DP_TB_1` | Feeder breaker position; initial state closed, `COT 3`/remote-command events are time-tagged |
| 1 | 2001 | `M_ME_NC_1` | Feeder current, 182.5 A closed / 0 A open |
| 1 | 2002 | `M_ME_NC_1` | Busbar voltage, 20.4 kV |
| 1 | 3001 | `C_DC_NA_1` | Breaker double command, select-before-execute only |
| 1 | 65535 | `M_ME_NC_1` | Boundary test measurement, 65.535 |
| 1 | 65536 | `M_ME_NC_1` | Boundary test measurement, 65.536 |
| 1 | 16777215 | `M_ME_NC_1` | Maximum 24-bit IOA test measurement, 167.77215 |
| 2 | 4001 | `M_ME_NC_1` | Auxiliary-station frequency, 50.02 Hz |

IOA 0 is not a configured monitoring point. In this harness it is the
address in a `C_IC_NA_1` station-interrogation command (Type ID 100). A
double command to unassigned `(CA 1, IOA 0)` receives a negative
confirmation with COT 47, “unknown information object address.” IEC
60870-5-101:2003+A1:2005 defines COT 47 as that unknown-address cause,
which is used by IEC 60870-5-104:2006+A1:2016. This is not a rule that
every possible use of address zero is invalid: the meaning depends on the
ASDU and configured point map.

## Command values and endpoints

The successful control commands use `C_DC_NA_1` at CA 1 / IOA 3001 with
`QU=0`: DCS 1 is OFF (`open`) and DCS 2 is ON (`close`). The SBO streams
select then execute the same value:

| Stream | Command |
|---|---|
| `02-sbo-open` | Select and execute `open` |
| `03-sbo-close` | Select and execute `close` |
| `05-exec-no-select` | Direct `close` execute, expected negative |
| `06-select-expiry` | Select `close`, wait 4500 ms, then execute `close`, expected negative after the 3000 ms timeout |
| `07-value-mismatch` | Select `close`, then execute `open`, expected negative |
| `09-ioa-zero-unassigned` | Direct `close` to unassigned IOA 0, expected negative COT 47 |

`08-stopdt-startdt` exercises STOPDT followed by STARTDT. The `01-gi`
stream performs station GI on CA 1; `04-gi-ca2` performs station GI on
CA 2. The control-centre streams are the source of startup/reconnect GI and
the spontaneous, TESTFR and t1 evidence.
