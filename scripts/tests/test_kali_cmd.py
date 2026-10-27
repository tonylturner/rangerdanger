"""Kali's startup command must stop when gateway setup fails."""
from __future__ import annotations

import json
import os
import shlex
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOCKERFILE = ROOT / "Dockerfile.kali"
GATEWAY_SCRIPT = "/usr/local/bin/set-gateway.sh"
EXPECTED_CMD = [
    "sh",
    "-c",
    "/usr/local/bin/set-gateway.sh && exec sleep infinity",
]


class KaliCommandTests(unittest.TestCase):
    def load_command(self) -> list[str]:
        commands = [
            json.loads(line.removeprefix("CMD "))
            for line in DOCKERFILE.read_text().splitlines()
            if line.startswith("CMD ")
        ]
        self.assertEqual(len(commands), 1, "Dockerfile.kali must have one exec-form CMD")
        return commands[0]

    def run_command(
        self, gateway_script: Path,
    ) -> tuple[subprocess.CompletedProcess[str], str]:
        command = self.load_command()
        self.assertEqual(command, EXPECTED_CMD)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            sleep_log = root / "sleep.log"
            fake_sleep = root / "sleep"
            fake_sleep.write_text(
                '#!/bin/sh\nprintf "%s\\n" "$*" > "$SLEEP_LOG"\n',
                encoding="utf-8",
            )
            fake_sleep.chmod(0o755)
            env = dict(
                os.environ,
                PATH=f"{root}:/usr/bin:/bin",
                SLEEP_LOG=str(sleep_log),
            )
            shell_command = command[2].replace(
                GATEWAY_SCRIPT, shlex.quote(str(gateway_script)), 1,
            )
            result = subprocess.run(
                ["sh", "-c", shell_command],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
                timeout=5,
                check=False,
            )
            sleep_output = sleep_log.read_text(encoding="utf-8") if sleep_log.exists() else ""
            return result, sleep_output

    def test_missing_gateway_script_fails_without_sleeping(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            missing = Path(tmp) / "missing-set-gateway.sh"
            result, sleep_output = self.run_command(missing)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sleep_output, "")

    def test_non_executable_gateway_script_fails_without_sleeping(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            gateway = Path(tmp) / "set-gateway.sh"
            gateway.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            gateway.chmod(0o644)
            result, sleep_output = self.run_command(gateway)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sleep_output, "")

    def test_failing_gateway_script_fails_without_sleeping(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            gateway = Path(tmp) / "set-gateway.sh"
            gateway.write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")
            gateway.chmod(0o755)
            result, sleep_output = self.run_command(gateway)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(sleep_output, "")

    def test_successful_gateway_script_execs_sleep(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            gateway = Path(tmp) / "set-gateway.sh"
            gateway.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            gateway.chmod(0o755)
            result, sleep_output = self.run_command(gateway)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sleep_output, "infinity\n")


if __name__ == "__main__":
    unittest.main()
