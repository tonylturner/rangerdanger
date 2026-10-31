# SPDX-License-Identifier: Apache-2.0
"""Bound complete owned subprocess groups, including Docker buildx children."""

import os
import signal
import subprocess


def run(command, timeout, sealed=False):
    with subprocess.Popen(
        command, start_new_session=True, text=True,
        stdout=subprocess.DEVNULL if sealed else subprocess.PIPE,
        stderr=subprocess.DEVNULL if sealed else subprocess.PIPE,
    ) as process:
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass  # The entire owned group already exited.
            try:
                process.communicate(timeout=3)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.communicate(timeout=3)
            raise
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
