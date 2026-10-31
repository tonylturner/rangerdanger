# SPDX-License-Identifier: Apache-2.0
"""Extract only our Apache-licensed packets; tshark is an opaque decoder."""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CAPTURES = ROOT / "services/iec104/captures"
SELECTIONS = {
    "edge-addressing": [11, 43, 44],
    "sbo-double-command": [8, 9, 10, 11, 13],
    "startup-gi-monitor": [33],
    "session-management": [4, 6, 23, 53, 54],
}
FIELDS = ["frame.number", "tcp.payload", "iec60870_asdu.typeid",
          "iec60870_asdu.causetx", "iec60870_asdu.oa", "iec60870_asdu.addr",
          "iec60870_asdu.ioa"]
expected_hashes = dict(line.split()[::-1] for line in
                       (CAPTURES / "SHA256SUMS").read_text().splitlines())
records = []
for name, frames in SELECTIONS.items():
    pcap = CAPTURES / (name + ".pcap")
    digest = hashlib.sha256(pcap.read_bytes()).hexdigest()
    assert digest == expected_hashes[pcap.name]
    args = ["/opt/homebrew/bin/tshark", "-r", str(pcap), "-Y",
            " || ".join(f"frame.number=={n}" for n in frames), "-T", "fields"]
    for field in FIELDS:
        args.extend(["-e", field])
    output = subprocess.run(args, check=True, capture_output=True, text=True,
                            timeout=30).stdout
    rows = output.splitlines()
    assert len(rows) == len(frames)
    for row in rows:
        frame, wire, type_id, cause, oa, ca, ioas = row.split("\t")
        assert wire
        record = dict(capture=pcap.name, frame=int(frame), sha256=digest, hex=wire)
        if type_id:
            record.update(type=int(type_id), cause=int(cause), originator=int(oa),
                          common_address=int(ca), addresses=[int(n) for n in ioas.split(",")])
        records.append(record)
Path(__file__).with_name("golden.json").write_text(json.dumps(records, indent=2) + "\n")
