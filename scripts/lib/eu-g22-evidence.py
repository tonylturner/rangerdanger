"""Attempt-local G22 packet correlation and evidence validation (stdlib only)."""

import datetime as dt
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

from containd_readiness import require_backend_protection

COMMAND = "iec104cmd 10.30.30.20 dc 3001 open -sbo --json"
PACKAGE = "eu-iec104-substation"
POINTS = ("feeder-breaker-position", "feeder-current",
          "feeder-voltage", "feeder-protection-trip")
EVENTS = (
    "baseline-field", "baseline-cc", "start", "weak-command",
    "consequence-field", "consequence-cc", "end", "decoded", "restore-start",
    "restoration-field", "restoration-cc", "improved-apply",
    "improved-control-before", "improved-start", "improved-command",
    "improved-held-field", "improved-end", "improved-control-after", "completed",
)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def epoch(value):
    """Parse range UTC without discarding subsecond capture precision."""
    require(isinstance(value, str), "missing/non-string time boundary")
    if value.endswith("Z"):
        base, _, fraction = value[:-1].partition(".")
        seconds = dt.datetime.fromisoformat(base).replace(tzinfo=dt.timezone.utc)
        return Decimal(int(seconds.timestamp())) + Decimal("0." + (fraction or "0"))
    try:
        result = Decimal(value)
    except ArithmeticError as exc:
        raise ValueError(f"invalid epoch {value!r}") from exc
    require(result.is_finite(), "nonfinite time boundary")
    return result


def field(node, name):
    values = node.findall(f".//field[@name='{name}']")
    require(len(values) == 1, f"expected one {name}, got {len(values)}")
    return values[0].attrib["show"]


def integer(node, name):
    value = field(node, name)
    return {"True": 1, "False": 0}.get(value, value)


def matches(node, prefix, expected):
    # All values must occur in this protocol/PDU, never frame-wide aggregates.
    try:
        return all(int(integer(node, prefix + key)) == value
                   for key, value in expected.items())
    except (ValueError, KeyError):
        return False


def decode(pcap, start, end, output, run=subprocess.run):
    Path(output).unlink(missing_ok=True)
    start, end = epoch(start), epoch(end)
    require(start < end, "empty/reversed capture window")
    raw = Path(str(output) + ".pdml")
    errors = Path(str(output) + ".tshark.err")
    with raw.open("wb") as stdout, errors.open("wb") as stderr:
        result = run(["tshark", "-n", "-r", str(pcap), "-T", "pdml"],
                     stdout=stdout, stderr=stderr, timeout=45, check=False)
    require(result.returncode == 0, f"tshark failed: {errors.read_text()}")
    selects, executes, writes, seen = [], [], [], set()
    for _, packet in ET.iterparse(raw, events=("end",)):
        if packet.tag != "packet":
            continue
        require(not packet.findall(".//*[@name='_ws.malformed']"), "malformed frame")
        require(field(packet, "frame.cap_len") == field(packet, "frame.len"),
                "truncated frame")
        stamp = epoch(field(packet, "frame.time_epoch"))
        if not start <= stamp <= end:
            packet.clear()
            continue
        if not packet.findall("./proto[@name='tcp']"):
            packet.clear()
            continue
        meta = {key: field(packet, name) for key, name in {
            "frame": "frame.number", "time": "frame.time_epoch",
            "source": "ip.src", "target": "ip.dst", "stream": "tcp.stream",
            "tcp_seq": "tcp.seq_raw", "source_port": "tcp.srcport",
            "target_port": "tcp.dstport"}.items()}
        mbap = None
        for index, proto in enumerate(packet.findall("proto")):
            name = proto.get("name")
            if name == "mbtcp":
                mbap = proto
            if name == "iec60870_asdu" and (
                meta["source"], meta["target"], meta["target_port"]
            ) == ("10.10.10.50", "10.30.30.20", "2404"):
                common = {"typeid": 46, "causetx": 6, "addr": 1, "ioa": 3001,
                          "numix": 1, "dco.on": 1, "nega": 0, "test": 0}
                for se, dest in ((1, selects), (0, executes)):
                    if matches(proto, "iec60870_asdu.", {**common, "dco.se": se}):
                        item = {**meta, "pdu": index, "offset": proto.get("pos"),
                                "ca": 1, "ioa": 3001, "dcs": 1, "select": se}
                        key = (meta["stream"], meta["tcp_seq"], proto.get("pos"), se)
                        if key not in seen:
                            seen.add(key)
                            dest.append(item)
            if name == "modbus":
                if mbap is not None and (
                    meta["source"], meta["target"], meta["target_port"]
                ) == ("10.30.30.20", "10.40.40.20", "502") and matches(
                    mbap, "mbtcp.", {"unit_id": 1, "prot_id": 0}
                ) and matches(proto, "modbus.", {
                    "func_code": 6, "reference_num": 0, "regval_uint16": 1
                }):
                    item = {**meta, "pdu": index, "offset": proto.get("pos"),
                            "transaction": field(mbap, "mbtcp.trans_id"),
                            "unit": 1, "function": 6, "address": 0, "value": 1}
                    key = (meta["stream"], meta["tcp_seq"], proto.get("pos"), "fc6")
                    if key not in seen:
                        seen.add(key)
                        writes.append(item)
                mbap = None
        packet.clear()

    def before(a, b):
        return (epoch(a["time"]) <= epoch(b["time"])
                and (int(a["frame"]), a["pdu"]) < (int(b["frame"]), b["pdu"]))

    for select in selects:
        for execute in executes:
            if select["stream"] != execute["stream"] or not before(select, execute):
                continue
            for write in writes:
                if before(execute, write):
                    record = {"start": str(start), "end": str(end),
                              "pcap": Path(pcap).name,
                              "sha256": hashlib.sha256(Path(pcap).read_bytes()).hexdigest(),
                              "select": select, "execute": execute, "fc6": write}
                    Path(output).write_text(json.dumps(record, indent=2) + "\n")
                    return record
    raise ValueError("missing ordered same-stream Kali OPEN select/execute -> RTU FC6")


def exec_result(result, success):
    require(isinstance(result["stdout"], str) and isinstance(result["stderr"], str)
            and type(result["exit_code"]) is int, "malformed exec response")
    if success:
        require(result["exit_code"] == 0, "Weak command failed")
        return "completed"
    text = result["stderr"] + "\n" + result["stdout"]
    require(result["exit_code"] != 0, "Improved command succeeded")
    for token, classification in (
        ("connection refused", "refusal"), ("no route", "no-route"),
        ("network is unreachable", "no-route"), ("not found", "tool-error"),
    ):
        require(token not in text.lower(), f"Improved retest is {classification}, not denial")
    require(re.search(
        r"connect failed:.*dial tcp.*10\.30\.30\.20:2404:.*"
        r"(i/o timeout|context deadline exceeded)", text),
        "Improved retest is not an explicit connect timeout")
    return "connect-timeout"


def field_state(state, position, supplied):
    breaker = state["devices"]["breaker-1"]
    process = state["process"]
    require(breaker["position"] == position, f"expected {position} field breaker")
    if position == "closed":
        require(breaker["protection_tripped"] is False, "restored breaker is tripped")
    require(process["buses"]["s1"]["live"] is supplied, "wrong S1 supply")
    require(process["truth_stale"] is False, "stale solver truth")
    require(process["devices"]["feeder-breaker"]["delivered"] is True
            and process["devices"]["feeder-breaker"]["solved_revision"] == breaker["revision"],
            "undelivered/stale solver revision")
    require(type(breaker["generation"]) is int and type(breaker["revision"]) is int,
            "invalid device identity")
    require(bool(process["solver_epoch"]) and type(process["solve_seq"]) is int,
            "missing solver identity")
    epoch(process["solved_at"])
    return breaker, process


def cc_state(state, position, since=None):
    require(state["schema"] == 3 and state["package_id"] == PACKAGE
            and bool(state["range_generation"]) and bool(state["cache_epoch"])
            and type(state["snapshot_seq"]) is int, "invalid CC/range identity")
    require(state["link"]["state"] == "started"
            and state["link"]["connection_seq"] > 0, "CC link is not started")
    selected = {}
    for name in POINTS:
        points = [p for p in state["points"] if p["id"] == name]
        require(len(points) == 1, f"missing/duplicate CC point {name}")
        point = points[0]
        # Preserve the whole point, including stale quality and original times.
        received = epoch(point["received_at"])
        age = epoch(state["rendered_at"]) - received
        require(age >= 0, f"future CC receipt {name}")
        selected[name] = {**point, "receipt_age_seconds": str(age)}
    point = selected["feeder-breaker-position"]
    require(point["value"] == position and point["stale_reason"] is None
            and all(point["quality"][q] is False for q in ("iv", "nt", "sb", "bl"))
            and point["quality"]["ov"] in (None, False), "non-GOOD/wrong CC position")
    if since is not None:
        require(epoch(point["received_at"]) >= since, "old cached CC position")
    return selected


def validate_record(record):
    raw = record["raw"]
    context = raw["context"]
    require(context["scenario"] == record["scenario"] and bool(context["attempt_id"]),
            "missing attempt/scenario identity")
    events = record["events"]
    require([e["event"] for e in events] == list(EVENTS), "incomplete/out-of-order walk")
    times = {e["event"]: epoch(e["at"]) for e in events}
    require(list(times.values()) == sorted(times.values()), "range event clock reversed")
    packets = raw["packets"]
    require(epoch(packets["start"]) == times["start"]
            and epoch(packets["end"]) == times["end"]
            and times["start"] < times["end"], "packet window mismatch")
    require(packets["sha256"] == record["pcap"]["sha256"], "PCAP hash mismatch")
    require(packets["pcap"] == record["pcap"]["file"], "PCAP filename mismatch")
    previous = times["start"]
    previous_order = (0, 0)
    for name in ("select", "execute", "fc6"):
        link = packets[name]
        stamp = epoch(link["time"])
        require(previous <= stamp <= times["end"], "packet link outside window/order")
        order = (int(link["frame"]), link["pdu"])
        require(order > previous_order, "packet PDU order reversed")
        require(bool(link["tcp_seq"]) and bool(link["stream"]), "missing TCP identity")
        previous = stamp
        previous_order = order
    select, execute, fc6 = (packets[n] for n in ("select", "execute", "fc6"))
    require(select["stream"] == execute["stream"], "command stream mismatch")
    for link, se in ((select, 1), (execute, 0)):
        require((link["source"], link["target"], link["target_port"], link["ca"],
                 link["ioa"], link["dcs"], link["select"])
                == ("10.10.10.50", "10.30.30.20", "2404", 1, 3001, 1, se),
                "incorrect IEC command identity")
    require((fc6["source"], fc6["target"], fc6["target_port"], fc6["unit"],
             fc6["function"], fc6["address"], fc6["value"])
            == ("10.30.30.20", "10.40.40.20", "502", 1, 6, 0, 1)
            and bool(fc6["transaction"]), "incorrect FC6 identity")
    stats = record["pcap"]["statistics"]
    for key in ("captured", "received by filter", "dropped by kernel"):
        values = re.findall(rf"^\s*(\d+) packets {key}\s*$", stats, re.M)
        require(len(values) == 1, "incomplete tcpdump statistics")
        if key == "dropped by kernel":
            require(int(values[0]) == 0, "kernel capture drops")
    exec_result(raw["weak-command"], True)
    record["improved_classification"] = exec_result(raw["improved-command"], False)
    require_backend_protection(raw["improved-apply"])
    before, baseline = field_state(raw["baseline-field"], "closed", True)
    opened, outage = field_state(raw["consequence-field"], "open", False)
    restored, recovery = field_state(raw["restoration-field"], "closed", True)
    held, held_process = field_state(raw["improved-held-field"], "closed", True)
    require(before["generation"] == opened["generation"] == restored["generation"]
            == held["generation"], "device generation changed")
    require(before["revision"] < opened["revision"] < restored["revision"]
            == held["revision"], "breaker revision chain mismatch")
    require(baseline["solver_epoch"] == outage["solver_epoch"] == recovery["solver_epoch"]
            == held_process["solver_epoch"], "solver epoch changed")
    require(baseline["solve_seq"] < outage["solve_seq"] < recovery["solve_seq"]
            <= held_process["solve_seq"], "solver sequence did not advance")
    require(times["start"] <= epoch(outage["solved_at"]) <= times["consequence-field"],
            "consequence solve outside attempt")
    require(epoch(fc6["time"]) <= epoch(outage["solved_at"]),
            "consequence solve predates FC6")
    require(times["restore-start"] <= epoch(recovery["solved_at"])
            <= times["restoration-field"], "recovery solve predates restoration")
    cc_names = ("baseline-cc", "consequence-cc", "restoration-cc",
                "improved-control-before", "improved-control-after")
    identity = raw["baseline-cc"]
    observations = {}
    last_seq = -1
    for name in cc_names:
        state = raw[name]
        since = (times["start"] if name == "consequence-cc" else
                 times["restore-start"] if name == "restoration-cc" else None)
        observations[name] = cc_state(state, "open" if name == "consequence-cc"
                                      else "closed", since)
        require(state["range_generation"] == identity["range_generation"],
                "CC range generation changed")
        if name in ("consequence-cc", "restoration-cc"):
            require(state["cache_epoch"] == identity["cache_epoch"]
                    and state["link"]["connection_seq"] == identity["link"]["connection_seq"],
                    "CC cache/session identity changed during Weak attempt")
        require(state["snapshot_seq"] >= last_seq, "CC snapshot sequence reversed")
        last_seq = state["snapshot_seq"]
        require(epoch(state["rendered_at"]) <= times[name], "CC observation from future")
    for name, boundary in (("improved-control-before", "restoration-cc"),
                           ("improved-control-after", "improved-end")):
        control = raw[name]
        link = control["link"]
        require(link["gi"]["state"] == "complete"
                and epoch(link["since"]) >= times[boundary]
                and epoch(link["gi"]["started_at"]) >= epoch(link["since"]),
                "Improved listener control is not a fresh connected GI")
        require(all(p["value"] is not None and p["stale_reason"] is None
                    and all(p["quality"][q] is False for q in ("iv", "nt", "sb", "bl"))
                    and p["quality"]["ov"] in (None, False)
                    and epoch(p["received_at"]) >= epoch(link["gi"]["started_at"])
                    for p in control["points"]), "unhealthy Improved positive control")
    record["cc_observations"] = observations
    record["package_id"] = PACKAGE
    record["range_generation"] = identity["range_generation"]
    record["attempt_id"] = context["attempt_id"]


def write_record(directory, scenario, walk_status):
    directory = Path(directory)
    record = {"schema": 1, "gate": "G22", "scenario": scenario, "status": "FAIL",
              "capture_namespace": "firewall", "clock": "range/firewall",
              "command": {"node": "kali-1", "source": "10.10.10.50",
                          "target": "10.30.30.20:2404", "literal": COMMAND},
              "wording": "The L4 source pin enforces; DPI detects and logs.",
              "events": [], "raw": {}, "raw_inputs": [], "errors": []}
    try:
        for path in sorted(directory.glob("G22-*")):
            if path.name == "G22-chain.json" or not path.is_file():
                continue
            record["raw_inputs"].append(path.name)
            if path.suffix == ".json":
                record["raw"][path.stem.removeprefix("G22-")] = json.loads(path.read_text())
        if (directory / "api-responses.log").is_file():
            record["raw_inputs"].append("api-responses.log")
        record["events"] = [json.loads(line) for line in
                            (directory / "G22-events.jsonl").read_text().splitlines()]
        pcap = directory / "G22-weak.pcap"
        record["pcap"] = {
            "file": pcap.name, "sha256": hashlib.sha256(pcap.read_bytes()).hexdigest(),
            "statistics": (directory / "G22-weak-tcpdump.log").read_text(),
        }
        require(walk_status == "0", "walk failed; partial raw evidence retained")
        validate_record(record)
        record["status"] = "PASS"
    except (OSError, ValueError, KeyError, TypeError) as exc:
        record["errors"].append(str(exc))
    (directory / "G22-chain.json").write_text(json.dumps(record, indent=2) + "\n")
    require(record["status"] == "PASS", "; ".join(record["errors"]))


def main(args):
    try:
        if len(args) == 6 and args[1] == "capture":
            decode(*args[2:])
        elif len(args) == 5 and args[1] == "record":
            write_record(*args[2:])
        else:
            raise ValueError("usage: capture PCAP START END OUTPUT | record DIR SCENARIO STATUS")
    except (OSError, ValueError, KeyError, ET.ParseError, subprocess.SubprocessError) as exc:
        print(f"G22 evidence: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
