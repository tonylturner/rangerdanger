from __future__ import annotations

import hashlib
import io
import json
import subprocess
import tempfile
import unittest
import urllib.error
import xml.etree.ElementTree as ET
from pathlib import Path
from unittest.mock import patch

from scripts.tests import test_fuxa_seed_live as live

ROOT = Path(__file__).resolve().parents[2]
SEED = ROOT / "lab-definitions/packages/us-dnp3-substation/seeds/fuxa"


class FuxaSeedTests(unittest.TestCase):
    def test_live_check_skips_only_a_confirmed_absent_us_range(self):
        for state in (
            {"phase": "none"},
            {"phase": "ready", "package": "eu-iec104-substation"},
        ):
            with urllib.error.HTTPError(
                "test", 502, "no upstream", {}, io.BytesIO()
            ) as error, self.subTest(state=state), patch.object(
                live.ViewerClient, "request", side_effect=[
                    error,
                    json.dumps(state),
                ],
            ):
                with self.assertRaises(unittest.SkipTest):
                    live.FuxaSeedLiveTests(
                        "test_viewer_receives_six_fresh_read_only_values").debug()

    def test_live_check_does_not_skip_an_unavailable_active_us_hmi(self):
        with urllib.error.HTTPError(
            "test", 502, "no upstream", {}, io.BytesIO()
        ) as error, patch.object(live.ViewerClient, "request", side_effect=[
            error,
            json.dumps({"phase": "ready", "package": "us-dnp3-substation"}),
        ]):
            with self.assertRaises(urllib.error.HTTPError):
                live.FuxaSeedLiveTests(
                    "test_viewer_receives_six_fresh_read_only_values").debug()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "server"
        self.root.mkdir()
        self.seed = Path(self.temp.name) / "seed"
        self.seed.mkdir()
        self.payload = b"test baked database"
        (self.seed / "project.fuxap.db").write_bytes(self.payload)
        (self.seed / "project.sha256").write_text(
            hashlib.sha256(self.payload).hexdigest() + "\n")

    def install(self):
        code = (
            "const {install}=require(process.argv[1]);"
            "console.log(install(process.argv[2],process.argv[3]));"
        )
        return subprocess.run(
            ["node", "-e", code, str(SEED / "install.js"),
             str(self.root), str(self.seed)],
            capture_output=True, text=True, timeout=10,
        )

    def test_fresh_install_and_restart(self):
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("starter installed", result.stdout)
        database = self.root / "_appdata/project.fuxap.db"
        self.assertEqual(database.read_bytes(), self.payload)
        before = database.stat().st_mtime_ns
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("existing data", result.stdout)
        self.assertEqual(database.stat().st_mtime_ns, before)
        self.assertEqual(list(database.parent.iterdir()), [database])

    def test_empty_mounted_directories_and_logs_are_fresh(self):
        for directory in ("_appdata", "_db", "_logs"):
            (self.root / directory).mkdir()
        (self.root / "_logs/old.log").write_text("log")
        self.assertEqual(self.install().returncode, 0)
        self.assertEqual(
            (self.root / "_appdata/project.fuxap.db").read_bytes(), self.payload)

    def test_existing_data_is_never_replaced(self):
        for relative in (
            "_appdata/project.fuxap.db", "_appdata/settings.js",
            "_appdata/legacy.fuxap", "_db/user.db", "_appdata/.hidden",
        ):
            with self.subTest(relative=relative):
                root = self.root / relative.replace("/", "-")
                root.mkdir()
                original = self.root
                self.root = root
                try:
                    user_file = root / relative
                    user_file.parent.mkdir()
                    user_file.write_bytes(b"user content, even if invalid")
                    result = self.install()
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn("existing data", result.stdout)
                    self.assertEqual(user_file.read_bytes(), b"user content, even if invalid")
                    self.assertEqual(
                        sorted(p for p in root.rglob("*") if p.is_file()),
                        [user_file],
                    )
                finally:
                    self.root = original

    def test_checksum_failure_leaves_no_partial_install(self):
        (self.seed / "project.fuxap.db").write_bytes(self.payload + b"changed")
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_missing_seed_fails_closed(self):
        (self.seed / "project.fuxap.db").unlink()
        self.assertNotEqual(self.install().returncode, 0)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_existing_project_does_not_need_seed(self):
        (self.root / "_appdata").mkdir()
        (self.root / "_appdata/project.fuxap.db").write_bytes(b"user")
        (self.seed / "project.fuxap.db").unlink()
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("existing data", result.stdout)

    def test_plugin_lock_contains_integrity_for_the_complete_nested_tree(self):
        manifest = json.loads((SEED / "plugin/package.json").read_text())
        lock = json.loads((SEED / "plugin/package-lock.json").read_text())
        self.assertTrue(manifest["private"])
        self.assertEqual(lock["lockfileVersion"], 3)
        self.assertEqual(lock["packages"][""]["dependencies"], manifest["dependencies"])
        module = "node_modules/modbus-serial"
        self.assertEqual(
            lock["packages"][module]["version"],
            manifest["dependencies"]["modbus-serial"],
        )
        packages = {key: value for key, value in lock["packages"].items() if key}
        self.assertGreater(len(packages), 1)
        for name, package in packages.items():
            with self.subTest(package=name):
                self.assertTrue(name == module or name.startswith(module + "/node_modules/"))
                self.assertRegex(package["version"], r"^\d+\.\d+\.\d+$")
                self.assertTrue(package["resolved"].startswith("https://registry.npmjs.org/"))
                self.assertRegex(package["integrity"], r"^sha512-[A-Za-z0-9+/]+={0,2}$")

    def test_image_installs_only_from_the_isolated_lock(self):
        dockerfile = (ROOT / "Dockerfile.fuxa-hmi").read_text()
        self.assertIn("npm ci --prefix /opt/rangerdanger/fuxa/plugin", dockerfile)
        self.assertIn("--install-strategy=nested", dockerfile)
        self.assertNotIn("npm install ", dockerfile)
        self.assertIn(
            "mv /opt/rangerdanger/fuxa/plugin/node_modules/modbus-serial node_modules/",
            dockerfile,
        )
        self.assertIn("sha256sum -c /tmp/fuxa-manifests.sha256", dockerfile)

    def test_project_registers_and_read_only_view(self):
        project = json.loads((SEED / "starter.fuxap").read_text())
        device, = project["devices"].values()
        self.assertEqual(device["type"], "ModbusTCP")
        self.assertEqual(device["property"]["address"], "10.30.30.20:502")
        self.assertEqual(device["property"]["slaveid"], 1)
        self.assertTrue(device["enabled"])
        self.assertEqual(device["polling"], 2000)
        # Independent transcription of services/rtac-sim/modbus.go's map.
        expected = {
            "t_breaker": ("400000", "1", "UInt16", 1),
            "t_recloser": ("400000", "2", "UInt16", 1),
            "t_reclose": ("400000", "3", "UInt16", 1),
            "t_tap": ("400000", "4", "Int16", 1),
            "t_voltage": ("300000", "1", "UInt16", 10),
            "t_current": ("300000", "4", "UInt16", 10),
        }
        self.assertEqual({
            key: (tag["memaddress"], tag["address"], tag["type"], tag["divisor"])
            for key, tag in device["tags"].items()
        }, expected)
        view, = project["hmi"]["views"]
        self.assertEqual(project["hmi"]["layout"]["start"], view["id"])
        svg = ET.fromstring(view["svgcontent"])
        self.assertNotIn("viewBox", svg.attrib)
        ids = {element.get("id") for element in svg.iter()}
        bindings = set()
        for item in view["items"].values():
            self.assertIn(item["id"], ids)
            self.assertEqual(item["type"], "svg-ext-value")
            prop = item["property"]
            self.assertEqual(prop["events"], [])
            self.assertEqual(prop["variableSrc"], device["id"])
            bindings.add(prop["variableId"])
        # The pinned viewer subscribes and matches device-values using bare tag
        # IDs. A device-qualified legacy ID silently filters out every value.
        self.assertEqual(bindings, set(expected))
        self.assertNotIn("value", device["tags"]["t_breaker"])
        self.assertIn("/substation", view["svgcontent"])


if __name__ == "__main__":
    unittest.main()
