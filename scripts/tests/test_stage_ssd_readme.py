"""Render only the full-bundle READMEs; no staging or Docker calls."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class FullReadmeRenderTests(unittest.TestCase):
    def test_readme_byte_parity_and_whole_folder_guidance(self):
        shell = (ROOT / "stage-ssd.sh").read_text()
        shell_row = re.search(r'^KERNEL_README_ROW="- .*"$', shell, re.M).group()
        shell_block = shell[shell.index('cat > "$OUT/README.md"'):
                            shell.index('say "wrote $OUT/README.md"')]
        ps = (ROOT / "stage-ssd.ps1").read_text()
        ps_row = re.search(r'^        \$kernelReadmeRow = ".*"$', ps, re.M).group()
        ps_block = ps[ps.index('Banner "Write README"'):
                      ps.index('Say "wrote $OutDir\\README.md"')]
        for subject in ("Document offline install", "A" * 100, "é🙂" * 50):
            with self.subTest(subject=subject), tempfile.TemporaryDirectory() as tmp:
                bash_out = Path(tmp) / "bash"
                ps_out = Path(tmp) / "powershell"
                bash_out.mkdir()
                ps_out.mkdir()
                script = r"""
date() { printf '%s\n' '2026-11-10T12:00:00Z'; }
git() {
    case "$*" in
        *rev-parse*) printf '%s\n' '12345678' ;;
        *log*) printf '%s\n' "$SUBJECT" ;;
        *) return 99 ;;
    esac
}
""" + shell_row + "\n" + shell_block
                result = subprocess.run(
                    ["bash", "-c", script], capture_output=True, text=True,
                    env=dict(os.environ, OUT=str(bash_out), VERSION="v2",
                             ROOT_DIR=str(ROOT), SUBJECT=subject, SELECTED_PACKAGES="us-dnp3-substation",
                             BINFMT_IMAGE="tonistiigi/binfmt:fixture"),
                    timeout=30,
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                result = powershell(r"""
$ErrorActionPreference = 'Stop'
function Banner($Message) {}
function Get-Date { [datetime]'2026-11-10T12:00:00Z' }
function Invoke-StageNative($Executable, $ArgList, $TimeoutSec) {
    if ($ArgList -contains 'rev-parse') { return '12345678' }
    if ($ArgList -contains 'log') { return $env:SUBJECT }
    throw 'unexpected native call'
}
$OutDir = $env:OUT
$RootDir = $env:REPO
$Version = 'v2'
$selected = @('us-dnp3-substation')
$BINFMT_IMAGE = 'tonistiigi/binfmt:fixture'
""" + ps_row + "\n" + ps_block, OUT=str(ps_out), SUBJECT=subject)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                actual = (bash_out / "README.md").read_bytes()
                self.assertEqual(actual, (ps_out / "README.md").read_bytes())
                self.assertIn(b"Copy the whole folder, including hidden files such as .version", actual)
                self.assertNotIn(b"Copy the four files", actual)
                self.assertIn(b"rangerdanger-wsl2-kernel", actual)
                self.assertIn(b"--packages us-dnp3-substation", actual)
                self.assertIn(b"-Packages us-dnp3-substation", actual)
                self.assertTrue(actual.endswith(b"\n"))
                self.assertNotIn(b"\r", actual)
