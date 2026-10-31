"""Bounded retries for SSD image pulls, using mocked Docker commands."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class StagePullRetryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.work = Path(self.temp.name)
        self.bin_dir = self.work / "bin"
        self.bin_dir.mkdir()
        self.calls_file = self.work / "docker-calls"
        docker = self.bin_dir / "docker"
        docker.write_text(
            """#!/bin/sh
test "$1" = pull || exit 88
count=0
if [ -f "$RD_PULL_CALLS" ]; then count=$(cat "$RD_PULL_CALLS"); fi
count=$((count + 1))
printf '%s\\n' "$count" > "$RD_PULL_CALLS"
printf 'mock docker progress %s\\n' "$count"
if [ "$count" -le "$RD_DOCKER_FAIL_UNTIL" ]; then
    echo 'mock registry failure' >&2
    exit 17
fi
""",
            encoding="utf-8",
        )
        docker.chmod(0o755)
        self.env = dict(
            os.environ,
            PATH=f"{self.bin_dir}{os.pathsep}{os.environ['PATH']}",
            RD_PULL_CALLS=str(self.calls_file),
            RD_DOCKER_FAIL_UNTIL="0",
            RD_STAGE_PULL_ATTEMPTS="3",
            RD_STAGE_PULL_DELAY_S="0",
        )

    def tearDown(self):
        self.temp.cleanup()

    def run_bash_helper(self, fail_until):
        env = dict(self.env, RD_DOCKER_FAIL_UNTIL=str(fail_until))
        script = r"""
set -u
say() { printf '[+] %s\n' "$*"; }
die() { printf 'FINAL: %s\n' "$*" >&2; exit 1; }
. "$RD_REPO/scripts/lib/stage-pull.sh"
image=ghcr.io/tonylturner/rangerdanger-corp-ws:v2
ref=image@sha256:fixture
stage_pull_image "$image" "$ref" \
    || die "docker pull failed for $ref on amd64 — see Docker's error above for the cause; refusing to write a partial bundle."
"""
        return subprocess.run(
            ["/bin/bash", "-c", script],
            cwd=ROOT,
            env=dict(env, RD_REPO=str(ROOT)),
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

    def test_bash_helper_retries_then_succeeds_and_shows_desktop_heads_up(self):
        result = self.run_bash_helper(fail_until=1)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls_file.read_text().strip(), "2")
        self.assertIn("large image (~2-3 GB desktop)", result.stdout)
        self.assertIn("retrying in 0s after registry/pull error", result.stdout)
        self.assertIn("mock docker progress 2", result.stdout)

    def test_bash_helper_stops_after_three_and_preserves_caller_failure(self):
        result = self.run_bash_helper(fail_until=9)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertEqual(self.calls_file.read_text().strip(), "3")
        self.assertIn(
            "FINAL: docker pull failed for image@sha256:fixture on amd64 — "
            "see Docker's error above for the cause; refusing to write a partial bundle.",
            result.stderr,
        )
        self.assertEqual(result.stdout.count("retrying in 0s"), 2)

    def test_both_bash_callers_keep_their_final_failure_messages(self):
        full = (ROOT / "stage-ssd.sh").read_text()
        delta = (ROOT / "stage-ssd-delta.sh").read_text()
        self.assertIn(
            """stage_pull_image "$img" "$ref" \\
            || die "docker pull failed for $ref on $arch — see Docker's error above for the cause; refusing to write a partial bundle." """.strip(),
            full,
        )
        self.assertIn(
            """stage_pull_image "$img" "$ref" \\
            || die "docker pull failed for new-version image $ref on $arch; see Docker's error above for the cause." """.strip(),
            delta,
        )

    def run_powershell_pull(self, mode):
        script = r"""
. "$env:REPO/scripts/lib/docker-bounded.ps1"
. "$env:REPO/scripts/lib/stage-bounded.ps1"
$script:calls = 0
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, [switch]$StreamOutput) {
    if (($DockerArgs -join ' ') -cne 'pull image@sha256:fixture') { throw 'pull args changed' }
    if ($TimeoutSec -ne 3600 -or -not $StreamOutput) { throw 'pull budget/progress changed' }
    $script:calls++
    if ($env:MODE -eq 'timeout') {
        return [pscustomobject]@{ExitCode=-1; TimedOut=$true; StdOut=''; StdErr=''}
    }
    $code = if ($env:MODE -eq 'success-second' -and $script:calls -eq 2) { 0 } else { 17 }
    return [pscustomobject]@{ExitCode=$code; TimedOut=$false; StdOut='payload'; StdErr='registry failure'}
}
try {
    $payload = Invoke-StagePull 'ghcr.io/tonylturner/rangerdanger-corp-ws:v2' 'image@sha256:fixture' 3600 -StreamOutput
    Write-Output "PAYLOAD=$payload"
} catch {
    Write-Output "ERROR=$($_.Exception.Message)"
}
Write-Output "CALLS=$script:calls"
"""
        return powershell(
            script,
            MODE=mode,
            RD_STAGE_PULL_ATTEMPTS="3",
            RD_STAGE_PULL_DELAY_S="0",
        )

    def test_powershell_pull_retries_nonzero_and_prints_corp_desktop_heads_up(self):
        result = self.run_powershell_pull("success-second")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("CALLS=2", result.stdout)
        self.assertIn("PAYLOAD=payload", result.stdout)
        self.assertIn("large image (~2-3 GB desktop)", result.stdout)
        self.assertIn("retrying in 0s after registry/pull error", result.stdout)

    def test_powershell_gives_up_after_three_and_preserves_failure(self):
        result = self.run_powershell_pull("failure")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("CALLS=3", result.stdout)
        self.assertIn("ERROR=docker pull image@sha256:fixture failed (exit 17): registry failure", result.stdout)

    def test_powershell_timeout_is_not_retried(self):
        result = self.run_powershell_pull("timeout")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("CALLS=1", result.stdout)
        self.assertIn("ERROR=docker pull image@sha256:fixture timed out after 3600s", result.stdout)
        self.assertNotIn("retrying", result.stdout)

    def test_all_four_stage_scripts_route_pulls_through_the_shared_helpers(self):
        for name in ("stage-ssd.sh", "stage-ssd-delta.sh"):
            text = (ROOT / name).read_text()
            self.assertIn('stage_pull_image "$img" "$ref"', text)
            self.assertNotIn('docker pull "$ref"', text)
        for name in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            text = (ROOT / name).read_text()
            self.assertIn("Invoke-StagePull $img $ref 3600 -StreamOutput", text)
            self.assertNotIn("Invoke-StageDocker @('pull', $ref)", text)


if __name__ == "__main__":
    unittest.main()
