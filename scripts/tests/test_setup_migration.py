"""Installer ordering and port preflight, with isolated functions and fake tools only."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "setup.sh").read_text()


class SetupMigrationTests(unittest.TestCase):
    def test_acquisition_failure_never_tears_down_and_success_acquires_all_packages(self):
        start = SOURCE.index('# ─── image acquisition')
        end = SOURCE.index('# ─── amd64 emulation', start)
        self.assertLess(start, SOURCE.index('"$ROOT_DIR/scripts/dev-down.sh"'))
        self.assertIn('range_compose "$id" pull', SOURCE[start:end])
        for mode in ("offline", "release", "source"):
            for fail in (False, True):
                with self.subTest(mode=mode, fail=fail), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    (root / "scripts").mkdir()
                    down = root / "scripts/dev-down.sh"
                    down.write_text('#!/bin/bash\necho teardown >> "$CALLS"\n')
                    down.chmod(0o755)
                    archive = root / "images.tar"
                    archive.write_text("fixture")
                    calls = root / "calls"
                    script = r'''
set -euo pipefail
say() { :; }; banner() { :; }; die() { exit 1; }
package_ids() { printf 'us\neu\n'; }
with_pull_retry() { "$@"; }
platform_compose() { echo "platform $*" >> "$CALLS"; }
range_compose() { echo "range $*" >> "$CALLS"; [ "$FAIL" = 0 ] || [ "$1" != eu ]; }
docker() { echo "docker $*" >> "$CALLS"; [ "$FAIL" = 0 ]; }
MGMT_OWNER=rangerdanger RANGE_PROJECT=rangerdanger OLD_PLATFORM=old VERSION=v2
''' + SOURCE[start:end]
                    env = dict(os.environ, ROOT_DIR=tmp, MODE=mode, TARBALL=str(archive),
                               CALLS=str(calls), FAIL=str(int(fail)))
                    result = subprocess.run(["bash", "-c", script], env=env,
                                            capture_output=True, text=True, timeout=10)
                    lines = calls.read_text().splitlines()
                    self.assertEqual(result.returncode == 0, not fail, result.stderr)
                    self.assertEqual("teardown" in lines, not fail)
                    if not fail:
                        self.assertEqual(lines[-1], "teardown")
                        if mode != "offline":
                            self.assertTrue(any("range eu" in line for line in lines))
                            self.assertTrue(any("range us" in line for line in lines))

    def test_port_probe_distinguishes_refused_listener_unknown_and_bounded_connect(self):
        function = SOURCE.split('probe_port() {', 1)[1].split('\n}', 1)[0]
        for body, state in (("return 0", "busy"),
                            ("echo 'Connection refused' >&2; return 1", "free"),
                            ("echo 'unsupported' >&2; return 1", "unknown"),
                            ("exec sleep 20", "busy")):
            with self.subTest(body=body):
                result = subprocess.run(["bash", "-c", "set -euo pipefail\n"
                    + "bash() { " + body + "; }\nprobe_port() {" + function
                    + '\n}\nprobe_port 8088\necho "$PORT_STATE"'],
                    capture_output=True, text=True, timeout=8)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.strip(), state)

    def test_port_identity_checks_both_projects(self):
        function = SOURCE.split('lab_held_ports() {', 1)[1].split('\n}', 1)[0]
        script = '''set -euo pipefail
. "$ROOT/scripts/lib/docker-bounded.sh"
RANGE_PROJECT=rangerdanger PLATFORM_PROJECT=rangerdanger-platform
calls=""
docker() { case "$*" in
  *project=rangerdanger-platform*) echo '127.0.0.1:8088->8080/tcp' ;;
  *project=rangerdanger*) echo '127.0.0.1:9080->8080/tcp, 127.0.0.1:2222->22/tcp' ;;
  *) return 99 ;;
esac; }
lab_held_ports() {''' + function + '\n}\nlab_held_ports'
        result = subprocess.run(
            ["bash", "-c", script], env=dict(os.environ, ROOT=str(ROOT)),
            capture_output=True, text=True, timeout=5,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(set(result.stdout.split()), {"8088", "9080", "2222"})

    def test_rerun_allows_own_partial_install_but_rejects_foreign_or_unknown_ports(self):
        start = SOURCE.index('PORTS_REQUIRED="8088')
        end = SOURCE.index('# ─── resolve the offline version', start)
        for state, own, expected in (("busy", True, 0), ("free", False, 0),
                                     ("busy", False, 1), ("unknown", False, 1)):
            with self.subTest(state=state, own=own):
                script = '''set -euo pipefail
say() { :; }; banner() { :; }; die() { exit 1; }
lsof() { return 1; }
CHECK_ONLY=1
probe_port() { PORT_STATE="$STATE"; PORT_ERR=fixture; }
lab_held_ports() { [ "$OWN" = 0 ] || echo '8088 9080 9443 2222'; }
''' + SOURCE[start:end]
                result = subprocess.run(["bash", "-c", script],
                    env=dict(os.environ, STATE=state, OWN=str(int(own))),
                    capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, expected, result.stderr)
