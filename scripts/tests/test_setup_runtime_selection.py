"""Selection preflight only: extracted setup blocks, no real setup or Docker."""
import json
from pathlib import Path
import tempfile
import unittest

from .test_installed_packages import HEADER, IDS, LOAD_PS, PS, SH, bash, fixture
from .test_setup_ps1_root import ROOT, powershell

EU, US = IDS[0], IDS[2]
SH_SELECTION = SH[SH.index("# shellcheck source=scripts/lib/installed-packages.sh"):
                  SH.index("# Platform and range Compose calls.")]
PS_SELECTION = PS[PS.index("$installed = @()"):PS.index("$info = ")]
DOC_RULE = (
    "Setup refuses before any changes if `backend/data/range.json` names a package\n"
    "the new set drops, in any phase; switch to a retained package in the range\n"
    "switcher (or `POST /api/range`) first, then rerun."
)


def record(package=EU, phase="ready"):
    # RecordStore.Save's real field names and MarshalIndent layout, including
    # nested receipt and escaped strings that must not be mistaken for package.
    return (json.dumps({
        "containd": {"applyId": "receipt", "protection": "protecting"},
        "schema": 1, "generation": 2, "phase": phase, "package": package,
        "revision": 1, "mode": "source", "root": "/fixture",
        "version": "fixture", "error": 'line\n"package": "not-the-package"',
        "updated_at": "2026-11-12T12:00:00Z",
    }, indent=2) + "\n").encode()


def snapshot(root):
    return {str(p.relative_to(root)): p.read_bytes()
            for p in root.rglob("*") if p.is_file()}


def selection(language, root, *, mode="release", add="", selected=US):
    if language == "bash":
        return bash(
            SH_SELECTION + r'''
docker() { printf '%s\n' "$*" >> "$ROOT_DIR/calls"; }
docker info
write_installed_packages
printf 'changed\n' > "$ROOT_DIR/.env"
''', root, PACKAGES_DIR=str(root / "lab-definitions/packages"),
            PACKAGES=selected, PACKAGES_SET="0" if add else "1",
            PACKAGE="" if add else US, ADD_PACKAGE=add, VERSION_SET="0",
            FROM_SOURCE=str(int(mode == "source")),
            TARBALL_DIR=str(root / "bundle") if mode == "offline" else "",
        )
    return powershell(
        '$ErrorActionPreference="Stop"\n' + LOAD_PS + r'''
$RootDir=$env:TESTROOT; $Version='latest'; $Package='us-dnp3-substation'
$FromSource=$env:MODE -eq 'source'; $FromTarballs=''
if ($env:MODE -eq 'offline') { $FromTarballs=Join-Path $RootDir 'bundle' }
$AddPackage=$env:ADD; $Packages=@($env:SELECTED); $PSBoundParameters=@{Packages=$Packages}
function Say($m) {}
if ($AddPackage) { $Package=''; $PSBoundParameters=@{} }
''' + PS_SELECTION + r'''
function Invoke-SetupDocker { [IO.File]::WriteAllText((Join-Path $RootDir 'calls'), 'info') }
Invoke-SetupDocker
Write-InstalledPackages $RootDir $selected
[IO.File]::WriteAllText((Join-Path $RootDir '.env'), "changed`n")
''', TESTROOT=str(root), MODE=mode, ADD=add, SELECTED=selected,
    )


class RuntimeSelectionTests(unittest.TestCase):
    def prepare(self, root, runtime):
        fixture(root)
        (root / "backend/data").mkdir(parents=True)
        if runtime is not None:
            (root / "backend/data/range.json").write_bytes(runtime)
        (root / "data/installed-packages").write_text(HEADER + EU + "\n" + US + "\n")
        (root / ".env").write_text("VERSION=v-installed\nRANGERDANGER_PACKAGE=us-dnp3-substation\n")
        (root / "data/install-mode").write_text("release\n")

    def test_dropped_runtime_package_refused_in_all_phases_and_modes_before_changes(self):
        for language in ("bash", "powershell"):
            for mode in ("source", "release", "offline"):
                for phase in ("stopping", "starting", "configuring", "ready", "failed", "stopped"):
                    with self.subTest(language=language, mode=mode, phase=phase), tempfile.TemporaryDirectory() as tmp:
                        root = Path(tmp)
                        self.prepare(root, record(phase=phase))
                        before = snapshot(root)
                        result = selection(language, root, mode=mode)
                        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                        for message in (EU, "Switch the range to a selected package first",
                                        "POST /api/range", "then re-run"):
                            self.assertIn(message, result.stderr)
                        self.assertEqual(snapshot(root), before)
                        self.assertFalse((root / "calls").exists())

    def test_retained_package_or_absent_record_allows_selection(self):
        compact = json.dumps(json.loads(record(US)), separators=(",", ":")).encode()
        unicode_record = json.loads(record(US))
        unicode_record["error"] = "déjà 日本 🔧"
        unicode_record = json.dumps(unicode_record, ensure_ascii=False).encode()
        for language in ("bash", "powershell"):
            for runtime in (None, record(US), record(US, "failed"),
                            b" \r\n" + compact + b" \r\n", unicode_record,
                            compact.replace(b'"package":"us', b'"pack\\u0061ge":"\\u0075s')):
                with self.subTest(language=language, runtime=runtime), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    self.prepare(root, runtime)
                    result = selection(language, root)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertTrue((root / "calls").exists())
                    self.assertEqual((root / "data/installed-packages").read_text(), HEADER + US + "\n")
                    if runtime is not None:
                        self.assertEqual((root / "backend/data/range.json").read_bytes(), runtime)

    def test_malformed_record_fails_closed_without_writes_or_docker(self):
        invalid = (
            b"", b"{", record(US)[:-2], record(US) + b"garbage",
            record(US) + b"{}", b"null", b"[]", b"{}", b'{"Package":"us-dnp3-substation"}',
            b'{"nested":{"package":"us-dnp3-substation"}}',
            b'{"package":null}', b'{"package":42}', b'{"package":[]}',
            b'{"package":""}', b'{"package":"../x"}', b'{"package":"US-dnp3-substation"}',
            b'{"package":"us-dnp3-substation","package":"eu-iec104-substation"}',
            b'{"package":"eu-iec104-substation","pack\\u0061ge":"us-dnp3-substation"}',
            b'{"package":"us-dnp3-substation",}',
            b'{"package":"us-dnp3-substation" /*comment*/}',
            b'{"package":"us-dnp3-substation","x":"\\q"}',
            b'{"package":"us-dnp3-substation","x":01}',
            b'{"package":"us-dnp3-substation","x":NaN}',
            b'{"package":"us-dnp3-substation","x":"raw\nnewline"}',
            b'{"package":"us-dnp3-substation"}\0',
            b'{"package":"us-dnp3-substation","x":"\xff"}',
            b'{"package":"us-dnp3-substation","x":"\xc0\xaf"}',
            b'{"package":"us-dnp3-substation","x":"\xed\xa0\x80"}',
        )
        for language in ("bash", "powershell"):
            for runtime in invalid:
                with self.subTest(language=language, runtime=runtime), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    self.prepare(root, runtime)
                    before = snapshot(root)
                    result = selection(language, root)
                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                    self.assertIn(str(root / "backend/data/range.json"), result.stderr)
                    self.assertEqual(snapshot(root), before)
                    self.assertFalse((root / "calls").exists())

    def test_non_file_and_dangling_runtime_records_are_not_absent(self):
        for language in ("bash", "powershell"):
            for kind in ("directory", "dangling-link"):
                with self.subTest(language=language, kind=kind), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    self.prepare(root, None)
                    path = root / "backend/data/range.json"
                    if kind == "directory":
                        path.mkdir()
                    else:
                        path.symlink_to(root / "absent")
                    result = selection(language, root)
                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                    self.assertIn(str(path), result.stderr)
                    self.assertFalse((root / "calls").exists())

    def test_add_unions_without_rejecting_retained_runtime_and_validates_idempotent_add(self):
        for language in ("bash", "powershell"):
            for idempotent in (False, True):
                with self.subTest(language=language, idempotent=idempotent), tempfile.TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    self.prepare(root, record(EU))
                    receipt = root / "data/installed-packages"
                    if not idempotent:
                        receipt.write_text(HEADER + EU + "\n")
                    result = selection(language, root, add=US)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual(receipt.read_text(), HEADER + EU + "\n" + US + "\n")
                    self.assertEqual((root / "backend/data/range.json").read_bytes(), record(EU))
                    # Even an already-acquired package must not hide corruption.
                    (root / "backend/data/range.json").write_bytes(b"{")
                    before = snapshot(root)
                    result = selection(language, root, add=US)
                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                    self.assertEqual(snapshot(root), before)

    def test_runtime_path_matches_backend_and_both_platform_mounts(self):
        server = (ROOT / "backend/cmd/server/main.go").read_text()
        self.assertIn("filepath.Join(filepath.Dir(cfg.DBPath), lifecycle.RecordFile)", server)
        self.assertIn('const RecordFile = "range.json"',
                      (ROOT / "backend/internal/lifecycle/record.go").read_text())
        for name in ("docker-compose.yml", "docker-compose.release.yml"):
            source = (ROOT / name).read_text()
            self.assertIn("RANGERDANGER_DB_PATH: /data/rangerdanger.db", source)
            self.assertIn("- ./backend/data:/data", source)

    def test_docs_and_entrypoints_keep_the_pre_side_effect_guard(self):
        for name in ("README.md", "docs/quickstart.md"):
            self.assertIn(DOC_RULE, (ROOT / name).read_text())
        self.assertLess(SH.index("\nassert_runtime_package_selected\n"),
                        SH.index("if ! docker_bounded 30 info"))
        self.assertLess(PS.index("\nAssert-RuntimePackageSelected $RootDir $selected\n"),
                        PS.index("$info = (Invoke-SetupDocker"))
        self.assertIn('exec "$ROOT_DIR/setup.sh" --from-source --skip-firewall-gate "$@"',
                      (ROOT / "scripts/dev-up.sh").read_text())
        self.assertIn("'setup.ps1') -FromSource -SkipFirewallGate @PSBoundParameters",
                      (ROOT / "scripts/dev-up.ps1").read_text())
