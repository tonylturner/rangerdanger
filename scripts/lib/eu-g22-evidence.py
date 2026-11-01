"""Attempt-local G22 packet correlation and evidence validation (stdlib only)."""

import datetime as dt
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import xml.etree.ElementTree as ET


def require(condition, message):
    if not condition:
        raise ValueError(message)


def epoch(value):
    """Parse range UTC without discarding subsecond capture precision."""
    if value.endswith("Z"):
        base, _, fraction = value[:-1].partition(".")
        seconds = dt.datetime.fromisoformat(base).replace(tzinfo=dt.timezone.utc)
        return Decimal(int(seconds.timestamp())) + Decimal("0." + (fraction or "0"))
    result = Decimal(value)
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


def main(args):
    try:
        if len(args) == 6 and args[1] == "capture":
            decode(*args[2:])
        else:
            raise ValueError("usage: eu-g22-evidence.py capture PCAP START END OUTPUT")
    except (OSError, ValueError, KeyError, ET.ParseError, subprocess.SubprocessError) as exc:
        print(f"G22 evidence: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
