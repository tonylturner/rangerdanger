# SPDX-License-Identifier: Apache-2.0
"""TCP stream reassembly and IEC 104 APCI/APDU handling."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta

MOD32 = 1 << 32
HALF32 = 1 << 31
MOD15 = 1 << 15
EPOCH = datetime(2000, 1, 1)


@dataclass(frozen=True)
class Segment:
    """One TCP payload segment, with its raw sequence number and capture frame."""

    seq: int
    payload: bytes
    frame: int
    syn: bool = False
    fin: bool = False
    rst: bool = False
    ack: bool = False


@dataclass
class APDU:
    """One APDU in a reassembled direction-specific TCP byte stream."""

    index: int
    frame: int
    stream_offset: int
    data: bytes
    kind: str
    u_function: str | None
    ns: int | None
    nr: int | None


@dataclass
class Reassembly:
    apdus: list[APDU]
    issues: list[str]
    duplicate_bytes: int
    payload_bytes: int
    covered_bytes: int


U_FUNCTIONS = {
    0x07: "STARTDT act",
    0x0B: "STARTDT con",
    0x13: "STOPDT act",
    0x23: "STOPDT con",
    0x43: "TESTFR act",
    0x83: "TESTFR con",
}

# CP56 starts after the indicated number of per-object data bytes. The IOA
# (three bytes) is not included. This table covers standard monitoring and
# command types used by this harness; unknown CP56 layouts fail closed.
CP56_LAYOUTS = {
    30: (8, 1),
    31: (8, 1),
    32: (9, 2),
    33: (12, 5),
    34: (10, 3),
    35: (10, 3),
    36: (12, 5),
    37: (12, 5),
    58: (8, 1),
    59: (8, 1),
    60: (8, 1),
    61: (10, 3),
    62: (10, 3),
    63: (12, 5),
    64: (12, 5),
    # C_CS_NA_1: IOA followed by CP56Time2a only, no value/qualifier.
    103: (7, 0),
}


def _unwrap(sequence: int, origin: int) -> int:
    """Return a signed sequence offset from origin, accounting for wrap."""
    return ((sequence - origin + HALF32) % MOD32) - HALF32


def _split_runs(byte_map: dict[int, tuple[int, int]]) -> list[tuple[int, bytes, list[int]]]:
    """Return contiguous runs as (stream offset, bytes, originating frames)."""
    if not byte_map:
        return []
    offsets = sorted(byte_map)
    runs: list[tuple[int, bytes, list[int]]] = []
    start = previous = offsets[0]
    value, frame = byte_map[start]
    data = bytearray((value,))
    frames = [frame]
    for offset in offsets[1:]:
        if offset != previous + 1:
            runs.append((start, bytes(data), frames))
            start = offset
            data = bytearray()
            frames = []
        value, frame = byte_map[offset]
        data.append(value)
        frames.append(frame)
        previous = offset
    runs.append((start, bytes(data), frames))
    return runs


def _parse_apdu(data: bytes, index: int, frame: int, offset: int) -> APDU:
    if len(data) < 6 or data[0] != 0x68:
        raise ValueError("invalid APDU start or truncated APCI")
    apdu_len = data[1]
    if apdu_len < 4 or apdu_len > 253 or len(data) != apdu_len + 2:
        raise ValueError(f"invalid APDU length {apdu_len}")
    first = data[2]
    if first & 0x01 == 0:
        return APDU(
            index,
            frame,
            offset,
            data,
            "I",
            None,
            int.from_bytes(data[2:4], "little") >> 1,
            int.from_bytes(data[4:6], "little") >> 1,
        )
    if first & 0x03 == 0x01:
        return APDU(
            index, frame, offset, data, "S", None, None,
            int.from_bytes(data[4:6], "little") >> 1,
        )
    if first & 0x03 == 0x03:
        return APDU(
            index,
            frame,
            offset,
            data,
            "U",
            U_FUNCTIONS.get(first, f"0x{first:02x}"),
            None,
            None,
        )
    raise ValueError(f"invalid APCI control byte 0x{first:02x}")


def reassemble(segments: list[Segment]) -> Reassembly:
    """Reassemble TCP bytes, ignoring identical retransmissions but not gaps."""
    if not segments:
        return Reassembly([], [], 0, 0, 0)
    payload_segments = [s for s in segments if s.payload]
    payload_bytes = sum(len(s.payload) for s in payload_segments)
    if not payload_segments:
        return Reassembly([], [], 0, 0, 0)

    origin_sequence = (payload_segments[0].seq + int(payload_segments[0].syn)) % MOD32
    byte_map: dict[int, tuple[int, int]] = {}
    issues: list[str] = []
    duplicate_bytes = 0
    for segment in payload_segments:
        seq = (segment.seq + int(segment.syn)) % MOD32
        start = _unwrap(seq, origin_sequence)
        for index, value in enumerate(segment.payload):
            offset = start + index
            previous = byte_map.get(offset)
            if previous is None:
                byte_map[offset] = (value, segment.frame)
            elif previous[0] == value:
                duplicate_bytes += 1
            else:
                issues.append(
                    f"conflicting retransmission at TCP stream offset {offset}"
                )

    runs = _split_runs(byte_map)
    if len(runs) > 1:
        for left, right in zip(runs, runs[1:]):
            issues.append(
                f"TCP sequence gap between stream offsets "
                f"{left[0] + len(left[1])} and {right[0]}"
            )

    apdus: list[APDU] = []
    for run_offset, run, frames in runs:
        cursor = 0
        while cursor < len(run):
            absolute = run_offset + cursor
            if run[cursor] != 0x68:
                issues.append(
                    f"uncovered TCP payload byte 0x{run[cursor]:02x} "
                    f"at stream offset {absolute}"
                )
                cursor += 1
                continue
            if cursor + 2 > len(run):
                issues.append(f"incomplete APDU length at stream offset {absolute}")
                break
            total = run[cursor + 1] + 2
            if total < 6 or total > 255:
                issues.append(
                    f"invalid APDU length {run[cursor + 1]} at stream offset {absolute}"
                )
                cursor += 1
                continue
            if cursor + total > len(run):
                issues.append(
                    f"incomplete APDU at stream offset {absolute}: "
                    f"needs {total} bytes, has {len(run) - cursor}"
                )
                break
            frame = frames[cursor]
            try:
                apdus.append(
                    _parse_apdu(run[cursor:cursor + total], len(apdus), frame, absolute)
                )
            except ValueError as error:
                issues.append(f"{error} at stream offset {absolute}")
            cursor += total
    return Reassembly(
        apdus, issues, duplicate_bytes, payload_bytes, len(byte_map)
    )


def cp56_offsets(apdu: APDU) -> tuple[list[int], str | None]:
    """Find CP56 byte offsets for known ASDU layouts; return a fail-closed note."""
    if apdu.kind != "I":
        return [], None
    if len(apdu.data) < 12:
        return [], "I-format APDU has no complete ASDU header"
    type_id = apdu.data[6]
    vsq = apdu.data[7]
    count = vsq & 0x7F
    sq = bool(vsq & 0x80)
    if type_id not in CP56_LAYOUTS:
        return [], None
    object_data_size, cp56_in_data = CP56_LAYOUTS[type_id]
    cursor = 12
    if sq and count:
        cursor += 3
    offsets: list[int] = []
    for _ in range(count):
        if not sq:
            cursor += 3
        cp56_offset = cursor + cp56_in_data
        if cp56_offset + 7 > len(apdu.data):
            return [], f"truncated CP56 object for ASDU type {type_id}"
        offsets.append(cp56_offset)
        cursor += object_data_size
    return offsets, None


def cp56_parts(data: bytes) -> dict[str, int | bool]:
    """Decode the CP56 clock fields while preserving reserved and flag bits."""
    if len(data) != 7:
        raise ValueError("CP56Time2a must be seven bytes")
    minute_byte, hour_byte, day_byte, month_byte, year_byte = data[2:]
    return {
        "milliseconds": int.from_bytes(data[:2], "little"),
        "minute": minute_byte & 0x3F,
        "minute_reserved": minute_byte & 0x40,
        "iv": bool(minute_byte & 0x80),
        "hour": hour_byte & 0x1F,
        "hour_reserved": hour_byte & 0x60,
        "su": bool(hour_byte & 0x80),
        "day": day_byte & 0x1F,
        "dow": (day_byte >> 5) & 0x07,
        "month": month_byte & 0x0F,
        "month_reserved": month_byte & 0xF0,
        "year": year_byte & 0x7F,
        "year_reserved": year_byte & 0x80,
    }


def cp56_datetime(data: bytes) -> datetime | None:
    """Return a valid CP56 wall time, or None when invalid/unusable."""
    parts = cp56_parts(data)
    if parts["iv"] or parts["milliseconds"] >= 60_000:
        return None
    try:
        base = datetime(
            2000 + int(parts["year"]),
            int(parts["month"]),
            int(parts["day"]),
            int(parts["hour"]),
            int(parts["minute"]),
        )
    except ValueError:
        return None
    return base + timedelta(milliseconds=int(parts["milliseconds"]))


def round_offset(value: int, tolerance_ms: int) -> int:
    """Round a signed millisecond offset to the nearest tolerance."""
    if value < 0:
        return -((-value + tolerance_ms // 2) // tolerance_ms * tolerance_ms)
    return (value + tolerance_ms // 2) // tolerance_ms * tolerance_ms


def normalized_cp56(
    data: bytes, first_time: datetime | None, tolerance_ms: int
) -> tuple[bytes, int | None]:
    """Encode the rounded relative time, retaining all CP56 status/reserved bits."""
    instant = cp56_datetime(data)
    if instant is None or first_time is None:
        return data, None
    delta = int((instant - first_time).total_seconds() * 1000)
    rounded = round_offset(delta, tolerance_ms)
    canonical = EPOCH + timedelta(milliseconds=rounded)
    parts = cp56_parts(data)
    milliseconds = canonical.second * 1000 + canonical.microsecond // 1000
    minute = canonical.minute | int(parts["minute_reserved"]) | (
        0x80 if parts["iv"] else 0
    )
    hour = canonical.hour | int(parts["hour_reserved"]) | (0x80 if parts["su"] else 0)
    day = canonical.day | (int(parts["dow"]) << 5)
    month = canonical.month | int(parts["month_reserved"])
    year = (canonical.year - 2000) | int(parts["year_reserved"])
    result = bytearray(data)
    result[0:2] = milliseconds.to_bytes(2, "little")
    result[2:7] = bytes((minute, hour, day, month, year))
    return bytes(result), rounded


def session_first_cp56(apdus: list[APDU]) -> datetime | None:
    """Find the first valid CP56 in frame/APDU order for a whole session."""
    for apdu in sorted(apdus, key=lambda item: (item.frame, item.index)):
        offsets, _ = cp56_offsets(apdu)
        for offset in offsets:
            value = cp56_datetime(apdu.data[offset:offset + 7])
            if value is not None:
                return value
    return None


def normalize_apdus(
    apdus: list[APDU],
    first_time: datetime | None,
    tolerance_ms: int,
    session: int,
    direction: str,
) -> tuple[list[bytes], list[dict[str, object]], list[str]]:
    """Rebase APCI sequence numbers and CP56 time while preserving all else."""
    first_ns: int | None = None
    first_nr: int | None = None
    normalized: list[bytes] = []
    audit: list[dict[str, object]] = []
    issues: list[str] = []

    def record_bytes(
        apdu: APDU, old: bytes, new: bytes, offset: int, normalization: str
    ) -> None:
        for local, (old_byte, new_byte) in enumerate(zip(old, new)):
            audit.append(
                {
                    "session": session,
                    "direction": direction,
                    "apdu": apdu.index,
                    "frame": apdu.frame,
                    "offset": offset + local,
                    "stream_offset": apdu.stream_offset + offset + local,
                    "old": f"{old_byte:02x}",
                    "new": f"{new_byte:02x}",
                    "normalization": normalization,
                }
            )

    for apdu in apdus:
        data = bytearray(apdu.data)
        if apdu.ns is not None:
            if first_ns is None:
                first_ns = apdu.ns
            ns = (apdu.ns - first_ns) % MOD15
            old = bytes(data[2:4])
            data[2:4] = (ns << 1).to_bytes(2, "little")
            record_bytes(apdu, old, bytes(data[2:4]), 2, "apci-sequence-rebase")
        if apdu.nr is not None:
            if first_nr is None:
                first_nr = apdu.nr
            nr = (apdu.nr - first_nr) % MOD15
            offset = 4
            old = bytes(data[offset:offset + 2])
            data[offset:offset + 2] = (nr << 1).to_bytes(2, "little")
            record_bytes(
                apdu, old, bytes(data[offset:offset + 2]), offset, "apci-sequence-rebase"
            )

        offsets, issue = cp56_offsets(apdu)
        if issue:
            issues.append(f"session {session} {direction} APDU {apdu.index}: {issue}")
        for offset in offsets:
            raw = bytes(data[offset:offset + 7])
            if len(raw) != 7:
                issues.append(
                    f"session {session} {direction} APDU {apdu.index}: "
                    "truncated CP56 value"
                )
                continue
            changed, relative = normalized_cp56(raw, first_time, tolerance_ms)
            if relative is None:
                continue
            record_bytes(apdu, raw, changed, offset, "cp56-relative")
            data[offset:offset + 7] = changed
        normalized.append(bytes(data))
    return normalized, audit, issues


def split_complete_apdus(data: bytes) -> list[bytes]:
    """Split complete APDUs from one segment payload (for TCP coalescing stats)."""
    result: list[bytes] = []
    cursor = 0
    while cursor + 2 <= len(data) and data[cursor] == 0x68:
        total = data[cursor + 1] + 2
        if total < 6 or cursor + total > len(data):
            break
        result.append(data[cursor:cursor + total])
        cursor += total
    return result
