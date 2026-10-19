import gzip
import hashlib
import io
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
TAG = "v0.1.36-rc.3"
SCRIPT = "stage-ssd-repo-update.sh"


class StageRepoUpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ssd repo stage ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.out = self.root / "output with spaces"
        shutil.copyfile(ROOT / SCRIPT, self.repo / SCRIPT)
        self.git("init", "-q")
        self.git("config", "user.name", "Test")
        self.git("config", "user.email", "test@example.invalid")
        (self.repo / "payload.txt").write_text("tag content\n")
        self.git("add", ".")
        self.git("-c", "commit.gpgsign=false", "commit", "-qm", "tagged snapshot")
        self.git("-c", "tag.gpgsign=false", "tag", "-a", TAG, "-m", "release")
        # Both HEAD and the working tree differ from the release tag.
        (self.repo / "payload.txt").write_text("HEAD content\n")
        self.git("add", "payload.txt")
        self.git("-c", "commit.gpgsign=false", "commit", "-qm", "after tag")
        (self.repo / "payload.txt").write_text("dirty working tree\n")
        (self.repo / "untracked.txt").write_text("never ship\n")

    def git(self, *args):
        return subprocess.run(
            ["git", "-C", str(self.repo), *args], check=True,
            capture_output=True, timeout=30,
        ).stdout

    def stage(self, tag=TAG):
        return subprocess.run(
            ["/bin/bash", str(self.repo / SCRIPT), str(self.out), tag],
            cwd=self.root, capture_output=True, text=True, timeout=30,
        )

    def test_stages_exact_tag_archive_bundle_checksums_and_readme(self):
        result = self.stage()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        archive = f"rangerdanger-{TAG}.tgz"
        bundle = f"rangerdanger-{TAG}.bundle"
        self.assertEqual({p.name for p in self.out.iterdir()}, {
            archive, bundle, "SHA256SUMS", "REPO-UPDATE-README.md",
        })
        expected = self.git("archive", "--prefix=rangerdanger/", TAG)
        self.assertEqual(gzip.decompress((self.out / archive).read_bytes()), expected)
        with tarfile.open(fileobj=io.BytesIO(expected)) as reference:
            with tarfile.open(self.out / archive) as staged:
                self.assertEqual(staged.getnames(), reference.getnames())
                self.assertEqual(staged.getnames()[0], "rangerdanger")
                self.assertTrue(all(n == "rangerdanger" or n.startswith("rangerdanger/")
                                    for n in staged.getnames()))
                for want, got in zip(reference.getmembers(), staged.getmembers()):
                    self.assertEqual((got.mode, got.type, got.linkname),
                                     (want.mode, want.type, want.linkname))
                    if want.isfile():
                        self.assertEqual(staged.extractfile(got).read(), reference.extractfile(want).read())
        verification = self.git("bundle", "verify", str(self.out / bundle))
        self.assertIn(f"refs/tags/{TAG}".encode(), verification)
        self.assertIn(f"refs/tags/{TAG}".encode(),
                      self.git("bundle", "list-heads", str(self.out / bundle)))
        # A fresh clone proves that the bundle has no missing prerequisites.
        clone = self.root / "offline clone"
        subprocess.run(["git", "clone", "-q", str(self.out / bundle), str(clone)],
                       check=True, capture_output=True, timeout=30)
        fetched = subprocess.run(
            ["git", "-C", str(clone), "rev-parse", f"refs/tags/{TAG}"],
            check=True, capture_output=True, timeout=30,
        ).stdout
        self.assertEqual(fetched, self.git("rev-parse", f"refs/tags/{TAG}"))
        for line in (self.out / "SHA256SUMS").read_text().splitlines():
            digest, name = line.split("  ", 1)
            self.assertEqual(digest, hashlib.sha256((self.out / name).read_bytes()).hexdigest())
        subprocess.run(["shasum", "-a", "256", "-c", "SHA256SUMS"], cwd=self.out,
                       check=True, capture_output=True, timeout=30)
        readme = (self.out / "REPO-UPDATE-README.md").read_text()
        for phrase in (
            f'tar xzf "$SSD/{archive}" -C ~', f'git fetch "$SSD/{bundle}" tag {TAG}',
            "./scripts/apply-repo-update.sh --from-tarballs", "Get-FileHash",
            "tar.exe -xzf", r".\scripts\apply-repo-update.ps1 -FromTarballs",
            "checksum", "VERSION=latest", "no .git",
        ):
            self.assertIn(phrase, readme)
        if shutil.which("pwsh"):
            # Exercise the published checksum recipe, including its refusal path.
            recipe = readme.split("```powershell\n", 1)[1].split("```", 1)[0]
            recipe = ("$SSD = '" + str(self.out).replace("'", "''") + "'\n"
                      + recipe.split("\n", 1)[1])
            command = ["pwsh", "-NoProfile", "-NonInteractive", "-Command", recipe]
            subprocess.run(command, check=True, capture_output=True, timeout=30)
            with (self.out / archive).open("ab") as stream:
                stream.write(b"tampered")
            rejected = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn("Checksum mismatch", rejected.stderr)

    def test_missing_tag_refused_without_output(self):
        result = self.stage("v9.9.9-missing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Tag does not exist", result.stderr)
        self.assertFalse(self.out.exists())

    def test_branch_name_is_not_accepted_as_a_tag(self):
        self.git("branch", "v9.9.9-branch")
        result = self.stage("v9.9.9-branch")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.out.exists())

    def test_existing_outputs_are_not_overwritten(self):
        self.out.mkdir()
        sums = self.out / "SHA256SUMS"
        sums.write_text("keep me\n")
        result = self.stage()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sums.read_text(), "keep me\n")
        self.assertEqual(list(self.out.iterdir()), [sums])

    def test_bash_syntax_and_shellcheck(self):
        subprocess.run(["bash", "-n", str(ROOT / SCRIPT)], check=True, timeout=30)
        if shutil.which("shellcheck"):
            subprocess.run(["shellcheck", str(ROOT / SCRIPT)], check=True, timeout=30)


if __name__ == "__main__":
    unittest.main()
