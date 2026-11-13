"""Selected SSD delta generators and rollback, with real files and fake Docker."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

from . import test_stage_ssd_delta_render as render
from .test_setup_ps1_root import ROOT


class SelectedDeltaTests(unittest.TestCase):
    def setUp(self):
        self.fixture = render.DeltaReadmeRenderTests()
        self.fixture.setUp()
        self.addCleanup(self.fixture.tearDown)
        fixture = self.fixture
        record = json.loads((fixture.record_dir / 'v2.json').read_text())
        models = record['compose_files']
        for owner, model in [('platform', models['platform'])] + list(models['packages'].items()):
            (fixture.root / f'{owner}.txt').write_text('\n'.join(model['services'].values()) + '\n')
        docker = fixture.bin_dir / 'docker'
        source = docker.read_text().replace('cat "$RD_COMPOSE_IMAGES"', '''file=''
        while [ "$#" -gt 0 ]; do
            if [ "$1" = '-f' ]; then shift; file="$1"; fi
            shift
        done
        owner=$(basename "$(dirname "$file")")
        [ "$(basename "$file")" != docker-compose.release.yml ] || owner=platform
        cat "$RD_MODELS/$owner.txt"''')
        docker.write_text(source)
        fixture.env['RD_MODELS'] = str(fixture.root)

    def render(self, language, source='us-dnp3-substation', target='us-dnp3-substation'):
        fixture = self.fixture
        if language == 'bash':
            output = fixture.root / 'bash-output'
            result = subprocess.run(['/bin/bash', str(ROOT / 'stage-ssd-delta.sh'), str(output), 'v1', 'v2',
                                     '--since-packages', source, '--packages', target],
                                    cwd=ROOT, env=fixture.env, capture_output=True, text=True, timeout=120)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            text = (output / 'DELTA-README.md').read_text()
        else:
            # Reuse the native HTTP/download fixtures, adding actual selection arguments.
            driver = fixture.root / 'render.ps1'
            fixture._run_powershell_generator(all_images=False)
            driver.write_text(driver.read_text().replace('-New v2 @options',
                              '-New v2 -SincePackages $env:SOURCE_SET -Packages $env:TARGET_SET @options'))
            output = fixture.root / 'selected-powershell-output'
            result = subprocess.run(['pwsh', '-NoLogo', '-NoProfile', '-File', str(driver)],
                                    cwd=ROOT, env=dict(fixture.env, RD_REPO=str(ROOT), RD_OUT=str(output),
                                                      RD_SINCE='v1', RD_ALL='0', RD_INCLUDE='',
                                                      SOURCE_SET=source, TARGET_SET=target),
                                    capture_output=True, text=True, timeout=120)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            text = (output / 'DELTA-README.md').read_text()
        return text, output

    def test_subset_and_expansion_compare_only_installed_source_closure(self):
        for language in ('bash', 'powershell'):
            with self.subTest(language=language):
                text, _ = self.render(language)
                self.assertNotIn('rangerdanger-iec104', text)
                self.assertNotIn('eu-iec104-substation/', text)
                # EU refs existed in the old GLOBAL record, not in this source install.
                text, output = self.render(language, target='eu-iec104-substation,us-dnp3-substation')
                indexes = list(output.glob('delta-image-index-*.txt'))
                self.assertEqual(len(indexes), 2)
                for index in indexes:
                    rows = [line.split('\t') for line in index.read_text().splitlines()[3:]]
                    self.assertTrue(any('rangerdanger-iec104' in row[1] for row in rows))
                    mapping = {}
                    for owner, image, path, checksum in rows:
                        self.assertEqual(mapping.setdefault(image, (path, checksum)), (path, checksum))
                        self.assertTrue((output / path).is_file())
                self.assertNotRegex(text, r'docker image tag [^\n]*containd')

    def test_apply_refuses_mismatched_set_and_rollback_restores_all_three_files(self):
        for language in ('bash', 'powershell'):
            with self.subTest(language=language):
                text, output = self.render(language)
                blocks = re.findall(r'```sh\n(.*?)\n```', text, flags=re.S)
                self.assertEqual(len(blocks), 2)
                with tempfile.TemporaryDirectory() as tmp:
                    home = Path(tmp)
                    install = home / 'rangerdanger'
                    (install / 'data').mkdir(parents=True)
                    original = {'.env': b'VERSION=v1\nRANGERDANGER_ROOT=preserved\n',
                                'data/installed-packages': b'rangerdanger-installed-packages-v1\nus-dnp3-substation\n',
                                'data/install-mode': b'release\n'}
                    for name, content in original.items(): (install / name).write_bytes(content)
                    (install / 'data/installed-packages').write_text('rangerdanger-installed-packages-v1\neu-iec104-substation\n')
                    prefix = blocks[0].split('# Only apply a delta')[0]
                    script = prefix.replace('DELTA_DIR="/path/to/delta-v2"', f'DELTA_DIR="{output}"')
                    script += '\ndocker() { echo unexpected-docker >&2; return 99; }\ndelta_preflight\n'
                    result = subprocess.run(['bash', '-c', script], cwd=install,
                                            env=dict(os.environ, HOME=tmp), capture_output=True, text=True, timeout=15)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('installed set mismatch', result.stderr)
                    self.assertNotIn('unexpected-docker', result.stderr)
                    for name, content in original.items(): (install / name).write_bytes(content)
                    snapshot = home / 'rangerdanger.before-v2.tar.gz'
                    import tarfile
                    with tarfile.open(snapshot, 'w:gz') as archive: archive.add(install, arcname='rangerdanger')
                    for name in original: (install / name).write_bytes(b'changed\n')
                    (install / 'new-file').write_text('not in snapshot')
                    rollback = blocks[1].replace('test -f "../rangerdanger.before-', '''docker() { return 0; }
compose_down_project() { return 0; }
start_platform_and_selected_range() { return 0; }
test -f "../rangerdanger.before-''', 1)
                    result = subprocess.run(['bash', '-c', rollback], cwd=install,
                                            env=dict(os.environ, HOME=tmp), capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    for name, content in original.items(): self.assertEqual((install / name).read_bytes(), content)
                    self.assertFalse((install / 'new-file').exists())

    def test_containd_backup_is_atomic_optional_when_absent_and_never_retagged(self):
        for language in ('bash', 'powershell'):
            text = (self.fixture._run_bash_generator() if language == 'bash'
                    else self.fixture._run_powershell_generator())
            backup = re.search(r'(?ms)^if \[ ! -f "../containd.before-v2.tar".*?^fi', text).group()
            restore = re.search(r'(?ms)^if \[ -f "../containd.before-v2.tar".*?^fi', text).group()
            for present, fail in ((False, False), (True, False), (True, True)):
                with self.subTest(language=language, present=present, fail=fail), tempfile.TemporaryDirectory() as tmp:
                    install = Path(tmp, 'rangerdanger')
                    install.mkdir()
                    script = r'''
set -e
ARCH=amd64
docker() {
    case "$1 $2" in
        'image inspect') [ "$PRESENT" = 1 ] ;;
        'save --platform') printf 'old latest' > "$5"; [ "$FAIL" = 0 ] ;;
        'load -i') [ "$(cat "$3")" = 'old latest' ] ;;
        *) echo 'unexpected Docker call' >&2; return 99 ;;
    esac
}
'''
                    env = dict(os.environ, PRESENT=str(int(present)), FAIL=str(int(fail)))
                    result = subprocess.run(['bash', '-c', script + backup], cwd=install, env=env,
                                            capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode == 0, not fail, result.stdout + result.stderr)
                    saved = Path(tmp, 'containd.before-v2.tar')
                    self.assertEqual(saved.exists(), present and not fail)
                    result = subprocess.run(['bash', '-c', script + restore], cwd=install, env=env,
                                            capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
