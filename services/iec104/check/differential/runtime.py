# SPDX-License-Identifier: Apache-2.0
"""Deadline-bounded Docker resources with explicit ownership and evidence."""

import ipaddress
import json
from pathlib import Path
import re
import subprocess
import time
import uuid

from scenario import HERE
from process_utils import run


def unused_subnet(networks):
    used = [
        ipaddress.ip_network(item["Subnet"])
        for network in networks
        for item in network["IPAM"]["Config"] or []
        if item.get("Subnet")
    ]
    for third in range(110, 250):
        candidate = ipaddress.ip_network(f"172.29.{third}.0/24")
        if not any(candidate.overlaps(other) for other in used):
            return str(candidate)
    raise RuntimeError("no unused differential subnet")


class Docker:
    def __init__(self, out, token=None):
        self.out = Path(out)
        self.out.mkdir(parents=True, exist_ok=True)
        self.token = token or uuid.uuid4().hex[:12]
        self.prefix = "rd-iec104-diff-" + self.token
        self.label = "rd.iec104.differential=" + self.token
        self.images = []
        self.commands = []

    def call(self, *args, timeout=20, sealed=False):
        command = ["docker", *map(str, args)]
        started = time.monotonic()
        record = {"argv": command, "timeout_s": timeout}
        try:
            result = run(command, timeout, sealed)
            record.update(returncode=result.returncode, elapsed_s=time.monotonic() - started)
            if not sealed:
                record.update(stdout=result.stdout, stderr=result.stderr)
            if result.returncode != 0:
                raise RuntimeError(f"command failed ({result.returncode}): {' '.join(command)}")
            return (result.stdout + (result.stderr if args[0] == "logs" else "")) if not sealed else ""
        except subprocess.TimeoutExpired:
            record.update(returncode=None, error="deadline exceeded",
                          elapsed_s=time.monotonic() - started)
            raise
        except BaseException as error:
            record.setdefault("returncode", None)
            record["error"] = type(error).__name__
            raise
        finally:
            self.commands.append(record)
            (self.out / "commands.json").write_text(json.dumps(self.commands, indent=2) + "\n")

    def inventory(self):
        ids = self.call("network", "ls", "-q").split()
        return json.loads(self.call("network", "inspect", *ids)) if ids else []

    def network(self, subnet):
        wanted = ipaddress.ip_network(subnet)
        for network in self.inventory():
            for config in network["IPAM"]["Config"] or []:
                if config.get("Subnet") and wanted.overlaps(ipaddress.ip_network(config["Subnet"])):
                    raise RuntimeError("subnet overlaps existing network " + network["Name"])
        identity = self.call("network", "create", "--internal", "--subnet", subnet,
                             "--label", self.label, self.prefix + "-net").strip()
        return identity

    def create(self, role, image, network, ip, command, mounts=(), extra=()):
        args = ["create", "--name", self.prefix + "-" + role,
                "--label", self.label, "--network", network, "--ip", ip]
        for source, dest, readonly in mounts:
            args.extend(["-v", f"{Path(source).resolve()}:{dest}" + (":ro" if readonly else "")])
        identity = self.call(*args, *extra, image, *command).strip()
        return identity

    def start(self, identity):
        self.call("start", identity)

    def wait(self, identity, timeout):
        status = self.call("wait", identity, timeout=timeout).strip()
        if status != "0":
            raise RuntimeError("container exited " + status + ": " + identity)

    def logs(self, identity, name):
        (self.out / name).write_text(self.call("logs", identity))

    def capture(self, namespace, tools, modbus):
        identity = self.call(
            "create", "--name", self.prefix + "-capture", "--label", self.label,
            "--network", "container:" + namespace, "--cap-drop", "ALL",
            "--cap-add", "NET_RAW", tools,
            "--immediate-mode", "-B", "65536", "-i", "eth0", "-p", "-s", "0",
            "-U", "-w", "/tmp/capture.pcap",
            "tcp", "port", "2404", *(["or", "tcp", "port", "502"] if modbus else []),
        ).strip()
        self.start(identity)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if "listening on eth0" in self.call("logs", identity):
                return identity
            time.sleep(0.05)
        raise TimeoutError("tcpdump did not signal readiness")

    def finish_capture(self, identity):
        failure = None
        try:
            self.call("kill", "--signal", "INT", identity)
            self.wait(identity, 10)
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            failure = error
        log = self.call("logs", identity)
        (self.out / "tcpdump.log").write_text(log)
        self.call("cp", identity + ":/tmp/capture.pcap", self.out / "capture.pcap")
        if failure:
            raise failure
        dropped = re.search(r"(\d+) packets dropped by kernel", log)
        captured = re.search(r"(\d+) packets captured", log)
        if not dropped or int(dropped[1]) or not captured or not int(captured[1]):
            raise RuntimeError("capture missing, empty or dropped packets")
        if (self.out / "capture.pcap").stat().st_size <= 24:
            raise RuntimeError("empty pcap")

    def helper_image(self):
        tag = self.prefix + "-capture:test"
        self.images.append(tag)
        self.call("build", "--label", self.label, "-f", HERE / "tools.Dockerfile", "-t", tag, HERE,
                  timeout=180, sealed=True)
        return tag

    def cleanup(self):
        errors = []
        # Label discovery also catches daemon-side creation completed after a
        # timed-out CLI. Never operate on prefix matches or external resources.
        for kind, listing, remove in (
            ("containers", ("ps", "-aq"), ("rm", "-f")),
            ("networks", ("network", "ls", "-q"), ("network", "rm")),
        ):
            try:
                identities = self.call(*listing, "--filter", "label=" + self.label).split()
                for identity in identities:
                    self.call(*remove, identity)
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                errors.append(kind + ": " + str(error))
        for image in self.images:
            try:
                # A failed build need not have produced its requested tag.
                existing = self.call("image", "ls", "-q", "--filter", "reference=" + image).strip()
                if existing:
                    self.call("image", "rm", image)
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                errors.append(str(error))
        if errors:
            raise RuntimeError("cleanup failed: " + "; ".join(errors))
