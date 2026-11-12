"""Native host receipt/selection parity. Only extracted installer blocks run."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell

SH = (ROOT / "setup.sh").read_text()
PS = (ROOT / "setup.ps1").read_text()
HEADER = "rangerdanger-installed-packages-v1\n"
IDS = ["eu-iec104-substation", "new-package", "us-dnp3-substation"]
LOAD_PS = '. "$env:REPO/scripts/lib/installed-packages.ps1"\n. "$env:REPO/scripts/lib/setup-package.ps1"\n'


def fixture(root):
    for name in IDS:
        directory = root / "lab-definitions/packages" / name
        directory.mkdir(parents=True)
        for file in ("package.yml", "compose.source.yml", "compose.release.yml"):
            (directory / file).write_text("fixture\n")
    for name in ("docker-compose.yml", "docker-compose.release.yml"):
        (root / name).write_text("fixture\n")
    (root / "data").mkdir()
    (root / "scripts/lib").mkdir(parents=True)
    (root / "scripts/lib/installed-packages.sh").write_bytes((ROOT / "scripts/lib/installed-packages.sh").read_bytes())


def bash(script, root, **env):
    return subprocess.run(
        ["bash", "-c", 'set -euo pipefail\ndie() { echo "$*" >&2; exit 1; }\nsay() { :; }\n'
         '. "$REPO/scripts/lib/installed-packages.sh"\n' + script],
        env=dict(os.environ, REPO=str(ROOT), ROOT_DIR=str(root), **env),
        capture_output=True, text=True, timeout=10,
    )


class InstalledPackagesTests(unittest.TestCase):
    def test_native_readers_reject_noncanonical_records(self):
        valid = HEADER + "eu-iec104-substation\nus-dnp3-substation\n"
        for text in (valid, "", HEADER, HEADER.rstrip("\n") + "-invalid\neu-iec104-substation\n", HEADER + "unknown\n",
                     HEADER + "us-dnp3-substation\neu-iec104-substation\n",
                     HEADER + "eu-iec104-substation\neu-iec104-substation\n",
                     valid[:-1], valid + "\n", valid.replace("\n", "\r\n"), "\ufeff" + valid, HEADER + "../x\n",
                     valid.replace("eu-", "e\0u-", 1)):
            with self.subTest(text=repr(text)), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                fixture(root)
                file = root / "data/installed-packages"
                file.write_bytes(text.encode())
                sh = bash("read_installed_packages", root)
                ps = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + 'Read-InstalledPackages $env:TESTROOT', TESTROOT=tmp)
                for result in (sh, ps):
                    self.assertEqual(result.returncode == 0, text == valid, result.stdout + result.stderr)
                    if text != valid:
                        self.assertIn(str(file), result.stderr)
                        self.assertIn("setup", result.stderr)

    def test_native_selection_normalizes_and_rejects_empty_unknown_tokens(self):
        for value in ("us-dnp3-substation,eu-iec104-substation,us-dnp3-substation",
                      "", ",", "eu-iec104-substation,", ",eu-iec104-substation",
                      "unknown", "../x", "EU-iec104-substation", "eu-iec104-substation,,new-package"):
            with self.subTest(value=value), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                fixture(root)
                sh = bash('normalize_packages "$VALUE"', root, VALUE=value)
                ps = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS +
                                'ConvertTo-PackageSelection $env:TESTROOT @($env:VALUE)', TESTROOT=tmp, VALUE=value)
                for result in (sh, ps):
                    self.assertEqual(result.returncode == 0, value.startswith("us-"), result.stdout + result.stderr)
                    if result.returncode == 0:
                        self.assertEqual(result.stdout.splitlines(), [IDS[0], IDS[2]])

    def test_atomic_native_writers_emit_identical_lf_record(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture(root)
            for existing in (False, True):
                with self.subTest(existing=existing):
                    sh = bash('SELECTED_PACKAGES="eu-iec104-substation\nus-dnp3-substation"\nwrite_installed_packages', root)
                    self.assertEqual(sh.returncode, 0, sh.stderr)
                    expected = (root / "data/installed-packages").read_bytes()
                    if not existing:
                        (root / "data/installed-packages").unlink()
                    ps = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS +
                                    'Write-InstalledPackages $env:TESTROOT @("eu-iec104-substation","us-dnp3-substation")', TESTROOT=tmp)
                    self.assertEqual(ps.returncode, 0, ps.stderr)
                    self.assertEqual((root / "data/installed-packages").read_bytes(), expected)
                    self.assertFalse(list((root / "data").glob("*.tmp.*")))

    def test_setup_selection_defaults_preserves_record_and_validates_active(self):
        sh_block = SH[SH.index('# shellcheck source=scripts/lib/installed-packages.sh'):SH.index('# Platform and range Compose calls.')]
        ps_block = PS[PS.index('$installed = @()'):PS.index('$info = ')]
        cases = [
            (None, None, "", IDS),
            ([IDS[0]], None, "", [IDS[0]]),
            ([IDS[0]], IDS[2], "", [IDS[2]]),
            (None, IDS[2] + "," + IDS[0] + "," + IDS[2], "", [IDS[0], IDS[2]]),
            (None, IDS[0], IDS[2], None),
            (None, "", "", None),
        ]
        for recorded, flag, active, expected in cases:
            with self.subTest(recorded=recorded, flag=flag, active=active), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                fixture(root)
                if recorded:
                    (root / "data/installed-packages").write_text(HEADER + "\n".join(recorded) + "\n")
                sh = bash(sh_block + '\nprintf "%s\\nPACKAGE=%s\\n" "$SELECTED_PACKAGES" "$PACKAGE"', root,
                          PACKAGES_DIR=str(root / "lab-definitions/packages"), PACKAGES=flag or "",
                          PACKAGES_SET=str(int(flag is not None)), PACKAGE=active, ADD_PACKAGE="",
                          FROM_SOURCE="0", TARBALL_DIR="")
                ps = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + """
$RootDir=$env:TESTROOT; $Version='latest'; $Package=$env:ACTIVE; $FromSource=$false; $FromTarballs=''; $AddPackage=''
$Packages=@($env:VALUE); $PSBoundParameters=@{}
if ($env:FLAG -eq '1') { $PSBoundParameters['Packages']=$Packages }
""" + ps_block + '\nConvertTo-Json -InputObject @{ids=@($selected);package=$Package} -Compress',
                                TESTROOT=tmp, ACTIVE=active, VALUE=flag or "", FLAG=str(int(flag is not None)))
                for result in (sh, ps):
                    self.assertEqual(result.returncode == 0, expected is not None, result.stdout + result.stderr)
                if expected is not None:
                    default = active or ("us-dnp3-substation" if "us-dnp3-substation" in expected else expected[0])
                    self.assertEqual(sh.stdout.splitlines(), expected + [f"PACKAGE={default}"])
                    self.assertEqual(json.loads(ps.stdout), {"ids": expected, "package": default})

    def test_selected_models_do_not_require_unselected_compose_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture(root)
            (root / "lab-definitions/packages/new-package/compose.release.yml").unlink()
            result = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + """
$models=@(Get-SetupModels $env:TESTROOT release @('eu-iec104-substation'))
ConvertTo-Json -InputObject @($models | ForEach-Object { $_.Package }) -Compress
""", TESTROOT=tmp)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout), ["", IDS[0]])

    def test_add_uses_installed_mode_version_and_keeps_running_state(self):
        sh_block = SH[SH.index('# shellcheck source=scripts/lib/installed-packages.sh'):SH.index('# Platform and range Compose calls.')]
        ps_block = PS[PS.index('$installed = @()'):PS.index('$info = ')]
        for mode in ("source", "release"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                fixture(root)
                (root / "data/installed-packages").write_text(HEADER + IDS[0] + "\n")
                (root / ".env").write_text("VERSION=v-installed\nRANGERDANGER_PACKAGE=eu-iec104-substation\n")
                (root / "data/install-mode").write_text(mode + "\n")
                before = (root / ".env").read_bytes()
                sh = bash(sh_block + '\nprintf "%s|%s|%s\\n" "$VERSION" "$MODE" "$ACQUIRE_PACKAGES"', root,
                          PACKAGES_DIR=str(root / "lab-definitions/packages"), PACKAGES="", PACKAGES_SET="0",
                          PACKAGE="", ADD_PACKAGE=IDS[2], FROM_SOURCE="0", TARBALL_DIR="", VERSION_SET="0")
                self.assertEqual(sh.returncode, 0, sh.stderr)
                self.assertEqual(sh.stdout.strip(), f"v-installed|{mode}|{IDS[2]}")
                ps = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + """
$RootDir=$env:TESTROOT; $Package=''; $FromSource=$false; $FromTarballs=''; $AddPackage='us-dnp3-substation'
$PSBoundParameters=@{}
function Say($m) {}
""" + ps_block + '\n"$Version|$Mode|$($acquire -join \',\')"', TESTROOT=tmp)
                self.assertEqual(ps.returncode, 0, ps.stderr)
                self.assertEqual(ps.stdout.strip(), sh.stdout.strip())
                self.assertEqual((root / ".env").read_bytes(), before)
        # Acquisition exits before root/kernel probes, teardown, platform up or range POST.
        sh_tail = SH[SH.index("write_installed_packages\n"):SH.index("# No teardown occurs")]
        ps_tail = PS[PS.index("Write-InstalledPackages $RootDir $selected"):PS.index("Invoke-KernelStep\n# The proxy")]
        self.assertLess(sh_tail.index('exit 0'), sh_tail.index('env_write VERSION'))
        self.assertLess(ps_tail.index('exit 0'), ps_tail.index('Write-SetupEnv'))

    def test_failed_acquisition_preserves_receipt_and_add_never_tears_down(self):
        sh_block = SH[SH.index('# ─── image acquisition'):SH.index('# No teardown occurs')]
        ps_block = PS[PS.index('$candidateImage = $env:CONTAIND_IMAGE'):PS.index('Invoke-KernelStep\n# The proxy')]
        for fail in ("", "fetch", "inspect"):
            for language in ("bash", "powershell"):
                with self.subTest(fail=fail, language=language), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    fixture(root)
                    old = (HEADER + IDS[0] + "\n").encode()
                    file = root / "data/installed-packages"
                    file.write_bytes(old)
                    (root / ".env").write_text("VERSION=v-installed\n")
                    if language == "bash":
                        result = bash(r'''
banner() { :; }
ensure_selected_emulation() { printf '%s' "$OPENPLC_SELECTED" > "$ROOT_DIR/emulation-selected"; }
package_ids() { echo us-dnp3-substation; }
with_pull_retry() { "$@"; }
platform_compose() { [ "$1" != config ] || echo platform:1; }
range_compose() {
    [ "$FAIL" != fetch ] || return 1
    [ "$2" != config ] || echo rangerdanger-openplc:1
}
docker_bounded() { [ "$FAIL" != inspect ]; }
MODE=release VERSION=v-installed ADD_PACKAGE=us-dnp3-substation
SELECTED_PACKAGES="eu-iec104-substation
us-dnp3-substation"
''' + sh_block, root, FAIL=fail)
                    else:
                        result = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + """
$RootDir=$env:TESTROOT; $Mode='release'; $Tarball=''; $AddPackage='us-dnp3-substation'
$selected=@('eu-iec104-substation','us-dnp3-substation')
$Models=@(Get-SetupModels $RootDir release @($AddPackage))
function Say($m) {}
function Start-Sleep {}
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    $failed=($env:FAIL -eq 'fetch' -and $DockerArgs -contains 'pull') -or
        ($env:FAIL -eq 'inspect' -and $DockerArgs[0] -eq 'image')
    $text=if ($DockerArgs -contains '--images') { 'selected:1' } else { '' }
    [pscustomobject]@{ExitCode=[int]$failed;TimedOut=$false;StdOut=$text;StdErr='fixture'}
}
""" + ps_block, TESTROOT=tmp, FAIL=fail)
                    self.assertEqual(result.returncode == 0, not fail, result.stdout + result.stderr)
                    expected = old if fail else (HEADER + IDS[0] + "\n" + IDS[2] + "\n").encode()
                    self.assertEqual(file.read_bytes(), expected)
                    self.assertEqual((root / ".env").read_text(), "VERSION=v-installed\n")
                    self.assertFalse((root / "data/install-mode").exists())
                    if language == "bash":
                        self.assertEqual((root / "emulation-selected").exists(), not fail)
                        if not fail:
                            self.assertEqual((root / "emulation-selected").read_text(), "1")

    def test_emulation_only_registers_for_selected_openplc_before_receipt(self):
        function = SH.split("ensure_selected_emulation() {\n", 1)[1].split("\n}\n", 1)[0]
        for selected, present in ((False, False), (True, False), (True, True)):
            with self.subTest(selected=selected, present=present), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                result = bash(r'''
OS=Linux ARCH=arm64
banner() { :; }; warn() { :; }
amd64_emulation_present() { [ "$PRESENT" = 1 ]; }
docker() { printf '%s\n' "$*" > "$ROOT_DIR/calls"; PRESENT=1; }
ensure_selected_emulation() {
''' + function + "\n}\nensure_selected_emulation", root,
                              OPENPLC_SELECTED=str(int(selected)), PRESENT=str(int(present)))
                self.assertEqual(result.returncode, 0, result.stderr)
                calls = root / "calls"
                self.assertEqual(calls.exists(), selected and not present)
                self.assertEqual((root / ".setup-binfmt-amd64").exists(), selected and not present)
                if calls.exists():
                    self.assertEqual(calls.read_text().strip(),
                                     "run --privileged --rm tonistiigi/binfmt:qemu-v10.2.1 --install amd64")
        self.assertLess(SH.index("\nensure_selected_emulation\n"), SH.index("\nwrite_installed_packages\n"))

    def test_install_documentation_pins_the_selection_contract(self):
        for name in ("README.md", "docs/quickstart.md"):
            text = (ROOT / name).read_text()
            for literal in ("--packages", "-Packages", "--add-package", "-AddPackage",
                            "data/installed-packages", "every discovered package"):
                self.assertIn(literal, text, name)

    def test_offline_add_loads_only_added_closure_preserves_mode_env_and_never_restarts(self):
        sh_selection = SH[SH.index('# shellcheck source=scripts/lib/installed-packages.sh'):SH.index('# Platform and range Compose calls.')]
        sh_version = SH[SH.index('# ─── resolve the offline version'):SH.index('# ─── write .env')]
        sh_acquire = SH[SH.index('# ─── image acquisition'):SH.index('# No teardown occurs')]
        ps_selection = PS[PS.index('$installed = @()'):PS.index('$info = ')]
        ps_version = PS[PS.index("$Tarball = ''"):PS.index('# Setup owns these keys;')]
        ps_acquire = PS[PS.index('$candidateImage = $env:CONTAIND_IMAGE'):PS.index('Invoke-KernelStep\n# The proxy')]
        for language in ('bash', 'powershell'):
            for error in ('', 'checksum', 'version', 'source'):
                with self.subTest(language=language, error=error), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    fixture(root)
                    (root / 'scripts/lib/image-index.sh').write_bytes((ROOT / 'scripts/lib/image-index.sh').read_bytes())
                    record = root / 'data/installed-packages'
                    original = (HEADER + IDS[0] + '\n').encode()
                    record.write_bytes(original)
                    (root / '.env').write_text('VERSION=v-installed\nRANGERDANGER_PACKAGE=eu-iec104-substation\n')
                    mode = b'source\n' if error == 'source' else b'release\n'
                    (root / 'data/install-mode').write_bytes(mode)
                    before_env = (root / '.env').read_bytes()
                    bundle = root / 'bundle'
                    bundle.mkdir()
                    version = 'v-installed' + ('-mismatch' if error == 'version' else '')
                    (bundle / '.version').write_text(version + '\n')
                    rows = [f'rangerdanger-image-index-v1\nversion\t{version}\narch\tamd64\n']
                    for owner, image in (('platform', 'platform:v-installed'), (IDS[2], 'us:v-installed')):
                        path = f'{owner}.tar'
                        (bundle / path).write_bytes(image.encode())
                        checksum = hashlib.sha256(image.encode()).hexdigest()
                        if error == 'checksum': checksum = ('0' if checksum[0] != '0' else '1') + checksum[1:]
                        rows.append(f'{owner}\t{image}\t{path}\t{checksum}\n')
                    (bundle / 'image-index-amd64.txt').write_text(''.join(rows))
                    if language == 'bash':
                        result = bash(r'''
banner() { :; }; ensure_selected_emulation() { :; }
platform_compose() { printf 'platform:v-installed\n'; }
range_compose() { [ "$1" = us-dnp3-substation ] || exit 99; printf 'us:v-installed\n'; }
docker_bounded() { printf '%s\n' "$*" >> "$ROOT_DIR/calls"; }
''' + sh_selection + '\n' + sh_version + '\n' + sh_acquire, root,
                                      PACKAGES_DIR=str(root / 'lab-definitions/packages'), PACKAGES='', PACKAGES_SET='0',
                                      PACKAGE='', ADD_PACKAGE=IDS[2], FROM_SOURCE='0', TARBALL_DIR=str(bundle),
                                      VERSION_SET='0', ARCH='amd64')
                    else:
                        result = powershell('$ErrorActionPreference="Stop"\n' + LOAD_PS + r'''
$RootDir=$env:TESTROOT; $Package=''; $FromSource=$false
$FromTarballs=$env:BUNDLE; $AddPackage='us-dnp3-substation'; $arch='amd64'
$PSBoundParameters=@{FromTarballs=$FromTarballs}
function Say($m) {}
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    [IO.File]::AppendAllText((Join-Path $RootDir 'calls'), ($DockerArgs -join ' ') + "`n")
    $text=''
    if ($DockerArgs -contains '--images') {
        $text=if ($DockerArgs -contains (Join-Path $RootDir 'docker-compose.release.yml')) { 'platform:v-installed' } else { 'us:v-installed' }
    }
    [pscustomobject]@{ExitCode=0;TimedOut=$false;StdOut=$text;StdErr=''}
}
''' + ps_selection + '\n' + ps_version + '\n' + ps_acquire, TESTROOT=tmp, BUNDLE=str(bundle))
                    self.assertEqual(result.returncode == 0, not error, result.stdout + result.stderr)
                    expected = original if error else (HEADER + IDS[0] + '\n' + IDS[2] + '\n').encode()
                    self.assertEqual(record.read_bytes(), expected)
                    self.assertEqual((root / '.env').read_bytes(), before_env)
                    self.assertEqual((root / 'data/install-mode').read_bytes(), mode)
                    calls = (root / 'calls').read_text() if (root / 'calls').exists() else ''
                    if error:
                        self.assertNotIn('load -i', calls)
                    else:
                        self.assertEqual(calls.count('load -i'), 2)
                    for forbidden in (' pull', ' build', ' up ', ' down', 'POST', 'eu-iec104-substation.tar'):
                        self.assertNotIn(forbidden, calls)
