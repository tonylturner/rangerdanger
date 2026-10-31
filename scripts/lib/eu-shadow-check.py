#!/usr/bin/env python3
"""Strict offline checks of documented containd v0.1.36 shadow evidence."""

import calendar
import datetime
import json
import re
import sys

IDS = (
    "observe-cc-breaker-command",
    "observe-cc-oltc-step",
    "observe-cc-oltc-raise-only",
)
FIELDS = ("shadowMatches", "wouldAllow", "wouldDeny")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def timestamp(value):
    require(isinstance(value, str) and bool(value), "missing timestamp")
    match = re.fullmatch(
        r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)", value
    )
    require(match is not None, "timestamp is not RFC3339Nano")
    parsed = datetime.datetime.fromisoformat(
        match[1] + match[3].replace("Z", "+00:00")
    )
    seconds = calendar.timegm(parsed.astimezone(datetime.timezone.utc).utctimetuple())
    return seconds * 1_000_000_000 + int((match[2] or "").ljust(9, "0"))


def load(path):
    with open(path, encoding="utf-8") as stream:
        return json.load(stream)


def counters(data):
    require(data.get("available") is True, "shadow counters unavailable")
    require(isinstance(data.get("snapshotVersion"), str)
            and bool(data["snapshotVersion"]), "missing snapshotVersion")
    timestamp(data.get("since"))
    require(data.get("unit") == "messages", "unexpected counter unit")
    rows = data.get("rules")
    require(isinstance(rows, list), "missing counter rules")
    indexed = {}
    for row in rows:
        rule = row.get("ruleId")
        require(isinstance(rule, str) and rule not in indexed, "invalid/duplicate ruleId")
        for key in FIELDS:
            value = row.get(key)
            require(type(value) is int and value >= 0, f"invalid {rule}.{key}")
        require(row["shadowMatches"] == row["wouldAllow"] + row["wouldDeny"],
                f"inconsistent counters: {rule}")
        indexed[rule] = row
    require(all(rule in indexed for rule in IDS), "missing applied monitor rule IDs")
    return indexed


def delta(before, after, phase):
    old, new = counters(before), counters(after)
    require(all(before[key] == after[key] for key in ("snapshotVersion", "since")),
            "shadow counter lifetime changed")
    diffs = {}
    for rule in IDS:
        diffs[rule] = {key: new[rule][key] - old[rule][key] for key in FIELDS}
        require(all(value >= 0 for value in diffs[rule].values()), "counter regressed")
    if phase == "breaker":
        require(diffs[IDS[0]]["wouldDeny"] >= 1, "missing breaker wouldDeny delta")
    else:
        require(phase in ("lower", "raise"), "unknown command phase")
        require(diffs[IDS[1]]["wouldAllow"] >= 1, "missing OLTC step delta")
        raised = diffs[IDS[2]]
        require(raised["shadowMatches"] == 0 if phase == "lower" else raised["wouldAllow"] >= 1,
                "incorrect OLTC raise-only delta")
    return diffs


def events(page, rule, start, end):
    require(isinstance(page, dict) and isinstance(page.get("events"), list),
            "missing event page")
    require(type(page.get("historyTruncated")) is bool, "event retention status unknown")
    require(not page.get("engineError"), "event engine query failed")
    # Overflow is global/conservative, not evidence that a retained positive
    # witness is invalid. Never use missing events to establish a zero count;
    # the lower/raise comparison uses applied counters, not event absence.
    if page["historyTruncated"]:
        print("shadow events: retention incomplete; positive witnesses only", file=sys.stderr)
    start, end = timestamp(start), timestamp(end)
    require(start <= end, "reversed event window")
    decision = "would_drop" if rule == IDS[0] else "would_allow"
    action = "DENY" if rule == IDS[0] else "ALLOW"
    for event in page["events"]:
        attrs = event.get("attributes") or {}
        if (event.get("kind") == "ics.decision" and attrs.get("mode") == "monitor"
                and attrs.get("ruleId") == rule and attrs.get("decision") == decision
                and attrs.get("action") == action and attrs.get("reason") == "rule"
                and start <= timestamp(event.get("timestamp")) <= end):
            print(json.dumps({"monitorEvidence": event}), file=sys.stderr)
            return True
    return False


def main(args):
    mode = args[0]
    if mode == "baseline":
        data = load(args[1])
        counters(data)
        print(json.dumps(data))
    elif mode == "delta":
        after = load(args[2])
        diffs = delta(load(args[1]), after, args[3])
        print(json.dumps({"snapshotVersion": after["snapshotVersion"], "since": after["since"],
                          "phase": args[3], "deltas": diffs}))
    elif mode == "events":
        page = load(args[1])
        found = events(page, *args[2:])
        # A missing hit can continue onto the next documented cursor page.
        if not found:
            require(bool(page.get("nextCursor")), "no matching monitor event in window")
            print(page["nextCursor"])
    else:
        raise ValueError("unknown shadow check")


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (ValueError, KeyError, TypeError, AttributeError, OSError) as error:
        sys.exit(f"shadow evidence: {error}")
