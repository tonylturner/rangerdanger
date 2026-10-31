#!/usr/bin/env python3
"""Reviewed YAML command inventory and its live execution/oracle adapter.

No Docker calls during extraction, pinning, validation, or unit tests.
--execute is the only live entry point; the shell gate owns policy changes.
"""
import argparse
import json
import re
import selectors
import subprocess
import sys
import time
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
EXPECTATIONS = ROOT / "scripts/lab-commands-expectations.json"

# Every reviewed oracle field the gate reads; anything else is a typo or a
# retired mechanism that would otherwise be ignored silently.
ORACLE_KEYS = {"intent", "match", "control_match", "nmap", "file",
               "zero_rc_denial", "policy", "state", "semantic", "eu_device", "peer"}

# Shared offline parser, also exercised against the UI in command-smoke-parity.test.ts.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from lab_commands_inventory import (US, extract, inventory, packages, scenario_files,
                                    extract_step_commands, infer_node_per_command)


def should_skip(cmd):
    return bool(re.search(r"/api/firewall/apply|^docker |^ssh\b", cmd))


BAD_OUTPUT = re.compile(
    r"unknown command|command not found|illegal (?:function|data)|"
    r"usage:|authentication failed|"
    r"no such file|permission denied", re.I)
FIREWALL_DENIAL = re.compile(r"(?:timed? out|timeout|filtered)", re.I)
NO_ACK_STATE_SEND = re.compile(r"Result: SENT \(no acknowledgement requested\)", re.I)
NON_DENIAL_OUTPUT = re.compile(r"(?:connection refused|no route to host)", re.I)


def key(row):
    return f'{row["scenario"]}|{row["step"]}|{row["command"]}|{row.get("occurrence", 1)}'


def validate_inventory(rows, data, target="", root=ROOT, package_id=US):
    if not rows or len({key(r) for r in rows}) != len(rows):
        raise ValueError("empty or duplicate command inventory")
    actual = inventory(rows, root, package_id)
    expected = data["inventory"]
    scenarios = [target] if target else list(expected)
    if not target and actual != expected:
        raise ValueError(f"inventory changed: expected {expected}, got {actual}")
    for scenario in scenarios:
        if actual[scenario] != expected[scenario]:
            raise ValueError(f"{scenario}: command counts changed")
    contracts = data["commands"]
    selected = {k for k in contracts if not target or k.startswith(target + "|")}
    if {key(r) for r in rows} != selected:
        raise ValueError("command inventory changed; review oracles and re-pin")
    for row in rows:
        contract = contracts[key(row)]
        for field in ("container", "policy", "prompt", "entry", "action"):
            if contract[field] != row[field]:
                raise ValueError(f"{key(row)}: {field} changed; review and re-pin")
        if row["action"] == "SKIP":
            if not contract.get("reason"):
                raise ValueError(f"{key(row)}: missing skip reason")
            continue
        oracle = contract.get("oracle") or {}
        if oracle.get("intent") not in ("read", "deny", "capture", "cli"):
            raise ValueError(f"{key(row)}: missing reviewed oracle")
        unknown = set(oracle) - ORACLE_KEYS
        if unknown:
            raise ValueError(f"{key(row)}: unknown oracle key {sorted(unknown)}")
        if oracle["intent"] != "capture":
            re.compile(oracle["match"])
        if oracle["intent"] == "deny":
            re.compile(oracle["control_match"])
        if "policy" in oracle and (
                oracle["intent"] != "deny" or oracle["policy"] != "hardened"):
            raise ValueError(f"{key(row)}: denial end-state override must be hardened")
        if oracle["intent"] == "capture" and not oracle.get("file"):
            raise ValueError("capture needs a file")
    minimum = sum(expected[s]["RUN"] for s in scenarios)
    if not target and minimum != data["minimum_executed"]:
        raise ValueError("minimum_executed disagrees with inventory")
    return minimum


def verdict(oracle, rc, stdout, stderr, *, control=False, frames=None):
    combined = stdout + "\n" + stderr
    if BAD_OUTPUT.search(combined) or "host execution timeout" in combined:
        return False
    intent = oracle["intent"]
    if intent == "capture":
        return rc in (0, 124, 130, 143) and frames is not None and frames > 0
    if intent == "deny":
        # nmap returns zero even for filtered ports; its semantic output
        # (not its return code) is the denial oracle. A refusal means the
        # packet reached the host, and "no route" is not firewall evidence.
        if NON_DENIAL_OUTPUT.search(combined):
            return False
        allowed_rc = (0, 1, 2, 124, 143) if oracle.get("zero_rc_denial") else (1, 2, 124, 143)
        if oracle.get("nmap"):
            allowed_rc = (0,)
        matched = bool(re.search(oracle["match"], combined, re.M))
        network_denial = bool(FIREWALL_DENIAL.search(combined))
        # The separate no-ack state oracle proves the weak write changed
        # device state and the hardened attempt did not. Its tool reports
        # "sent" without waiting for an application acknowledgement; that
        # is valid only alongside the out-of-band state comparison.
        state_denial = bool(
            oracle.get("state") and oracle.get("zero_rc_denial")
            and NO_ACK_STATE_SEND.search(combined))
        return control and rc in allowed_rc and matched and (network_denial or state_denial)
    return (rc == 0 and not re.search(r"(?:read|write).*failed", combined, re.I)
            and bool(re.search(oracle["match"], stdout, re.M)))


def shell_command(command):
    """Keep head's visible output, but drain input so pipefail tests the reader.

    Early head exit can make tshark return 16 (broken pipe). sed without `q`
    reads the entire PCAP, preserving missing/corrupt-file and late failures
    rather than allowing an arbitrary producer error code.
    """
    return re.sub(r"\|\s*head -(\d+)\s*$", r"| sed -n '1,\1p'", command)


def invocation(row, timeout):
    command = shell_command(row["command"]) if row["prompt"] == "shell" else row["command"]
    prefix = ["docker", "exec", "-i", row["container"], "timeout",
              "-s", "INT", str(timeout)]
    if row["container"] == "rangerdanger-firewall":
        if row["prompt"] == "cli":
            return prefix + ["containd", "cli"], command + "\nexit\n"
        # Model the actual terminal's first prompt and only the documented
        # transition. Do not execute Linux tools behind the student's back.
        return (prefix + ["bash", "-c",
                         "containd cli; printf '__RD_SHELL_READY__\\n'; exec bash -o pipefail"],
                row["entry"] + "\n" + command + "\nexit\n")
    return prefix + ["bash", "-o", "pipefail", "-c", command], None


def run_staged_shell(argv, row, timeout):
    """Do not let the CLI's buffered reader swallow subsequent bash input."""
    with subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE) as process:
        process.stdin.write((row["entry"] + "\n").encode())
        process.stdin.flush()
        prefix = b""
        errors = b""
        marker = b"__RD_SHELL_READY__\n"
        deadline = time.monotonic() + timeout + 5
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            selector.register(process.stderr, selectors.EVENT_READ)
            while marker not in prefix and time.monotonic() < deadline:
                for entry, _ in selector.select(0.1):
                    chunk = entry.fileobj.read1(65536)
                    if not chunk:
                        selector.unregister(entry.fileobj)
                    elif entry.fileobj is process.stdout:
                        prefix += chunk
                    else:
                        errors += chunk
                if process.poll() is not None:
                    break
        if marker not in prefix:
            process.kill()
            out, err = process.communicate()
            return subprocess.CompletedProcess(
                argv, 125, (prefix + out).decode(errors="replace"),
                (errors + err).decode(errors="replace") + "\ndocumented shell transition failed")
        try:
            out, err = process.communicate((shell_command(row["command"]) + "\nexit\n").encode(),
                                           timeout=timeout + 10)
        except subprocess.TimeoutExpired:
            process.kill()
            out, err = process.communicate()
            return subprocess.CompletedProcess(
                argv, 124, (prefix + out).decode(errors="replace"),
                (errors + err).decode(errors="replace") + "\nhost execution timeout")
        return subprocess.CompletedProcess(
            argv, process.returncode, (prefix + out).decode(errors="replace"),
            (errors + err).decode(errors="replace"))


def execute_command(row, oracle, directory, timeout, positive=False, control=False):
    """Keep separate stdout/stderr files even on a timeout or exec failure."""
    directory.mkdir(parents=True, exist_ok=True)
    stem = directory / ("control" if positive else "command")
    if positive:
        oracle = {"intent": "read", "match": oracle["control_match"]}
    argv, stdin = invocation(row, timeout)
    try:
        if row["container"] == "rangerdanger-firewall" and row["prompt"] == "shell":
            result = run_staged_shell(argv, row, timeout)
        else:
            result = subprocess.run(argv, input=stdin, text=True, capture_output=True,
                                    timeout=timeout + 10)
        rc, out, err = result.returncode, result.stdout, result.stderr
    except subprocess.TimeoutExpired as exc:
        rc = 124
        out = (exc.stdout or b"").decode(errors="replace")
        err = (exc.stderr or b"").decode(errors="replace") + "\nhost execution timeout"
    stem.with_suffix(".stdout").write_text(out)
    stem.with_suffix(".stderr").write_text(err)
    frames = None
    if oracle["intent"] == "capture":
        # tcpdump ships in the firewall; tshark is not guaranteed there.
        # Decoding with tcpdump -r validates PCAP rather than trusting stat().
        decoded = subprocess.run(
            ["docker", "exec", row["container"], "timeout", "5", "tcpdump",
             "-nn", "-r", oracle["file"]], capture_output=True, text=True, timeout=10)
        (directory / "capture.stdout").write_text(decoded.stdout)
        (directory / "capture.stderr").write_text(decoded.stderr)
        frames = len(decoded.stdout.splitlines()) if decoded.returncode == 0 else 0
    passed = verdict(oracle, rc, out, err, control=control, frames=frames)
    if oracle["intent"] == "cli" and oracle.get("file"):
        exported = subprocess.run(
            ["docker", "exec", row["container"], "cat", oracle["file"]],
            capture_output=True, text=True, timeout=10)
        (directory / "export.stdout").write_text(exported.stdout)
        (directory / "export.stderr").write_text(exported.stderr)
        try:
            document = json.loads(exported.stdout)
            passed = passed and exported.returncode == 0 and isinstance(document["firewall"], dict)
        except (ValueError, KeyError, TypeError):
            passed = False
    (directory / ("control.json" if positive else "result.json")).write_text(
        json.dumps({"key": key(row), "rc": rc, "passed": passed, "frames": frames}))
    print(out, end="")
    print(err, end="", file=sys.stderr)
    print(f'{"PASS" if passed else "FAIL"} rc={rc}: {key(row)}')
    return passed


def device_request(state, directory, phase, reset=False):
    """Out-of-band observer, never a replacement for the documented attack."""
    argv = ["docker", "exec", state["container"], "timeout", "5",
            "wget", "-qO-"]
    if reset:
        argv += ["--header=Content-Type: application/json",
                 "--post-data=" + json.dumps(state["reset"])]
    argv += ["http://127.0.0.1:8080/api/" + ("command" if reset else "state")]
    response = subprocess.run(argv, capture_output=True, text=True, timeout=10)
    (directory / (phase + ".stdout")).write_text(response.stdout)
    (directory / (phase + ".stderr")).write_text(response.stderr)
    if response.returncode != 0:
        raise ValueError(f"state observer failed: {response.stderr}")
    document = json.loads(response.stdout)
    if reset and document.get("result") != "executed":
        raise ValueError(f"state reset failed: {document}")
    return document


def execute(row, oracle, directory, timeout, positive=False, control=False):
    state = oracle.get("state")
    if not state:
        return execute_command(row, oracle, directory, timeout, positive, control)
    directory.mkdir(parents=True, exist_ok=True)
    phase = "control" if positive else "command"
    # No-Ack sends can return rc=0 both when delivered and when dropped. Prove
    # the weak control changed the device, then prove the denied send did not.
    # Reset via the device-local HTTP observer and fail on dirty cleanup.
    passed = False
    try:
        device_request(state, directory, phase + "-prepare", reset=True)
        initial = device_request(state, directory, phase + "-before")
        if initial[state["field"]] is not state["initial"]:
            raise ValueError("state precondition did not take effect")
        passed = execute_command(row, oracle, directory, timeout, positive, control)
        time.sleep(1)  # Allow the once-only send to reach the device.
        actual = device_request(state, directory, phase + "-after")
        expected = state["allowed"] if positive else state["denied"]
        passed = passed and actual[state["field"]] is expected
    finally:
        device_request(state, directory, phase + "-restore", reset=True)
        restored = device_request(state, directory, phase + "-restored")
        if restored[state["field"]] is not state["initial"]:
            raise ValueError("state oracle cleanup failed")
    result_path = directory / ("control.json" if positive else "result.json")
    result = json.loads(result_path.read_text())
    result.update(passed=passed, state_actual=actual[state["field"]], state_expected=expected)
    result_path.write_text(json.dumps(result))
    print(f'{"PASS" if passed else "FAIL"} device-state oracle: {key(row)}')
    return passed



def expectations_path(root, package_id):
    suffix = "" if package_id == US else "-" + package_id
    return Path(root) / f"scripts/lab-commands-expectations{suffix}.json"


def check_package(root, package_id):
    try:
        rows = extract(root, package_id=package_id)
        data = json.loads(expectations_path(root, package_id).read_text())
        return validate_inventory(rows, data, root=root, package_id=package_id)
    except (ValueError, KeyError, OSError) as exc:
        raise ValueError(f"{package_id}: {exc}. Add/update {expectations_path(root, package_id).name}: "
                         "include every scenario (even zero-command scenarios) and every command; "
                         "RUN needs a reviewed oracle, SKIP needs a reviewed reason. "
                         "Use --repin --package <package>, review all changes, then rerun --guard. "
                         "See docs/lab-authoring.md.") from exc

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", nargs="?", default="")
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--repin", action="store_true",
                        help="update counts/metadata, preserve only exact-key oracles; review new null oracles")
    parser.add_argument("--matrix", action="store_true")
    parser.add_argument("--guard", action="store_true", help="validate every package offline")
    parser.add_argument("--package", default=US)
    parser.add_argument("--execute", type=int, metavar="ROW")
    parser.add_argument("--positive-control", action="store_true")
    parser.add_argument("--control-passed", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--timeout", type=int, default=10)
    args = parser.parse_args()
    if args.target in packages(args.root):
        args.package, args.target = args.target, ""
    if args.guard:
        for package_id in packages(args.root):
            check_package(args.root, package_id)
        print("All package scenario command contracts reviewed and pinned")
        return
    rows = extract(args.root, args.target, args.package)
    expectations = expectations_path(args.root, args.package)
    if expectations.exists():
        data = json.loads(expectations.read_text())
    elif args.repin:
        data = {"schema": 1, "commands": {}}
    else:
        raise ValueError(f"missing {expectations}; run --guard for author instructions")
    if args.repin:
        if args.target:
            raise ValueError("--repin requires the complete inventory")
        previous = data["commands"]
        data["inventory"] = inventory(rows, args.root, args.package)
        data["minimum_executed"] = sum(r["action"] == "RUN" for r in rows)
        data["commands"] = {
            key(row): {**{f: row[f] for f in (
                "container", "policy", "prompt", "entry", "action")},
                "oracle": previous.get(key(row), {}).get("oracle"),
                "reason": previous.get(key(row), {}).get("reason")}
            for row in rows}
        expectations.write_text(json.dumps(data, indent=2) + "\n")
        return
    minimum = validate_inventory(rows, data, args.target, args.root, args.package)
    if args.execute is not None:
        if args.output is None:
            raise ValueError("--execute needs --output")
        row = rows[args.execute]
        if row["action"] != "RUN":
            raise ValueError("cannot execute a skipped row")
        oracle = data["commands"][key(row)]["oracle"]
        if oracle.get("file"):
            # Never accept a stale capture/export as this run's evidence.
            subprocess.run(["docker", "exec", row["container"], "rm", "-f",
                            oracle["file"]], check=True, timeout=10)
        passed = execute(row, oracle, args.output, args.timeout,
                         args.positive_control, args.control_passed)
        sys.exit(0 if passed else 1)
    elif args.matrix:
        for idx, row in enumerate(rows):
            oracle = data["commands"][key(row)].get("oracle") or {}
            intent = oracle.get("intent", "skip")
            # Manual intermediate policy edits are deliberately not automated.
            # A reviewed deny probe can instead use the reference end state;
            # its positive control still runs against weak in the shell gate.
            policy = oracle.get("policy", row["policy"])
            print("|".join(str(x) for x in (
                idx, row["scenario"], row["step"], policy, row["action"], intent)))
        print(f"minimum executed: {minimum}", file=sys.stderr)
    else:
        print(json.dumps(rows, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, yaml.YAMLError, subprocess.SubprocessError) as exc:
        print(f"lab command gate: {exc}", file=sys.stderr)
        sys.exit(1)
