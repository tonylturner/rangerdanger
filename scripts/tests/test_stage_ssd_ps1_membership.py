"""Differential fixtures against the unchanged Python delta-candidates authority."""
import copy
import json
import unittest

from scripts import release_image_plan
from .test_setup_ps1_root import ROOT, powershell


PREFIX = "ghcr.io/tonylturner/rangerdanger-"


def record(tag, platform=None, packages=None):
    def model(services):
        return dict(file="compose.release.yml", sha256="a" * 64, services=services)

    models = dict(platform=model(platform or {}), packages={
        owner: model(services) for owner, services in (packages or {}).items()
    })
    repositories = {
        release_image_plan._image_repository(ref)
        for item in [models["platform"], *models["packages"].values()]
        for ref in item["services"].values()
    }
    return dict(schema=1, release=tag, compose_files=models, images=[
        dict(image=repo.rsplit("/", 1)[-1]) for repo in sorted(repositories) if repo.startswith(PREFIX)
    ])


def fixture():
    old = record("v1", {"backend": PREFIX + "backend:v1", "proxy": "nginx:1"}, {
        "a": {"relay": PREFIX + "relay:v1", "tool": "registry:5000/tool:z"},
        "b": {"tool": "registry:5000/tool:a"},
    })
    new = record("v2", {"backend": PREFIX + "backend:v2", "proxy": "nginx:1"}, {
        "a": {"relay": PREFIX + "relay:v2", "tool": "registry:5000/tool:new"},
        "c": {"vendor": PREFIX + "vendor:v2", "base": "alpine@sha256:abc"},
        "d": {"tool": "registry:5000/tool:a"},
    })
    images = [ref for model in [new["compose_files"]["platform"], *new["compose_files"]["packages"].values()]
              for ref in model["services"].values()]
    return dict(old=old, new=new, images=images, include=False)


RUN = r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/release-membership.ps1"
$answers = @()
foreach ($case in ($env:CASES | ConvertFrom-Json)) {
    try {
        $rows = @(Get-DeltaImageCandidates -PreviousJson $case.old -NewJson $case.new `
            -ComposeImages $case.images -SinceTag 'v1' -NewTag 'v2' -IncludeUpstream:($case.include))
        $answers += @{ok=$true; rows=$rows}
    } catch { $answers += @{ok=$false; error="$_"} }
}
ConvertTo-Json -Depth 30 -Compress -InputObject $answers
"""


class MembershipTests(unittest.TestCase):
    def compare(self, cases):
        wire = [dict(case, old=json.dumps(case["old"]), new=json.dumps(case["new"])) for case in cases]
        result = powershell(RUN, CASES=json.dumps(wire))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        answers = json.loads(result.stdout)
        self.assertEqual(len(answers), len(cases))
        for case, answer in zip(cases, answers):
            with self.subTest(case=case):
                try:
                    expected = release_image_plan.delta_image_candidates(
                        previous_record=case["old"], new_record=case["new"], compose_images=case["images"],
                        since_tag="v1", new_tag="v2", include_upstream=case["include"],
                    )
                except release_image_plan.PlanError:
                    self.assertFalse(answer["ok"], answer)
                else:
                    self.assertTrue(answer["ok"], answer)
                    self.assertEqual(answer["rows"], expected)

    def test_candidates_owner_matching_fallback_new_images_upstream_flags_and_order(self):
        cases = []
        for include in (False, True):
            case = fixture()
            case["include"] = include
            case["images"] = list(reversed(case["images"])) + case["images"][:2]
            cases.append(case)
        # No owner match: select the ordinal-minimum old reference.
        case = fixture()
        case["new"]["compose_files"]["packages"]["a"]["services"].pop("tool")
        case["images"].remove("registry:5000/tool:new")
        cases.append(case)
        # Shared owners, multiple tags, case-sensitive references, empty and singleton inventories.
        cases.extend([
            dict(old=record("v1"), new=record("v2"), images=[], include=False),
            dict(old=record("v1"), new=record("v2", {"one": "ALPINE:1"}), images=["ALPINE:1"], include=False),
            dict(old=record("v1"), new=record("v2", {"one": PREFIX + "backend:v2"}),
                 images=[PREFIX + "backend:v2"], include=True),
        ])
        self.compare(cases)

    def test_release_schema_metadata_services_and_inventory_rejections(self):
        cases = []
        for target in ("old", "new"):
            for field, values in {
                "schema": (True, 1.0, 2, "1", None),
                "release": ("wrong", [], {}, None),
                "images": ({}, None, [], [{"image": "rangerdanger-extra"}]),
                "compose_files": ({}, [], None),
            }.items():
                for value in values:
                    case = fixture()
                    case[target][field] = value
                    cases.append(case)
            for field, values in {
                "file": (1, None), "sha256": ("A" * 64, "a" * 63, "a" * 64 + "\n", None),
                "services": ([], None, {"bad": ""}, {"bad": 1}, {"bad": ["nginx:1"]}),
            }.items():
                for value in values:
                    case = fixture()
                    case[target]["compose_files"]["platform"][field] = value
                    cases.append(case)
            case = fixture()
            case[target]["compose_files"]["packages"]["a"] = []
            cases.append(case)
            case = fixture()
            case[target]["Schema"] = case[target].pop("schema")
            cases.append(case)
        self.compare(cases)

    def test_compose_membership_and_first_party_tag_digest_rejections(self):
        cases = []
        for alteration in ("missing-first", "missing-upstream", "extra", "case", "wrong-tag", "digest"):
            case = fixture()
            if alteration == "missing-first":
                case["images"].remove(PREFIX + "backend:v2")
            elif alteration == "missing-upstream":
                case["images"].remove("nginx:1")
            elif alteration == "extra":
                case["images"].append("extra:1")
            elif alteration == "case":
                case["images"][0] = case["images"][0].upper()
            else:
                reference = PREFIX + ("backend:wrong" if alteration == "wrong-tag" else "backend@sha256:abc")
                case["new"]["compose_files"]["platform"]["services"]["backend"] = reference
                case["images"][0] = reference
            cases.append(case)
        self.compare(cases)

    def test_delta_script_calls_native_membership_without_external_commands(self):
        source = (ROOT / "stage-ssd-delta.ps1").read_text()
        self.assertIn('. "$PSScriptRoot/scripts/lib/release-membership.ps1"', source)
        self.assertIn("-IncludeUpstream:($IncludeUpstream -or $All)", source)
        block = source[source.index("$membershipDir ="):source.index("if (-not $candidateRows)")]
        self.assertNotIn("python", block.lower())
        self.assertIn("Get-DeltaImageCandidates", block)

    def test_actual_download_selection_block_flags_and_cleanup(self):
        for flag in ("default", "upstream", "all", "bad-record", "download-failed"):
            with self.subTest(flag=flag):
                case = fixture()
                if flag == "bad-record":
                    case["new"]["schema"] = 2
                result = powershell(r"""
$ErrorActionPreference='Stop'
. "$env:REPO/scripts/lib/release-membership.ps1"
$fixture=$env:FIXTURE | ConvertFrom-Json
$RootDir=$env:REPO; $Since='v1'; $New='v2'
$IncludeUpstream=$env:FLAG -eq 'upstream'; $All=$env:FLAG -eq 'all'
$allImages=$fixture.images; $ownerRepo='fixture/repo'
function Die($Message) { throw $Message }
function Invoke-WebRequest($Uri, $OutFile, [switch]$UseBasicParsing, $ErrorAction) {
    if (-not $UseBasicParsing) { throw 'IE parsing requested' }
    if ($env:FLAG -eq 'download-failed') { throw 'fixture HTTP failure' }
    $body=if ($Uri -match '/v1/') { $fixture.old } else { $fixture.new }
    [IO.File]::WriteAllText($OutFile, $body, (New-Object Text.UTF8Encoding $false))
}
$source=[IO.File]::ReadAllText("$env:REPO/stage-ssd-delta.ps1")
$start=$source.IndexOf('$membershipDir =')
$end=$source.IndexOf('$sinceRef =')
$failed=$false
try { Invoke-Expression $source.Substring($start, $end-$start) }
catch { $failed=$true }
if (Test-Path $membershipDir) { throw 'membership temp directory leaked' }
if ($failed -ne ($env:FLAG -in @('bad-record', 'download-failed'))) { throw 'incorrect membership outcome' }
if (-not $failed) { ConvertTo-Json -Depth 20 -Compress -InputObject @($candidateRows) }
""", FLAG=flag, FIXTURE=json.dumps(dict(case, old=json.dumps(case["old"]), new=json.dumps(case["new"]))))
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                if flag not in ("bad-record", "download-failed"):
                    expected = release_image_plan.delta_image_candidates(
                        previous_record=case["old"], new_record=case["new"], compose_images=case["images"],
                        since_tag="v1", new_tag="v2", include_upstream=flag != "default",
                    )
                    self.assertEqual(json.loads(result.stdout), expected)

    def test_touched_powershell_encoding_and_parser(self):
        for name in ("scripts/lib/release-membership.ps1", "stage-ssd-delta.ps1"):
            with self.subTest(name=name):
                (ROOT / name).read_bytes().decode("ascii")
                result = powershell(r"""
$tokens=$null; $errors=$null
$null=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:FILE", [ref]$tokens, [ref]$errors)
if ($errors.Count) { $errors | Out-String | Write-Output; exit 1 }
""", FILE=name)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
