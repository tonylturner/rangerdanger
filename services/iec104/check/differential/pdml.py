# SPDX-License-Identifier: Apache-2.0
"""Disk-backed, packet-at-a-time hierarchical PDML for large captures."""

from contextlib import contextmanager
from pathlib import Path
import subprocess
import tempfile
import xml.etree.ElementTree as ET


class PDMLError(Exception):
    """The decoder failed or did not produce well-formed PDML."""


@contextmanager
def decode_file(command, timeout):
    with tempfile.TemporaryDirectory(prefix="rd-iec104-pdml-") as directory:
        path = Path(directory) / "capture.pdml"
        try:
            with path.open("wb") as output:
                result = subprocess.run(
                    command, stdout=output, stderr=subprocess.PIPE,
                    text=True, timeout=timeout,
                )
            if result.returncode:
                raise PDMLError(f"tshark decode exited {result.returncode}: {result.stderr.strip()}")
            yield path
        except (OSError, subprocess.TimeoutExpired) as error:
            raise PDMLError(str(error)) from error


def packets(path):
    """Release each packet subtree before advancing to the next one."""
    try:
        context = ET.iterparse(path, events=("start", "end"))
        _, root = next(context)
        for event, element in context:
            if event == "end" and element.tag == "packet":
                yield element
                root.clear()
    except (ET.ParseError, StopIteration) as error:
        raise PDMLError("could not parse tshark PDML: " + str(error)) from error
