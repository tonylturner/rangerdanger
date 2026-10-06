from __future__ import annotations

import hashlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
_DEFAULT_RECORD = object()

import release_image_plan


class PlannerFixture(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name) / "repo"
        self.root.mkdir()
        source_inventory = json.loads(
            (ROOT / ".github/release-images.json").read_text(encoding="utf-8"))
        self.inventory = source_inventory
        self.inventory_path = Path(".github/release-images.json")
        (self.root / ".github").mkdir()
        (self.root / ".github/workflows").mkdir(parents=True)
        (self.root / ".github/release-images.json").write_text(
            json.dumps(self.inventory, indent=2) + "\n", encoding="utf-8")
        self._write(".dockerignore", "ignore\n")
        self._write(".gitattributes", "*.txt text\n")
        self._write(".github/workflows/release.yml", "release policy\n")
        self._write("scripts/release_image_plan.py", "planner\n")
        self._write("scripts/check_release_inputs.py", "checker\n")
        self._write("docker-compose.release.yml", "services: {}\n")
        for image in self.inventory["images"]:
            self._write(image["dockerfile"], "FROM scratch\n")
            for entry in image["inputs"]:
                path = entry["path"]
                if entry["kind"] == "file":
                    self._write(path, "base file\n")
                elif entry["kind"] == "dir":
                    self._write(f"{path}/seed.txt", "base directory member\n")
                else:
                    directory, _, pattern = path.rpartition("/")
                    if directory:
                        name = {
                            "services/shared": "types",
                            "dnp3go": "application",
                        }.get(directory, "main")
                    else:
                        name = "member"
                    filename = pattern.replace("*", name)
                    self._write(f"{directory}/{filename}" if directory else filename,
                                "base glob member\n")
        self._run_git("init", "-q")
        self._run_git("config", "user.name", "Tony Turner")
        self._run_git("config", "user.email", "tony@sentinel24.com")
        self._commit("baseline")
        self.baseline = self._git_text("rev-parse", "HEAD")
        self.digests = {
            image["image"]: "sha256:" + hashlib.sha256(
                image["image"].encode("utf-8")).hexdigest()
            for image in self.inventory["images"]
        }
        records = []
        for image in self.inventory["images"]:
            records.append({
                "image": image["image"],
                "repository": self._repository(image["image"]),
                "decision": "build",
                "root_digest": self.digests[image["image"]],
                "platforms": image["platforms"],
                "build_revision": self.baseline,
                "build_created": "2026-01-01T00:00:00Z",
                "source_release": "v0.1.32",
                "policy_version": self.inventory["policy_version"],
            })
        self.record_path = Path(self.temp.name) / "previous-release.json"
        self._write_outside(
            self.record_path,
            json.dumps({
                "schema": 1,
                "release": "v0.1.33",
                "policy_version": self.inventory["policy_version"],
                "images": records,
            }),
        )

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _write(self, path: str, content: str) -> None:
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")

    @staticmethod
    def _write_outside(path: Path, content: str) -> None:
        path.write_text(content, encoding="utf-8")

    def _run_git(self, *args: str) -> str:
        result = subprocess.run(
            ["git", "-C", str(self.root), *args],
            check=True,
            capture_output=True,
            text=True,
        )
        return result.stdout.strip()

    def _git_text(self, *args: str) -> str:
        return self._run_git(*args)

    def _commit(self, subject: str) -> None:
        self._run_git("add", "-A")
        self._run_git("commit", "-qm", subject)

    def _change_commit(self, path: str, content: str) -> str:
        self._write(path, content)
        self._commit(f"change {path}")
        return self._git_text("rev-parse", "HEAD")

    def _repository(self, image: str) -> str:
        return f"{self.inventory['registry']}/{self.inventory['namespace']}/{image}"

    def _inspect(
        self,
        *,
        mismatch_tag: str | None = None,
        unresolved: str | None = None,
        mismatch_platform: str | None = None,
        attestations: bool = True,
        missing_tag: bool = False,
    ):
        image_by_name = {item["image"]: item for item in self.inventory["images"]}

        def inspect(reference: str) -> dict[str, object]:
            image = reference.split("/")[-1].split("@", 1)[0].split(":", 1)[0]
            if image == unresolved and "@" in reference:
                raise RuntimeError("synthetic registry lookup failure")
            if missing_tag and reference.endswith(":v0.1.33"):
                raise RuntimeError("tag not found")
            digest = self.digests[image]
            if image == mismatch_tag and reference.endswith(":v0.1.33"):
                digest = "sha256:" + "f" * 64
            platforms = list(image_by_name[image]["platforms"])
            if image == mismatch_platform and "@" in reference:
                platforms = platforms[:1]
            descriptors: list[dict[str, object]] = []
            image_payload: dict[str, object] = {}
            for platform in platforms:
                os_name, architecture = platform.split("/", 1)
                descriptors.append({
                    "mediaType": "application/vnd.oci.image.manifest.v1+json",
                    "digest": "sha256:" + "a" * 64,
                    "size": 1618,
                    "platform": {"architecture": architecture, "os": os_name},
                    "annotations": None,
                })
                image_payload[platform] = {
                    "architecture": architecture,
                    "os": os_name,
                }
            if attestations:
                for platform in platforms:
                    _, architecture = platform.split("/", 1)
                    descriptors.append({
                        "mediaType": "application/vnd.oci.image.manifest.v1+json",
                        "digest": "sha256:" + architecture[0] * 64,
                        "size": 838,
                        "platform": {"architecture": "unknown", "os": "unknown"},
                        "annotations": {
                            "vnd.docker.reference.digest": "sha256:" + "a" * 64,
                            "vnd.docker.reference.type": "attestation-manifest",
                        },
                    })
            return {
                "name": reference,
                "manifest": {
                    "schemaVersion": 2,
                    "mediaType": "application/vnd.oci.image.index.v1+json",
                    "digest": digest,
                    "size": 1609,
                    "manifests": descriptors,
                },
                "image": image_payload,
            }

        return inspect

    def _plan(
        self,
        *,
        commit: str | None = None,
        tag: str = "v0.1.34",
        event: str = "tag_push",
        record_path: Path | None | object = _DEFAULT_RECORD,
        inspect=None,
        override: bool = False,
    ) -> dict[str, object]:
        with patch.object(release_image_plan.check_release_inputs, "validate",
                          return_value=[]):
            return release_image_plan.build_plan(
                root=self.root,
                tag=tag,
                commit=commit or self._git_text("rev-parse", "HEAD"),
                event=event,
                override=override,
                previous_record_path=(
                    self.record_path if record_path is _DEFAULT_RECORD else record_path
                ),
                inventory_path=self.inventory_path,
                inspect=inspect or self._inspect(),
            )

    @staticmethod
    def _decisions(plan: dict[str, object]) -> dict[str, str]:
        return {image["image"]: image["decision"] for image in plan["images"]}


class ReleaseImagePlanTests(PlannerFixture):
    def _github_output_values(self, plan: dict[str, object]) -> dict[str, str]:
        output_path = Path(self.temp.name) / "github-output"
        release_image_plan._emit_github_output(plan, output_path)
        return dict(
            line.split("=", 1)
            for line in output_path.read_text(encoding="utf-8").splitlines()
        )

    def test_docs_change_promotes_15_and_always_builds_backend(self) -> None:
        commit = self._change_commit("README.md", "documentation only\n")
        plan = self._plan(commit=commit)
        decisions = self._decisions(plan)
        self.assertEqual(decisions["rangerdanger-backend"], "build")
        self.assertEqual(sum(value == "promote" for value in decisions.values()), 15)
        self.assertEqual(sum(value == "build" for value in decisions.values()), 1)

    def test_simulator_input_decision_matrix(self) -> None:
        cases = [
            (
                "services/relay-sim/new.go",
                "new relay source\n",
                {"rangerdanger-relay-sim"},
            ),
            (
                "services/shared/types.go",
                "changed shared source\n",
                {
                    "rangerdanger-relay-sim",
                    "rangerdanger-recloser-sim",
                    "rangerdanger-regulator-sim",
                    "rangerdanger-capbank-sim",
                    "rangerdanger-rtac-sim",
                    "rangerdanger-historian-sim",
                    "rangerdanger-gps-sim",
                },
            ),
            (
                "dnp3go/application.go",
                "changed DNP3 source\n",
                {
                    "rangerdanger-relay-sim",
                    "rangerdanger-recloser-sim",
                    "rangerdanger-regulator-sim",
                    "rangerdanger-capbank-sim",
                    "rangerdanger-rtac-sim",
                    "rangerdanger-kali",
                    "rangerdanger-vendor-jump",
                    "rangerdanger-eng-ws",
                    "rangerdanger-openplc",
                },
            ),
        ]
        for path, content, affected in cases:
            with self.subTest(path=path):
                self.tearDown()
                self.setUp()
                commit = self._change_commit(path, content)
                decisions = self._decisions(self._plan(commit=commit))
                build_names = {
                    name for name, decision in decisions.items() if decision == "build"
                }
                self.assertEqual(build_names, affected | {"rangerdanger-backend"})

    def test_each_global_control_invalidates_every_image(self) -> None:
        count = len(self.inventory["images"])
        cases = [
            (".gitattributes", "*.txt text eol=lf\n"),
            ("Dockerfile.frontend.dockerignore", "frontend/generated\n"),
        ]
        for path, content in cases:
            with self.subTest(path=path):
                self.tearDown()
                self.setUp()
                commit = self._change_commit(path, content)
                decisions = self._decisions(self._plan(commit=commit))
                self.assertEqual(len(decisions), count)
                self.assertEqual(set(decisions.values()), {"build"})

        self.tearDown()
        self.setUp()
        self.inventory["policy_version"] += 1
        self._write(".github/release-images.json",
                    json.dumps(self.inventory, indent=2) + "\n")
        self._commit("bump policy")
        decisions = self._decisions(self._plan())
        self.assertEqual(len(decisions), count)
        self.assertEqual(set(decisions.values()), {"build"})

    def test_missing_record_stale_policy_and_dispatch_never_inspect_registry(self) -> None:
        no_record_inspect = Mock(side_effect=AssertionError("registry must not be inspected"))
        plan = self._plan(record_path=None, inspect=no_record_inspect)
        self.assertEqual(set(self._decisions(plan).values()), {"build"})
        self.assertEqual(no_record_inspect.call_count, 0)

        record = json.loads(self.record_path.read_text(encoding="utf-8"))
        for item in record["images"]:
            item["policy_version"] = 0
        self.record_path.write_text(json.dumps(record), encoding="utf-8")
        stale_inspect = Mock(side_effect=AssertionError("stale policy must fail closed"))
        stale_plan = self._plan(inspect=stale_inspect)
        self.assertEqual(set(self._decisions(stale_plan).values()), {"build"})
        self.assertEqual(stale_inspect.call_count, 0)

        record["policy_version"] = 0
        for item in record["images"]:
            item["policy_version"] = self.inventory["policy_version"]
        self.record_path.write_text(json.dumps(record), encoding="utf-8")
        top_level_inspect = Mock(side_effect=AssertionError("stale record must fail closed"))
        top_level_plan = self._plan(inspect=top_level_inspect)
        self.assertEqual(set(self._decisions(top_level_plan).values()), {"build"})
        self.assertEqual(top_level_inspect.call_count, 0)

        dispatch_inspect = Mock(side_effect=AssertionError("dispatch never promotes"))
        dispatch = self._plan(event="dispatch", inspect=dispatch_inspect)
        self.assertEqual(set(self._decisions(dispatch).values()), {"build"})
        self.assertEqual(dispatch_inspect.call_count, 0)

    def test_unreadable_record_builds_every_image(self) -> None:
        missing = Path(self.temp.name) / "missing.json"
        decisions = self._decisions(self._plan(record_path=missing))
        self.assertEqual(set(decisions.values()), {"build"})
        unreadable = Path(self.temp.name) / "unreadable.json"
        unreadable.write_bytes(b"\xff")
        unreadable_decisions = self._decisions(
            self._plan(record_path=unreadable))
        self.assertEqual(set(unreadable_decisions.values()), {"build"})

    def test_unreadable_baseline_tree_is_a_per_image_build(self) -> None:
        record = json.loads(self.record_path.read_text(encoding="utf-8"))
        frontend = next(
            item for item in record["images"]
            if item["image"] == "rangerdanger-frontend"
        )
        frontend["build_revision"] = "f" * 40
        self.record_path.write_text(json.dumps(record), encoding="utf-8")
        inspect = Mock(side_effect=AssertionError(
            "unreadable baseline must not inspect the registry"))
        plan = self._plan(inspect=inspect)
        results = {item["image"]: item for item in plan["images"]}
        self.assertEqual(results["rangerdanger-frontend"]["decision"], "build")
        self.assertIn("object identity unavailable",
                      results["rangerdanger-frontend"]["reason"])
        self.assertEqual(inspect.call_count, 14)

    def test_rebuild_all_override_builds_every_image_without_registry_access(self) -> None:
        inspect = Mock(side_effect=AssertionError("override must not inspect registry"))
        plan = self._plan(override=True, inspect=inspect)
        self.assertEqual(set(self._decisions(plan).values()), {"build"})
        self.assertTrue(plan["override"])
        self.assertEqual(inspect.call_count, 0)

    def test_registry_trust_failures_build_the_affected_image(self) -> None:
        cases = [
            ("record/tag disagreement", self._inspect(mismatch_tag="rangerdanger-frontend"),
             "rangerdanger-frontend"),
            ("digest unresolvable", self._inspect(unresolved="rangerdanger-frontend"),
             "rangerdanger-frontend"),
            ("platform mismatch", self._inspect(mismatch_platform="rangerdanger-frontend"),
             "rangerdanger-frontend"),
            ("previous tag unavailable", self._inspect(missing_tag=True),
             "rangerdanger-frontend"),
        ]
        for label, inspect, affected in cases:
            with self.subTest(case=label):
                decisions = self._decisions(self._plan(inspect=inspect))
                self.assertEqual(decisions[affected], "build")

    def test_attestation_descriptors_do_not_change_platform_set(self) -> None:
        plan = self._plan(inspect=self._inspect())
        decisions = self._decisions(plan)
        self.assertEqual(decisions["rangerdanger-backend"], "build")
        self.assertEqual(sum(value == "promote" for value in decisions.values()), 15)

    def test_real_buildx_registry_shape_and_single_manifest_shape(self) -> None:
        payload = self._inspect()(
            "ghcr.io/tonylturner/rangerdanger-gps-sim@"
            + self.digests["rangerdanger-gps-sim"])
        self.assertEqual(set(payload), {"name", "manifest", "image"})
        manifest = payload["manifest"]
        self.assertEqual(set(manifest), {
            "schemaVersion", "mediaType", "digest", "size", "manifests",
        })
        self.assertEqual(len(manifest["manifests"]), 4)
        self.assertEqual(
            sum(
                bool(item["annotations"])
                and item["annotations"].get("vnd.docker.reference.type")
                == "attestation-manifest"
                for item in manifest["manifests"]
            ),
            2,
        )
        self.assertEqual(
            release_image_plan._registry_identity(payload),
            (self.digests["rangerdanger-gps-sim"], {"linux/amd64", "linux/arm64"}),
        )

        single_manifest = {
            "name": "ghcr.io/example/image:tag",
            "manifest": {
                "schemaVersion": 2,
                "mediaType": "application/vnd.oci.image.manifest.v1+json",
                "digest": "sha256:" + "c" * 64,
                "size": 1234,
            },
            "image": {"architecture": "arm64", "os": "linux"},
        }
        self.assertEqual(
            release_image_plan._registry_identity(single_manifest),
            ("sha256:" + "c" * 64, {"linux/arm64"}),
        )
        single_without_root_digest = json.loads(json.dumps(single_manifest))
        del single_without_root_digest["manifest"]["digest"]
        with self.assertRaisesRegex(ValueError, "manifest.digest"):
            release_image_plan._registry_identity(single_without_root_digest)

    def test_global_guard_failure_exits_nonzero_but_image_registry_failure_exits_zero(
        self,
    ) -> None:
        original_cwd = Path.cwd()
        os.chdir(self.root)
        try:
            common_args = [
                "--tag", "v0.1.34",
                "--commit", self._git_text("rev-parse", "HEAD"),
                "--event", "tag_push",
                "--previous-record", str(self.record_path),
            ]
            stdout = io.StringIO()
            stderr = io.StringIO()
            with patch.object(
                release_image_plan.check_release_inputs,
                "validate",
                return_value=[
                    release_image_plan.check_release_inputs.Violation(
                        "Dockerfile.frontend", 4, "rangerdanger-frontend",
                        "unsupported recipe",
                    )
                ],
            ), redirect_stdout(stdout), redirect_stderr(stderr):
                global_exit = release_image_plan.main(common_args)
            self.assertEqual(global_exit, 1)
            self.assertIn("release input guard rejected", stderr.getvalue())
            self.assertEqual(stdout.getvalue(), "")

            stdout = io.StringIO()
            stderr = io.StringIO()
            with patch.object(
                release_image_plan.check_release_inputs,
                "validate",
                return_value=[],
            ), patch.object(
                release_image_plan,
                "inspect_registry",
                side_effect=self._inspect(unresolved="rangerdanger-frontend"),
            ), redirect_stdout(stdout), redirect_stderr(stderr):
                image_exit = release_image_plan.main(common_args)
            self.assertEqual(image_exit, 0)
            self.assertEqual(stderr.getvalue(), "")
            plan = json.loads(stdout.getvalue())
            decisions = {item["image"]: item for item in plan["images"]}
            self.assertEqual(decisions["rangerdanger-frontend"]["decision"], "build")
            self.assertIn("recorded digest cannot be resolved",
                          decisions["rangerdanger-frontend"]["reason"])
        finally:
            os.chdir(original_cwd)

    def test_bad_cli_tag_commit_and_event_exit_nonzero(self) -> None:
        original_cwd = Path.cwd()
        os.chdir(self.root)
        try:
            base_args = [
                "--tag", "v0.1.34",
                "--commit", self._git_text("rev-parse", "HEAD"),
                "--event", "tag_push",
            ]
            with patch.object(
                release_image_plan.check_release_inputs,
                "validate",
                return_value=[],
            ):
                with redirect_stderr(io.StringIO()):
                    self.assertEqual(
                        release_image_plan.main([
                            "--tag", "not a tag",
                            *base_args[2:],
                        ]),
                        1,
                    )
                    self.assertEqual(
                        release_image_plan.main([
                            *base_args[:2],
                            "--commit", "not-a-commit",
                            *base_args[4:],
                        ]),
                        1,
                    )
                    with self.assertRaises(SystemExit) as bad_event:
                        release_image_plan.main([
                            *base_args[:4],
                            "--event", "not-an-event",
                        ])
            self.assertEqual(bad_event.exception.code, 2)
        finally:
            os.chdir(original_cwd)

    def test_stable_and_prerelease_tag_plans_on_build_and_promote_paths(self) -> None:
        no_record = self._plan(
            tag="v0.1.34", record_path=Path(self.temp.name) / "missing.json")
        self.assertEqual(set(self._decisions(no_record).values()), {"build"})
        self.assertEqual(release_image_plan.planned_tags("v0.1.34"),
                         ["v0.1.34", "latest"])
        self.assertEqual(release_image_plan.planned_tags("v0.1.34-rc.1"),
                         ["v0.1.34-rc.1"])

    def test_publish_matrix_excludes_frontend_while_plan_keeps_all_images(self) -> None:
        plan = self._plan(record_path=Path(self.temp.name) / "no-record.json")
        values = self._github_output_values(plan)
        matrix = json.loads(values["matrix"])
        plan_json = json.loads(json.dumps(plan))
        inventory_names = [image["image"] for image in self.inventory["images"]]

        self.assertEqual(
            [image["image"] for image in plan_json["images"]],
            inventory_names,
        )
        self.assertEqual(
            [image["image"] for image in matrix["include"]],
            [name for name in inventory_names if name != "rangerdanger-frontend"],
        )

    def test_publish_matrix_platforms_are_inventory_lists(self) -> None:
        plan = self._plan(record_path=Path(self.temp.name) / "no-record.json")
        values = self._github_output_values(plan)
        matrix = json.loads(values["matrix"])
        inventory = {image["image"]: image for image in self.inventory["images"]}

        for entry in matrix["include"]:
            with self.subTest(image=entry["image"]):
                self.assertIsInstance(entry["platforms"], list)
                self.assertEqual(
                    entry["platforms"],
                    inventory[entry["image"]]["platforms"],
                )

    def test_github_output_matrix_has_only_the_frozen_publish_fields(self) -> None:
        plan = self._plan(record_path=Path(self.temp.name) / "no-record.json")
        values = self._github_output_values(plan)
        matrix = json.loads(values["matrix"])
        self.assertEqual(
            set(matrix["include"][0]),
            {"image", "dockerfile", "target", "platforms", "decision", "source_digest"},
        )
        self.assertEqual(values["build_count"], "16")
        self.assertEqual(values["promote_count"], "0")
        self.assertEqual(values["frontend_decision"], "build")

        promoted = self._plan(tag="v0.1.34-rc.1")
        self.assertEqual(
            sum(value == "promote" for value in self._decisions(promoted).values()), 15)
        self.assertEqual(release_image_plan.planned_tags("v0.1.34-rc.1"),
                         ["v0.1.34-rc.1"])

    def test_git_identity_captures_mode_changes_in_globs_and_directory_trees(self) -> None:
        before = self.baseline
        path = self.root / "services/shared/types.go"
        path.chmod(0o755)
        self._commit("change mode")
        after = self._git_text("rev-parse", "HEAD")
        self.assertNotEqual(
            release_image_plan._identity(
                self.root, before, "glob", "services/shared/*.go"),
            release_image_plan._identity(
                self.root, after, "glob", "services/shared/*.go"),
        )
        self.assertNotEqual(
            release_image_plan._identity(self.root, before, "dir", "services/shared"),
            release_image_plan._identity(self.root, after, "dir", "services/shared"),
        )


if __name__ == "__main__":
    unittest.main()
