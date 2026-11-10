"""Installer argument errors stop before any Docker or staging work."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
OPTIONS = (
    ("setup.sh", [], "--version", "a release version", "v0.2.1"),
    ("setup.sh", [], "--from-tarballs",
     "a directory (the folder containing images-<arch>.tar)", "./images"),
    ("setup.sh", [], "--package", "a package ID", "us-dnp3-substation"),
    ("stage-ssd-delta.sh", ["./out", "v1", "v2"], "--include",
     "a comma-separated image list", "backend,frontend"),
)


class InstallerArgumentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        tools = Path(self.temp.name)
        self.docker_calls = tools / "docker-calls"
        docker = tools / "docker"
        docker.write_text(
            '#!/bin/sh\nprintf called > "$DOCKER_CALLS"\nexit 99\n'
        )
        docker.chmod(0o755)
        self.env = dict(
            os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
            DOCKER_CALLS=str(self.docker_calls),
        )

    def run_script(self, script, args, executable="bash"):
        command = (["pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-File"]
                   if executable == "pwsh" else ["bash"])
        result = subprocess.run(
            [*command, str(ROOT / script), *args],
            cwd=ROOT, env=self.env, capture_output=True, text=True, timeout=10,
        )
        self.assertFalse(self.docker_calls.exists(), "argument parsing called Docker")
        self.assertNotIn("unbound variable", result.stdout + result.stderr)
        return result

    def assert_value_error(self, script, args, option, description):
        result = self.run_script(script, args)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(f"{option} needs {description}", result.stderr)

    def test_missing_values(self):
        for script, prefix, option, description, _ in OPTIONS:
            with self.subTest(script=script, option=option):
                self.assert_value_error(script, [*prefix, option], option, description)

    def test_empty_values(self):
        for script, prefix, option, description, _ in OPTIONS:
            for args in ([option, ""], [option + "="]):
                with self.subTest(script=script, args=args):
                    self.assert_value_error(script, [*prefix, *args], option, description)

    def test_next_flag_is_not_a_value(self):
        for script, prefix, option, description, _ in OPTIONS:
            next_flag = ["--all"] if prefix else ["--package", "x"]
            for args in ([option, *next_flag], [option, "-h"],
                         [option + "=" + next_flag[0]]):
                with self.subTest(script=script, args=args):
                    self.assert_value_error(script, [*prefix, *args], option, description)

    def test_valid_values_reach_parse_only_exit(self):
        for script, prefix, option, _, value in OPTIONS:
            for args in ([option, value], [option + "=" + value]):
                with self.subTest(script=script, args=args):
                    # setup has a help exit after parsing; delta exits on an
                    # unknown argument before it enumerates images or writes.
                    stop = "--parse-only" if prefix else "--help"
                    result = self.run_script(script, [*prefix, *args, stop])
                    if prefix:
                        self.assertEqual(result.returncode, 1, result.stderr)
                        self.assertIn("Unknown argument: --parse-only", result.stdout)
                    else:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertIn("Usage:", result.stdout)

    @unittest.skipUnless(shutil.which("pwsh"), "pwsh is not installed")
    def test_powershell_reports_missing_from_tarballs(self):
        result = self.run_script(
            "setup.ps1", ["-FromTarballs"],
            executable="pwsh",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Missing an argument for parameter 'FromTarballs'", result.stderr)
