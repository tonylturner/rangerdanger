# SPDX-License-Identifier: Apache-2.0
"""Sequential isolated role comparisons, with capture and bounded barriers."""

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time

import compare
import malformed
from process_utils import run as run_owned
from runtime import Docker, unused_subnet
from scenario import CHECK, HERE, load, point_map, roles, scenario


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def terminate(_signum, _frame):
    raise KeyboardInterrupt("terminated; cleaning owned Docker resources")


def side(case_path, role_images, reference, out, subnet):
    out.mkdir(parents=True, exist_ok=False)
    case = scenario(case_path)
    prefix = subnet.rsplit(".", 1)[0]
    shutil.copyfile(case_path, out / "scenario.json")
    shutil.copyfile(reference, out / "baseline.json")
    (out / "points.json").write_text(json.dumps(point_map(subnet, case["point_map"]), indent=2) + "\n")
    docker = Docker(out)
    capture = None
    logs = {}
    failure = None
    try:
        image_ids = {
            key: docker.call("image", "inspect", "--format", "{{.Id}}", image).strip()
            for key, image in role_images.items()
        }
        baseline = load(reference)
        for role, image in role_images.items():
            if image == baseline.get("image") and image_ids[role] != baseline["image_id"]:
                raise ValueError("reference image tag no longer matches its sealed ID")
        (out / "provenance.json").write_text(json.dumps({
            "images": role_images, "image_ids": image_ids, "subnet": subnet,
            "scenario_sha256": digest(case_path),
            "point_map_sha256": digest(out / "points.json"),
            "harness_sha256": {path.name: digest(path) for path in HERE.glob("*.py")},
        }, indent=2) + "\n")
        # Execute the inspected immutable IDs, not tags that could be retagged
        # between provenance collection and container creation.
        role_images = image_ids
        network = docker.network(subnet)
        tools = docker.helper_image()
        mounts = [(HERE, "/h", True), (CHECK, "/check", True), (out, "/e", False)]
        fixture = docker.create(
            "fixture", role_images["peer"], network, prefix + ".20",
            ["python3", "/h/fixture.py"], mounts, ["--entrypoint", ""])
        logs[fixture] = "fixture.log"
        docker.start(fixture)
        if case["kind"] != "raw-cc":
            rtu = docker.create(
                "rtu", role_images["rtu"], network, prefix + ".10",
                ["iec104-rtu", "--config", "/e/points.json", "--bind", prefix + ".10", "--port", "2404"],
                mounts, ["--entrypoint", ""],
            )
            logs[rtu] = "rtu.log"
            docker.start(rtu)
            docker.call("exec", fixture, "python3", "/h/driver.py", "ready", "--prefix", prefix, timeout=25)
            capture = docker.capture(rtu, tools, case["capture_modbus"])
        if case["kind"] == "raw-rtu":
            peer = docker.create(
                "peer", role_images["peer"], network, prefix + ".30",
                ["python3", "/h/driver.py", "client", "--prefix", prefix],
                mounts, ["--entrypoint", ""],
            )
            logs[peer] = "peer.log"
            docker.start(peer)
            docker.wait(peer, case["timeout_s"] + 10)
        elif case["kind"] == "cmd-rtu":
            for index, action in enumerate(case["actions"]):
                if action["op"] == "gi":
                    command = ["iec104cmd", prefix + ".10", "monitor", "--json", "--hold", "1"]
                elif action["op"] == "sbo":
                    command = ["iec104cmd", prefix + ".10", "dc", "3001",
                               "close" if action["args"]["value"] == 2 else "open", "-sbo"]
                else:
                    raise ValueError("unsupported command-role action")
                client = docker.create(
                    "cmd-" + str(index), role_images["cmd"], network, prefix + ".30",
                    command, mounts, ["--entrypoint", ""],
                )
                logs[client] = "cmd-" + str(index) + ".log"
                docker.start(client)
                docker.wait(client, action["timeout_s"])
        else:
            if case["kind"] == "raw-cc":
                peer = docker.create(
                    "peer", role_images["peer"], network, prefix + ".10",
                    ["python3", "/h/driver.py", "server", "--prefix", prefix],
                    mounts, ["--entrypoint", ""],
                )
                logs[peer] = "peer.log"
                docker.start(peer)
                deadline = time.monotonic() + 10
                while "LISTENING" not in docker.call("logs", peer):
                    if time.monotonic() >= deadline:
                        raise TimeoutError("scripted server not ready")
                    time.sleep(0.05)
                capture = docker.capture(peer, tools, case["capture_modbus"])
            cc = docker.create(
                "cc", role_images["cc"], network, prefix + ".30",
                ["iec104-cc", "--config", "/e/points.json", "--bind", prefix + ".30",
                 "--target", prefix + ".10:2404", "--http", prefix + ".30:8080"],
                mounts, ["--entrypoint", ""],
            )
            logs[cc] = "cc.log"
            docker.start(cc)
            if case["kind"] == "raw-cc":
                deadline = time.monotonic() + case["timeout_s"]
                result = out / "peer-result.json"
                while not result.exists():
                    if time.monotonic() >= deadline:
                        raise TimeoutError("scripted server completion deadline")
                    time.sleep(0.05)
                if not load(result)["passed"]:
                    raise RuntimeError("scripted server failed")
                docker.call("exec", fixture, "python3", "/h/driver.py", "cc-ready",
                            "--prefix", prefix, timeout=25)
            else:
                if [action["op"] for action in case["actions"]] != ["gi"]:
                    raise ValueError("CC/RTU scenario currently supports GI barrier only")
                docker.call("exec", fixture, "python3", "/h/driver.py", "cc-ready",
                            "--prefix", prefix, timeout=25)
            docker.call("stop", "-t", "5", cc)
            docker.wait(cc, 10)
            if case["kind"] == "raw-cc":
                deadline = time.monotonic() + 10
                while not (out / "peer-closed").exists():
                    if time.monotonic() >= deadline:
                        raise TimeoutError("scripted server close barrier")
                    time.sleep(0.02)
                to_finish, capture = capture, None
                docker.finish_capture(to_finish)
                (out / "capture-stopped").touch()
                docker.wait(peer, 10)
    except BaseException as error:
        failure = error
    finally:
        errors = []
        if capture:
            try:
                docker.finish_capture(capture)
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                errors.append(str(error))
        for identity, name in logs.items():
            try:
                docker.logs(identity, name)
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                errors.append(str(error))
        try:
            docker.cleanup()
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            errors.append(str(error))
        if failure or errors:
            message = str(failure) + ("; " + "; ".join(errors) if errors else "")
            (out / "error.json").write_text(json.dumps({"error": message}) + "\n")
            if failure:
                raise failure
            raise RuntimeError(message)


def run_case(path, candidate_path, baseline_roles, candidate_roles, reference, out, tshark,
             *, role_set=None, allowlist=HERE / "allowlist.json"):
    case = scenario(path)
    # Select once; each side rechecks overlap immediately before creation.
    inspect = Docker(out / case["id"] / "inventory")
    subnet = unused_subnet(inspect.inventory())
    for name, source, images in (("baseline", path, baseline_roles),
                                 ("candidate", candidate_path or path, candidate_roles)):
        side(source, images, reference, out / case["id"] / name, subnet)
        shutil.copyfile(out / "tshark-version.txt", out / case["id"] / name / "tshark-version.txt")
    output = out / case["id"]
    command = [
        sys.executable, str(HERE / "compare.py"), str(output / "baseline/capture.pcap"),
        str(output / "candidate/capture.pcap"), "--out", str(output / "comparison"),
        "--tshark", tshark, "--timeout", "180",
        "--allowlist", str(allowlist), "--scenario-id", case["id"],
    ]
    declarations = case.get("malformed", [])
    if declarations:
        resolved = malformed.resolve(declarations,
            load(output / "baseline/peer-result.json")["associations"],
            load(output / "candidate/peer-result.json")["associations"])
        declared_path = output / "declared-malformed.json"
        declared_path.write_text(json.dumps(resolved, indent=2) + "\n")
        command.extend(["--declared-malformed", str(declared_path)])
    if role_set is not None:
        command.extend(["--role-set", role_set])
    record = {"argv": command, "returncode": None, "timeout_s": 900}
    try:
        compared = run_owned(command, timeout=900)
        record.update(returncode=compared.returncode, stdout=compared.stdout, stderr=compared.stderr)
    finally:
        (output / "compare-command.json").write_text(json.dumps(record, indent=2) + "\n")
    print(compared.stdout, end="", flush=True)
    if compared.stderr:
        print(compared.stderr, file=sys.stderr, end="", flush=True)
    result = compared.returncode
    if result not in (0, 1):
        return 2
    report = load(output / "comparison/report.json")
    # A pair of equally empty/incomplete captures must never pass.
    for name in ("baseline", "candidate"):
        # tshark session count is independently asserted, not inferred from
        # successful process exit or the peer's own frame accounting.
        command = [tshark, "-r", str(output / name / "capture.pcap"),
                   "-Y", "tcp.port == 2404", "-T", "fields", "-e", "tcp.stream"]
        decoded = subprocess.run(command, capture_output=True, text=True, check=True, timeout=30)
        sessions = set(decoded.stdout.split())
        if len(sessions) != case["expected_sessions"]:
            raise RuntimeError(f"{name}: expected {case['expected_sessions']} sessions, got {len(sessions)}")
    return 0 if report["passed"] else 1


def run_cases(paths, candidate_path, baseline_roles, candidate_roles, reference, out, tshark,
              *, role_set=None, allowlist=HERE / "allowlist.json"):
    """Retain each result, including failed peers, without skipping the catalog."""
    results = []
    for path in paths:
        result = {"scenario": path.stem, "role_set": role_set, "exit_code": None}
        try:
            result["exit_code"] = run_case(path, candidate_path, baseline_roles,
                                          candidate_roles, reference, out, tshark,
                                          role_set=role_set, allowlist=allowlist)
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
            result.update(exit_code=2, error=str(error))
            print(f"{path.stem}: {error}", file=sys.stderr, flush=True)
        results.append(result)
        (out / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    return max(result["exit_code"] for result in results)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario", type=Path, nargs="?")
    parser.add_argument("--catalog", action="store_true")
    parser.add_argument("--candidate-scenario", type=Path, help="negative-control variant; single case only")
    parser.add_argument("--baseline", type=Path, required=True, help="strict JSON role-image map")
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--reference", type=Path, required=True, help="materialized baseline manifest")
    parser.add_argument("--role-set", help="exact comparison identity, e.g. all-go or go-rtu")
    parser.add_argument("--allowlist", type=Path, default=HERE / "allowlist.json")
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--tshark", default=shutil.which("tshark") or compare.DEFAULT_TSHARK)
    args = parser.parse_args()
    if args.catalog == bool(args.scenario) or (args.catalog and args.candidate_scenario):
        parser.error("choose one scenario or --catalog")
    signal.signal(signal.SIGTERM, terminate)
    try:
        baseline_roles, candidate_roles = roles(args.baseline), roles(args.candidate)
        reference = load(args.reference)
        if not isinstance(reference.get("image_id"), str) or not reference["image_id"].startswith("sha256:"):
            raise ValueError("reference manifest has no built image ID")
        paths = sorted((HERE / "scenarios").glob("*.json")) if args.catalog else [args.scenario]
        if not paths:
            raise ValueError("empty catalog")
        entries = compare.load_allowlist(args.allowlist)
        for path in paths:
            # Reject missing/invalid context before creating any Docker resources.
            compare.select_allowlist(entries, scenario(path)["id"], args.role_set)
        args.out.mkdir(parents=True, exist_ok=True)
        version = subprocess.run([args.tshark, "--version"], check=True, capture_output=True, text=True, timeout=10)
        (args.out / "tshark-version.txt").write_text(version.stdout)
        return run_cases(paths, args.candidate_scenario, baseline_roles,
                         candidate_roles, args.reference, args.out, args.tshark,
                         role_set=args.role_set, allowlist=args.allowlist)
    except (OSError, ValueError, RuntimeError, compare.InputError,
            subprocess.SubprocessError, KeyboardInterrupt) as error:
        print("run.py: " + str(error), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
