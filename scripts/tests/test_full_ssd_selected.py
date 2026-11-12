"""Full-bundle selected membership and per-ref saving, with fixture-only commands."""
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class FullSelectedTests(unittest.TestCase):
    def test_saved_archive_has_exact_ref_and_release_in_both_languages(self):
        reference = 'ghcr.io/tonylturner/rangerdanger-backend:v2'
        for language in ('bash', 'powershell'):
            for tags in ([reference], [], [reference, 'extra:v2'],
                         [reference.replace(':v2', ':v1')]):
                with self.subTest(language=language, tags=tags), tempfile.TemporaryDirectory() as tmp:
                    archive = Path(tmp, 'image.tar')
                    manifest = json.dumps([{'RepoTags': tags}]).encode()
                    with tarfile.open(archive, 'w') as output:
                        member = tarfile.TarInfo('manifest.json')
                        member.size = len(manifest)
                        output.addfile(member, io.BytesIO(manifest))
                    if language == 'bash':
                        source = (ROOT / 'stage-ssd.sh').read_text()
                        function = re.search(r'(?ms)^verify_archive\(\) \{\n.*?^PY\n\}', source).group()
                        result = subprocess.run(
                            ['bash', '-c', 'VERSION=v2\n' + function + '\nverify_archive "$ARCHIVE" "$REF"'],
                            env=dict(os.environ, ARCHIVE=str(archive), REF=reference),
                            capture_output=True, text=True, timeout=15)
                    else:
                        result = powershell(r'''
$ErrorActionPreference='Stop'
$t=$null; $e=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/stage-ssd.ps1", [ref]$t, [ref]$e)
$f=$ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Verify-Archive'}, $true)[0]
Invoke-Expression $f.Extent.Text
function Die($m) { throw $m }
function Invoke-StageNative($Executable, $ArgList, $TimeoutSec) {
    & $Executable @ArgList
    if ($LASTEXITCODE -ne 0) { throw 'tar failed' }
}
$TarExe='tar'; $Version='v2'
Verify-Archive $env:ARCHIVE @($env:REF)
''', ARCHIVE=str(archive), REF=reference)
                    self.assertEqual(result.returncode == 0, tags == [reference],
                                     result.stdout + result.stderr)

    def test_unique_refs_saved_once_and_containd_never_retagged_in_both_languages(self):
        for language in ('bash', 'powershell'):
            with self.subTest(language=language), tempfile.TemporaryDirectory() as tmp:
                if language == 'bash':
                    source = (ROOT / 'stage-ssd.sh').read_text()
                    function = re.search(r'(?ms)^stage_arch\(\) \{\n.*?^\}', source).group()
                    script = r'''
set -euo pipefail
. "$REPO/scripts/lib/image-index.sh"
say() { :; }; banner() { :; }; die() { echo "$*" >&2; exit 1; }
verify_archive() { test -f "$1"; }
stage_pull_image() { :; }
docker() {
    printf '%s\n' "$*" >> "$OUT/calls"
    if [ "$1" = save ]; then
        while [ "$1" != -o ]; do shift; done
        printf 'archive' > "$2"
    fi
}
VERSION=v2
BINFMT_IMAGE='tonistiigi/binfmt:qemu-v10.2.1'
IMAGE_MEMBERSHIP=$(printf 'platform\tshared:v2\nus\tshared:v2\nus\tghcr.io/tonylturner/containd:latest\n')
OPENPLC_OWNERS=''
PREFLIGHT_IMAGES=(shared:v2 ghcr.io/tonylturner/containd:latest)
PREFLIGHT_AMD64_REFS=(shared:v2 containd@sha256:fixture)
PREFLIGHT_ARM64_REFS=(shared:v2 containd@sha256:fixture)
PREFLIGHT_ARM64_CROSS=(no no)
''' + function + '\nstage_arch amd64 "$(printf \'shared:v2\\nghcr.io/tonylturner/containd:latest\\n\')"'
                    result = subprocess.run(['bash', '-c', script], capture_output=True, text=True,
                                            env=dict(os.environ, REPO=str(ROOT), OUT=tmp), timeout=15)
                else:
                    result = powershell(r'''
$ErrorActionPreference='Stop'
$t=$null; $e=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/stage-ssd.ps1", [ref]$t, [ref]$e)
$f=$ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Invoke-StageArch'}, $true)[0]
Invoke-Expression $f.Extent.Text
function Say($m) {}; function Banner($m) {}; function Die($m) { throw $m }
function Verify-Archive($path, $expected) { if (-not (Test-Path $path)) { throw 'missing archive' } }
function Invoke-StagePull { }
function Invoke-StageDocker($DockerArgs, $TimeoutSec, [switch]$StreamOutput) {
    [IO.File]::AppendAllText((Join-Path $OutDir 'calls'), ($DockerArgs -join ' ') + "`n")
    if ($DockerArgs[0] -eq 'save') {
        $path=$DockerArgs[[Array]::IndexOf($DockerArgs, '-o') + 1]
        [IO.File]::WriteAllText($path, 'archive')
    }
}
$OutDir=$env:OUT; $Version='v2'; $openplcOwners=@()
$resolved=@('shared:v2', 'ghcr.io/tonylturner/containd:latest')
$imageMembership=@([pscustomobject]@{Owner='platform'; Image='shared:v2'},
    [pscustomobject]@{Owner='us'; Image='shared:v2'},
    [pscustomobject]@{Owner='us'; Image=$resolved[1]})
$preflightRefs=@{}
$preflightRefs['shared:v2']=@{amd64='shared:v2'; arm64='shared:v2'; arm64Cross=$false}
$preflightRefs[$resolved[1]]=@{amd64='containd@sha256:fixture'; arm64='containd@sha256:fixture'; arm64Cross=$false}
Invoke-StageArch amd64
''', OUT=tmp)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                output = Path(tmp)
                calls = (output / 'calls').read_text().splitlines()
                self.assertEqual(sum(call.startswith('save ') for call in calls), 2)
                self.assertFalse(any(call.startswith('tag ') and 'containd' in call for call in calls))
                rows = [line.split('\t') for line in (output / 'image-index-amd64.txt').read_text().splitlines()[3:]]
                self.assertEqual(len(rows), 3)
                self.assertEqual(rows[0][2:], rows[1][2:])
                self.assertEqual({row[0] for row in rows}, {'platform', 'us'})
                self.assertEqual(len(list((output / 'images/amd64').glob('*.tar'))), 2)

    def test_enumeration_reads_only_platform_and_selected_package_models(self):
        for language in ('bash', 'powershell'):
            with self.subTest(language=language), tempfile.TemporaryDirectory() as tmp:
                if language == 'bash':
                    source = (ROOT / 'stage-ssd.sh').read_text()
                    function = re.search(r'(?ms)^collect_release_images\(\) \{\n.*?^\}', source).group()
                    script = r'''
set -euo pipefail
SELECTED_PACKAGES=us-dnp3-substation
ROOT_DIR="$REPO"; COMPOSE_FILE="$REPO/docker-compose.release.yml"; VERSION=v2
docker() {
    while [ "$1" != -f ]; do shift; done
    printf '%s\n' "$2" >> "$CALLS"
    printf 'shared:v2\n'
}
''' + function + '\ncollect_release_images\n'
                    result = subprocess.run(['bash', '-c', script], capture_output=True, text=True,
                                            env=dict(os.environ, REPO=str(ROOT), CALLS=f'{tmp}/calls'), timeout=15)
                else:
                    result = powershell(r'''
$ErrorActionPreference='Stop'
$source=[IO.File]::ReadAllText("$env:REPO/stage-ssd.ps1")
$start=$source.IndexOf('$releaseFiles =')
$end=$source.IndexOf('# Compose interpolation is')
$RootDir=$env:REPO; $ComposeFile=Join-Path $RootDir 'docker-compose.release.yml'
$selected=@('us-dnp3-substation'); $Version='v2'
function Die($m) { throw $m }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    [IO.File]::AppendAllText($env:CALLS, $DockerArgs[4] + "`n")
    [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut="shared:v2`n"}
}
Invoke-Expression $source.Substring($start, $end-$start)
''', CALLS=f'{tmp}/calls', OS='fixture-posix')
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                calls = Path(tmp, 'calls').read_text().splitlines()
                self.assertEqual(calls, [str(ROOT / 'docker-compose.release.yml'),
                                         str(ROOT / 'lab-definitions/packages/us-dnp3-substation/compose.release.yml')])
