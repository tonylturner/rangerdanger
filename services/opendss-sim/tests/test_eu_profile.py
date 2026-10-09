import json
import os
import subprocess
import sys
from pathlib import Path

import pytest
from eu_profile import ProfileError, load_profile

SERVICE_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = SERVICE_DIR.parents[1]
PROCESS_DIR = REPO_ROOT / "lab-definitions/packages/eu-iec104-substation/process"


def local_profile(tmp_path: Path) -> Path:
    profile = json.loads((PROCESS_DIR / "profile.json").read_text(encoding="utf-8"))
    profile["circuit_file"] = str(PROCESS_DIR / "feeder.dss")
    path = tmp_path / "profile.json"
    path.write_text(json.dumps(profile), encoding="utf-8")
    return path


def test_package_profile_freezes_elements_and_electrical_case(tmp_path: Path) -> None:
    package_profile = json.loads(
        (PROCESS_DIR / "profile.json").read_text(encoding="utf-8")
    )
    assert package_profile == {
        "schema": 1,
        "id": "eu-feeder-20kv",
        "frequency_hz": 50,
        "primary_kv": 20,
        "circuit_file": "/profiles/feeder.dss",
        "load_kw": 1000,
        "load_pf": 1.0,
        "elements": {
            "breaker": "Line.Breaker",
            "load": "Load.FeederLoad",
            "load_bus": "loadbus",
        },
        "device": {
            "id": "feeder-breaker",
            "truth_url": "http://10.50.50.30:8080",
        },
        "solve_interval_ms": 1000,
        "request_timeout_ms": 500,
        "truth_stale_after_ms": 3000,
    }

    profile = load_profile(local_profile(tmp_path))
    assert profile.circuit_file.name == "feeder.dss"
    assert profile.frequency_hz == 50
    assert profile.primary_kv == 20


def test_invalid_profile_fails_service_import(tmp_path: Path) -> None:
    invalid_profile = tmp_path / "invalid.json"
    invalid_profile.write_text('{"schema": 1}', encoding="utf-8")
    environment = os.environ.copy()
    environment["OPENDSS_PROFILE"] = str(invalid_profile)
    result = subprocess.run(
        [sys.executable, "-c", "import main"],
        cwd=SERVICE_DIR,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode != 0
    assert "profile fields mismatch" in result.stderr


def test_profile_rejects_missing_circuit_file(tmp_path: Path) -> None:
    profile_path = local_profile(tmp_path)
    profile = json.loads(profile_path.read_text(encoding="utf-8"))
    profile["circuit_file"] = str(tmp_path / "missing.dss")
    profile_path.write_text(json.dumps(profile), encoding="utf-8")
    with pytest.raises(ProfileError, match="does not exist"):
        load_profile(profile_path)
