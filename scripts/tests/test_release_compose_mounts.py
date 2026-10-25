import subprocess
import unittest
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[2]
MOUNTS = (
    "./scripts/vendor-jump-services.sh:/custom-cont-init.d/01-vendor-services.sh:ro",
    "./scripts/set-gateway.sh:/custom-cont-init.d/10-set-gateway.sh:ro",
)


class ReleaseComposeMountTests(unittest.TestCase):
    def test_both_compose_files_mount_both_init_scripts(self):
        for name in ("docker-compose.yml", "docker-compose.release.yml"):
            with self.subTest(compose=name):
                compose = yaml.safe_load((ROOT / name).read_text())
                volumes = compose["services"]["vendor_jump"]["volumes"]
                for mount in MOUNTS:
                    self.assertIn(mount, volumes)
                self.assertEqual(volumes.index(MOUNTS[1]), volumes.index(MOUNTS[0]) + 1)

    def test_both_mounted_scripts_have_executable_git_mode_and_lf(self):
        for mount in MOUNTS:
            path = mount.split(":")[0].removeprefix("./")
            with self.subTest(script=path):
                entry = subprocess.run(
                    ["git", "ls-files", "-s", "--", path],
                    cwd=ROOT, check=True, capture_output=True, text=True, timeout=10,
                ).stdout
                self.assertEqual(entry.split()[0], "100755")
                self.assertNotIn(b"\r", (ROOT / path).read_bytes())

    def test_offline_overlay_does_not_replace_vendor_volumes(self):
        overlay = yaml.safe_load((ROOT / "docker-compose.offline.yml").read_text())
        vendor = overlay["services"]["vendor_jump"]
        self.assertNotIn("volumes", vendor)
        self.assertEqual(vendor["pull_policy"], "never")


if __name__ == "__main__":
    unittest.main()
