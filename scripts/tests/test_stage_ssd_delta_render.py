from __future__ import annotations

import copy
import json
import os
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
        ;;
    save)
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

    def _run_bash_generator(self) -> str:
        output = self.root / "bash-output"
        result = subprocess.run(
            ["bash", str(ROOT / "stage-ssd-delta.sh"), str(output), "v1", "v2", "--all"],
            cwd=ROOT,
            env=self.env,
            capture_output=True,
            text=True,
            timeout=120,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return (output / "DELTA-README.md").read_text(encoding="utf-8")

    def _run_powershell_generator(self) -> str:
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
    throw "stub: optional kernel asset is not published"
}
& (Join-Path $env:RD_REPO "stage-ssd-delta.ps1") `
    -OutDir $env:RD_OUT -Since v1 -New v2 -All
if ($LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
""".strip(),
            encoding="utf-8",
        )
        env = dict(self.env, RD_REPO=str(ROOT), RD_OUT=str(output))
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
        return (output / "DELTA-README.md").read_text(encoding="utf-8")

    def _assert_literal_delta_recipe(self, readme: str, powershell: bool = False) -> None:
        self.assertEqual(
            readme.count('docker compose -p "$project" down -v --remove-orphans'),
            2,
        )
        self.assertEqual(
            readme.count('docker compose -p "$project" down --remove-orphans'),
            2,
        )
        expected_project = (
            'project="$1"\n    compose_dir=$(mktemp -d)'
            if powershell else
            'local project="$1" compose_dir range_containers volume_names remaining_volumes volume'
        )
        for literal in (
            expected_project,
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
            self._run_powershell_generator(), powershell=True)


if __name__ == "__main__":
    unittest.main()
