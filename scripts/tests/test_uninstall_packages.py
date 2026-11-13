"""Selected image inventories from the actual uninstall blocks; no teardown."""
import json
from pathlib import Path
import tempfile
import unittest

from .test_installed_packages import HEADER, ROOT, bash, fixture
from .test_setup_ps1_root import powershell

PRESENT = "\n".join([
    "ghcr.io/tonylturner/rangerdanger-backend:v-installed",
    "ghcr.io/tonylturner/rangerdanger-eu:v-installed",
    "ghcr.io/tonylturner/rangerdanger-eu:old",
    "ghcr.io/tonylturner/rangerdanger-us:v-installed",
    "ghcr.io/tonylturner/containd:latest",
    "rangerdanger-platform-backend:latest",
    "rangerdanger-eu:latest", "rangerdanger-us:latest", "alpine:3.21", "nginx:1.27-alpine",
])


class UninstallPackagesTests(unittest.TestCase):
    def test_bash_inventory_uses_exact_selected_references(self):
        source = (ROOT / "scripts/uninstall-rangerdanger.sh").read_text()
        block = source[source.index("# Only exact references"):source.index('say "Release images')]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture(root)
            (root / "data/installed-packages").write_text(HEADER + "eu-iec104-substation\n")
            (root / ".env").write_text("VERSION=v-installed\n")
            result = bash(r'''
warn() { echo "$*" >&2; }
REMOVE_IMAGES=1 REMOVE_DEV_IMAGES=1 REMOVE_BASE_IMAGES=1
docker() {
    [ "$VERSION" = v-installed ] || return 98
    echo "$*" >> "$ROOT_DIR/calls"
    case "$*" in
        *docker-compose.release.yml*) printf 'ghcr.io/tonylturner/rangerdanger-backend:v-installed\nnginx:1.27-alpine\n' ;;
        *eu-iec104-substation/compose.release.yml*) printf 'ghcr.io/tonylturner/rangerdanger-eu:v-installed\nghcr.io/tonylturner/containd:latest\n' ;;
        *docker-compose.yml*) printf 'rangerdanger-platform-backend\nnginx:1.27-alpine\n' ;;
        *eu-iec104-substation/compose.source.yml*) printf 'rangerdanger-eu\nalpine:3.21\n' ;;
        *) return 99 ;;
    esac
}
''' + block + '\nprintf "RELEASE\\n%s\\nDEV\\n%s\\nBASE\\n%s\\n" "$ALL_IMAGES" "$DEV_IMAGES" "$BASE_IMAGES"',
                          root, PRESENT_IMAGES=PRESENT, VERSION="v-foreign")
            self.assertEqual(result.returncode, 0, result.stderr)
            lines = result.stdout.splitlines()
            self.assertNotIn("ghcr.io/tonylturner/rangerdanger-us:v-installed", lines)
            self.assertNotIn("ghcr.io/tonylturner/rangerdanger-eu:old", lines)
            self.assertNotIn("rangerdanger-us:latest", lines)
            for ref in PRESENT.splitlines():
                if "-us" not in ref and ":old" not in ref:
                    self.assertIn(ref, lines)
            self.assertNotIn("us-dnp3-substation", (root / "calls").read_text())
            self.assertTrue((root / "data/installed-packages").exists())

    def test_powershell_inventory_uses_exact_selected_references(self):
        source = (ROOT / "scripts/uninstall-rangerdanger.ps1").read_text()
        block = source[source.index('. "$RootDir/scripts/lib/installed-packages.ps1"'):source.index('Say "Release images')]
        block = block.replace('. "$RootDir/scripts/lib/', '. "$env:REPO/scripts/lib/')
        split = block.index('$rdImages = @();')
        mock = r'''
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($env:VERSION -ne 'v-installed') { throw 'inherited version leaked into uninstall' }
    $script:calls += ($DockerArgs -join ' ')
    $text = switch -Wildcard ($DockerArgs -join ' ') {
        '*docker-compose.release.yml*' { "ghcr.io/tonylturner/rangerdanger-backend:v-installed`nnginx:1.27-alpine" }
        '*eu-iec104-substation/compose.release.yml*' { "ghcr.io/tonylturner/rangerdanger-eu:v-installed`nghcr.io/tonylturner/containd:latest" }
        '*docker-compose.yml*' { "rangerdanger-platform-backend`nnginx:1.27-alpine" }
        '*eu-iec104-substation/compose.source.yml*' { "rangerdanger-eu`nalpine:3.21" }
        default { throw 'unselected model' }
    }
    [pscustomobject]@{ExitCode=0;TimedOut=$false;StdOut=$text;StdErr=''}
}
'''
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture(root)
            (root / "data/installed-packages").write_text(HEADER + "eu-iec104-substation\n")
            (root / ".env").write_text("VERSION=v-installed\n")
            result = powershell(r'''
$ErrorActionPreference='Stop'
$RootDir=$env:TESTROOT; $DaemonRoot=$RootDir; $RemoveImages=$true; $RemoveDevImages=$true; $RemoveBaseImages=$true
$presentImages=@($env:PRESENT -split "`n"); $script:calls=@()
function Warn($m) { throw $m }
''' + block[:split] + mock + block[split:] + """
if ($env:VERSION -ne 'v-foreign') { throw 'inherited version not restored' }
ConvertTo-Json -InputObject @{release=@($rdImages);dev=@($rdDevImages);base=@($rdBaseImages);calls=@($script:calls)} -Compress
""", TESTROOT=tmp, PRESENT=PRESENT, VERSION="v-foreign")
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            data = json.loads(result.stdout)
            self.assertEqual(sorted(data["release"]), sorted([
                "ghcr.io/tonylturner/rangerdanger-backend:v-installed",
                "ghcr.io/tonylturner/rangerdanger-eu:v-installed", "ghcr.io/tonylturner/containd:latest",
            ]))
            self.assertEqual(sorted(data["dev"]), ["rangerdanger-eu:latest", "rangerdanger-platform-backend:latest"])
            self.assertEqual(sorted(data["base"]), ["alpine:3.21", "nginx:1.27-alpine"])
            self.assertNotIn("us-dnp3-substation", " ".join(data["calls"]))
            self.assertTrue((root / "data/installed-packages").exists())
