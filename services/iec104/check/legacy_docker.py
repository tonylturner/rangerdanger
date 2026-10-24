# SPDX-License-Identifier: Apache-2.0
"""Bound the legacy gate's Docker calls and label every owned resource."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parent / "differential"))
from process_utils import run


def checked(args, timeout=20, sealed=False):
    result = run(["docker", *args], timeout=timeout, sealed=sealed)
    if result.returncode:
        raise RuntimeError("Docker command failed: " + " ".join(args))
    return result.stdout or ""


def inventory():
    containers = [json.loads(line) for line in checked(["ps", "-a", "--format", "json"]).splitlines()]
    networks = [json.loads(line) for line in checked(["network", "ls", "--format", "json"]).splitlines()]
    images = [json.loads(line) for line in checked(["image", "ls", "--format", "json"]).splitlines()]
    return containers, networks, images


def preflight(prefix):
    containers, networks, images = inventory()
    names = [item["Names"] for item in containers] + [item["Name"] for item in networks]
    names += [item["Repository"] for item in images]
    if any(name == prefix or name.startswith(prefix + "-") for name in names):
        raise RuntimeError("check prefix already in use: " + prefix)


def prepare(args, prefix, label):
    args = list(args)
    if args[0] in ("run", "create"):
        if "--network" not in args:
            args[1:1] = ["--network", "none"]
        if "--name" not in args:
            args[1:1] = ["--name", prefix + "-job-" + uuid.uuid4().hex[:12]]
        name = args[args.index("--name") + 1]
        if not name.startswith(prefix + "-"):
            raise ValueError("container outside owned prefix")
        args[1:1] = ["--label", label]
    elif args[:2] == ["network", "create"]:
        if not args[-1].startswith(prefix + "-"):
            raise ValueError("network outside owned prefix")
        args[2:2] = ["--internal", "--label", label]
    elif args[0] == "build":
        if "-t" not in args or not args[args.index("-t") + 1].startswith(prefix + "-"):
            raise ValueError("build needs an owned image tag")
        args[1:1] = ["--label", label]
    return args


def cleanup(prefix, label):
    errors = []
    for listing, remove in (
        (["ps", "-aq"], ["rm", "-f"]),
        (["network", "ls", "-q"], ["network", "rm"]),
    ):
        try:
            identities = checked([*listing, "--filter", "label=" + label]).split()
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            errors.append(str(error))
            continue
        for identity in identities:
            try:
                checked([*remove, identity])
            except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                errors.append(str(error))
    # Remove only our two preflight-checked tags, never an image ID shared
    # with somebody else's tag.
    for suffix in ("iec104", "iec104-base"):
        tag = prefix + "-" + suffix + ":test"
        try:
            if checked(["image", "ls", "-q", "--filter", "reference=" + tag]).strip():
                checked(["image", "rm", tag])
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            errors.append(str(error))
    if errors:
        raise RuntimeError("cleanup failed: " + "; ".join(errors))


def main():
    prefix = os.environ["RD_CHECK_PREFIX"]
    owner = os.environ["RD_CHECK_OWNER"]
    if not re.fullmatch("rd[a-z0-9-]+", prefix) or not re.fullmatch("[0-9a-f]{32}", owner):
        raise ValueError("invalid check ownership")
    label = "rd.iec104.legacy=" + owner
    args = sys.argv[1:]
    if args == ["preflight"]:
        preflight(prefix)
        return 0
    if args == ["cleanup"]:
        cleanup(prefix, label)
        return 0
    args = prepare(args, prefix, label)
    result = run(["docker", *args], timeout=900 if args[0] == "build" else 300, sealed=False)
    print(result.stdout, end="")
    print(result.stderr, end="", file=sys.stderr)
    return result.returncode


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("legacy_docker: " + str(error), file=sys.stderr)
        sys.exit(2)
