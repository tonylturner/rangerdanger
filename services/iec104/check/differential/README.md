<!-- SPDX-License-Identifier: Apache-2.0 -->

# IEC 104 capture comparator

This Docker-free tool compares two raw captures for the IEC 104 differential
harness. It uses Python 3 standard-library modules and TShark 4.6.x as an
independent black-box decoder. It checks the four first-party captures under
`services/iec104/captures/`; those files are inputs and must not be edited.

```sh
python3 compare.py BASELINE.pcap CANDIDATE.pcap \
  [--allowlist allowlist.json] [--sessions session-map.json] \
  --out build/iec104-compare
```

The default decoder is `/opt/homebrew/bin/tshark`. `--tshark` can select an
installed executable on another host, but the comparator rejects versions
outside 4.6.x. It discovers fields with `tshark -G fields` and verifies the
required names before decoding hierarchical PDML; it does not parse flattened
`-T fields` output. Every TShark subprocess is bounded (60 seconds by default;
set `--timeout` to change that bound). The time comparison tolerance defaults
to 1000 ms and can be changed with `--cp56-tolerance-ms`.

On Ubuntu 26.04, AppArmor confines `/usr/bin/tshark` to system and `/tmp`
files, so it cannot read captures in a checkout under your home directory.
Allow the checkout in the profile's site-local file, as CI does:

```bash
echo "file r $PWD/**," | sudo tee /etc/apparmor.d/local/tshark
sudo apparmor_parser -r /etc/apparmor.d/tshark
```

Run it from the repository root.

The command always writes `report.json` and `report.md` on a completed
comparison. Exit status is 0 for a pass, 1 for protocol/decode/allow-list
differences, and 2 for a tool or input error.

## Three independent gates and one report-only comparison

### A. Message-level semantics (gate)

TShark's decoded IEC APCIs are emitted in capture order, tagged with the
connection ordinal and direction (`client_to_server` or `server_to_client`).
Each record includes I/S/U frame kind and U function; when present, each ASDU
includes type, SQ, object count, COT cause and P/N/T flags, OA, CA, ordered IOA
and value arrays, raw float bits, quality bytes, time-tag presence and decoded
CP24/CP56 fields (including IV, SU, DOW and reserved bits), command
select/execute and qualifier values, and QOI.

ASDUs and their repeated objects are taken from each PDML protocol subtree.
No ASDU or object list is sorted, collapsed, or zipped against a global flat
field list. CP56 clock/date fields are retained in the JSON evidence, while
their semantic comparison uses the rounded offset from the session's first
usable CP56 value. DOW, IV, SU, reserved bits, and time-tag presence remain
exact. CP24 values compare exactly.

### B. Reassembled APDU bytes (gate)

TCP payloads are collected per TShark connection and direction, unwrapped
around 32-bit TCP sequence rollover, and reassembled by sequence position.
Identical retransmitted bytes are counted once; conflicting overlaps and
sequence gaps fail decode validity. Complete APDUs are split from the stream,
so TCP segmentation and coalescing do not define message boundaries. The
comparator's own APCI parser also verifies that all unique port-2404 payload
bytes belong to complete APDUs.

Only these bytes are normalized:

1. I-frame N(S)/N(R) and S-frame N(R) are independently rebased from the
   first observed value in each session/direction, modulo 32768. Relative
   deltas and wrap remain significant.
2. For known CP56-bearing ASDU layouts, valid CP56 timestamps become their
   session-relative offset, rounded to the configured tolerance. The first
   valid timestamp is the reference; invalid or unusable timestamps remain
   byte-exact. CP56 IV, SU, DOW, reserved bits and encoding status remain
   exact.

Every changed byte is listed in `normalization_audit`, with side, session,
direction, frame, APDU index, APDU/stream byte offsets, and old/new hex bytes.
The implemented CP56 APCI layouts cover types 30–37 and 58–64; if TShark
decodes a CP56 timestamp in a different ASDU layout, validity fails rather
than silently comparing it without the required normalization.

### C. Independent decode validity (gate)

TShark must exit successfully on each raw capture. `_ws.malformed`, expert
errors on IEC/port-2404 payload frames, invalid or incomplete APCIs, TCP
reassembly gaps/conflicts, a TShark/APCI APDU count mismatch, or uncovered
TCP payload bytes fail this gate. TCP retransmissions are supported and do
not count as additional APDU bytes.

A scenario may additionally declare deliberate decoder diagnostics with
`"malformed": [{"step": 1, "message": "input"}, {"step": 1, "message": "reply"}]`.
Steps are zero-based action indexes; messages are the crafted input or its direct
reply, never an S acknowledgment or an unrelated subsequent message. The peer
records each association as a session, direction and direction-local APDU index.
The runner resolves both sides independently and saves `declared-malformed.json`;
standalone comparison accepts that file via `--declared-malformed`.

Every declared message must have diagnostics on **both** sides, identical in
text, order and multiplicity after removing only the capture frame prefix.
Missing, stale, duplicated, mismatched or ambiguous associations fail gate C.
Packet-level diagnostics on coalesced messages fail closed rather than excusing
another message. All original diagnostics and declaration results remain in the
JSON/Markdown evidence. Undeclared diagnostics still fail; reassembly, coverage,
APCI/parser and normalization errors can never be declared. Gates A/B compare
these messages and bytes unchanged. This is not an allow-list exception and the
shipped allow-list stays empty.

### D. TCP behavior (report only)

Per-session/direction segment and data-segment counts, captured and unique
payload bytes, duplicate bytes, coalesced segments, ACK and ACK-only counts,
ACK inter-arrival intervals and ACK/data-segment ratio, FIN/RST/SYN counts,
and APDU counts appear in the report. Differences here are evidence only and
never fail a comparison.

## Sessions

Without `--sessions`, the Nth connection containing port 2404 in each capture
pairs by capture start order (zero-based in report/allow-list). Unpaired
connections are differences. An explicit JSON object maps baseline indexes to
candidate indexes:

```json
{
  "0": 1,
  "1": 0
}
```

The map is one-to-one; omitted sessions are unpaired.

## Exact allow-list policy

`allowlist.json` ships empty. Entries suppress only exact A/B differences and
must be narrow, reviewed, and bounded. One entry uses either the exact
`baseline` and `candidate` values, or a numeric `delta` (candidate minus
baseline):

```json
[
  {
    "id": "approved-change-123",
    "scope": {
      "scenarios": ["rtu-quality"],
      "role_set": "go-rtu",
      "comparison": "A",
      "session": 0,
      "direction": "server_to_client",
      "record_range": [4, 4]
    },
    "field": "asdu.quality_bits[0].bits",
    "baseline": "00",
    "candidate": "01",
    "reason": "Reviewed scenario-specific interoperability variance",
    "approved_by": "Tony / approval reference",
    "max_matches": 1
  }
]
```

`scenarios` is a nonempty list of unique, exact scenario IDs; `role_set` is one
exact comparison identity chosen by the caller (for example `all-go`, `go-rtu`,
`go-clients`, or `cc-reference`). Both use lowercase letters, digits and hyphens
only. The identity describes the whole baseline/candidate role pairing, not an
individual program or an inferred image name. Wildcards are forbidden in
scenario IDs, role-set IDs, directions and fields; numeric field indexes are
literal. Old unscoped entries are rejected.

`record_range` is optional and inclusive. The `comparison`, zero-based session,
exact direction, and field are required. `approved_by` and `reason` must be
non-empty. Each entry is selected only when **both** the current scenario ID
and role-set ID match its scope. Only selected entries can suppress differences,
become stale (zero matches), overlap, or exceed `max_matches`. Counts reset for
each comparison: listing two scenarios requires a match in each, independently.
Entries naming other scenarios or role sets are inactive, not stale.

For a nonempty allow-list, pass `--role-set ID` to `run.py`; it forwards the
scenario's JSON `id` (not its filename) and that identity to every comparison.
`--allowlist FILE` selects a reviewed file without editing the shipped empty
list. Missing/invalid context is rejected before Docker resources are created.
For standalone `compare.py`, supply both `--scenario-id ID` and `--role-set ID`.
Context may be omitted only with an empty allow-list, so existing C/C checks
need no approvals or implicit role identity. Never reuse a role-set identity
for a different pairing to broaden an approval.

JSON reports record context, selected entries, inactive entry IDs and issues;
matches remain visible in both reports. Decode-validity and TCP-report-only
results cannot be allow-listed.

## Tests

Run from this directory:

```sh
python3 -m unittest -v
```

Tests read the committed captures and write mutations only to temporary
directories. They cover self-comparison, semantic/APDU mutations, CP56
normalization and flags, TCP segmentation/coalescing/retransmission, and
allow-list acceptance and failure rules.
Large captures use temporary disk-backed PDML and packet-at-a-time parsing,
not a whole-document XML tree. ASDUs bind to their preceding I-format APCI;
coalesced S/U frames do not consume an ASDU slot.

## Live differential runner

Docker is authorized only for this isolated test harness, not for starting
the range/platform stacks. All processes are headless. Build the sealed
reference from its pinned first-party revision, then compare the catalog:

```sh
python3 services/iec104/check/reference/build.py --out build/iec104-reference
python3 services/iec104/check/differential/run.py --catalog \
  --baseline build/iec104-reference/roles.json \
  --candidate build/iec104-reference/roles.json \
  --reference build/iec104-reference/baseline.json \
  --role-set cc-reference \
  --out build/iec104-differential
```

For a single case replace `--catalog` with its JSON path. Output directories
must be fresh: evidence is never overwritten. Both role files are strict
JSON objects with exactly `rtu`, `cc`, `cmd`, `peer` image names. Phase 3
can replace each program's image independently; the unchanged first-party
raw peer uses a Python 3.12 image. Role images must already exist locally.
The runner resolves and executes immutable image IDs before creating peers,
so retagging cannot change the implementation after provenance collection.
No host ports, production entrypoint/gateway setup, or containd images are
used. The explicit executable/flag contracts remain those of the programs.

Scenarios use schema 1: an ID, role kind, deadline, capture-Modbus switch,
versioned point-map profile, expected session count and ordered
`{op,args,timeout_s}` actions. Unknown/duplicate keys, nonfinite numbers,
unsupported actions and role-incompatible operations are rejected. The
status-only feeder profile deliberately has no analog cyclic points.
`analog-cyclic` uses a four-point profile with the Modbus fixture's constant
28.9 A / 20 kV registers and unchanged 2000 ms cyclic timers. Each received,
acknowledged two-object periodic ASDU is a cycle barrier; after exactly three
cycles the peer stops data transfer. It checks the complete ASDU bytes,
including COT, ordering, values and quality, without a sampling sleep.
Full 42-point analog/cyclic behavior is also exercised by `check/run.sh`.
STARTDT, GI ACT-TERM, select/execute, fresh Modbus reads, GPS requests and
scripted-server acknowledgments are barriers, never fixed startup sleeps.
The scripted server uses exact fixed CP56 IV timestamps; the real RTU uses
a controlled GPS fixture and the comparator's existing CP56 normalization.

The catalog isolates raw-client→RTU, CLI→RTU, CC→RTU, and raw-server→CC.
`ninth-connection` holds eight sockets behind individual STARTDT barriers,
dials nine, observes TCP EOF without sending APCI, then checks all eight
incumbents with TESTFR and shuts them down with STOPDT barriers.
The three `zero-*` cases use a raw client for RTU command/GI and a scripted
server with the **real CC**, not the CLI, for the empty monitor report. Normal
GI completes behind the CC's actual w=1 S barriers before injecting the monitor;
there is no assumption about CLI GI acknowledgments. A zero command requires an
S followed by the exact negative COT47 zero-count mirror; a zero GI requires S
then EOF with no ASDU; a zero monitor requires S then a TESTFR confirmation to
prove survival. Only the crafted input and command mirror are declared malformed.

No sleep determines this wire transcript; all nine TCP streams remain
visible to the unchanged comparator and its independent session-count check.
It includes session controls, SBO, negative type/COT/CA/IOA responses,
spontaneous position/trip values, source IV/NT recovery, fixed CP56 IV reports,
a full k=12 output window,
and real 15-bit sequence wrap
through 32770 reports. `w=1` makes the scripted-server ACK barriers explicit.
All time-tagged scenario types (30/31) already have comparator CP56 layouts.

Each side runs serially on the same unused subnet. Every creation carries
an unpredictable owner label; cleanup selects only that exact label, even
after failures. Capture shares the endpoint network namespace, listens on
`eth0`, uses a 2048-byte snaplen, immediate delivery and packet flushing, and
confirms readiness before connecting. The 256 MiB packet ring measures
`frame_size=2128 × frame_nr=126145` (268,436,560 bytes) with Alpine 3.22's
tcpdump/libpcap, enough for more than 65,600 packets. This snaplen fits a
complete MTU-1500 Ethernet frame while avoiding the 65 KiB TPACKET_V2 frame
size selected by full snaplen. Empty captures, kernel drops, truncated pcap
records (`incl_len < orig_len`), missing sessions and failed processes fail
the run.

Evidence includes pcap, tcpdump statistics, command/exit/deadline records,
image IDs, materialized baseline, scenario/point-map/harness hashes, fixture,
peer/CLI/CC logs, TShark version and comparator reports. Raw frame bytes stay
in the original pcap: do not duplicate transport-dependent frame hex in
semantic diffs. APDU differences and their normalization audits identify
the relevant protocol bytes.

`--candidate-scenario FILE` supplies a deliberately changed single-case
negative control; it is incompatible with `--catalog`. It is not an
allow-list or normalization. Exit codes remain 0 pass, 1 conformance
differences, 2 input/tool/process/deadline failure. Signals and failures
trigger owned-resource cleanup without an automatic retry. The caller
removes the separately built reference tag when finished:

```sh
docker image rm rd-iec104-ref:c1de19412b69
```

## Legacy Go-only behavioral gate

`services/iec104/check/run.sh` builds the canonical production
`services/iec104/Dockerfile`, with all three binaries and no build target.
Go model assertions run as ordinary Go tests, not a Docker model-check stage.
The C implementation is built only by the sealed reference builder.

The legacy gate gives every container (including short-lived commands) an
`rd*` name and unique owner label. Its bounded Docker wrapper refuses prefix
collisions before installing cleanup, creates internal networks with no host
ports, and cleans only that run's labels and two preflight-checked image
tags. Default prefixes are unique; `RD_CHECK_PREFIX` can set a fresh prefix.
The Python fixture image is inspected first and pulled through the bounded
wrapper (300 s) on a cache miss, including on fresh CI runners. It is a shared
dependency, not an owned build output: cleanup never removes its tag.
