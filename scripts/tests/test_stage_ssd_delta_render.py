from __future__ import annotations

import copy
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))

import check_release_inputs


class DeltaReadmeRenderTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.bin_dir = self.root / "bin"
        self.bin_dir.mkdir()
        self.record_dir = self.root / "records"
        self.record_dir.mkdir()
        self._write_release_records()
        self._write_stub_tools()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _write_release_records(self) -> None:
        new_models = check_release_inputs.release_compose_metadata(ROOT, "v2")
        old_models = copy.deepcopy(new_models)
        for model in [old_models["platform"], *old_models["packages"].values()]:
            model["services"] = {
                service: (
                    image.rsplit(":", 1)[0] + ":v1"
                    if image.startswith("ghcr.io/tonylturner/rangerdanger-")
                    else image
                )
                for service, image in model["services"].items()
            }
        inventory = json.loads(
            (ROOT / ".github/release-images.json").read_text(encoding="utf-8"))
        for release, models in (("v1", old_models), ("v2", new_models)):
            record = {
                "schema": 1,
                "release": release,
                "images": [
                    {"image": image["image"]} for image in inventory["images"]
                ],
                "compose_files": models,
            }
            (self.record_dir / f"{release}.json").write_text(
                json.dumps(record), encoding="utf-8")
        references = {
            reference
            for model in [new_models["platform"], *new_models["packages"].values()]
            for reference in model["services"].values()
        }
        (self.root / "compose-images.txt").write_text(
            "".join(f"{image}\n" for image in sorted(references)),
            encoding="utf-8",
        )

    def _write_stub_tools(self) -> None:
        docker = r"""#!/bin/bash
set -euo pipefail
case "${1:-}" in
    compose)
        cat "$RD_COMPOSE_IMAGES"
        ;;
    buildx)
        printf '{"manifest":{"digest":"sha256:%064d","manifests":[{"digest":"sha256:%064d","platform":{"os":"linux","architecture":"amd64"}},{"digest":"sha256:%064d","platform":{"os":"linux","architecture":"arm64"}}]}}' 0 1 2
        ;;
    pull|tag)
        [ "${RD_NO_IMAGE_STAGING:-0}" = 0 ]
        ;;
    save)
        [ "${RD_NO_IMAGE_STAGING:-0}" = 0 ]
        output=""
        while [ "$#" -gt 0 ]; do
            if [ "$1" = "-o" ]; then
                shift
                output="$1"
                break
            fi
            shift
        done
        [ -n "$output" ]
        printf 'stub archive\n' > "$output"
        ;;
    *)
        echo "unexpected docker invocation: $*" >&2
        exit 2
        ;;
esac
"""
        self._write_executable("docker", docker)
        self._write_executable("curl", "#!/bin/sh\nexit 22\n")
        self._write_executable("dot_clean", "#!/bin/sh\nexit 0\n")

        sitecustomize = r"""
import os
from pathlib import Path
import urllib.request

class Response:
    def __init__(self, body):
        self.body = body
    def __enter__(self):
        return self
    def __exit__(self, *args):
        return None
    def read(self):
        return self.body

def urlopen(request, *args, **kwargs):
    url = request.full_url if hasattr(request, "full_url") else str(request)
    match = __import__("re").search(r"/releases/download/([^/]+)/release-images\.json$", url)
    if match:
        return Response((Path(os.environ["RD_RECORD_DIR"]) / f"{match.group(1)}.json").read_bytes())
    raise RuntimeError(f"unexpected network request in staging test: {url}")

urllib.request.urlopen = urlopen
"""
        shim_dir = self.root / "python"
        shim_dir.mkdir()
        (shim_dir / "sitecustomize.py").write_text(sitecustomize, encoding="utf-8")
        python_path = os.pathsep.join((str(shim_dir), str(ROOT / "scripts")))
        self.env = dict(
            os.environ,
            PATH=f"{self.bin_dir}{os.pathsep}{os.environ['PATH']}",
            PYTHONPATH=python_path,
            RD_RECORD_DIR=str(self.record_dir),
            RD_COMPOSE_IMAGES=str(self.root / "compose-images.txt"),
            GH_OWNER_REPO="fixture/rangerdanger",
        )

    def _write_executable(self, name: str, content: str) -> None:
        path = self.bin_dir / name
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def _run_bash_generator(
        self, since: str = "v1", *, all_images: bool = True, include: str = "",
    ) -> str:
        output = self.root / "bash-output"
        result = subprocess.run(
            ["/bin/bash", str(ROOT / "stage-ssd-delta.sh"), str(output), since, "v2"]
            + (["--all"] if all_images else [])
            + (["--include", include] if include else []),
            cwd=ROOT,
            env=self.env,
            capture_output=True,
            text=True,
            timeout=120,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return (output / "DELTA-README.md").read_bytes().decode("utf-8")

    def _run_powershell_generator(
        self, since: str = "v1", *, all_images: bool = True, include: str = "",
    ) -> str:
        pwsh = shutil.which("pwsh")
        if not pwsh:
            self.skipTest("pwsh is unavailable; PowerShell render cannot run")
        output = self.root / "powershell-output"
        driver = self.root / "render.ps1"
        driver.write_text(
            r"""
function Invoke-WebRequest {
    [CmdletBinding()]
    param(
        [string]$Uri,
        [string]$OutFile,
        [string]$Method,
        [switch]$UseBasicParsing,
        [int]$TimeoutSec
    )
    if ($Uri -match '/releases/download/([^/]+)/release-images\.json$') {
        Copy-Item (Join-Path $env:RD_RECORD_DIR "$($Matches[1]).json") $OutFile
        return
    }
    if ($env:RD_KERNEL) {
        if ($Method -eq 'Head') { return @{ StatusCode = 200 } }
        $source = if ($Uri.EndsWith('.sha256')) { $env:RD_KERNEL_SHA } else { $env:RD_KERNEL }
        Copy-Item $source $OutFile
        return
    }
    throw "stub: optional kernel asset is not published"
}
$options = @{}
if ($env:RD_ALL -eq '1') { $options.All = $true }
if ($env:RD_INCLUDE) { $options.Include = $env:RD_INCLUDE }
& (Join-Path $env:RD_REPO "stage-ssd-delta.ps1") `
    -OutDir $env:RD_OUT -Since $env:RD_SINCE -New v2 @options
if ($LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
""".strip(),
            encoding="utf-8",
        )
        env = dict(
            self.env, RD_REPO=str(ROOT), RD_OUT=str(output), RD_SINCE=since,
            RD_ALL="1" if all_images else "0", RD_INCLUDE=include,
        )
        result = subprocess.run(
            [pwsh, "-NoLogo", "-NoProfile", "-File", str(driver)],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            timeout=120,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return (output / "DELTA-README.md").read_bytes().decode("utf-8")

    def _assert_literal_delta_recipe(self, readme: str) -> None:
        self.assertEqual(
            readme.count('docker compose -p "$project" down -v --remove-orphans'),
            2,
        )
        self.assertEqual(
            readme.count('docker compose -p "$project" down --remove-orphans'),
            2,
        )
        for literal in (
            'local project="$1" compose_dir range_containers volume_names remaining_volumes volume',
            'local ready attempt range_status phase',
            'compose_dir=$(mktemp -d) || return 1',
            'range_containers=$(docker ps -aq --filter "label=com.docker.compose.project=$project")',
            'range_status=$(curl -fsS --max-time 5 http://127.0.0.1:8088/api/range)',
            '[ "$phase" = "ready" ] && return 0',
            'remaining_volumes=$(docker volume ls -q) || return 1',
            "grep -Fxq \"$volume\"",
        ):
            self.assertIn(literal, readme)
        self.assertNotIn('docker compose -p rangerdanger-platform down -v', readme)
        self.assertIn('docker compose -p rangerdanger-platform', readme)
        self.assertRegex(
            readme,
            r'if \[ "\$project" = "rangerdanger" \]; then(?s:.*?)'
            r'docker compose -p "\$project" down -v --remove-orphans(?s:.*?)'
            r'elif ! \(cd "\$compose_dir" && docker compose -p "\$project" '
            r'down --remove-orphans\); then',
        )
        self.assertIn('compose_down_project rangerdanger\n', readme)
        self.assertIn('compose_down_project rangerdanger-platform\n', readme)
        self.assertNotRegex(readme, r'local project="(?:STAGE_ARG1|/[^"]+)"')

    def test_both_generators_render_literal_recipe_and_scoped_volume_teardown(self) -> None:
        self._assert_literal_delta_recipe(self._run_bash_generator())
        self._assert_literal_delta_recipe(
            self._run_powershell_generator())

    def test_readme_byte_parity_and_shell_syntax(self) -> None:
        # Cover empty/nonempty tables, unchanged tags, mutable-tag backups,
        # and the optional kernel row without contacting Docker or a registry.
        cases = (
            ("all", "v1", True, ""),
            ("repo-only", "v1", False, ""),
            ("same-release", "v2", False, ""),
            ("mixed-with-kernel", "v1", False, "backend"),
        )
        for name, since, all_images, include in cases:
            with self.subTest(case=name):
                if name == "mixed-with-kernel":
                    self._enable_kernel_asset()
                bash = self._run_bash_generator(since, all_images=all_images, include=include)
                powershell = self._run_powershell_generator(
                    since, all_images=all_images, include=include)
                if name == "all":
                    self.assertIn(":before-v2", bash)
                elif name == "mixed-with-kernel":
                    self.assertIn("`rangerdanger-wsl2-kernel` + `.sha256`", bash)
                    self.assertIn("docker image tag ghcr.io/", bash)
                else:
                    self.assertIn("# No image archive was created;", bash)
                normalized = []
                for readme in (bash, powershell):
                    value, count = re.subn(
                        rb"(?m)^Staged \S+ (?=for upgrade from )",
                        b"Staged <timestamp> ", readme.encode("utf-8"),
                    )
                    self.assertEqual(count, 1)
                    normalized.append(value)
                self.assertEqual(*normalized)
                blocks = re.findall(r"```sh\n(.*?)\n```", bash, flags=re.DOTALL)
                self.assertEqual(len(blocks), 2)
                source = (ROOT / "stage-ssd-delta.sh").read_text(encoding="utf-8")
                for function in ("compose_down_project", "start_platform_and_selected_range"):
                    literal = re.search(
                        rf"(?m)^{function}\(\) \{{\n.*?^\}}", source, flags=re.DOTALL,
                    )
                    self.assertIsNotNone(literal)
                    for block in blocks:
                        self.assertIn(literal.group(), block)
                for shell in ("/bin/bash", shutil.which("zsh")):
                    if not shell:
                        continue  # zsh is not installed on every Linux CI host.
                    for block in blocks:
                        result = subprocess.run(
                            [shell, "-n"], input=block, text=True,
                            capture_output=True, timeout=10, check=False,
                        )
                        self.assertEqual(result.returncode, 0, result.stderr)

    def _enable_kernel_asset(self) -> bytes:
        kernel = b"fixture WSL kernel\n"
        kernel_path = self.root / "kernel"
        kernel_path.write_bytes(kernel)
        checksum = self.root / "kernel.sha256"
        checksum.write_text(
            hashlib.sha256(kernel).hexdigest() + "  rangerdanger-wsl2-kernel\n",
            encoding="ascii",
        )
        self.env.update(
            RD_KERNEL=str(kernel_path),
            RD_KERNEL_SHA=str(checksum),
        )
        self._write_executable("curl", r"""#!/bin/bash
set -euo pipefail
for arg in "$@"; do
    [ "$arg" != "--head" ] || exit 0
done
source="$RD_KERNEL"
output=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        *.sha256) source="$RD_KERNEL_SHA" ;;
        -o) shift; output="$1" ;;
    esac
    shift
done
[ -n "$output" ]
cp "$source" "$output"
""")
        return kernel

    def test_repo_only_delta_under_system_bash(self) -> None:
        kernel = self._enable_kernel_asset()
        self.env["RD_NO_IMAGE_STAGING"] = "1"
        # Both a same-release repo refresh and an unchanged-image upgrade.
        for since in ("v2", "v1"):
            with self.subTest(since=since):
                readme = self._run_bash_generator(since, all_images=False)
                self._assert_literal_delta_recipe(readme)
                self.assertIn(
                    "# No image archive was created; no image load is needed.", readme)
                self.assertNotIn('docker load -i', readme)
                self.assertIn("# No mutable image tags need to be parked.", readme)
                self.assertIn("# No mutable image tags need to be restored.", readme)
                self.assertIn(
                    f"docker image tag ghcr.io/tonylturner/rangerdanger-backend:{since} "
                    "ghcr.io/tonylturner/rangerdanger-backend:v2", readme)
                self.assertIn("## Unchanged (kept from prior install)", readme)
                self.assertIn("`rangerdanger-wsl2-kernel` + `.sha256`", readme)
                output = self.root / "bash-output"
                self.assertEqual(
                    {p.name for p in output.iterdir()},
                    {"rangerdanger.tgz", "rangerdanger-wsl2-kernel",
                     "rangerdanger-wsl2-kernel.sha256", "DELTA-README.md"},
                )
                self.assertEqual((output / "rangerdanger-wsl2-kernel").read_bytes(), kernel)
                self.assertGreater((output / "rangerdanger.tgz").stat().st_size, 0)


if __name__ == "__main__":
    unittest.main()
