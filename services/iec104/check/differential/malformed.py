# SPDX-License-Identifier: Apache-2.0
"""Truthful scenario declarations, resolved to direction-local APDU indexes.

Only black-box decoder diagnostics can be declared. Independent reassembly,
coverage and parser failures are never eligible. Ambiguous coalesced frames
fail closed: a packet diagnostic cannot excuse an unrelated APDU.
"""

from collections import Counter

DIRECTIONS = {"client_to_server", "server_to_client"}


def validate(declarations, *, resolved=False):
    if not isinstance(declarations, list):
        raise ValueError("malformed declarations must be a list")
    seen = set()
    for item in declarations:
        keys = {"step", "message", "baseline", "candidate"} if resolved else {"step", "message"}
        if not isinstance(item, dict) or set(item) != keys:
            raise ValueError("invalid malformed declaration keys")
        if type(item["step"]) is not int or item["step"] < 0 or item["message"] not in ("input", "reply"):
            raise ValueError("invalid malformed step/message")
        identity = (item["step"], item["message"])
        if identity in seen:
            raise ValueError("duplicate malformed step/message")
        seen.add(identity)
        if resolved:
            for side in ("baseline", "candidate"):
                selector = item[side]
                if not isinstance(selector, dict) or set(selector) != {"session", "direction", "index"}:
                    raise ValueError("invalid malformed message association")
                if (type(selector["session"]) is not int or selector["session"] < 0
                        or type(selector["index"]) is not int or selector["index"] < 0
                        or not isinstance(selector["direction"], str)
                        or selector["direction"] not in DIRECTIONS):
                    raise ValueError("invalid malformed message selector")


def resolve(declarations, baseline, candidate):
    """Bind scenario intent to peer-observed messages, not capture frame IDs."""
    validate(declarations)
    result = []
    for declaration in declarations:
        item = dict(declaration)
        for side, associations in (("baseline", baseline), ("candidate", candidate)):
            matches = [a for a in associations if a["step"] == item["step"]
                       and a["message"] == item["message"]]
            if len(matches) != 1:
                raise ValueError(f"{side}: missing/ambiguous malformed association {declaration}")
            item[side] = {key: matches[0][key] for key in ("session", "direction", "index")}
        result.append(item)
    validate(result, resolved=True)
    return result


def compare(declarations, baseline, candidate):
    """Return remaining validity errors and an auditable declaration result."""
    validate(declarations, resolved=True)
    captures = {"baseline": baseline, "candidate": candidate}
    accepted = {side: Counter() for side in captures}
    used = {side: set() for side in captures}
    issues, audit = [], []
    for item in declarations:
        diagnostics = {}
        errors = []
        for side, capture in captures.items():
            selector = item[side]
            key = (selector["session"], selector["direction"])
            index = selector["index"]
            identity = (*key, index)
            apdus = capture.apdus.get(key, [])
            if identity in used[side] or index >= len(apdus):
                errors.append(f"{side}: duplicate/missing declared APDU")
                continue
            used[side].add(identity)
            frame = apdus[index].frame
            # Both the independent parser and decoder must associate exactly
            # one message with the diagnostic packet. Never guess on coalescing.
            parsed = [a for values in capture.apdus.values() for a in values if a.frame == frame]
            decoded = [a for values in capture.records.values() for a in values if a["frame"] == frame]
            messages = capture.frame_diagnostics.get(frame, [])
            if len(parsed) != 1 or len(decoded) != 1 or not messages:
                errors.append(f"{side}: declared APDU has no unambiguous diagnostic")
                continue
            # Normalise ONLY the packet identity; diagnostic text and multiplicity
            # remain exact, including every expert error and malformed marker.
            diagnostics[side] = [m.removeprefix(f"frame {frame}: ") for m in messages]
        if not errors and diagnostics["baseline"] != diagnostics["candidate"]:
            errors.append("declared decoder diagnostics differ")
        if errors:
            issues.extend(f"step {item['step']} {item['message']}: {e}" for e in errors)
        else:
            for side, capture in captures.items():
                selector = item[side]
                frame = capture.apdus[(selector["session"], selector["direction"])][selector["index"]].frame
                accepted[side].update(capture.frame_diagnostics[frame])
        audit.append({**item, "diagnostics": diagnostics, "issues": errors})
    remaining = {}
    for side, capture in captures.items():
        remaining[side] = []
        for error in capture.validity_errors:
            if accepted[side][error]:
                accepted[side][error] -= 1
            else:
                remaining[side].append(error)
    return remaining, issues, audit
