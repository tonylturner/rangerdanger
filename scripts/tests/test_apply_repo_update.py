import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = ("apply-repo-update.sh", "apply-repo-update.ps1")

# These are executables on an isolated PATH, not mocks of a live Docker daemon.
STUB = """#!{python}
import json
import os
import sys
from pathlib import Path

tool = Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["TEST_LOG"], "a") as log:
    log.write(json.dumps([tool, *args]) + "\\n")
failure = os.environ.get("TEST_FAIL", "")
if tool == "git":
    if "rev-parse" in args:
        if failure == "git":
            sys.exit(1)
        print(os.environ["TEST_ROOT"])
    elif "status" in args:
        if failure == "status":
            sys.exit(1)
        if os.environ.get("TEST_DIRTY"):
            print(os.environ["TEST_DIRTY"])
    else:
        sys.exit(99)
elif args[:2] == ["image", "inspect"]:
    if args[2] in os.environ.get("TEST_MISSING", "").split(","):
        sys.exit(1)
elif args[0] == "compose":
    if args[-2:] == ["up", "--help"]:
        if failure == "help":
            sys.exit(1)
        print("--no-build" if failure == "old" else "--pull string  always|missing|never")
    elif args[-2:] == ["config", "--images"]:
        if failure == "images":
            sys.exit(1)
        if failure != "empty":
            print("ghcr.io/example/backend:latest\\nnginx:1.27-alpine\\nghcr.io/example/vendor:v0.1.35")
    elif args[-2:] == ["config", "-q"]:
        if failure == "config":
            sys.exit(1)
    elif args[-3:] == ["restart", "backend", "proxy"]:
        if failure == "restart":
            sys.exit(1)
    elif args[-8:] == ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", "vendor_jump"]:
        if failure == "recreate":
            sys.exit(1)
    else:
        sys.exit(99)
else:
    sys.exit(99)
"""


class UpdateScriptStaticTests(unittest.TestCase):
    def test_twins_only_use_safe_recreate_flags_and_never_download(self):
        for name in SCRIPTS:
            with self.subTest(script=name):
                source = (ROOT / "scripts" / name).read_text()
                self.assertIn(
                    "up -d --no-deps --no-build --pull never --force-recreate vendor_jump",
                    source,
                )
                self.assertIn("restart backend proxy", source)
                self.assertIn("config -q", source)
                self.assertIn("config --images", source)
                self.assertIn("image inspect", source)
                self.assertIn("status --porcelain --untracked-files=no", source)
                self.assertIn("docker-compose.offline.yml", source)
                self.assertIn("--project-directory", source)
                self.assertNotRegex(source, r"(?i)(?<![\w-])down(?![\w-])")
                self.assertNotRegex(source, r"(?<![\w-])-[vV](?![\w-])")
                self.assertNotIn("--renew-anon-volumes", source)
                # A pull policy flag is permitted; a pull command is not.
                self.assertNotRegex(source, r"(?im)^\s*&?\s*docker\b[^\n]*?(?<!-)\bpull\b")
                self.assertNotRegex(source, r"(?im)^\s*(?:&\s*)?docker\b[^\n]*(?<![\w-])build(?![\w-])")
                self.assertNotRegex(source, r"(?im)^\s*(?:Set-Content|Add-Content|Remove-Item)\b")
                self.assertNotIn("sed -i", source)

    def test_bash_syntax_and_shellcheck(self):
        script = str(ROOT / "scripts/apply-repo-update.sh")
        subprocess.run(["bash", "-n", script], check=True, timeout=30)
        if shutil.which("shellcheck"):
            subprocess.run(["shellcheck", script], check=True, timeout=30)


class UpdateScriptStubTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="repo update ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        (self.root / ".git").mkdir()
        (self.root / "scripts").mkdir()
        for name in SCRIPTS:
            shutil.copyfile(ROOT / "scripts" / name, self.root / "scripts" / name)
        for name in ("docker-compose.release.yml", "docker-compose.offline.yml"):
            shutil.copyfile(ROOT / name, self.root / name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for name in ("docker", "git"):
            path = self.bin / name
            path.write_text(STUB.format(python=sys.executable))
            path.chmod(0o755)
        self.log = self.root / "calls.jsonl"
        self.env = {
            **os.environ,
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "TEST_ROOT": str(self.root),
            "TEST_LOG": str(self.log),
        }
        self.runners = [("bash", ["bash"])]
        if shutil.which("pwsh"):
            self.runners.append(("powershell", ["pwsh", "-NoProfile", "-NonInteractive", "-File"]))
        self.kept = {}
        for name in (".env", "data/evidence.txt", "backend/data/rangerdanger.db"):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"student data, unchanged\n")
            self.kept[path] = path.read_bytes()

    def run_script(self, runner, *, offline=False, **environment):
        name, command = runner
        suffix = ".sh" if name == "bash" else ".ps1"
        args = [*command, str(self.root / "scripts" / ("apply-repo-update" + suffix))]
        if offline:
            flag = "--from-tarballs" if name == "bash" else "-FromTarballs"
            args += [flag, str(self.root)]
        self.log.unlink(missing_ok=True)
        result = subprocess.run(
            args, env={**self.env, **environment}, cwd=self.root.parent,
            capture_output=True, text=True, timeout=30,
        )
        calls = [json.loads(line) for line in self.log.read_text().splitlines()]
        for path, content in self.kept.items():
            self.assertEqual(path.read_bytes(), content)
        return result, calls

    @staticmethod
    def mutations(calls):
        return [
            call for call in calls
            if "restart" in call or ("up" in call and "--help" not in call)
        ]

    def test_success_and_offline_keep_data_and_use_only_cached_images(self):
        for runner in self.runners:
            for offline in (False, True):
                with self.subTest(runner=runner[0], offline=offline):
                    result, calls = self.run_script(runner, offline=offline)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn("http://localhost:8088", result.stdout)
                    self.assertEqual(len(self.mutations(calls)), 2)
                    self.assertEqual(self.mutations(calls)[0][-3:], ["restart", "backend", "proxy"])
                    self.assertEqual(self.mutations(calls)[1][-8:], [
                        "up", "-d", "--no-deps", "--no-build", "--pull", "never",
                        "--force-recreate", "vendor_jump",
                    ])
                    inspections = [call for call in calls if call[:3] == ["docker", "image", "inspect"]]
                    self.assertEqual(len(inspections), 3)
                    config = next(i for i, call in enumerate(calls) if call[-2:] == ["config", "-q"])
                    self.assertLess(max(calls.index(call) for call in inspections), config)
                    self.assertLess(config, calls.index(self.mutations(calls)[0]))
                    for call in calls:
                        if call[:2] == ["docker", "compose"]:
                            self.assertEqual(
                                str(self.root / "docker-compose.offline.yml") in call, offline
                            )

    def test_dirty_checkout_names_edits_without_contacting_docker(self):
        for runner in self.runners:
            with self.subTest(runner=runner[0]):
                result, calls = self.run_script(
                    runner, TEST_DIRTY=" M proxy/nginx.conf\nM  scripts/set-gateway.sh"
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("proxy/nginx.conf", result.stdout + result.stderr)
                self.assertIn("scripts/set-gateway.sh", result.stdout + result.stderr)
                self.assertFalse(any(call[0] == "docker" for call in calls))

    def test_archive_skips_git_guard_and_keeps_offline_overlay(self):
        (self.root / ".git").rmdir()
        for runner in self.runners:
            with self.subTest(runner=runner[0]):
                result, calls = self.run_script(runner, offline=True, TEST_FAIL="git")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("No .git metadata; skipped Git tracked-file guard", result.stdout)
                self.assertFalse(any(call[0] == "git" for call in calls))
                for call in calls:
                    if call[:2] == ["docker", "compose"]:
                        self.assertIn(str(self.root / "docker-compose.offline.yml"), call)
                self.assertEqual(len(self.mutations(calls)), 2)

    def test_git_metadata_file_keeps_tracked_edit_guard(self):
        (self.root / ".git").rmdir()
        (self.root / ".git").write_text("gitdir: /fixture/worktree\n")
        for runner in self.runners:
            with self.subTest(runner=runner[0]):
                result, calls = self.run_script(runner, TEST_DIRTY=" M proxy/nginx.conf")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("proxy/nginx.conf", result.stdout + result.stderr)
                self.assertFalse(any(call[0] == "docker" for call in calls))

    def test_archive_keeps_all_non_git_preflights(self):
        (self.root / ".git").rmdir()
        for runner in self.runners:
            for failure in ("old", "help", "images", "empty", "config"):
                with self.subTest(runner=runner[0], failure=failure):
                    result, calls = self.run_script(runner, offline=True, TEST_FAIL=failure)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(self.mutations(calls), [])
            result, calls = self.run_script(runner, offline=True, TEST_MISSING="nginx:1.27-alpine")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("nginx:1.27-alpine", result.stdout + result.stderr)
            self.assertEqual(self.mutations(calls), [])

    def test_real_git_checkout_with_tracked_edits_is_refused(self):
        # Use real Git for the guard; Docker remains an isolated executable stub.
        (self.bin / "git").unlink()
        for args in (
            ["init", "-q", str(self.root)],
            ["-C", str(self.root), "add", "scripts", "docker-compose.release.yml"],
            ["-C", str(self.root), "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "-c", "commit.gpgsign=false", "commit", "-qm", "fixture"],
        ):
            subprocess.run(["git", *args], check=True, capture_output=True, timeout=30)
        with (self.root / "docker-compose.release.yml").open("a") as stream:
            stream.write("\n# local edit\n")
        for runner in self.runners:
            with self.subTest(runner=runner[0]):
                self.log.unlink(missing_ok=True)
                name, command = runner
                suffix = ".sh" if name == "bash" else ".ps1"
                result = subprocess.run(
                    [*command, str(self.root / "scripts" / ("apply-repo-update" + suffix))],
                    env=self.env, capture_output=True, text=True, timeout=30,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("docker-compose.release.yml", result.stdout + result.stderr)
                self.assertFalse(self.log.exists(), "Docker must not be contacted")

    def test_missing_images_are_all_named_before_any_service_change(self):
        missing = "nginx:1.27-alpine,ghcr.io/example/vendor:v0.1.35"
        for runner in self.runners:
            with self.subTest(runner=runner[0]):
                result, calls = self.run_script(runner, TEST_MISSING=missing)
                self.assertNotEqual(result.returncode, 0)
                for image in missing.split(","):
                    self.assertIn(image, result.stdout + result.stderr)
                self.assertEqual(self.mutations(calls), [])

    def test_preflight_failures_never_change_services(self):
        for runner in self.runners:
            for failure in ("git", "status", "old", "help", "images", "empty", "config"):
                with self.subTest(runner=runner[0], failure=failure):
                    result, calls = self.run_script(runner, TEST_FAIL=failure)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("Repo update applied", result.stdout)
                    self.assertEqual(self.mutations(calls), [])

    def test_restart_and_recreate_failures_never_claim_success(self):
        for runner in self.runners:
            for failure, count in (("restart", 1), ("recreate", 2)):
                with self.subTest(runner=runner[0], failure=failure):
                    result, calls = self.run_script(runner, TEST_FAIL=failure)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("Repo update applied", result.stdout)
                    self.assertEqual(len(self.mutations(calls)), count)


if __name__ == "__main__":
    unittest.main()
