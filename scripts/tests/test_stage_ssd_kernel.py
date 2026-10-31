"""Full SSD kernel assets are required; requests are mocked, never networked."""
import hashlib
from pathlib import Path
import tempfile
import unittest

from .test_setup_bounds import run_bash
from .test_setup_ps1_root import ROOT, powershell


class StageKernelTests(unittest.TestCase):
    def test_stager_is_ascii_bom_free_and_parses(self):
        data = (ROOT / "stage-ssd.ps1").read_bytes()
        self.assertTrue(data.isascii())
        self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
        result = powershell("""
$t=$null; $e=$null
[Management.Automation.Language.Parser]::ParseFile("$env:REPO/stage-ssd.ps1", [ref]$t, [ref]$e) | Out-Null
if ($e.Count) { throw ($e | Out-String) }
""")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_powershell_distinguishes_confirmed_absence_from_request_failure(self):
        for mode in ("404", "403", "500", "timeout"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                result = powershell(r"""
$ErrorActionPreference='Stop'
$source=[IO.File]::ReadAllText("$env:REPO/stage-ssd.ps1")
$start=$source.IndexOf('$kernelPublished =')
$end=$source.IndexOf('Banner "Write README"')
function Die($Message) { throw $Message }
function Invoke-WebRequest {
    param($Uri, $Method, [switch]$UseBasicParsing, $TimeoutSec, $ErrorAction)
    if ($Method -ne 'Head' -or $TimeoutSec -ne 10) { throw 'unexpected request' }
    $error_=New-Object System.Exception 'fixture request failure'
    if ($env:MODE -ne 'timeout') {
        $error_ | Add-Member NoteProperty Response ([pscustomobject]@{StatusCode=[int]$env:MODE})
    }
    throw $error_
}
$kernelUrl='fixture'; $Version='v2'
try { Invoke-Expression $source.Substring($start, $end-$start); exit 99 }
catch { Write-Output $_ }
""", MODE=mode)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("Bundle incomplete", result.stdout)
                self.assertIn("absent (HTTP 404)" if mode == "404" else
                              "request failed (not confirmed absent)", result.stdout)

    def test_bash_missing_request_failure_and_checksum_verdicts(self):
        source = (ROOT / "stage-ssd.sh").read_text()
        block = source[source.index('KERNEL_README_ROW=""'):
                       source.index('banner "Write README"')]
        mock = r"""
say() { :; }
banner() { :; }
die() { echo "$*" >&2; exit 1; }
curl() {
    local output="" head=0 budget="" connect="" url=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --head) head=1 ;;
            --max-time) shift; budget="$1" ;;
            --connect-timeout) shift; connect="$1" ;;
            -o) shift; output="$1" ;;
            https:*) url="$1" ;;
        esac
        shift
    done
    [ "$connect" = 10 ] || return 99
    if [ "$head" = 1 ]; then
        [ "$budget" = 30 ] || return 99
        case "$MODE" in
            timeout) return 28 ;;
            404|403|500) printf '%s' "$MODE"; return 0 ;;
            *) printf '200'; return 0 ;;
        esac
    fi
    case "$url" in
        *.sha256)
            [ "$budget" = 120 ] || return 99
            [ "$MODE" != checksum-timeout ] || return 28
            if [ "$MODE" = invalid ]; then echo invalid > "$output"
            elif [ "$MODE" = mismatch ]; then printf '%064d\n' 0 > "$output"
            else printf '%s  kernel\n' "$SHA" > "$output"; fi ;;
        *)
            [ "$budget" = 600 ] || return 99
            printf fixture > "$output"
            [ "$MODE" != kernel-timeout ] || return 28 ;;
    esac
}
"""
        for mode in ("ok", "404", "403", "500", "timeout", "kernel-timeout",
                     "checksum-timeout", "invalid", "mismatch"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                result = run_bash(mock + block, OUT=tmp, MODE=mode, VERSION="v2",
                                  SHA=hashlib.sha256(b"fixture").hexdigest())
                self.assertEqual(result.returncode, 0 if mode == "ok" else 1,
                                 result.stdout + result.stderr)
                self.assertEqual(len(list(Path(tmp).iterdir())), 2 if mode == "ok" else 0)
                if mode == "404":
                    self.assertIn("absent (HTTP 404)", result.stderr)
                elif mode in ("403", "500", "timeout"):
                    self.assertIn("request failed", result.stderr)
                    self.assertIn("not confirmed absent", result.stderr)
