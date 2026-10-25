"""Freeze the release graph across extraction and execute its record validator."""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
RECORD_WORKFLOW = ROOT / ".github/workflows/release-record.yml"


def record_script():
    workflow = yaml.safe_load(RECORD_WORKFLOW.read_text())
    return next(step["run"] for step in workflow["jobs"]["github-release"]["steps"]
                if step.get("name") == "Validate all image results and compose release record")


class ReleaseWorkflowTests(unittest.TestCase):
    def test_expanded_workflow_matches_pre_split_policy(self):
        # Snapshot of the complete parsed release.yml before extraction:
        # triggers, permissions, graph, conditions, env, action inputs and scripts.
        # BaseLoader keeps "on" a string rather than YAML 1.1's boolean key.
        workflow = yaml.load(
            (ROOT / ".github/workflows/release.yml").read_text(), Loader=yaml.BaseLoader)
        reusable = yaml.load(RECORD_WORKFLOW.read_text(), Loader=yaml.BaseLoader)
        self.assertEqual(set(reusable["on"]), {"workflow_call"})
        self.assertEqual(reusable["permissions"], workflow["permissions"])
        job = workflow["jobs"]["github-release"]
        self.assertEqual(job.pop("uses"), "./.github/workflows/release-record.yml")
        self.assertEqual(job.pop("with"), {
            "tag": "${{ inputs.tag }}",
            "commit": "${{ needs.plan.outputs.commit }}",
            "override": "${{ needs.plan.outputs.override }}",
        })
        extracted = reusable["jobs"]["github-release"]
        self.assertEqual(set(extracted), {"name", "runs-on", "steps"})
        # Restore only the explicit workflow-call argument substitutions.
        encoded = json.dumps(extracted).replace("inputs.commit", "needs.plan.outputs.commit")
        extracted = json.loads(encoded.replace("inputs.override", "needs.plan.outputs.override"))
        job.update(extracted)
        digest = hashlib.sha256(json.dumps(workflow, sort_keys=True).encode()).hexdigest()
        self.assertEqual(digest, "103f307c377cc06c2f2d330491b08e78e8b8ef0a0d4a09007ab706d9a603ed2a")
        self.assertLess(len((ROOT / ".github/workflows/release.yml").read_text().splitlines()), 1000)
        self.assertLess(len(RECORD_WORKFLOW.read_text().splitlines()), 1000)

    def test_record_validator_accepts_complete_builds_and_promotions_and_rejects_bad_evidence(self):
        inventory = json.loads((ROOT / ".github/release-images.json").read_text())
        commit = "a" * 40
        planned = []
        results = []
        for image in inventory["images"]:
            decision = "build" if image["image"] == "rangerdanger-frontend" else "promote"
            digest = "sha256:" + hashlib.sha256(image["image"].encode()).hexdigest()
            planned.append(dict(
                image=image["image"], repository="ghcr.io/tonylturner/" + image["image"],
                decision=decision, platforms=image["platforms"], source_digest=digest,
                source_build_revision="b" * 40, source_build_created="2026-01-01T00:00:00Z",
                source_release="v1.0.0"))
            result = dict(
                image=image["image"], repository=planned[-1]["repository"],
                decision=decision, platforms=image["platforms"], root_digest=digest,
                policy_version=inventory["policy_version"],
                build_revision=commit if decision == "build" else "b" * 40,
                build_created="2026-01-01T00:00:00Z",
                source_release="v2.0.0" if decision == "build" else "v1.0.0")
            if decision == "build":
                result["platform_builds"] = [
                    dict(platform=platform, build_revision=commit,
                         build_created="2026-01-01T00:00:00Z")
                    for platform in image["platforms"]
                ]
            results.append(result)
        plan = dict(
            release="v2.0.0", commit=commit, override=False,
            policy_version=inventory["policy_version"], images=planned,
            compose_files=dict(platform=dict(file="docker-compose.release.yml",
                                             sha256="c" * 64, services={}), packages={}))
        cases = ["complete", "missing", "duplicate", "promotion-digest", "platforms",
                 "frontend-provenance", "policy", "identity", "compose-hash"]
        script = record_script()
        for case in cases:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                observed = copy.deepcopy(results)
                current_plan = copy.deepcopy(plan)
                if case == "missing":
                    observed.pop()
                elif case == "duplicate":
                    observed.append(observed[0])
                elif case == "promotion-digest":
                    digest = observed[0]["root_digest"]
                    observed[0]["root_digest"] = digest[:-1] + ("0" if digest[-1] != "0" else "1")
                elif case == "platforms":
                    observed[0]["platforms"] = []
                elif case == "frontend-provenance":
                    next(item for item in observed if item["decision"] == "build")["platform_builds"] = []
                elif case == "policy":
                    observed[0]["policy_version"] = inventory["policy_version"] + 1
                elif case == "identity":
                    current_plan["commit"] = "d" * 40
                elif case == "compose-hash":
                    current_plan["compose_files"]["platform"]["sha256"] = "bad"
                (root / "plan.json").write_text(json.dumps(current_plan))
                result_dir = root / "results"
                result_dir.mkdir()
                for index, result in enumerate(observed):
                    (result_dir / f"plan-result-{index}.json").write_text(json.dumps(result))
                record = root / "record.json"
                env = dict(os.environ, PLAN_PATH=str(root / "plan.json"),
                           RESULTS_PATH=str(result_dir), RECORD_PATH=str(record),
                           INVENTORY_PATH=str(ROOT / ".github/release-images.json"),
                           RELEASE_TAG=plan["release"], RELEASE_COMMIT=commit, OVERRIDE="false")
                run = subprocess.run(["bash", "-e", "-c", script], env=env,
                                     capture_output=True, text=True, timeout=10)
                self.assertEqual(run.returncode == 0, case == "complete", run.stderr)
                if case == "complete":
                    self.assertEqual(json.loads(record.read_text())["images"], results)
                else:
                    self.assertFalse(record.exists())
