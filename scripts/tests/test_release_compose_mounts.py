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
        for name in ("lab-definitions/packages/us-dnp3-substation/compose.source.yml",
                     "lab-definitions/packages/us-dnp3-substation/compose.release.yml"):
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

    def test_offline_uses_same_package_mounts_with_pull_never(self):
        self.assertFalse((ROOT / "docker-compose.offline.yml").exists())
        source = (ROOT / "setup.sh").read_text()
        self.assertIn('RANGE_FILE_NAME="compose.release.yml"', source)
        self.assertIn('[ "$MODE" = offline ] && UP_FLAGS+=(--pull never)', source)
        lifecycle = (ROOT / "backend/internal/lifecycle/compose.go").read_text()
        self.assertIn('"--pull", "never"', lifecycle)
        # Package models, not a replacement offline model, own these mounts.
        for name in ("docker-compose.yml", "docker-compose.release.yml"):
            self.assertNotIn("vendor_jump", yaml.safe_load((ROOT / name).read_text())["services"])


if __name__ == "__main__":
    unittest.main()
