# SPDX-License-Identifier: Apache-2.0
"""Build the unchanged C recipe from a fixed first-party Git revision.

Compiler/dependency output is discarded, not exposed to clean-room lanes.
Only our Git archive is extracted here; upstream sources exist only in Docker.
"""

import argparse
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import sys
import signal

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "differential"))
from process_utils import run as run_owned

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]
INPUTS = ["services/iec104/Dockerfile", "services/iec104/src",
          "services/iec104/LICENSE", "services/iec104/README.md",
          "services/iec104/entrypoint.sh", "scripts/set-gateway.sh"]


def build(out):
    metadata = json.loads((HERE / "baseline.json").read_text())
    revision = metadata["commit"]
    tag = "rd-iec104-ref:" + revision[:12]
    existing = subprocess.run(
        ["docker", "image", "inspect", tag],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15,
    )
    if existing.returncode == 0:
        raise RuntimeError("reference tag already exists; refusing to overwrite " + tag)
    try:
        materialize(metadata, tag, out)
    except BaseException:
        found = subprocess.run(
            ["docker", "image", "ls", "-q", "--filter", "reference=" + tag],
            capture_output=True, text=True, check=True, timeout=15,
        )
        if found.stdout.strip():
            subprocess.run(["docker", "image", "rm", tag], check=True,
                           capture_output=True, timeout=15)
        raise


def materialize(metadata, tag, out):
    revision = metadata["commit"]
    with tempfile.TemporaryDirectory(prefix="rd-iec104-reference-") as directory:
        archive = subprocess.run(
            ["git", "-C", str(ROOT), "archive", revision, "--", *INPUTS],
            capture_output=True, check=True, timeout=30,
        ).stdout
        with tarfile.open(fileobj=io.BytesIO(archive)) as source:
            # Git archive contains only repository-owned build inputs.
            source.extractall(directory, filter="data")
        result = run_owned(
            ["docker", "build", "-f", directory + "/services/iec104/Dockerfile",
             "-t", tag, directory],
            timeout=900, sealed=True,
        )
        if result.returncode:
            raise RuntimeError("sealed reference build failed (output intentionally withheld)")
    metadata["image"] = tag
    metadata["image_id"] = subprocess.run(
        ["docker", "image", "inspect", "--format", "{{.Id}}", tag],
        check=True, capture_output=True, text=True, timeout=15,
    ).stdout.strip()
    out.mkdir(parents=True, exist_ok=True)
    (out / "baseline.json").write_text(json.dumps(metadata, indent=2) + "\n")
    (out / "roles.json").write_text(json.dumps(
        {"rtu": tag, "cc": tag, "cmd": tag, "peer": "python:3.12-alpine@sha256:b658b05ce9c4eb97ec6fc19424818a1f31993659c2b505a532f470e83807815d"},
        indent=2,
    ) + "\n")
    print("Reference build PASS:", tag, metadata["image_id"])


def terminate(_signum, _frame):
    raise KeyboardInterrupt("reference build terminated")


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, terminate)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    try:
        build(args.out)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        parser.exit(2, str(error) + "\n")
