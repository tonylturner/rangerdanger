"""EU node-exec and independent semantic/audit evidence."""
import json
import os
import re
import subprocess
import time
from pathlib import Path
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
EU = "eu-iec104-substation"


def request(api, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = Request(api + path, data=data, headers={"Content-Type": "application/json"})
    with urlopen(req, timeout=40) as response:
        return json.load(response)


def semantic(kind, stdout):
    """No exit-only success for GI, register reads, or structured API output."""
    if not kind or kind == "pcap-filter":
        return True  # empty filters mean NOT OBSERVED; the PCAP is decoded separately
    if kind == "registers":
        values = re.findall(
            r"^\s*\[(\d+)\]:[ \t]*(-?\d+)(?:[ \t]+\(-?\d+\))?[ \t]*$", stdout, re.M)
        return [int(index) for index, _ in values] == list(range(11))
    if kind == "io-stat":
        rows = re.findall(r"^\|\s*\d+(?:\.\d+)?\s*<>\s*\d+(?:\.\d+)?\s*\|\s*(\d+)", stdout, re.M)
        return bool(rows) and sum(map(int, rows)) > 0
    try:
        if kind == "gi":
            events = [json.loads(line) for line in stdout.splitlines()]
            # The one-shot CLI emits result/gi/ok only after GI ACT-TERM.
            return any(e.get("event") == "result" and e.get("op") == "gi"
                       and e.get("status") == "ok" for e in events)
        value = json.loads(stdout)
        if kind == "historian":
            return isinstance(value, list) and bool(value) and all(
                isinstance(p.get("id"), str) and isinstance(p.get("ioa"), int) and p.get("last")
                for p in value)
        if kind == "reconnect":
            return value["trigger"] == "operator" and bool(value["requested_at"])
    except (ValueError, KeyError, TypeError, AttributeError):
        return False
    raise ValueError(f"unknown EU semantic oracle {kind}")


def audit_verdict(before, after, initial, final, peer, allowed):
    """Require a fresh, continuous audit generation and source-attributed write."""
    cursor, latest = before["latest_seq"] or 0, after["latest_seq"] or 0
    if (not (before["generation"] == after["generation"] ==
             initial["generation"] == final["generation"]) or
            (after["oldest_seq"] or 0) > cursor + 1 or latest < cursor or
            [e["seq"] for e in after["entries"]] != list(range(cursor + 1, latest + 1))):
        return False
    writes = [e for e in after["entries"] if e["seq"] > cursor
              and e.get("context", {}).get("protocol") == "modbus"
              and e.get("context", {}).get("peer", "").startswith(peer)
              and e.get("result") == "accepted"]
    if allowed:
        return (len(writes) == 1 and writes[0]["operation"] == "open"
                and writes[0]["after"]["position"] == "open"
                and initial["position"] == "closed" and final["position"] == "open"
                and final["revision"] > initial["revision"])
    return (not writes and final["position"] == initial["position"] == "closed"
            and final["revision"] == initial["revision"])


class EUAdapter:
    def __init__(self, api, directory):
        self.api = api
        self.directory = Path(directory).resolve()
        self.directory.mkdir(parents=True, exist_ok=True)

    def helper(self, *args):
        with (self.directory / "lifecycle.log").open("a") as log:
            subprocess.run(
                ["bash", (ROOT / "scripts/lib/lab-commands-eu.sh").as_posix(),
                 self.directory.as_posix(), *args], check=True, timeout=180,
                stdout=log, stderr=subprocess.STDOUT,
                env={**os.environ, "RANGERDANGER_API": self.api})

    def management(self, node, endpoint, directory, phase):
        path = directory / (phase + ".json")
        self.helper("management", node, endpoint, path.as_posix())
        return json.loads(path.read_text())

    def decode(self, directory, container, path):
        result = subprocess.run(
            ["docker", "exec", container, "timeout", "5", "tcpdump", "-nn", "-r", path],
            text=True, capture_output=True, timeout=10)
        (directory / "capture.stdout").write_text(result.stdout)
        (directory / "capture.stderr").write_text(result.stderr)
        return len(result.stdout.splitlines()) if result.returncode == 0 else 0

    def execute(self, gate, row, oracle, directory, timeout, positive=False, control=False):
        directory.mkdir(parents=True, exist_ok=True)
        phase = "control" if positive else "command"
        effective = {"intent": "read", "match": oracle["control_match"]} if positive else oracle
        device = oracle.get("eu_device")
        if device:
            initial = self.management(device, "/api/state", directory, phase + "-before")
            before = self.management(device, "/api/audit?after=0", directory, phase + "-audit-before")
        if oracle["intent"] == "capture":
            subprocess.run(["docker", "exec", row["container"], "rm", "-f", oracle["file"]],
                           check=True, timeout=10)
            self.helper("traffic")
        response = request(self.api, f'/api/workshop/nodes/{row["node"]}/exec',
                           {"command": row["command"], "timeout_sec": max(timeout, 25)})
        out, err, rc = response["stdout"], response["stderr"], response["exit_code"]
        (directory / (phase + ".stdout")).write_text(out)
        (directory / (phase + ".stderr")).write_text(err)
        (directory / (phase + "-exec.json")).write_text(json.dumps(response, indent=2))
        frames = None
        if oracle["intent"] == "capture":
            frames = self.decode(directory, row["container"], oracle["file"])
        passed = gate.verdict(effective, rc, out, err, control=control, frames=frames)
        # Unlike legacy US timed captures, the rotating EU capture must finish.
        if oracle["intent"] == "capture":
            passed = passed and rc == 0
        if not positive:
            passed = passed and semantic(oracle.get("semantic"), out)
        if oracle.get("semantic") == "pcap-filter":
            # Empty matching output is meaningful only against a decodable,
            # nonempty capture from this run, not a missing or corrupt file.
            passed = passed and self.decode(
                directory, "rangerdanger-firewall", "/data/captures/baseline.pcap") > 0
        if device:
            allowed = positive or oracle["intent"] != "deny"
            deadline = time.monotonic() + 5
            while True:
                final = self.management(device, "/api/state", directory, phase + "-after")
                if not allowed or final["position"] == "open" or time.monotonic() >= deadline:
                    break
                time.sleep(0.2)
            if not allowed:
                time.sleep(1)
                final = self.management(device, "/api/state", directory, phase + "-after")
            after = self.management(device, f'/api/audit?after={before["latest_seq"] or 0}',
                                    directory, phase + "-audit-after")
            passed = passed and audit_verdict(before, after, initial, final, oracle["peer"], allowed)
        if passed and oracle.get("semantic") == "reconnect":
            self.helper("reconnect-check", json.loads(out)["requested_at"])
        (directory / ("control.json" if positive else "result.json")).write_text(
            json.dumps({"key": gate.key(row), "passed": bool(passed), "rc": rc, "frames": frames}))
        print(f'{"PASS" if passed else "FAIL"} rc={rc}: {gate.key(row)}')
        return passed
