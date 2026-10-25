from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GATE = ROOT / "scripts/govulncheck-gate.sh"


class GovulncheckGateTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.tmp_path = Path(self.temp.name)
        fake_bin = self.tmp_path / "bin"
        fake_bin.mkdir()
        fake = fake_bin / "govulncheck"
        fake.write_text(
            '#!/bin/sh\ncat "$GOVULNCHECK_FIXTURE"\n',
            encoding="utf-8",
        )
        fake.chmod(0o755)
        self.env = os.environ.copy()
        self.env["PATH"] = f"{fake_bin}{os.pathsep}{self.env['PATH']}"
        self.env["ALLOWED"] = ""
        self.fixture_path = self.tmp_path / "findings.json"
        self.env["GOVULNCHECK_FIXTURE"] = str(self.fixture_path)

    def tearDown(self) -> None:
        self.temp.cleanup()

    def run_gate(self, records: list[dict[str, object]]) -> subprocess.CompletedProcess[str]:
        self.fixture_path.write_text(
            "\n".join(json.dumps(record) for record in records) + "\n",
            encoding="utf-8",
        )
        return subprocess.run(
            ["bash", str(GATE), "dnp3go"],
            cwd=ROOT,
            env=self.env,
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
        )

    def test_module_and_package_findings_without_function_are_ignored(self) -> None:
        result = self.run_gate(
            [
                {"finding": {"osv": "GO-TEST-MODULE", "trace": []}},
                {
                    "finding": {
                        "osv": "GO-TEST-PACKAGE",
                        "trace": [{"package": "example/package"}],
                    }
                },
            ]
        )

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no findings", result.stdout)

    def test_symbol_level_finding_without_allowlist_entry_fails(self) -> None:
        result = self.run_gate(
            [
                {
                    "finding": {
                        "osv": "GO-TEST-SYMBOL",
                        "trace": [{"package": "example/package", "function": "Called"}],
                    }
                }
            ]
        )

        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("GO-TEST-SYMBOL", result.stdout)


if __name__ == "__main__":
    unittest.main()
