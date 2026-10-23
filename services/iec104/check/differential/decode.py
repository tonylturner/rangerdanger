# SPDX-License-Identifier: Apache-2.0
"""Pinned-TShark capture decoding and ordered semantic record extraction."""

from __future__ import annotations

import re
import shutil
import subprocess
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any

from apdu import (
    APDU,
    Segment,
    cp56_datetime,
    cp56_offsets,
    cp56_parts,
    reassemble,
    round_offset,
    split_complete_apdus,
)

DEFAULT_TSHARK = "/opt/homebrew/bin/tshark"
SUBPROCESS_TIMEOUT = 60
REQUIRED_FIELDS = {
    "frame.number",
    "frame.time_epoch",
    "frame.protocols",
    "ip.src",
    "ip.dst",
    "tcp.srcport",
    "tcp.dstport",
    "tcp.stream",
    "tcp.len",
    "tcp.seq_raw",
    "tcp.payload",
    "tcp.flags.syn",
    "tcp.flags.ack",
    "tcp.flags.fin",
    "tcp.flags.reset",
    "iec60870_104.type",
    "iec60870_104.utype",
    "iec60870_104.apdulen",
    "iec60870_asdu.typeid",
    "iec60870_asdu.sq",
    "iec60870_asdu.numix",
    "iec60870_asdu.causetx",
    "iec60870_asdu.nega",
    "iec60870_asdu.test",
    "iec60870_asdu.oa",
    "iec60870_asdu.addr",
    "iec60870_asdu.ioa",
    "iec60870_asdu.cp56time",
    "iec60870_asdu.cp56time.ms",
    "iec60870_asdu.cp56time.min",
    "iec60870_asdu.cp56time.hour",
    "iec60870_asdu.cp56time.day",
    "iec60870_asdu.cp56time.dow",
    "iec60870_asdu.cp56time.month",
    "iec60870_asdu.cp56time.year",
    "iec60870_asdu.cp56time.iv",
    "iec60870_asdu.cp56time.su",
    "iec60870_asdu.cp24time",
    "iec60870_asdu.cp24time.ms",
    "iec60870_asdu.cp24time.min",
    "iec60870_asdu.cp24time.iv",
    "iec60870_asdu.qds",
    "iec60870_asdu.siq",
    "iec60870_asdu.diq",
    "iec60870_asdu.float",
    "iec60870_asdu.normval",
    "iec60870_asdu.scalval",
    "iec60870_asdu.sco",
    "iec60870_asdu.dco",
    "iec60870_asdu.rco",
    "iec60870_asdu.qos",
    "iec60870_asdu.qoi",
}

U_FUNCTIONS = {
    1: "STARTDT act",
    2: "STARTDT con",
    4: "STOPDT act",
    8: "STOPDT con",
    16: "TESTFR act",
    32: "TESTFR con",
}
TIME_FIELDS = {
    "iec60870_asdu.cp56time": "cp56",
    "iec60870_asdu.cp24time": "cp24",
}
VALUE_FIELDS = {
    "iec60870_asdu.float",
    "iec60870_asdu.normval",
    "iec60870_asdu.scalval",
    "iec60870_asdu.bitstring",
    "iec60870_asdu.bcr.count",
}
QUALITY_FIELDS = {
    "iec60870_asdu.siq",
    "iec60870_asdu.diq",
    "iec60870_asdu.qds",
}
COMMAND_FIELDS = {
    "iec60870_asdu.sco",
    "iec60870_asdu.dco",
    "iec60870_asdu.rco",
    "iec60870_asdu.qos",
}


class DecodeError(Exception):
    """A capture/tool error that prevents a meaningful comparison."""


@dataclass
class CaptureResult:
    path: Path
    tshark_version: str
    stream_to_session: dict[str, int]
    stream_first_frame: dict[str, int]
    records: dict[tuple[int, str], list[dict[str, Any]]]
    apdus: dict[tuple[int, str], list[APDU]]
    segments: dict[tuple[int, str], list[Segment]]
    tcp_report: dict[int, dict[str, dict[str, Any]]]
    validity_errors: list[str]
    normalization_audit: list[dict[str, object]]

    @property
    def session_count(self) -> int:
        return len(self.stream_to_session)


def _run(command: list[str], timeout: int) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            command, check=False, capture_output=True, text=True, timeout=timeout
        )
    except FileNotFoundError as error:
        raise DecodeError(f"tool not found: {command[0]}") from error
    except subprocess.TimeoutExpired as error:
        raise DecodeError(f"subprocess timed out after {timeout}s: {command[0]}") from error


def _tshark_version(tshark: str, timeout: int) -> str:
    result = _run([tshark, "-v"], timeout)
    if result.returncode:
        raise DecodeError(f"tshark -v exited {result.returncode}: {result.stderr.strip()}")
    match = re.search(r"TShark \(Wireshark\) (\d+\.\d+\.\d+)", result.stdout)
    if not match:
        raise DecodeError("could not parse tshark version output")
    version = match.group(1)
    if not version.startswith("4.6."):
        raise DecodeError(f"tshark 4.6.x is required, found {version}")
    return version


def _validate_fields(tshark: str, timeout: int) -> None:
    result = _run([tshark, "-G", "fields"], timeout)
    if result.returncode:
        raise DecodeError(
            f"tshark -G fields exited {result.returncode}: {result.stderr.strip()}"
        )
    available = {
        line.split("\t")[2]
        for line in result.stdout.splitlines()
        if line.startswith("F\t") and len(line.split("\t")) >= 3
    }
    missing = sorted(REQUIRED_FIELDS - available)
    if missing:
        raise DecodeError("tshark lacks required fields: " + ", ".join(missing))


def _field_values(node: ET.Element, name: str) -> list[ET.Element]:
    return [field for field in node.findall(".//field") if field.get("name") == name]


def _one(node: ET.Element, name: str) -> ET.Element | None:
    found = _field_values(node, name)
    return found[0] if found else None


def _number(field: ET.Element | None, default: int | None = None) -> int | None:
    if field is None:
        return default
    value = field.get("show") or field.get("value")
    if value is None:
        return default
    try:
        return int(value, 0)
    except ValueError:
        try:
            return int(value, 10)
        except ValueError:
            return default


def _bool(field: ET.Element | None) -> bool | None:
    if field is None:
        return None
    value = (field.get("show") or field.get("value") or "").lower()
    if value in {"true", "1"}:
        return True
    if value in {"false", "0"}:
        return False
    try:
        return bool(int(value, 0))
    except ValueError:
        return None


def _fields_for_asdu(proto: ET.Element) -> dict[str, list[ET.Element]]:
    values: dict[str, list[ET.Element]] = {}
    for field in proto.findall(".//field"):
        name = field.get("name")
        if name:
            values.setdefault(name, []).append(field)
    return values


def _raw_value(field: ET.Element) -> str | None:
    value = field.get("value")
    return value.lower() if value else None


def _time_tag(field: ET.Element, kind: str) -> dict[str, Any]:
    values = {
        child.get("name"): child
        for child in field.findall(".//field")
        if child.get("name")
    }
    raw = _raw_value(field)
    try:
        raw_bytes = bytes.fromhex(raw or "")
    except ValueError:
        raw_bytes = b""
    if kind == "cp56" and len(raw_bytes) == 7:
        parts = cp56_parts(raw_bytes)
        fields: dict[str, Any] = {
            "ms": parts["milliseconds"],
            "min": parts["minute"],
            "hour": parts["hour"],
            "day": parts["day"],
            "dow": parts["dow"],
            "month": parts["month"],
            "year": parts["year"],
            "iv": parts["iv"],
            "su": parts["su"],
            "reserved": {
                "minute": parts["minute_reserved"],
                "hour": parts["hour_reserved"],
                "month": parts["month_reserved"],
                "year": parts["year_reserved"],
            },
        }
        decoded_names = (
            "iec60870_asdu.cp56time.ms",
            "iec60870_asdu.cp56time.min",
            "iec60870_asdu.cp56time.hour",
            "iec60870_asdu.cp56time.day",
            "iec60870_asdu.cp56time.dow",
            "iec60870_asdu.cp56time.month",
            "iec60870_asdu.cp56time.year",
            "iec60870_asdu.cp56time.iv",
            "iec60870_asdu.cp56time.su",
        )
    else:
        fields = {}
        prefix = "iec60870_asdu.cp24time"
        for suffix in ("ms", "min", "iv"):
            value = _number(values.get(f"{prefix}.{suffix}"))
            if value is not None:
                fields[suffix] = value
        decoded_names = tuple(f"{prefix}.{suffix}" for suffix in ("ms", "min", "iv"))

    missing = [name for name in decoded_names if name not in values]
    if missing:
        raise DecodeError("PDML time tag is missing fields: " + ", ".join(missing))
    record: dict[str, Any] = {"kind": kind, "fields": fields, "raw": raw}
    if kind == "cp56":
        record["relative_ms"] = None
    return record


def _asdu_record(proto: ET.Element | None) -> dict[str, Any] | None:
    if proto is None:
        return None
    by_name = _fields_for_asdu(proto)
    get = lambda name: by_name.get(name, [])
    field_num = lambda name: [_number(field) for field in get(name)]
    type_id = _number(get("iec60870_asdu.typeid")[0]) if get(
        "iec60870_asdu.typeid"
    ) else None
    cot = get("iec60870_asdu.causetx")
    cause = _number(cot[0]) if cot else None
    cot_byte = cot[0].get("unmaskedvalue", "").lower() if cot else None
    values = []
    for field in proto.findall(".//field"):
        name = field.get("name")
        if name in VALUE_FIELDS:
            values.append(
                {
                    "field": name.removeprefix("iec60870_asdu."),
                    "value": field.get("show"),
                    "raw_bits": _raw_value(field),
                }
            )
    qualities = []
    for field in proto.findall(".//field"):
        name = field.get("name")
        if name in QUALITY_FIELDS:
            qualities.append(
                {
                    "field": name.removeprefix("iec60870_asdu."),
                    "bits": _raw_value(field),
                }
            )
    tags = []
    for field in proto.findall(".//field"):
        name = field.get("name")
        if name in TIME_FIELDS:
            tags.append(_time_tag(field, TIME_FIELDS[name]))
    commands = []
    for field in proto.findall(".//field"):
        name = field.get("name")
        if name not in COMMAND_FIELDS:
            continue
        prefix = name + "."
        subfields = {
            child.get("name", "").removeprefix(prefix): child.get("show")
            for child in field.findall(".//field")
            if child.get("name", "").startswith(prefix)
        }
        commands.append(
            {
                "field": name.removeprefix("iec60870_asdu."),
                "raw_bits": _raw_value(field),
                "s_e": subfields.get("se"),
                "qu": subfields.get("qu"),
                "ql": subfields.get("ql"),
            }
        )
    return {
        "type": type_id,
        "sq": _bool(get("iec60870_asdu.sq")[0])
        if get("iec60870_asdu.sq")
        else None,
        "count": _number(get("iec60870_asdu.numix")[0])
        if get("iec60870_asdu.numix")
        else None,
        "cot": {
            "cause": cause,
            "negative": _bool(get("iec60870_asdu.nega")[0])
            if get("iec60870_asdu.nega")
            else None,
            "test": _bool(get("iec60870_asdu.test")[0])
            if get("iec60870_asdu.test")
            else None,
            "raw": cot_byte,
        },
        "oa": _number(get("iec60870_asdu.oa")[0]) if get("iec60870_asdu.oa") else None,
        "ca": _number(get("iec60870_asdu.addr")[0])
        if get("iec60870_asdu.addr")
        else None,
        "ioa": field_num("iec60870_asdu.ioa"),
        "values": values,
        "quality_bits": qualities,
        "time_tag_present": bool(tags),
        "time_tags": tags,
        "command_qualifiers": commands,
        "qoi": field_num("iec60870_asdu.qoi"),
    }


def _packet_fields(packet: ET.Element) -> dict[str, list[ET.Element]]:
    result: dict[str, list[ET.Element]] = {}
    for field in packet.findall(".//field"):
        name = field.get("name")
        if name:
            result.setdefault(name, []).append(field)
    return result


def _first_number(fields: dict[str, list[ET.Element]], name: str) -> int | None:
    values = fields.get(name, [])
    return _number(values[0]) if values else None


def _first_decimal(fields: dict[str, list[ET.Element]], name: str) -> Decimal | None:
    values = fields.get(name, [])
    if not values:
        return None
    try:
        return Decimal(values[0].get("show", ""))
    except (InvalidOperation, TypeError):
        return None


def _first_bool(fields: dict[str, list[ET.Element]], name: str) -> bool:
    values = fields.get(name, [])
    return bool(_bool(values[0])) if values else False


def _frame_number(fields: dict[str, list[ET.Element]]) -> int:
    return _first_number(fields, "frame.number") or 0


def _direction(source: int, destination: int) -> str:
    if destination == 2404:
        return "client_to_server"
    if source == 2404:
        return "server_to_client"
    raise DecodeError(f"TCP direction has no port 2404: {source} -> {destination}")


def _decoded_frame_kind(proto: ET.Element) -> tuple[str, str | None]:
    # APCI fields are siblings of the ASDU dissector, so callers pass a single
    # protocol proto here rather than relying on a field-column projection.
    kind_field = _one(proto, "iec60870_104.type")
    kind_value = _number(kind_field)
    kind = {0: "I", 1: "S", 3: "U"}.get(kind_value, f"unknown-{kind_value}")
    if kind != "U":
        return kind, None
    u_field = _one(proto, "iec60870_104.utype")
    u_value = _number(u_field)
    return kind, U_FUNCTIONS.get(u_value, f"0x{u_value:02x}" if u_value is not None else None)


def _semantic_record(
    frame: int,
    session: int,
    direction: str,
    iec_proto: ET.Element,
    asdu_proto: ET.Element | None,
) -> dict[str, Any]:
    kind, u_function = _decoded_frame_kind(iec_proto)
    return {
        "frame": frame,
        "session": session,
        "direction": direction,
        "frame_kind": kind,
        "u_function": u_function,
        "asdu": _asdu_record(asdu_proto),
    }


def _parse_cp56_ms(record: dict[str, Any]) -> datetime | None:
    raw = record.get("raw")
    if not isinstance(raw, str):
        return None
    try:
        return cp56_datetime(bytes.fromhex(raw))
    except ValueError:
        return None


def _add_relative_times(
    records: dict[tuple[int, str], list[dict[str, Any]]], tolerance_ms: int
) -> None:
    sessions: dict[int, list[tuple[dict[str, Any], dict[str, Any]]]] = {}
    for events in records.values():
        for event in events:
            asdu = event.get("asdu")
            if not asdu:
                continue
            for tag in asdu["time_tags"]:
                if tag["kind"] == "cp56":
                    sessions.setdefault(event["session"], []).append((event, tag))
    for tags in sessions.values():
        tags.sort(key=lambda pair: pair[0]["frame"])
        first_time = next(
            (instant for _, tag in tags if (instant := _parse_cp56_ms(tag)) is not None),
            None,
        )
        for _, tag in tags:
            instant = _parse_cp56_ms(tag)
            if instant is not None and first_time is not None:
                delta = int((instant - first_time).total_seconds() * 1000)
                tag["relative_ms"] = round_offset(delta, tolerance_ms)


def _expert_errors(packet: ET.Element, frame: int, is_iec_frame: bool) -> list[str]:
    errors = []
    for field in packet.findall(".//field"):
        name = field.get("name", "")
        if name == "_ws.malformed" or name.startswith("_ws.malformed."):
            errors.append(f"frame {frame}: {name}")
        elif is_iec_frame and name == "_ws.expert.severity":
            severity = (field.get("show") or field.get("showname") or "").lower()
            if "error" in severity:
                message = _one(packet, "_ws.expert.message")
                detail = message.get("show") if message is not None else severity
                errors.append(f"frame {frame}: expert error: {detail}")
        elif is_iec_frame and name.startswith("iec104."):
            errors.append(f"frame {frame}: {name}")
    for proto in packet.findall("./proto"):
        name = proto.get("name", "")
        if is_iec_frame and name.startswith("iec104."):
            errors.append(f"frame {frame}: {name}")
        if is_iec_frame and name == "iec60870_asdu":
            for field in proto.findall(".//field"):
                field_name = field.get("name", "")
                if field_name in {
                    "iec60870_asdu.segment.overlap.conflict",
                    "iec60870_asdu.segment.multipletails",
                    "iec60870_asdu.segment.toolongsegment",
                    "iec60870_asdu.segment.error",
                }:
                    errors.append(f"frame {frame}: {field_name}")
    return errors


def decode_capture(
    capture: Path,
    tshark: str = DEFAULT_TSHARK,
    timeout: int = SUBPROCESS_TIMEOUT,
    cp56_tolerance_ms: int = 1000,
) -> CaptureResult:
    """Decode one raw capture, then independently reassemble its TCP/APDU bytes."""
    if not capture.is_file():
        raise DecodeError(f"capture is not a file: {capture}")
    tshark_path = shutil.which(tshark) if "/" not in tshark else tshark
    if not tshark_path or not Path(tshark_path).is_file():
        raise DecodeError(f"tshark executable not found: {tshark}")
    version = _tshark_version(tshark_path, timeout)
    _validate_fields(tshark_path, timeout)
    result = _run(
        [
            tshark_path,
            "-n",
            "-o",
            "tcp.desegment_tcp_streams:TRUE",
            "-r",
            str(capture),
            "-T",
            "pdml",
        ],
        timeout,
    )
    if result.returncode:
        raise DecodeError(
            f"tshark decode exited {result.returncode} for {capture}: "
            f"{result.stderr.strip()}"
        )
    try:
        root = ET.fromstring(result.stdout)
    except ET.ParseError as error:
        raise DecodeError(f"could not parse tshark PDML for {capture}: {error}") from error

    stream_first_frame: dict[str, int] = {}
    packet_data: list[tuple[ET.Element, dict[str, list[ET.Element]]]] = []
    validity_errors: list[str] = []
    for packet in root.findall("packet"):
        fields = _packet_fields(packet)
        frame = _frame_number(fields)
        packet_data.append((packet, fields))
        source = _first_number(fields, "tcp.srcport")
        destination = _first_number(fields, "tcp.dstport")
        is_iec_frame = (
            any(proto.get("name") == "iec60870_104" for proto in packet.findall("./proto"))
            or (
                (source == 2404 or destination == 2404)
                and (_first_number(fields, "tcp.len") or 0) > 0
            )
        )
        validity_errors.extend(_expert_errors(packet, frame, is_iec_frame))
        stream = _first_number(fields, "tcp.stream")
        if (
            source is not None
            and destination is not None
            and (source == 2404 or destination == 2404)
            and stream is not None
        ):
            key = str(stream)
            stream_first_frame[key] = min(
                frame, stream_first_frame.get(key, frame)
            )

    stream_to_session = {
        stream: index
        for index, (stream, _) in enumerate(
            sorted(stream_first_frame.items(), key=lambda item: item[1])
        )
    }
    records: dict[tuple[int, str], list[dict[str, Any]]] = {}
    segments: dict[tuple[int, str], list[Segment]] = {}
    tcp_report: dict[int, dict[str, dict[str, Any]]] = {}
    ack_timestamps: dict[tuple[int, str], list[Decimal]] = {}
    decoded_counts: dict[tuple[int, str], int] = {}
    for packet, fields in packet_data:
        frame = _frame_number(fields)
        source = _first_number(fields, "tcp.srcport")
        destination = _first_number(fields, "tcp.dstport")
        stream_value = _first_number(fields, "tcp.stream")
        if (
            source is None
            or destination is None
            or stream_value is None
            or source != 2404 and destination != 2404
        ):
            continue
        stream = str(stream_value)
        session = stream_to_session[stream]
        direction = _direction(source, destination)
        key = (session, direction)
        metric = tcp_report.setdefault(session, {}).setdefault(
            direction,
            {
                "segments": 0,
                "data_segments": 0,
                "captured_payload_bytes": 0,
                "unique_payload_bytes": 0,
                "duplicate_payload_bytes": 0,
                "ack_segments": 0,
                "ack_only_segments": 0,
                "ack_cadence_ms": [],
                "syn_segments": 0,
                "fin_segments": 0,
                "rst_segments": 0,
                "coalesced_segments": 0,
                "apdu_count": 0,
                "ack_per_data_segment": 0.0,
            },
        )
        metric["segments"] += 1
        payload_field = fields.get("tcp.payload", [])
        try:
            payload = bytes.fromhex(payload_field[0].get("value", "")) if payload_field else b""
        except ValueError:
            validity_errors.append(f"frame {frame}: malformed tcp.payload field")
            payload = b""
        reported_tcp_length = _first_number(fields, "tcp.len") or 0
        if len(payload) != reported_tcp_length:
            validity_errors.append(
                f"frame {frame}: TCP payload is truncated "
                f"({len(payload)} captured of {reported_tcp_length} bytes)"
            )
        flags = (
            ("syn", "tcp.flags.syn", "syn_segments"),
            ("fin", "tcp.flags.fin", "fin_segments"),
            ("rst", "tcp.flags.reset", "rst_segments"),
        )
        flag_values = {}
        for name, field_name, metric_name in flags:
            value = _first_bool(fields, field_name)
            flag_values[name] = value
            if value:
                metric[metric_name] += 1
        ack = _first_bool(fields, "tcp.flags.ack")
        if ack:
            metric["ack_segments"] += 1
            timestamp = _first_decimal(fields, "frame.time_epoch")
            if timestamp is not None:
                ack_timestamps.setdefault(key, []).append(timestamp)
        if ack and not payload and not any(flag_values.values()):
            metric["ack_only_segments"] += 1
        if payload:
            metric["data_segments"] += 1
            metric["captured_payload_bytes"] += len(payload)
            if len(split_complete_apdus(payload)) > 1:
                metric["coalesced_segments"] += 1
        sequence = _first_number(fields, "tcp.seq_raw")
        if sequence is None:
            validity_errors.append(f"frame {frame}: TCP payload has no raw sequence")
            continue
        segments.setdefault(key, []).append(
            Segment(
                sequence,
                payload,
                frame,
                syn=flag_values["syn"],
                fin=flag_values["fin"],
                rst=flag_values["rst"],
                ack=ack,
            )
        )

        iec_protos = packet.findall("./proto[@name='iec60870_104']")
        asdu_protos = packet.findall("./proto[@name='iec60870_asdu']")
        if len(asdu_protos) > len(iec_protos):
            validity_errors.append(
                f"frame {frame}: tshark produced ASDU without matching IEC APCI"
            )
        decoded_counts[key] = decoded_counts.get(key, 0) + len(iec_protos)
        for index, proto in enumerate(iec_protos):
            asdu_proto = asdu_protos[index] if index < len(asdu_protos) else None
            records.setdefault(key, []).append(
                _semantic_record(frame, session, direction, proto, asdu_proto)
            )
    apdus: dict[tuple[int, str], list[APDU]] = {}
    audit: list[dict[str, object]] = []
    for key, stream_segments in segments.items():
        rebuilt = reassemble(stream_segments)
        apdus[key] = rebuilt.apdus
        metric = tcp_report[key[0]][key[1]]
        metric["unique_payload_bytes"] = rebuilt.covered_bytes
        metric["duplicate_payload_bytes"] = rebuilt.duplicate_bytes
        metric["apdu_count"] = len(rebuilt.apdus)
        data_segments = int(metric["data_segments"])
        metric["ack_per_data_segment"] = (
            int(metric["ack_only_segments"]) / data_segments if data_segments else 0.0
        )
        times = ack_timestamps.get(key, [])
        metric["ack_cadence_ms"] = [
            float((right - left) * 1000) for left, right in zip(times, times[1:])
        ]
        for issue in rebuilt.issues:
            validity_errors.append(f"session {key[0]} {key[1]}: {issue}")
        parsed_bytes = sum(len(apdu.data) for apdu in rebuilt.apdus)
        if parsed_bytes != rebuilt.covered_bytes:
            validity_errors.append(
                f"session {key[0]} {key[1]}: TCP payload coverage mismatch "
                f"({parsed_bytes} APDU bytes, {rebuilt.covered_bytes} unique bytes)"
            )
        decoded = decoded_counts.get(key, 0)
        if decoded != len(rebuilt.apdus):
            validity_errors.append(
                f"session {key[0]} {key[1]}: tshark decoded {decoded} IEC APDUs, "
                f"APCI parser found {len(rebuilt.apdus)}"
            )
        for event, apdu in zip(records.get(key, []), rebuilt.apdus):
            offsets, issue = cp56_offsets(apdu)
            if issue:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: {issue}"
                )
            tags = [
                tag
                for tag in (event.get("asdu") or {}).get("time_tags", [])
                if tag["kind"] == "cp56"
            ]
            if apdu.kind != event["frame_kind"]:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    "TShark/APCI frame-kind disagreement"
                )
            if apdu.kind == "U" and apdu.u_function != event["u_function"]:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    "TShark/APCI U-function disagreement"
                )
            if apdu.kind == "I" and len(apdu.data) < 12:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    "I-frame has no complete ASDU header"
                )
            elif apdu.kind == "I" and event.get("asdu") is None:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    "TShark did not decode the I-frame ASDU"
                )
            elif apdu.kind == "I" and event["asdu"]["type"] != apdu.data[6]:
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    "TShark/APCI ASDU-type disagreement"
                )
            if len(tags) != len(offsets):
                if offsets or tags:
                    validity_errors.append(
                        f"session {key[0]} {key[1]} APDU {apdu.index}: "
                        f"TShark decoded {len(tags)} CP56 values, "
                        f"APCI parser found {len(offsets)}"
                    )
            for tag, offset in zip(tags, offsets):
                if tag["raw"] != apdu.data[offset:offset + 7].hex():
                    validity_errors.append(
                        f"session {key[0]} {key[1]} APDU {apdu.index}: "
                        "TShark/APCI CP56-byte disagreement"
                    )
            if tags and not offsets:
                type_id = apdu.data[6] if apdu.kind == "I" and len(apdu.data) > 6 else None
                validity_errors.append(
                    f"session {key[0]} {key[1]} APDU {apdu.index}: "
                    f"unmapped CP56 layout for type {type_id}"
                )
    _add_relative_times(records, cp56_tolerance_ms)
    return CaptureResult(
        capture,
        version,
        stream_to_session,
        stream_first_frame,
        records,
        apdus,
        segments,
        tcp_report,
        validity_errors,
        audit,
    )
