from __future__ import annotations

import hashlib
import importlib.util
import os
import shutil
import socket
import sqlite3
import struct
import subprocess
import tempfile
import threading
import unittest
from contextlib import contextmanager
from pathlib import Path
from typing import Iterator
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = ROOT / "scripts/openplc-entrypoint.sh"
SEED_SOURCE = ROOT / "lab-definitions/packages/us-dnp3-substation/seeds/openplc"
CHECK_RUNTIME_SOURCE = SEED_SOURCE / "check_runtime.py"
CHECK_RUNTIME_SPEC = importlib.util.spec_from_file_location(
    "openplc_seed_runtime_check", CHECK_RUNTIME_SOURCE)
assert CHECK_RUNTIME_SPEC is not None and CHECK_RUNTIME_SPEC.loader is not None
check_runtime = importlib.util.module_from_spec(CHECK_RUNTIME_SPEC)
CHECK_RUNTIME_SPEC.loader.exec_module(check_runtime)


class FakeModbusSocket:
    def __init__(self, registers: tuple[int, int, int, int]) -> None:
        # %MW0-%MW3: close request, status, close output, heartbeat.
        body = b"\x03\x08" + struct.pack(">4H", *registers)
        self.response = struct.pack(">HHHB", 1, 0, len(body) + 1, 1) + body

    def __enter__(self) -> FakeModbusSocket:
        return self

    def __exit__(self, *args: object) -> None:
        return None

    def settimeout(self, timeout: float) -> None:
        del timeout

    def sendall(self, request: bytes) -> None:
        del request

    def recv(self, size: int) -> bytes:
        data, self.response = self.response[:size], self.response[size:]
        return data


class FakeClock:
    def __init__(self) -> None:
        self.now = 0.0

    def monotonic(self) -> float:
        return self.now

    def sleep(self, duration: float) -> None:
        self.now += duration


class OpenPLCEntrypointTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.workdir = self.root / "workdir"
        self.openplc = self.root / "openplc"
        self.seed = self.root / "seed"
        self.bin = self.root / "bin"
        self.workdir.mkdir()
        (self.openplc / "st_files").mkdir(parents=True)
        (self.openplc / "scripts").mkdir()
        self.bin.mkdir()
        shutil.copytree(SEED_SOURCE, self.seed)
        self.sqlite_log = self.root / "sqlite.log"
        self.gateway_log = self.root / "gateway.log"
        self.curl_log = self.root / "curl.log"
        self.modbus_server: socket.socket | None = None
        self.modbus_stop = threading.Event()
        self.modbus_thread: threading.Thread | None = None

        install = self.seed / "install.py"
        install_source = install.read_text(encoding="utf-8")
        self.assertEqual(install_source.count('pathlib.Path("/root/OpenPLC_v3/webserver")'), 1)
        install.write_text(
            install_source.replace(
                'pathlib.Path("/root/OpenPLC_v3/webserver")',
                f'pathlib.Path("{self.openplc}")',
            ),
            encoding="utf-8",
        )
        self._write_seed_manifest()

        source = ENTRYPOINT.read_text(encoding="utf-8")
        replacements = {
            "/usr/local/bin/set-gateway.sh": "set-gateway.sh",
            'WORKDIR="/workdir"': f'WORKDIR="{self.workdir}"',
            'OPENPLC_DIR="/root/OpenPLC_v3/webserver"':
                f'OPENPLC_DIR="{self.openplc}"',
            'SEED_DIR="/opt/openplc-us"': f'SEED_DIR="{self.seed}"',
            "/var/lib/openplc-us-seed.sha256": str(self.root / "seed.sha256"),
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
        self._write_executable(
            self.bin / "curl",
            '#!/bin/sh\nprintf "%s\\n" "$*" >> "$CURL_LOG"\n'
            '[ "${CURL_EXIT:-0}" -eq 0 ]\n',
        )
        self._write_executable(
            self.bin / "netstat",
            '#!/bin/sh\nprintf "tcp :::502 LISTEN\\n"\n',
        )
        self._write_executable(self.bin / "sleep", "#!/bin/sh\nexit 0\n")
        self._write_executable(
            self.openplc / "start_openplc.sh",
            "#!/bin/sh\nprintf 'started\\n' >> \"$START_LOG\"\nexit 0\n",
        )
        self._write_executable(
            self.openplc / "scripts/compile_program.sh",
            '#!/bin/sh\n'
            'printf "%s\\n" "$1" >> "$COMPILER_LOG"\n'
            'printf "compiler output\\n"\n'
            'if [ "${COMPILER_EXIT:-0}" -ne 0 ]; then exit "$COMPILER_EXIT"; fi\n'
            'printf "#!/bin/sh\\n" > core/openplc\n'
            'chmod +x core/openplc\n',
        )
        (self.openplc / "core").mkdir()
        self._make_database()

    def tearDown(self) -> None:
        self._stop_modbus_server()
        self.temp.cleanup()

    @staticmethod
    def _write_executable(path: Path, contents: str) -> None:
        path.write_text(contents, encoding="utf-8")
        path.chmod(0o755)

    def _write_seed_manifest(self) -> None:
        names = ("substation-automation.st", "install.py", "check_runtime.py")
        manifest = "".join(
            f"{hashlib.sha256((self.seed / name).read_bytes()).hexdigest()}  {name}\n"
            for name in names
        )
        (self.seed / "seed.sha256").write_text(manifest, encoding="utf-8")

    def _make_database(self) -> None:
        with self._database() as db:
            db.execute(
                "CREATE TABLE Programs "
                "(Name TEXT, Description TEXT, File TEXT, Date_upload INTEGER)"
            )
            db.execute("CREATE TABLE Slave_dev (name TEXT)")
            db.execute("CREATE TABLE Settings (Key TEXT PRIMARY KEY, Value TEXT)")
            db.execute("INSERT INTO Programs VALUES ('old', 'old', 'old.st', 0)")
            db.execute("INSERT INTO Slave_dev VALUES ('old remote device')")
            db.execute("INSERT INTO Settings VALUES ('Start_run_mode', 'false')")
            db.commit()

    @contextmanager
    def _database(self) -> Iterator[sqlite3.Connection]:
        database = sqlite3.connect(self.openplc / "openplc.db")
        try:
            yield database
        finally:
            database.close()

    def _run(self, **extra_env: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update({
            "PATH": f"{self.bin}{os.pathsep}{env['PATH']}",
            "SQLITE_LOG": str(self.sqlite_log),
            "GATEWAY_LOG": str(self.gateway_log),
            "CURL_LOG": str(self.curl_log),
            "COMPILER_LOG": str(self.root / "compiler.log"),
            "START_LOG": str(self.root / "start.log"),
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
        (self.workdir / "example.st").write_text(
            "PROGRAM example END_PROGRAM\n", encoding="utf-8")

    def _start_modbus_server(self) -> None:
        self.modbus_server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.modbus_server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.modbus_server.bind(("127.0.0.1", 0))
        self.modbus_server.listen()
        self.modbus_server.settimeout(0.2)
        port = self.modbus_server.getsockname()[1]
        checker = self.seed / "check_runtime.py"
        source = checker.read_text(encoding="utf-8")
        self.assertEqual(source.count("PORT = 502"), 1)
        checker.write_text(source.replace("PORT = 502", f"PORT = {port}"), encoding="utf-8")
        self._write_seed_manifest()

        def serve() -> None:
            heartbeat = 0
            while not self.modbus_stop.is_set():
                try:
                    connection, _ = self.modbus_server.accept()
                except socket.timeout:
                    continue
                except OSError:
                    return
                with connection:
                    connection.settimeout(1)
                    request = b""
                    while len(request) < 12:
                        chunk = connection.recv(12 - len(request))
                        if not chunk:
                            break
                        request += chunk
                    if len(request) != 12:
                        continue
                    heartbeat = (heartbeat + 1) & 0xFFFF
                    body = b"\x03\x08" + struct.pack(">4H", 0, 15, 0, heartbeat)
                    response = struct.pack(">HHHB", struct.unpack(">H", request[:2])[0],
                                           0, len(body) + 1, 1) + body
                    connection.sendall(response)

        self.modbus_thread = threading.Thread(target=serve, daemon=True)
        self.modbus_thread.start()

    def _stop_modbus_server(self) -> None:
        self.modbus_stop.set()
        if self.modbus_server is not None:
            self.modbus_server.close()
        if self.modbus_thread is not None:
            self.modbus_thread.join(timeout=1)

    def test_baked_seed_installs_no_master_devices_and_waits_for_heartbeat(self) -> None:
        self._start_modbus_server()

        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("using baked US OpenPLC seed", result.stdout)
        self.assertIn("US OpenPLC seed heartbeat advanced", result.stdout)
        self.assertIn("=== Starting OpenPLC ===", result.stdout)
        self.assertTrue(self.gateway_log.is_file())
        self.assertEqual((self.openplc / "active_program").read_text(), "substation-automation.st\n")
        with self._database() as db:
            self.assertEqual(
                db.execute("SELECT File FROM Programs").fetchall(),
                [("substation-automation.st",)],
            )
            self.assertEqual(db.execute("SELECT COUNT(*) FROM Slave_dev").fetchone()[0], 0)
            settings = dict(db.execute("SELECT Key,Value FROM Settings"))
        self.assertEqual(settings["Modbus_port"], "502")
        self.assertEqual(settings["Dnp3_port"], "disabled")
        self.assertEqual(settings["Enip_port"], "disabled")
        self.assertEqual(
            (self.openplc / "mbconfig.cfg").read_text().splitlines()[0],
            'Num_Devices = "0"',
        )
        self.assertTrue((self.root / "seed.sha256").is_file())

    def test_seed_hash_failure_prevents_compilation_and_startup(self) -> None:
        (self.seed / "substation-automation.st").write_text("changed\n", encoding="utf-8")

        result = self._run()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("baked US OpenPLC seed hash verification failed", result.stderr)
        self.assertNotIn("=== Starting OpenPLC ===", result.stdout)
        self.assertFalse((self.root / "compiler.log").exists())

    def test_database_install_failure_prevents_startup(self) -> None:
        with self._database() as db:
            db.execute("DROP TABLE Slave_dev")
            db.commit()

        result = self._run()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("failed to install and verify the baked US OpenPLC seed database",
                      result.stderr)
        self.assertNotIn("=== Starting OpenPLC ===", result.stdout)

    def test_workdir_program_overrides_baked_seed(self) -> None:
        self._add_program()

        result = self._run()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Found workdir ST override: example.st", result.stdout)
        self.assertNotIn("US OpenPLC seed heartbeat advanced", result.stdout)
        self.assertEqual((self.openplc / "active_program").read_text(), "example.st\n")
        self.assertEqual(len(self.sqlite_log.read_text().splitlines()), 5)

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


class OpenPLCRuntimeCheckTests(unittest.TestCase):
    @staticmethod
    def _run_with_registers(
        registers: list[tuple[int, int, int, int]],
    ) -> tuple[FakeClock, int]:
        clock = FakeClock()
        connections = [FakeModbusSocket(values) for values in registers]
        with (
            patch.object(check_runtime.socket, "create_connection",
                         side_effect=connections) as create_connection,
            patch.object(check_runtime.time, "monotonic", side_effect=clock.monotonic),
            patch.object(check_runtime.time, "sleep", side_effect=clock.sleep),
        ):
            check_runtime.main()
        return clock, create_connection.call_count

    def test_incomplete_first_read_retries_until_heartbeat_advances(self) -> None:
        clock, connection_count = self._run_with_registers([
            (0, 0, 0, 0),
            (0, 15, 0, 100),
            (0, 15, 0, 101),
        ])
        self.assertEqual(clock.now, 1.0)
        self.assertEqual(connection_count, 3)

    def test_never_complete_retries_until_deadline_then_reports_last_error(self) -> None:
        clock = FakeClock()
        connections = [FakeModbusSocket((0, 0, 0, 0)) for _ in range(30)]
        with (
            patch.object(check_runtime.socket, "create_connection",
                         side_effect=connections) as create_connection,
            patch.object(check_runtime.time, "monotonic", side_effect=clock.monotonic),
            patch.object(check_runtime.time, "sleep", side_effect=clock.sleep),
            self.assertRaisesRegex(
                RuntimeError,
                "within 15 seconds: feeder interlock status is incomplete",
            ),
        ):
            check_runtime.main()
        self.assertEqual(create_connection.call_count, 30)
        self.assertEqual(clock.now, 15.0)


if __name__ == "__main__":
    unittest.main()
