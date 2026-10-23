# SPDX-License-Identifier: Apache-2.0
"""CLI and report generation for the IEC 104 differential capture comparator."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Any

from apdu import normalize_apdus, session_first_cp56
from decode import DEFAULT_TSHARK, DecodeError, decode_capture

DIRECTIONS = ("client_to_server", "server_to_client")
ALLOWLIST_SCOPE_KEYS = {"comparison", "session", "direction", "record_range"}
ALLOWLIST_KEYS = {
    "id",
    "scope",
    "field",
    "baseline",
    "candidate",
    "delta",
    "reason",
    "approved_by",
    "max_matches",
}


class InputError(Exception):
    """A bad CLI input, session map, or allow-list."""


def _read_json(path: Path, description: str) -> Any:
    try:
        with path.open(encoding="utf-8") as source:
            return json.load(source)
    except OSError as error:
        raise InputError(f"could not read {description} {path}: {error}") from error
    except json.JSONDecodeError as error:
        raise InputError(f"invalid JSON in {description} {path}: {error}") from error


def load_allowlist(path: Path) -> list[dict[str, Any]]:
    data = _read_json(path, "allow-list")
    if not isinstance(data, list):
        raise InputError("allow-list root must be a JSON array")
    entries = []
    ids: set[str] = set()
    for index, entry in enumerate(data):
        where = f"allow-list entry {index}"
        if not isinstance(entry, dict) or set(entry) - ALLOWLIST_KEYS:
            raise InputError(f"{where} must be an object with only documented keys")
        required = {
            "id",
            "scope",
            "field",
            "reason",
            "approved_by",
            "max_matches",
        }
        if not required.issubset(entry):
            raise InputError(f"{where} is missing required keys")
        if ("baseline" in entry or "candidate" in entry) == ("delta" in entry):
            raise InputError(
                f"{where} must contain baseline+candidate or delta, but not both"
            )
        if ("baseline" in entry) != ("candidate" in entry):
            raise InputError(f"{where} must provide both baseline and candidate")
        identifier = entry["id"]
        if not isinstance(identifier, str) or not identifier.strip() or identifier in ids:
            raise InputError(f"{where} has an empty or duplicate id")
        ids.add(identifier)
        scope = entry["scope"]
        if not isinstance(scope, dict) or set(scope) - ALLOWLIST_SCOPE_KEYS:
            raise InputError(f"{where}.scope has unknown keys")
        if not {"comparison", "session", "direction"}.issubset(scope):
            raise InputError(f"{where}.scope needs comparison, session, and direction")
        if scope["comparison"] not in {"A", "B"}:
            raise InputError(f"{where}.scope.comparison must be A or B")
        if (
            not isinstance(scope["session"], int)
            or isinstance(scope["session"], bool)
            or scope["session"] < 0
        ):
            raise InputError(f"{where}.scope.session must be a non-negative integer")
        if scope["direction"] not in {*DIRECTIONS, "session"}:
            raise InputError(f"{where}.scope.direction is not an exact direction")
        if "record_range" in scope:
            record_range = scope["record_range"]
            if (
                not isinstance(record_range, list)
                or len(record_range) != 2
                or any(
                    not isinstance(number, int)
                    or isinstance(number, bool)
                    or number < 0
                    for number in record_range
                )
                or record_range[0] > record_range[1]
            ):
                raise InputError(
                    f"{where}.scope.record_range must be [inclusive_start, "
                    "inclusive_end]"
                )
        field = entry["field"]
        if (
            not isinstance(field, str)
            or not field
            or any(marker in field for marker in ("*", "?", "[", "]"))
        ):
            raise InputError(f"{where}.field must be a specific, non-wildcard field")
        for name in ("reason", "approved_by"):
            if not isinstance(entry[name], str) or not entry[name].strip():
                raise InputError(f"{where}.{name} must be non-empty")
        maximum = entry["max_matches"]
        if not isinstance(maximum, int) or isinstance(maximum, bool) or maximum < 1:
            raise InputError(f"{where}.max_matches must be a positive integer")
        if "delta" in entry and (
            not isinstance(entry["delta"], (float, int))
            or isinstance(entry["delta"], bool)
        ):
            raise InputError(f"{where}.delta must be numeric")
        entries.append(entry)
    return entries


def load_session_map(path: Path | None) -> dict[int, int] | None:
    if path is None:
        return None
    data = _read_json(path, "session map")
    if not isinstance(data, dict):
        raise InputError("session map root must map baseline session indexes to candidate indexes")
    result: dict[int, int] = {}
    used_candidates: set[int] = set()
    for baseline, candidate in data.items():
        if not re.fullmatch(r"0|[1-9]\d*", baseline):
            raise InputError(f"invalid baseline session index {baseline!r}")
        if (
            not isinstance(candidate, int)
            or isinstance(candidate, bool)
            or candidate < 0
        ):
            raise InputError(f"invalid candidate session index for {baseline}")
        base_index = int(baseline)
        if candidate in used_candidates:
            raise InputError("session map must not pair a candidate session more than once")
        used_candidates.add(candidate)
        result[base_index] = candidate
    return result


def _message_view(record: dict[str, Any]) -> dict[str, Any]:
    asdu = record.get("asdu")
    if asdu is None:
        asdu_view = None
    else:
        asdu_view = dict(asdu)
        normalized_tags = []
        for tag in asdu["time_tags"]:
            if tag["kind"] == "cp56":
                fields = tag["fields"]
                normalized_tags.append(
                    {
                        "kind": "cp56",
                        "relative_ms": tag.get("relative_ms"),
                        "iv": fields["iv"],
                        "su": fields["su"],
                        "dow": fields["dow"],
                        "reserved": fields["reserved"],
                    }
                )
            else:
                normalized_tags.append(tag)
        asdu_view["time_tags"] = normalized_tags
    return {
        "frame_kind": record["frame_kind"],
        "u_function": record["u_function"],
        "asdu": asdu_view,
    }


def _diff_values(
    comparison: str,
    session: int,
    direction: str,
    index: int,
    path: str,
    baseline: Any,
    candidate: Any,
) -> list[dict[str, Any]]:
    if type(baseline) is not type(candidate):
        return [
            {
                "comparison": comparison,
                "session": session,
                "direction": direction,
                "index": index,
                "field": path or "record",
                "baseline": baseline,
                "candidate": candidate,
            }
        ]
    if isinstance(baseline, dict):
        result = []
        for key in sorted(set(baseline) | set(candidate)):
            child_path = f"{path}.{key}" if path else key
            if key not in baseline or key not in candidate:
                result.append(
                    {
                        "comparison": comparison,
                        "session": session,
                        "direction": direction,
                        "index": index,
                        "field": child_path,
                        "baseline": baseline.get(key),
                        "candidate": candidate.get(key),
                    }
                )
            else:
                result.extend(
                    _diff_values(
                        comparison,
                        session,
                        direction,
                        index,
                        child_path,
                        baseline[key],
                        candidate[key],
                    )
                )
        return result
    if isinstance(baseline, list):
        result = []
        for item_index in range(max(len(baseline), len(candidate))):
            child_path = f"{path}[{item_index}]"
            if item_index >= len(baseline) or item_index >= len(candidate):
                result.append(
                    {
                        "comparison": comparison,
                        "session": session,
                        "direction": direction,
                        "index": index,
                        "field": child_path,
                        "baseline": baseline[item_index]
                        if item_index < len(baseline)
                        else None,
                        "candidate": candidate[item_index]
                        if item_index < len(candidate)
                        else None,
                    }
                )
            else:
                result.extend(
                    _diff_values(
                        comparison,
                        session,
                        direction,
                        index,
                        child_path,
                        baseline[item_index],
                        candidate[item_index],
                    )
                )
        return result
    if baseline != candidate:
        return [
            {
                "comparison": comparison,
                "session": session,
                "direction": direction,
                "index": index,
                "field": path or "record",
                "baseline": baseline,
                "candidate": candidate,
            }
        ]
    return []


def _record_diffs(
    baseline: list[dict[str, Any]],
    candidate: list[dict[str, Any]],
    session: int,
    direction: str,
) -> list[dict[str, Any]]:
    result = []
    for index in range(max(len(baseline), len(candidate))):
        if index >= len(baseline) or index >= len(candidate):
            result.append(
                {
                    "comparison": "A",
                    "session": session,
                    "direction": direction,
                    "index": index,
                    "field": "record.present",
                    "baseline": _message_view(baseline[index])
                    if index < len(baseline)
                    else None,
                    "candidate": _message_view(candidate[index])
                    if index < len(candidate)
                    else None,
                }
            )
            continue
        result.extend(
            _diff_values(
                "A",
                session,
                direction,
                index,
                "",
                _message_view(baseline[index]),
                _message_view(candidate[index]),
            )
        )
    return result


def _apdu_diffs(
    baseline: list[bytes],
    candidate: list[bytes],
    session: int,
    direction: str,
) -> list[dict[str, Any]]:
    result = []
    for index in range(max(len(baseline), len(candidate))):
        if index >= len(baseline) or index >= len(candidate):
            result.append(
                {
                    "comparison": "B",
                    "session": session,
                    "direction": direction,
                    "index": index,
                    "field": "apdu.present",
                    "baseline": baseline[index].hex() if index < len(baseline) else None,
                    "candidate": candidate[index].hex()
                    if index < len(candidate)
                    else None,
                }
            )
            continue
        left, right = baseline[index], candidate[index]
        common = min(len(left), len(right))
        for offset in range(common):
            if left[offset] != right[offset]:
                result.append(
                    {
                        "comparison": "B",
                        "session": session,
                        "direction": direction,
                        "index": index,
                        "field": f"apdu[{index}].byte[{offset}]",
                        "baseline": f"{left[offset]:02x}",
                        "candidate": f"{right[offset]:02x}",
                    }
                )
        if len(left) != len(right):
            result.append(
                {
                    "comparison": "B",
                    "session": session,
                    "direction": direction,
                    "index": index,
                    "field": f"apdu[{index}].length",
                    "baseline": len(left),
                    "candidate": len(right),
                }
            )
    return result


def _session_pairs(
    baseline_count: int,
    candidate_count: int,
    explicit: dict[int, int] | None,
) -> tuple[list[tuple[int, int]], list[int], list[int]]:
    pairs_map = explicit if explicit is not None else {
        index: index
        for index in range(min(baseline_count, candidate_count))
    }
    pairs = []
    unmatched_baseline = []
    matched_candidates: set[int] = set()
    for base, candidate in sorted(pairs_map.items()):
        if base >= baseline_count:
            unmatched_baseline.append(base)
            continue
        if candidate >= candidate_count:
            unmatched_baseline.append(base)
            continue
        pairs.append((base, candidate))
        matched_candidates.add(candidate)
    paired_baselines = {base for base, _ in pairs}
    unmatched_baseline.extend(
        index for index in range(baseline_count) if index not in paired_baselines
    )
    unmatched_candidate = [
        index for index in range(candidate_count) if index not in matched_candidates
    ]
    return pairs, sorted(set(unmatched_baseline)), unmatched_candidate


def _message_time_audit(capture: Any, side: str) -> list[dict[str, Any]]:
    entries = []
    for (session, direction), records in capture.records.items():
        for index, record in enumerate(records):
            asdu = record.get("asdu")
            if not asdu:
                continue
            for tag_index, tag in enumerate(asdu["time_tags"]):
                if tag["kind"] != "cp56":
                    continue
                fields = tag["fields"]
                entries.append(
                    {
                        "side": side,
                        "session": session,
                        "direction": direction,
                        "frame": record["frame"],
                        "record": index,
                        "time_tag": tag_index,
                        "raw": tag["raw"],
                        "relative_ms": tag.get("relative_ms"),
                        "retained": {
                            "iv": fields["iv"],
                            "su": fields["su"],
                            "dow": fields["dow"],
                            "reserved": fields["reserved"],
                        },
                        "normalization": "cp56-relative-message",
                    }
                )
    return entries


def _unpaired_diff(
    comparison: str, session: int, side: str
) -> dict[str, Any]:
    return {
        "comparison": comparison,
        "session": session,
        "direction": "session",
        "index": 0,
        "field": "session.unpaired",
        "baseline": side == "baseline",
        "candidate": side == "candidate",
    }


def _in_scope(difference: dict[str, Any], entry: dict[str, Any]) -> bool:
    scope = entry["scope"]
    if (
        difference["comparison"] != scope["comparison"]
        or difference["session"] != scope["session"]
        or difference["direction"] != scope["direction"]
        or difference["field"] != entry["field"]
    ):
        return False
    if "record_range" in scope:
        start, end = scope["record_range"]
        if not start <= difference["index"] <= end:
            return False
    if "delta" in entry:
        left, right = difference["baseline"], difference["candidate"]
        return (
            isinstance(left, (int, float))
            and not isinstance(left, bool)
            and isinstance(right, (int, float))
            and not isinstance(right, bool)
            and right - left == entry["delta"]
        )
    return (
        difference["baseline"] == entry["baseline"]
        and difference["candidate"] == entry["candidate"]
    )


def _apply_allowlist(
    differences: list[dict[str, Any]], entries: list[dict[str, Any]]
) -> tuple[list[dict[str, Any]], list[str]]:
    result = [dict(difference) for difference in differences]
    counts = {entry["id"]: 0 for entry in entries}
    issues: list[str] = []
    for difference in result:
        matches = [entry for entry in entries if _in_scope(difference, entry)]
        if len(matches) > 1:
            issues.append(
                f"difference {difference['field']} matches multiple allow-list entries"
            )
        elif matches:
            entry = matches[0]
            counts[entry["id"]] += 1
            difference["allowed"] = True
            difference["allowlist_id"] = entry["id"]
    for entry in entries:
        count = counts[entry["id"]]
        if not count:
            issues.append(f"stale allow-list entry {entry['id']!r} matched no differences")
        if count > entry["max_matches"]:
            issues.append(
                f"allow-list entry {entry['id']!r} matched {count} differences, "
                f"over max_matches={entry['max_matches']}"
            )
    for difference in result:
        difference.setdefault("allowed", False)
    return result, issues


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _session_metrics(
    capture: Any, session: int
) -> dict[str, dict[str, int | float]]:
    return capture.tcp_report.get(session, {})


def _run_comparison(
    baseline_path: Path,
    candidate_path: Path,
    allowlist_path: Path,
    sessions_path: Path | None,
    tshark: str,
    timeout: int,
    cp56_tolerance_ms: int,
) -> dict[str, Any]:
    entries = load_allowlist(allowlist_path)
    explicit_pairs = load_session_map(sessions_path)
    baseline = decode_capture(
        baseline_path, tshark, timeout, cp56_tolerance_ms
    )
    candidate = decode_capture(
        candidate_path, tshark, timeout, cp56_tolerance_ms
    )
    if explicit_pairs is not None:
        for base, other in explicit_pairs.items():
            if base >= baseline.session_count or other >= candidate.session_count:
                raise InputError(
                    f"session map pair {base}->{other} references a missing connection"
                )
    pairs, unmatched_baseline, unmatched_candidate = _session_pairs(
        baseline.session_count, candidate.session_count, explicit_pairs
    )
    message_differences: list[dict[str, Any]] = []
    apdu_differences: list[dict[str, Any]] = []
    for base_session, candidate_session in pairs:
        for direction in DIRECTIONS:
            message_differences.extend(
                _record_diffs(
                    baseline.records.get((base_session, direction), []),
                    candidate.records.get((candidate_session, direction), []),
                    base_session,
                    direction,
                )
            )
        session_baseline_apdus = [
            apdu
            for key, values in baseline.apdus.items()
            if key[0] == base_session
            for apdu in values
        ]
        session_candidate_apdus = [
            apdu
            for key, values in candidate.apdus.items()
            if key[0] == candidate_session
            for apdu in values
        ]
        first_baseline_time = session_first_cp56(session_baseline_apdus)
        first_candidate_time = session_first_cp56(session_candidate_apdus)
        normalized_baseline: dict[str, list[bytes]] = {}
        normalized_candidate: dict[str, list[bytes]] = {}
        for side, capture, session, first_time, output in (
            (
                "baseline",
                baseline,
                base_session,
                first_baseline_time,
                normalized_baseline,
            ),
            (
                "candidate",
                candidate,
                candidate_session,
                first_candidate_time,
                normalized_candidate,
            ),
        ):
            for direction in DIRECTIONS:
                apdus = capture.apdus.get((session, direction), [])
                values, audit, issues = normalize_apdus(
                    apdus, first_time, cp56_tolerance_ms, session, direction
                )
                output[direction] = values
                capture.normalization_audit.extend(
                    [{"side": side, **entry} for entry in audit]
                )
                capture.validity_errors.extend(issues)
        for direction in DIRECTIONS:
            apdu_differences.extend(
                _apdu_diffs(
                    normalized_baseline[direction],
                    normalized_candidate[direction],
                    base_session,
                    direction,
                )
            )
    for session in unmatched_baseline:
        message_differences.append(_unpaired_diff("A", session, "baseline"))
        apdu_differences.append(_unpaired_diff("B", session, "baseline"))
    for session in unmatched_candidate:
        message_differences.append(_unpaired_diff("A", session, "candidate"))
        apdu_differences.append(_unpaired_diff("B", session, "candidate"))

    protocol_differences, allowlist_issues = _apply_allowlist(
        message_differences + apdu_differences, entries
    )
    message_differences = [
        item for item in protocol_differences if item["comparison"] == "A"
    ]
    apdu_differences = [
        item for item in protocol_differences if item["comparison"] == "B"
    ]
    validity_differences = []
    for side, capture in (("baseline", baseline), ("candidate", candidate)):
        for index, error in enumerate(capture.validity_errors):
            validity_differences.append(
                {
                    "comparison": "C",
                    "session": -1,
                    "direction": "session",
                    "index": index,
                    "field": "decode.validity",
                    "side": side,
                    "message": error,
                }
            )
    allowlist_issues = sorted(set(allowlist_issues))
    allowlist_differences = [
        {
            "comparison": "allowlist",
            "session": -1,
            "direction": "session",
            "field": "allowlist.validity",
            "message": issue,
        }
        for issue in allowlist_issues
    ]
    differences = (
        message_differences
        + apdu_differences
        + validity_differences
        + allowlist_differences
    )
    a_pass = not any(not item["allowed"] for item in message_differences)
    b_pass = not any(not item["allowed"] for item in apdu_differences)
    c_pass = not validity_differences
    tcp_comparison = []
    for base, candidate_index in pairs:
        base_metrics = _session_metrics(baseline, base)
        candidate_metrics = _session_metrics(candidate, candidate_index)
        delta: dict[str, dict[str, int | float]] = {}
        for direction in DIRECTIONS:
            left = base_metrics.get(direction, {})
            right = candidate_metrics.get(direction, {})
            delta[direction] = {
                key: right.get(key, 0) - left.get(key, 0)
                for key in sorted(set(left) | set(right))
                if isinstance(right.get(key, 0), (int, float))
                and isinstance(left.get(key, 0), (int, float))
            }
        tcp_comparison.append(
            {
                "baseline_session": base,
                "candidate_session": candidate_index,
                "baseline": base_metrics,
                "candidate": candidate_metrics,
                "delta": delta,
            }
        )
    return {
        "schema": "iec104-differential-report-v1",
        "inputs": {
            "baseline": {"path": str(baseline_path), "sha256": _sha256(baseline_path)},
            "candidate": {
                "path": str(candidate_path),
                "sha256": _sha256(candidate_path),
            },
            "tshark": baseline.tshark_version,
            "cp56_tolerance_ms": cp56_tolerance_ms,
            "session_pairs": [
                {"baseline": base, "candidate": other} for base, other in pairs
            ],
            "unpaired_baseline_sessions": unmatched_baseline,
            "unpaired_candidate_sessions": unmatched_candidate,
        },
        "comparisons": {
            "A_message_level": {
                "gate": True,
                "status": "pass" if a_pass else "fail",
                "difference_count": sum(
                    not item["allowed"] for item in message_differences
                ),
                "differences": message_differences,
                "normalization_audit": {
                    "baseline": _message_time_audit(baseline, "baseline"),
                    "candidate": _message_time_audit(candidate, "candidate"),
                },
                "records": {
                    "baseline": {
                        f"{session}:{direction}": records
                        for (session, direction), records in baseline.records.items()
                    },
                    "candidate": {
                        f"{session}:{direction}": records
                        for (session, direction), records in candidate.records.items()
                    },
                },
            },
            "B_apdu_level": {
                "gate": True,
                "status": "pass" if b_pass else "fail",
                "difference_count": sum(not item["allowed"] for item in apdu_differences),
                "differences": apdu_differences,
                "normalization_audit": {
                    "baseline": baseline.normalization_audit,
                    "candidate": candidate.normalization_audit,
                },
            },
            "C_decode_validity": {
                "gate": True,
                "status": "pass" if c_pass else "fail",
                "difference_count": len(validity_differences),
                "differences": validity_differences,
                "baseline_errors": baseline.validity_errors,
                "candidate_errors": candidate.validity_errors,
            },
            "D_tcp_report_only": {
                "gate": False,
                "status": "report-only",
                "sessions": tcp_comparison,
            },
        },
        "allowlist": {
            "path": str(allowlist_path),
            "entries": entries,
            "issues": allowlist_issues,
            "status": "pass" if not allowlist_issues else "fail",
        },
        "passed": a_pass and b_pass and c_pass and not allowlist_issues,
        "differences": differences,
    }


def _markdown(report: dict[str, Any]) -> str:
    comparisons = report["comparisons"]
    lines = [
        "# IEC 104 differential comparison",
        "",
        f"- Baseline: `{Path(report['inputs']['baseline']['path']).name}`",
        f"- Candidate: `{Path(report['inputs']['candidate']['path']).name}`",
        f"- TShark: {report['inputs']['tshark']}",
        f"- CP56 tolerance: {report['inputs']['cp56_tolerance_ms']} ms",
        f"- Overall: **{'PASS' if report['passed'] else 'FAIL'}**",
        "",
        "| Comparison | Gate | Status | Unallowed differences |",
        "|---|---:|---|---:|",
    ]
    for key in ("A_message_level", "B_apdu_level", "C_decode_validity", "D_tcp_report_only"):
        entry = comparisons[key]
        count = entry.get("difference_count", "—")
        lines.append(
            f"| {key} | {'yes' if entry['gate'] else 'no'} | "
            f"{entry['status']} | {count} |"
        )
    lines.extend(["", "## Differences", ""])
    shown = report["differences"][:12]
    if not shown:
        lines.append("No gate differences.")
    else:
        for difference in shown:
            state = "allowed" if difference.get("allowed") else "FAIL"
            lines.append(
                f"- **{state} {difference['comparison']}** session "
                f"{difference['session']} `{difference['direction']}` "
                f"record {difference.get('index', '—')} "
                f"`{difference['field']}`: "
                f"{difference.get('baseline')!r} → "
                f"{difference.get('candidate')!r}"
                + (
                    f" (allow-list {difference['allowlist_id']})"
                    if difference.get("allowlist_id")
                    else ""
                )
                + (
                    f" — {difference['message']}"
                    if difference.get("message")
                    else ""
                )
            )
        if len(report["differences"]) > len(shown):
            lines.append(f"- … {len(report['differences']) - len(shown)} more.")
    if report["allowlist"]["issues"]:
        lines.extend(["", "## Allow-list failures", ""])
        lines.extend(f"- {issue}" for issue in report["allowlist"]["issues"])
    lines.extend(
        [
            "",
            "TCP segment/ACK/FIN/RST metrics are report-only and do not affect the gate.",
            "",
        ]
    )
    return "\n".join(lines)


def _positive_integer(value: str) -> int:
    try:
        result = int(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("must be an integer") from error
    if result <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Compare IEC 104 captures at message, APDU, decode, and TCP levels."
    )
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("--allowlist", type=Path, default=Path(__file__).with_name("allowlist.json"))
    parser.add_argument("--sessions", type=Path, help="JSON map of baseline to candidate session indexes")
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--tshark", default=DEFAULT_TSHARK)
    parser.add_argument("--timeout", type=_positive_integer, default=60)
    parser.add_argument("--cp56-tolerance-ms", type=_positive_integer, default=1000)
    arguments = parser.parse_args(argv)
    try:
        report = _run_comparison(
            arguments.baseline,
            arguments.candidate,
            arguments.allowlist,
            arguments.sessions,
            arguments.tshark,
            arguments.timeout,
            arguments.cp56_tolerance_ms,
        )
        arguments.out.mkdir(parents=True, exist_ok=True)
        (arguments.out / "report.json").write_text(
            json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
        )
        (arguments.out / "report.md").write_text(_markdown(report), encoding="utf-8")
        print(_markdown(report), end="")
        return 0 if report["passed"] else 1
    except (DecodeError, InputError, OSError) as error:
        print(f"compare.py: {error}", file=sys.stderr)
        error_report = {
            "schema": "iec104-differential-report-v1",
            "passed": False,
            "error": {"type": type(error).__name__, "message": str(error)},
        }
        try:
            arguments.out.mkdir(parents=True, exist_ok=True)
            (arguments.out / "report.json").write_text(
                json.dumps(error_report, indent=2, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
            (arguments.out / "report.md").write_text(
                "# IEC 104 differential comparison\n\n"
                f"**ERROR ({type(error).__name__}):** {error}\n",
                encoding="utf-8",
            )
        except OSError as report_error:
            print(f"compare.py: could not write error report: {report_error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
