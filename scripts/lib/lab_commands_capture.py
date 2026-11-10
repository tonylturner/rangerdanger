"""One container-owned capture spanning a scenario's source-test rows.

Only the unique directory/PID created here is signalled or removed. The student
command retains its own rotation bound; HTTP/Run deadlines never own tcpdump.
"""
import json
from pathlib import Path
import shlex
import subprocess
import time
import uuid


def decode(directory, container, path):
    """Validate actual packets, not a header-only/stale file or decoder errors."""
    result = subprocess.run(
        ["docker", "exec", container, "timeout", "5", "tcpdump", "-nn", "-r", path],
        text=True, capture_output=True, timeout=10)
    (directory / "capture.stdout").write_text(result.stdout)
    (directory / "capture.stderr").write_text(result.stderr)
    return len(result.stdout.splitlines()) if result.returncode == 0 else 0


class Capture:
    def __init__(self, gate, row, oracle, directory):
        self.gate, self.row, self.oracle = gate, row, oracle
        self.directory = Path(directory)
        self.state = "/tmp/rd-lab-capture-" + uuid.uuid4().hex
        self.started = False

    def docker(self, *args, detached=False):
        return subprocess.run(
            ["docker", "exec", *(["-d"] if detached else []), self.row["container"], *args],
            text=True, capture_output=True, check=True, timeout=15)

    def start(self):
        self.directory.mkdir(parents=True, exist_ok=True)
        args = shlex.split(self.row["command"])
        if (args[0] != "tcpdump" or "-G" not in args or "-W" not in args
                or args[args.index("-W") + 1] != "1"
                or int(args[args.index("-G") + 1]) <= 0
                or args[args.index("-w") + 1] != self.oracle["file"]):
            raise ValueError("capture requires the reviewed bounded tcpdump command")
        # Verify the same documented CLI -> Linux transition as ordinary rows.
        probe = {**self.row, "command": "true"}
        argv, _ = self.gate.invocation(probe, 5)
        transition = self.gate.run_staged_shell(argv, probe, 5)
        if (transition.returncode or self.gate.BAD_OUTPUT.search(
                transition.stdout + transition.stderr)
                or "documented shell transition failed" in transition.stderr):
            raise ValueError("documented shell transition failed before capture")
        self.docker("mkdir", self.state)
        self.started = True  # cleanup owns this directory even if launch fails
        self.docker("rm", "-f", self.oracle["file"])
        # Job control prevents an asynchronous tcpdump inheriting SIGINT=ignore.
        # wait records its real exit status for early-exit and stop validation.
        script = (
            'set -m\n'
            f'{self.row["command"]} >{self.state}/stdout 2>{self.state}/stderr &\n'
            'pid=$!\n'
            f'echo "$pid" >{self.state}/pid\n'
            'wait "$pid"\n'
            f'echo "$?" >{self.state}/rc\n')
        self.docker("bash", "-c", script, detached=True)
        deadline = time.monotonic() + 5
        while True:
            status = self.status()
            if status["running"] and "listening on" in status["stderr"]:
                print(f'START capture: {self.gate.key(self.row)}')
                return
            if status["rc"] or time.monotonic() >= deadline:
                raise ValueError(f"capture did not become ready: {status}")
            time.sleep(0.1)

    def status(self):
        output = self.docker("bash", "-c",
                             f'cat {self.state}/rc 2>/dev/null || true; '
                             f'echo __LOG__; cat {self.state}/stderr 2>/dev/null || true; '
                             f'echo __LIVE__; '
                             f'if [ -f {self.state}/pid ] && '
                             f'kill -0 "$(cat {self.state}/pid)" 2>/dev/null && '
                             f'[ ! -f {self.state}/rc ]; then echo yes; fi').stdout
        rc, _, rest = output.partition("__LOG__\n")
        err, _, live = rest.partition("__LIVE__\n")
        return dict(rc=rc.strip(), stderr=err, running=live.strip() == "yes")

    def require_running(self):
        status = self.status()
        if not status["running"]:
            raise ValueError(f"capture ended before source tests finished: {status}")

    def finish(self, wait=False):
        # Baseline lessons have no intervening source rows: observe their full
        # authored window, rather than stopping an empty capture immediately.
        if wait:
            args = shlex.split(self.row["command"])
            deadline = time.monotonic() + int(args[args.index("-G") + 1]) + 10
            while self.status()["running"]:
                if time.monotonic() >= deadline:
                    raise ValueError("capture rotation did not finish within its bound")
                time.sleep(0.2)
        self.stop()
        status = self.status()
        out = self.docker("cat", self.state + "/stdout").stdout
        (self.directory / "command.stdout").write_text(out)
        (self.directory / "command.stderr").write_text(status["stderr"])
        frames = decode(self.directory, self.row["container"], self.oracle["file"])
        passed = (status["rc"] in ("0", "130") and
                  self.gate.verdict(self.oracle, int(status["rc"]), out,
                                    status["stderr"], frames=frames))
        (self.directory / "result.json").write_text(json.dumps(dict(
            key=self.gate.key(self.row), passed=passed, rc=status["rc"], frames=frames)))
        print(f'{"PASS" if passed else "FAIL"} capture frames={frames}: {self.gate.key(self.row)}')
        return passed

    def stop(self):
        if not self.started:
            return
        # Check the process is still our tcpdump before signalling its PID.
        self.docker("bash", "-c",
                    f'if [ -f {self.state}/pid ] && [ ! -f {self.state}/rc ]; then '
                    f'pid=$(cat {self.state}/pid); '
                    'if kill -0 "$pid" 2>/dev/null; then '
                    '[ "$(cat /proc/"$pid"/comm)" = tcpdump ] || exit 1; '
                    'kill -INT "$pid"; fi; fi')
        deadline = time.monotonic() + 10
        while not self.status()["rc"]:
            if time.monotonic() >= deadline:
                raise ValueError("capture failed to stop with SIGINT")
            time.sleep(0.1)

    def cleanup(self):
        if self.started:
            self.stop()
            self.docker("rm", "-rf", self.state)
            self.started = False
