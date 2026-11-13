"""Native SSD index readers; Docker calls are fixtures, never a real daemon."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class ImageIndexTests(unittest.TestCase):
    def test_readers_reject_invalid_bundles_before_loading_and_load_only_selected(self):
        for language in ('bash', 'powershell'):
            for error in ('', 'checksum', 'traversal', 'absolute', 'drive', 'version',
                          'missing-package', 'arch', 'release', 'legacy', 'symlink'):
                with self.subTest(language=language, error=error), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    (root / 'images').mkdir()
                    archive = root / 'images/shared.tar'
                    archive.write_bytes(b'fake docker archive')
                    checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
                    path = 'images/shared.tar'
                    if error == 'checksum':
                        checksum = ('1' if checksum[0] != '1' else '2') + checksum[1:]
                    if error == 'traversal': path = '../outside.tar'
                    if error == 'absolute': path = str(archive)
                    if error == 'drive': path = 'C:/outside.tar'
                    if error == 'symlink':
                        (root / 'link').symlink_to(root / 'images', target_is_directory=True)
                        path = 'link/shared.tar'
                    index = ('rangerdanger-image-index-v1' + ('-unknown' if error == 'version' else '') + '\n'
                             'version\t' + ('v2' + '-mismatch' if error == 'release' else 'v2') + '\n'
                             'arch\t' + ('arm64' if error == 'arch' else 'amd64') + '\n'
                             f'platform\tshared:v2\t{path}\t{checksum}\n')
                    if error != 'missing-package':
                        index += f'us\tshared:v2\t{path}\t{checksum}\n'
                    # Unselected archive is intentionally absent: must never be read or loaded.
                    index += f'eu\tunselected:v2\timages/unselected.tar\t{"0" * 64}\n'
                    (root / '.version').write_text('v2\n')
                    if error != 'legacy': (root / 'image-index-amd64.txt').write_text(index)
                    else: (root / 'images-amd64.tar').write_bytes(b'legacy monolith')
                    if language == 'bash':
                        script = '''set -euo pipefail
die() { echo "$*" >&2; exit 1; }
. "$REPO/scripts/lib/image-index.sh"
archives=$(validate_image_index "$BUNDLE" amd64 v2 us shared:v2)
# This fixture represents Docker only after the COMPLETE preflight succeeds.
while IFS= read -r archive; do printf '%s\\n' "$archive" >> "$BUNDLE/loaded"; done <<< "$archives"
'''
                        result = subprocess.run(['bash', '-c', script], capture_output=True, text=True,
                                                env=dict(os.environ, REPO=str(ROOT), BUNDLE=tmp), timeout=15)
                    else:
                        result = powershell('''$ErrorActionPreference='Stop'
. "$env:REPO/scripts/lib/image-index.ps1"
$archives = @(Read-ImageIndex $env:BUNDLE amd64 v2 @('us') @('shared:v2'))
foreach ($archive in $archives) { [IO.File]::AppendAllText((Join-Path $env:BUNDLE 'loaded'), "$archive`n") }
''', BUNDLE=tmp)
                    self.assertEqual(result.returncode == 0, not error, result.stdout + result.stderr)
                    if error in ('legacy', 'version'):
                        self.assertIn('rangerdanger-image-index-v1', result.stdout + result.stderr)
                    if error: self.assertFalse((root / 'loaded').exists())
                    else: self.assertEqual((root / 'loaded').read_text().splitlines(), [str(archive)])

    def test_doc_contract_is_strict_and_global_publication_is_not_filtered(self):
        for name in ('README.md', 'docs/quickstart.md', 'docs/workshop-ssd.md'):
            text = (ROOT / name).read_text()
            for literal in ('rangerdanger-image-index-v1', 'Unselected archives are never loaded',
                            'data/installed-packages', 'data/install-mode', '.env',
                            '--since-packages', '-SincePackages', 'before loading',
                            'safe relative paths', 'checksums', 'every package', '--add-package <id> --from-tarballs <dir>',
                            '-AddPackage <id> -FromTarballs <dir>'):
                self.assertIn(literal, text, (name, literal))
        for name in ('scripts/check_release_inputs.py', '.github/workflows/release.yml'):
            self.assertNotIn('--packages', (ROOT / name).read_text())
