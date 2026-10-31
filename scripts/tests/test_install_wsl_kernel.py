"""Kernel installer safety with real file IO and mocked external operations."""
import hashlib
from pathlib import Path
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


LOAD = r"""
$ErrorActionPreference='Stop'
$source=[IO.File]::ReadAllText("$env:REPO/scripts/install-wsl-kernel.ps1")
$t=$null; $e=$null
$ast=[Management.Automation.Language.Parser]::ParseInput($source, [ref]$t, [ref]$e)
if ($e.Count) { throw ($e | Out-String) }
foreach ($f in $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]}, $true)) {
    Invoke-Expression $f.Extent.Text
}
$Managed=@{
    KernelDir=(Join-Path $env:WORK 'managed')
    KernelTarget=(Join-Path $env:WORK 'managed/kernel')
    KernelFile='rangerdanger-wsl2-kernel'
    Sha256File='rangerdanger-wsl2-kernel.sha256'
    WslConfig=(Join-Path $env:WORK '.wslconfig')
}
$Managed.WslConfigBak="$($Managed.WslConfig).bak"
$Managed.WslConfigBakFg="$($Managed.WslConfig).bak.foreign"
$Managed.WslConfigKernelValue=$env:KERNEL_VALUE
"""

MAIN = r"""
function Test-WindowsWsl2Backend { @{Supported=$true; Kernel='WSL2 fixture'} }
function Test-NftQueueSupported {
    if ($DryRun) { throw 'DryRun must not run a container or nft probe' }
    @{Result=$env:PROBE; ProbeSource='fixture'}
}
function Get-DockerInfoBounded($Format, $TimeoutSec) {
    if ($Format -ne '{{.Architecture}}' -or $TimeoutSec -ne 30) { throw 'unbounded architecture query' }
    $env:ARCH
}
function Invoke-DownloadVerified { throw 'unexpected download' }
function Invoke-WslShutdown { throw 'unexpected shutdown' }
function Read-Host { 'n' }
$DryRun=$env:DRY -eq '1'
$Restore=$env:RESTORE -eq '1'
$Force=$env:FORCE -eq '1'
$KernelPath=$env:LOCAL_KERNEL
$ExpectedSha256=$env:SHA
$Test=$false; $Yes=$false
Invoke-Expression $source.Substring($source.IndexOf('# --- Mode: -Test'))
"""


class KernelInstallerTests(unittest.TestCase):
    def test_dry_run_and_declined_install_do_not_change_any_files(self):
        for mode in ("download", "local", "foreign", "restore", "decline"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                config = root / ".wslconfig"
                config.write_text("[wsl2]\nkernel=foreign\n# café\n", encoding="utf-8")
                local = root / "local-kernel"
                local.write_bytes(b"fixture")
                (root / "managed").mkdir()
                (root / "managed/kernel").write_bytes(b"old managed kernel")
                before = {p.relative_to(root): p.read_bytes()
                          for p in root.rglob("*") if p.is_file()}
                result = powershell(
                    LOAD + MAIN, WORK=tmp, KERNEL_VALUE="C:/Users/André/kernel",
                    DRY=str(int(mode != "decline")), RESTORE=str(int(mode == "restore")),
                    FORCE="1", ARCH="amd64", PROBE="Missing", SHA="",
                    LOCAL_KERNEL=str(local) if mode == "local" else "",
                )
                self.assertEqual(result.returncode, 10 if mode == "decline" else 0,
                                 result.stdout + result.stderr)
                self.assertIn("No changes made", result.stdout)
                self.assertEqual(before, {p.relative_to(root): p.read_bytes()
                                          for p in root.rglob("*") if p.is_file()})

    def test_unicode_merge_write_and_partial_restore_preserve_other_content(self):
        for encoding in ("utf-8", "utf-8-sig", "utf-16"):
            with self.subTest(encoding=encoding), tempfile.TemporaryDirectory() as tmp:
                config = Path(tmp) / ".wslconfig"
                config.write_text("# café 日本\n[wsl2]\nmemory=4GB\n", encoding=encoding)
                result = powershell(LOAD + r"""
$merge=Get-WslConfigMerged $Managed.WslConfigKernelValue
Write-WslConfigText $merge.Merged
""", WORK=tmp, KERNEL_VALUE="C:/Users/André/日本/kernel")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                content = config.read_text(encoding="utf-8")
                self.assertIn("# café 日本", content)
                self.assertIn("kernel=C:/Users/André/日本/kernel", content)
                self.assertFalse(config.read_bytes().startswith(b"\xef\xbb\xbf"))
                result = powershell(LOAD + r"""
$Restore=$true; $DryRun=$false; $Test=$false
function Invoke-WslShutdown {}
function Wait-DockerReconnect {}
Invoke-Expression $source.Substring($source.IndexOf('# --- Mode: -Test'))
""", WORK=tmp, KERNEL_VALUE="C:/Users/André/日本/kernel")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                restored = config.read_text(encoding="utf-8")
                self.assertIn("# café 日本", restored)
                self.assertIn("memory=4GB", restored)
                self.assertNotIn("kernel=", restored)

    def test_partial_restore_keeps_comments_and_other_section_headers(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / ".wslconfig"
            config.write_text("# café\n[wsl2]\nkernel=C:/kernel\n[experimental]\n")
            result = powershell(LOAD + r"""
$Restore=$true; $DryRun=$false; $Test=$false
function Invoke-WslShutdown {}
function Wait-DockerReconnect {}
Invoke-Expression $source.Substring($source.IndexOf('# --- Mode: -Test'))
""", WORK=tmp, KERNEL_VALUE="C:/kernel")
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(config.read_text(), "# café\n[wsl2]\n[experimental]\n")

    def test_confirmed_local_install_preserves_unicode_and_original_backup(self):
        with tempfile.TemporaryDirectory(prefix="kernel André ") as tmp:
            root = Path(tmp)
            original = "# café 日本\n[wsl2]\nmemory=4GB\n".encode("utf-8")
            (root / ".wslconfig").write_bytes(original)
            local = root / "input"
            local.write_bytes(b"fixture")
            driver = MAIN.replace(
                "Invoke-Expression $source.Substring",
                """
$Yes=$true
function Invoke-WslShutdown {}
function Wait-DockerReconnect { 'fixture kernel' }
Invoke-Expression $source.Substring""",
            )
            result = powershell(
                LOAD + driver, WORK=tmp, KERNEL_VALUE=tmp + "/managed/kernel",
                DRY="0", RESTORE="0", FORCE="1", ARCH="amd64", PROBE="Supported",
                LOCAL_KERNEL=str(local), SHA=hashlib.sha256(b"fixture").hexdigest(),
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual((root / "managed/kernel").read_bytes(), b"fixture")
            self.assertEqual((root / ".wslconfig.bak").read_bytes(), original)
            self.assertIn("kernel=" + tmp + "/managed/kernel",
                          (root / ".wslconfig").read_text(encoding="utf-8"))

    def test_invalid_utf8_fails_without_rewriting_configuration(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / ".wslconfig"
            original = b"# caf\xe9\n[wsl2]\n"
            config.write_bytes(original)
            result = powershell(LOAD + """
try { Get-WslConfigMerged 'C:/kernel'; exit 99 } catch { exit 0 }
""", WORK=tmp)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(config.read_bytes(), original)

    def test_arm64_missing_refuses_but_supported_is_noop(self):
        for probe in ("Missing", "Supported"):
            with self.subTest(probe=probe), tempfile.TemporaryDirectory() as tmp:
                result = powershell(
                    LOAD + MAIN, WORK=tmp, DRY="0", RESTORE="0", FORCE="0",
                    ARCH="aarch64", PROBE=probe, KERNEL_VALUE="C:/kernel",
                )
                self.assertEqual(result.returncode, 12 if probe == "Missing" else 0,
                                 result.stdout + result.stderr)
                self.assertEqual(list(Path(tmp).iterdir()), [])
                if probe == "Missing":
                    self.assertIn("No ARM64 kernel asset", result.stdout)

    def test_shutdown_is_bounded_and_failure_is_fatal_with_recovery_message(self):
        for mode in ("ok", "timeout", "missing", "error"):
            with self.subTest(mode=mode):
                result = powershell(LOAD + r"""
function Invoke-NativeBounded($Executable, $ArgList, $TimeoutSec) {
    if ($Executable -ne 'wsl.exe' -or $ArgList.Count -ne 1 -or
        $ArgList[0] -ne '--shutdown' -or $TimeoutSec -ne 60) { throw 'shutdown budget lost' }
    if ($env:MODE -eq 'missing') { return $null }
    @{TimedOut=($env:MODE -eq 'timeout'); ExitCode=([int]($env:MODE -eq 'error')); StdErr='fixture'}
}
Invoke-WslShutdown
""", WORK="/unused", MODE=mode)
                self.assertEqual(result.returncode, 0 if mode == "ok" else 1,
                                 result.stdout + result.stderr)
                if mode != "ok":
                    self.assertIn("Restart WSL and Docker Desktop manually", result.stdout)

    def test_downloads_have_budgets_and_verify_sidecar_without_network(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = powershell(LOAD + r"""
function Invoke-WebRequest($Uri, $OutFile, [switch]$UseBasicParsing, $TimeoutSec, $ErrorAction) {
    $budget=if ($Uri -eq 'kernel') {600} else {120}
    if ($TimeoutSec -ne $budget) { throw 'download budget lost' }
    $body=if ($Uri -eq 'kernel') {'fixture'} else {$env:SHA}
    [IO.File]::WriteAllText($OutFile, $body)
}
Invoke-DownloadVerified 'kernel' 'checksum' '' (Join-Path $env:WORK 'download')
""", WORK=tmp, SHA=hashlib.sha256(b"fixture").hexdigest())
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual((Path(tmp) / "download").read_bytes(), b"fixture")

    def test_changed_powershell_is_ascii_bom_free_and_parses(self):
        for path in ("setup.ps1", "scripts/install-wsl-kernel.ps1"):
            data = (ROOT / path).read_bytes()
            self.assertTrue(data.isascii())
            self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
            result = powershell("""
$t=$null; $e=$null
[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:FILE", [ref]$t, [ref]$e) | Out-Null
if ($e.Count) { throw ($e | Out-String) }
""", FILE=path)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
