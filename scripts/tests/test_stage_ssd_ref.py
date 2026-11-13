"""Release-reference preflight for all full and delta SSD stagers."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class StageSsdRefTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.base = Path(self.temp.name)
        self.repo = self.base / "repo"
        self.repo.mkdir()
        for name in ("stage-ssd.sh", "stage-ssd.ps1",
                     "stage-ssd-delta.sh", "stage-ssd-delta.ps1"):
            shutil.copy2(ROOT / name, self.repo / name)
        shutil.copytree(ROOT / "scripts/lib", self.repo / "scripts/lib")
        package = self.repo / "lab-definitions/packages/us-dnp3-substation"
        package.mkdir(parents=True)
        (package / "package.yml").write_text("id: us-dnp3-substation\n", encoding="utf-8")
        (package / "compose.release.yml").write_text("services: {}\n", encoding="utf-8")
        (self.repo / "docker-compose.release.yml").write_text("services: {}\n", encoding="utf-8")
        self.bin_dir = self.base / "bin"
        self.bin_dir.mkdir()
        self.docker_calls = self.base / "docker-calls"
        docker = self.bin_dir / "docker"
        docker.write_text(
            "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RD_DOCKER_CALLS\"\nexit 99\n",
            encoding="utf-8",
        )
        docker.chmod(0o755)
        self.env = dict(
            os.environ,
            PATH=f"{self.bin_dir}{os.pathsep}{os.environ['PATH']}",
            RD_DOCKER_CALLS=str(self.docker_calls),
        )
        self.git("init", "-q")
        (self.repo / "fixture.txt").write_text("release one\n", encoding="utf-8")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "-qm", "release one")
        self.git("tag", "v1")
        (self.repo / "fixture.txt").write_text("release two\n", encoding="utf-8")
        self.git("add", "fixture.txt")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                 "commit", "-qm", "release two")
        self.git("tag", "v2")

    def tearDown(self):
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.run(
            ["git", "-C", str(self.repo), *args],
            check=True,
            capture_output=True,
            text=True,
            timeout=15,
        )

    def run_stager(self, name, version, output_name):
        output = self.base / output_name
        if name.endswith(".sh"):
            args = ["/bin/bash", str(self.repo / name), str(output)]
            if name == "stage-ssd.sh":
                args.append(version)
            else:
                args.extend(("v0", version))
            result = subprocess.run(
                args, cwd=self.repo, env=self.env, capture_output=True,
                text=True, timeout=30,
            )
        else:
            args = ["pwsh", "-NoLogo", "-NoProfile", "-NonInteractive",
                    "-File", str(self.repo / name), str(output)]
            if name == "stage-ssd.ps1":
                args.append(version)
            else:
                args.extend(("v0", version))
            result = subprocess.run(
                args, cwd=self.repo, env=self.env, capture_output=True,
                text=True, timeout=30,
            )
        return result, output

    def assert_preflight_refusal(self, name, version, expected):
        result, output = self.run_stager(name, version, f"{name}-{version}")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(expected, result.stdout + result.stderr)
        self.assertFalse(self.docker_calls.exists())
        self.assertFalse(output.exists())

    def test_all_stagers_refuse_tag_that_does_not_match_head_before_docker(self):
        self.assertNotEqual(
            self.git("rev-parse", "HEAD").stdout.strip(),
            self.git("rev-parse", "v1^{commit}").stdout.strip(),
        )
        for name in ("stage-ssd.sh", "stage-ssd.ps1",
                     "stage-ssd-delta.sh", "stage-ssd-delta.ps1"):
            with self.subTest(stager=name):
                self.assert_preflight_refusal(name, "v1", "HEAD does not match")

    def test_matching_release_tag_passes_preflight_to_mocked_compose(self):
        for name in ("stage-ssd.sh", "stage-ssd.ps1",
                     "stage-ssd-delta.sh", "stage-ssd-delta.ps1"):
            with self.subTest(stager=name):
                result, _ = self.run_stager(name, "v2", f"{name}-matching-tag")
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("HEAD does not match", result.stdout + result.stderr)
                self.assertTrue(self.docker_calls.is_file())
                calls = self.docker_calls.read_text(encoding="utf-8").splitlines()
                self.assertTrue(calls)
                self.assertTrue(all(call.startswith("compose ") for call in calls))
                self.docker_calls.unlink()

    def test_all_stagers_reject_mutable_latest_before_docker(self):
        for name in ("stage-ssd.sh", "stage-ssd.ps1",
                     "stage-ssd-delta.sh", "stage-ssd-delta.ps1"):
            with self.subTest(stager=name):
                self.assert_preflight_refusal(name, "latest", "mutable")

    def test_all_stagers_refuse_dirty_and_untracked_working_tree(self):
        (self.repo / "fixture.txt").write_text("dirty\n", encoding="utf-8")
        injected = self.repo / "lab-definitions/packages/untracked/package.yml"
        injected.parent.mkdir(parents=True)
        injected.write_text("id: untracked\n", encoding="utf-8")
        for name in ("stage-ssd.sh", "stage-ssd.ps1",
                     "stage-ssd-delta.sh", "stage-ssd-delta.ps1"):
            with self.subTest(stager=name):
                self.assert_preflight_refusal(name, "v2", "working tree must be clean")

    def test_operator_docs_explain_the_release_tag_precondition(self):
        for name in ("docs/quickstart.md", "docs/workshop-ssd.md"):
            text = (ROOT / name).read_text()
            with self.subTest(document=name):
                self.assertIn("HEAD", text)
                self.assertIn("clean", text)
                self.assertIn("latest", text)
                self.assertIn("git worktree add --detach", text)


if __name__ == "__main__":
    unittest.main()
