#!/usr/bin/env python3
"""Common semantic worker for PowerShell and the EU shell entry point.

Only main/run touch a live range. All inventory checks finish before preflight.
EU mutations use the existing shell lifecycle helpers, including on Windows
via a configured Bash environment with curl, jq and python3 on PATH.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

from containd_readiness import require_backend_protection
from lab_commands_eu import EU, EUAdapter, request
from lab_commands_inventory import ROOT, US, extract, packages

spec = importlib.util.spec_from_file_location("lab_command_gate", ROOT / "scripts/lib/lab-commands-extract.py")
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


class Lifecycle:
    def __init__(self, package_id, api, directory):
        self.package = package_id
        self.api = api
        self.eu = EUAdapter(api, directory) if package_id == EU else None

    def canary(self, allowed):
        port = 2404 if self.eu else 502
        deadline = time.monotonic() + 15
        while True:
            result = subprocess.run(
                ["docker", "exec", "rangerdanger-kali", "timeout", "1", "bash", "-c",
                 f"exec 3<>/dev/tcp/10.30.30.20/{port}"], timeout=5,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            # Docker/tool failures must never masquerade as dataplane denial.
            if result.returncode not in (0, 1, 124):
                raise ValueError(f"canary tool/exec failure: rc={result.returncode}")
            if (result.returncode == 0) == allowed:
                return
            if time.monotonic() >= deadline:
                raise ValueError(f"{self.package} canary did not reconcile: allow={allowed}")
            time.sleep(0.5)

    def policy(self, name):
        name = "improved" if name == "hardened" else name
        if name not in ("weak", "improved"):
            raise ValueError(f"unsupported policy {name}")
        if self.eu:
            self.eu.helper("policy", name)
        else:
            receipt = request(self.api, "/api/firewall/apply", {"config": name})
            require_backend_protection(receipt)
            active = request(self.api, "/api/firewall/active")
            if active.get("active_config") != name:
                raise ValueError(f"policy apply failed: expected {name}, got {active}")
        self.canary(name == "weak")

    def reset(self):
        if self.eu:
            self.eu.helper("reset")

    def execute(self, row, oracle, directory, timeout, positive=False, control=False):
        if self.eu:
            return self.eu.execute(gate, row, oracle, directory, timeout, positive, control)
        if oracle.get("file"):
            subprocess.run(["docker", "exec", row["container"], "rm", "-f", oracle["file"]],
                           check=True, timeout=10)
        return gate.execute(row, oracle, directory, timeout, positive, control)


def preflight(package_id, api):
    tools = ["docker"] + (["bash", "curl", "jq", "python3"] if package_id == EU else [])
    for tool in tools:
        if not shutil.which(tool):
            raise ValueError(f"missing required host tool: {tool}")
    request(api, "/api/health")
    status = request(api, "/api/range")
    if status.get("package") != package_id or status.get("phase") != "ready":
        raise ValueError(f"refusing wrong/non-ready range: expected {package_id}, got {status}")
    require_backend_protection(status)


def run(package_id, target, api, directory, timeout):
    rows = extract(target=target, package_id=package_id)
    data = json.loads(gate.expectations_path(ROOT, package_id).read_text())
    minimum = gate.validate_inventory(rows, data, target, package_id=package_id)
    if package_id not in (US, EU):
        raise ValueError(f"no reviewed live lifecycle adapter for {package_id}")
    preflight(package_id, api)
    lifecycle = Lifecycle(package_id, api, directory)
    directory = Path(directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    executed = passed = skipped = 0
    current = ""
    failed = False
    try:
        lifecycle.reset()
        for index, row in enumerate(rows):
            contract = data["commands"][gate.key(row)]
            if row["action"] == "SKIP":
                skipped += 1
                print(f'SKIP {gate.key(row)}: {contract["reason"]}')
                continue
            oracle = contract["oracle"]
            control = False
            destination = directory / str(index)
            if oracle["intent"] == "deny":
                # A fresh device reset precedes policy apply, never follows it.
                if oracle.get("eu_device"):
                    lifecycle.reset()
                lifecycle.policy("weak")
                current = "weak"
                if not lifecycle.execute(row, oracle, destination, timeout, positive=True):
                    failed = True
                    continue
                control = True
            if oracle.get("eu_device"):
                lifecycle.reset()
                current = ""
            policy = oracle.get("policy", row["policy"])
            if current != policy:
                lifecycle.policy(policy)
                current = policy
            executed += 1
            if lifecycle.execute(row, oracle, destination, timeout, control=control):
                passed += 1
            else:
                failed = True
    finally:
        # Cleanup errors propagate, never downgrade to a successful summary.
        cleanup_passed = False
        try:
            try:
                lifecycle.reset()
            finally:
                lifecycle.policy("weak")
            cleanup_passed = True
        finally:
            summary = dict(executed=executed, passed=passed, failed=executed - passed,
                           skipped=skipped, unverified=minimum - executed, minimum=minimum,
                           cleanup_passed=cleanup_passed)
            (directory / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
            print(" ".join(f"{k}={v}" for k, v in summary.items()))
    return int(failed or executed != minimum or passed != minimum)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", nargs="?", default="")
    parser.add_argument("--package", default=US)
    parser.add_argument("--timeout", type=int, default=int(os.environ.get("LAB_CMD_TIMEOUT", "10")))
    parser.add_argument("--output", type=Path,
                        default=Path(os.environ.get("LAB_CMD_LOG_DIR", "build/lab-commands")))
    args = parser.parse_args()
    if args.target in packages():
        args.package, args.target = args.target, ""
    api = os.environ.get("RANGERDANGER_API", "http://localhost:8088").rstrip("/")
    try:
        return run(args.package, args.target, api, args.output, args.timeout)
    except (ValueError, KeyError, OSError, subprocess.SubprocessError) as exc:
        print(f"lab command smoke: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
