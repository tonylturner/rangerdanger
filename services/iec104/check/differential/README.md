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

`record_range` is optional and inclusive. Wildcards are rejected. The
`comparison`, zero-based session, exact direction, and field are required.
`approved_by` and `reason` must be non-empty. A stale entry or one that matches
more than `max_matches` fails the run. Allow-list matches are visible in both
reports; decode-validity and TCP-report-only results cannot be allow-listed.

## Tests

Run from this directory:

```sh
python3 -m unittest -v
```

Tests read the committed captures and write mutations only to temporary
directories. They cover self-comparison, semantic/APDU mutations, CP56
normalization and flags, TCP segmentation/coalescing/retransmission, and
allow-list acceptance and failure rules.
