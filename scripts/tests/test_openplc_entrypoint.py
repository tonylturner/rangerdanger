from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = ROOT / "scripts/openplc-entrypoint.sh"


class OpenPLCEntrypointTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.workdir = self.root / "workdir"
        self.openplc = self.root / "openplc"
        self.bin = self.root / "bin"
        self.workdir.mkdir()
        (self.openplc / "st_files").mkdir(parents=True)
        (self.openplc / "scripts").mkdir()
        self.bin.mkdir()
        self.sqlite_log = self.root / "sqlite.log"
        self.gateway_log = self.root / "gateway.log"

        source = ENTRYPOINT.read_text(encoding="utf-8")
        replacements = {
            "/usr/local/bin/set-gateway.sh": "set-gateway.sh",
            'WORKDIR="/workdir"': f'WORKDIR="{self.workdir}"',
            'OPENPLC_DIR="/root/OpenPLC_v3/webserver"':
                f'OPENPLC_DIR="{self.openplc}"',
            "/root/OpenPLC_v3/start_openplc.sh":
                str(self.openplc / "start_openplc.sh"),
        }
        for original, replacement in replacements.items():
            self.assertEqual(source.count(original), 1, f"expected one {original!r}")
            source = source.replace(original, replacement)
        self.assertEqual(
            ENTRYPOINT.read_text(encoding="utf-8").count(
                "/usr/local/bin/set-gateway.sh"),
            1,
            "the production gateway call must remain unchanged",
        )

        self.script = self.root / "entrypoint.sh"
        self.script.write_text(source, encoding="utf-8")
        self.script.chmod(0o755)
        self._write_executable(
            self.bin / "set-gateway.sh",
            '#!/bin/sh\nprintf "stub called\\n" >> "$GATEWAY_LOG"\n',
        )
        self._write_executable(
            self.bin / "sqlite3",
            '#!/bin/sh\n'
            'printf "%s\\n" "$2" >> "$SQLITE_LOG"\n'
            'case "$2" in *"${SQLITE_FAIL_MATCH:-}"*) '
            '[ -z "${SQLITE_FAIL_MATCH:-}" ] || exit 23 ;; esac\n',
        )
        self._write_executable(self.bin / "curl", "#!/bin/sh\nexit 0\n")
        self._write_executable(self.bin / "netstat", '#!/bin/sh\nprintf "tcp :::502 LISTEN\\n"\n')
        self._write_executable(self.bin / "sleep", "#!/bin/sh\nexit 0\n")
        self._write_executable(self.openplc / "start_openplc.sh", "#!/bin/sh\nexit 0\n")
        self._write_executable(
            self.openplc / "scripts/compile_program.sh",
            '#!/bin/sh\n'
            'printf "%s\\n" "$1" > "$COMPILER_LOG"\n'
            'printf "compiler output\\n"\n'
            'exit "${COMPILER_EXIT:-0}"\n',
        )
        (self.openplc / "openplc.db").touch()

    def tearDown(self) -> None:
        self.temp.cleanup()

    @staticmethod
    def _write_executable(path: Path, contents: str) -> None:
        path.write_text(contents, encoding="utf-8")
        path.chmod(0o755)

    def _run(self, **extra_env: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update({
            "PATH": f"{self.bin}{os.pathsep}{env['PATH']}",
            "SQLITE_LOG": str(self.sqlite_log),
            "GATEWAY_LOG": str(self.gateway_log),
            "COMPILER_LOG": str(self.root / "compiler.log"),
        })
        env.update(extra_env)
        return subprocess.run(
            ["/bin/bash", str(self.script)],
            cwd=self.root,
            env=env,
            text=True,
            capture_output=True,
            check=False,
            timeout=10,
        )

    def _add_program(self) -> None:
        (self.workdir / "example.st").write_text("PROGRAM example END_PROGRAM\n", encoding="utf-8")

    def test_compile_failure_prevents_startup(self) -> None:
        self._add_program()

        result = self._run(COMPILER_EXIT="19")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ERROR: Compilation failed for example.st", result.stderr)
        self.assertNotIn("=== Starting OpenPLC ===", result.stdout)
        self.assertTrue((self.root / "compiler.log").is_file())
        self.assertTrue(self.gateway_log.is_file())
        self.assertFalse(self.sqlite_log.exists())

    def test_each_database_statement_failure_prevents_startup(self) -> None:
        self._add_program()
        failures = (
            ("INSERT OR REPLACE", "Failed to register example.st"),
            ("UPDATE Settings SET Current_program", "Failed to set the current program"),
            ("INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('current_program'",
             "Failed to register the current program setting"),
            ("UPDATE Settings SET Value", "Failed to enable OpenPLC auto-start"),
            ("INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('Start_run_mode'",
             "Failed to register OpenPLC auto-start"),
        )

        for statement, message in failures:
            with self.subTest(statement=statement):
                self.sqlite_log.unlink(missing_ok=True)
                result = self._run(SQLITE_FAIL_MATCH=statement)

                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("=== Starting OpenPLC ===", result.stdout)

    def test_missing_database_prevents_startup(self) -> None:
        self._add_program()
        (self.openplc / "openplc.db").unlink()

        result = self._run()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ERROR: OpenPLC database not found", result.stderr)
        self.assertNotIn("=== Starting OpenPLC ===", result.stdout)
        self.assertFalse(self.sqlite_log.exists())

    def test_successful_program_registers_and_starts(self) -> None:
        self._add_program()

        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Compilation successful!", result.stdout)
        self.assertIn("Database updated, auto-start enabled", result.stdout)
        self.assertIn("=== Starting OpenPLC ===", result.stdout)
        self.assertEqual((self.openplc / "active_program").read_text(), "example.st\n")
        self.assertEqual(len(self.sqlite_log.read_text().splitlines()), 5)

    def test_no_program_keeps_blank_program_behavior(self) -> None:
        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            f"No ST file found in {self.workdir}, starting with blank program",
            result.stdout,
        )
        self.assertIn("=== Starting OpenPLC ===", result.stdout)
        self.assertFalse((self.root / "compiler.log").exists())
        self.assertFalse(self.sqlite_log.exists())


if __name__ == "__main__":
    unittest.main()
