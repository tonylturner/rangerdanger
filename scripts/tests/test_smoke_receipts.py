"""Archive receipt missing-container handling, without Docker or live services."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


class SmokeReceiptTests(unittest.TestCase):
    def run_receipt(self, mode):
        workflow = yaml.safe_load((ROOT / ".github/workflows/smoke.yml").read_text())
        steps = [step for job in workflow["jobs"].values() for step in job["steps"]
                 if step.get("name") == "Archive environment receipts"]
        self.assertEqual(len(steps), 1)
        script = steps[0]["run"]
        start = script.index("if image=$(docker inspect")
        snippet = script[start:script.index("fi\n", start) + 3]
        stub = '''
docker() {
  printf '%s\\n' "$*" >>"$CALLS"
  if [ "$1" = inspect ]; then
    [ "$MODE" != absent ] || return 1
    printf 'sha256:fixture\\n'
  elif [ "$1 $2" = 'image inspect' ]; then
    [ "$MODE" != image-error ] || return 7
    printf '[{"Id":"sha256:fixture"}]\\n'
  else return 99
  fi
}
'''
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / "build/smoke-receipts").mkdir(parents=True)
            result = subprocess.run(
                ["bash", "-euo", "pipefail", "-c", stub + snippet], cwd=path,
                env={**os.environ, "MODE": mode, "CALLS": str(path / "calls")},
                text=True, capture_output=True, timeout=5)
            receipt = path / "build/smoke-receipts/containd.json"
            return result.returncode, (path / "calls").read_text(), receipt.exists()

    def test_missing_container_is_not_an_archive_error(self):
        code, calls, exists = self.run_receipt("absent")
        self.assertEqual(code, 0)
        self.assertNotIn("image inspect", calls)
        self.assertFalse(exists)

    def test_present_container_records_its_actual_image(self):
        code, calls, exists = self.run_receipt("present")
        self.assertEqual(code, 0)
        self.assertIn("image inspect sha256:fixture", calls)
        self.assertTrue(exists)

    def test_real_image_inspect_errors_are_not_swallowed(self):
        code, _, _ = self.run_receipt("image-error")
        self.assertEqual(code, 7)


if __name__ == "__main__":
    unittest.main()
