"""Deterministic range-clock inputs for the G22 reducer and stubbed walk."""

import importlib.util
import json
from pathlib import Path
import sys


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


HERE = Path(__file__).parent
packets = load("g22_packets", HERE / "make_g22_chain_pcap.py")
EVENT_TIMES = {
    "baseline-field": 0, "baseline-cc": 0.1, "start": 1,
    "weak-command": 2.2, "consequence-field": 3.5, "consequence-cc": 3.6,
    "end": 4, "decoded": 4.1, "restore-start": 5, "restoration-field": 6,
    "restoration-cc": 6.1, "improved-apply": 7, "improved-control-before": 8,
    "improved-start": 8.5, "improved-command": 18.6, "improved-held-field": 18.7,
    "improved-end": 18.8, "improved-control-after": 20, "completed": 21,
}


def utc(seconds):
    return f"1970-01-01T00:00:{seconds:012.9f}Z"


def seed(directory):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)

    def write(name, obj):
        (directory / f"G22-{name}.json").write_text(json.dumps(obj) + "\n")

    write("context", {"attempt_id": "offline-attempt", "scenario": "eu-hardening-configurations"})
    with (directory / "G22-events.jsonl").open("w") as events:
        for name, seconds in EVENT_TIMES.items():
            events.write(json.dumps({"event": name, "at": utc(seconds)}) + "\n")
    (directory / "clock-lines").write_text(
        "\n".join(utc(v) for v in EVENT_TIMES.values()) + "\n")
    baseline = json.loads((HERE / "fixtures/field-baseline.json").read_text())
    for name, revision, seq, solved, supplied in (
        ("baseline-field", 9, 23, 0, True), ("consequence-field", 10, 24, 3.2, False),
        ("restoration-field", 11, 25, 5.5, True),
        ("improved-held-field", 11, 26, 18.7, True),
    ):
        state = json.loads(json.dumps(baseline))
        device = state["devices"]["breaker-1"]
        device.update(revision=revision, position="closed" if supplied else "open",
                      changed_at=utc(solved))
        process = state["process"]
        process.update(solve_seq=seq, solved_at=utc(solved))
        process["buses"]["s1"]["live"] = supplied
        process["devices"]["feeder-breaker"].update(revision=revision, solved_revision=revision)
        write(name, state)
    baseline_cc = json.loads((HERE / "fixtures/operator-gi-untimed.json").read_text())
    for seq, (name, receipt, since) in enumerate((
        ("baseline-cc", 0, -10), ("consequence-cc", 3.4, -10),
        ("restoration-cc", 5.8, -10), ("improved-control-before", 7.7, 7.5),
        ("improved-control-after", 19.2, 19),
    ), 1):
        state = json.loads(json.dumps(baseline_cc))
        state.update(package_id="eu-iec104-substation", range_generation="range-42",
                     snapshot_seq=seq, rendered_at=utc(EVENT_TIMES[name]))
        state["link"].update(state="started", connection_seq=1 if seq < 4 else seq - 2,
                             since="1969-12-31T23:59:50Z" if since < 0 else utc(since))
        gi = state["link"]["gi"]
        gi.update(state="complete", started_at=(
            "1969-12-31T23:59:51Z" if since < 0 else utc(since + 0.1)))
        for point in state["points"]:
            point.update(received_at=utc(receipt), stale_reason=None, source_time=None, cot=3)
            point["quality"] = {q: False for q in ("iv", "nt", "sb", "bl", "ov")}
            if point["kind"] not in ("float", "step-position"):
                point["quality"]["ov"] = None
            if point["id"] == "feeder-breaker-position":
                point["value"] = "open" if name == "consequence-cc" else "closed"
            if point["id"] == "feeder-protection-trip":
                point["value"] = False
            if point["id"] == "feeder-current" and name == "consequence-cc":
                # Old analog receipt is evidence, not fresh position feedback.
                point["received_at"] = utc(0)
        write(name, state)
    write("weak-command", {"exit_code": 0, "stdout": '{"result":"completed"}', "stderr": ""})
    write("improved-command", {
        "exit_code": 2, "stdout": "",
        "stderr": "connect failed: dial tcp 10.30.30.20:2404: i/o timeout",
    })
    write("improved-apply", {"containd": {"applyId": "fixture-apply", "protection": "protecting"}})
    packets.generate(directory / "G22-weak.pcap", "good")
    (directory / "G22-weak-tcpdump.log").write_text(
        "3 packets captured\n3 packets received by filter\n0 packets dropped by kernel\n")


if __name__ == "__main__":
    seed(sys.argv[1])
