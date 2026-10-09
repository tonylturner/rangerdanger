#!/usr/bin/env python3
"""Plan fail-closed build or exact-root promotion decisions for a release."""

from __future__ import annotations

import argparse
import fnmatch
import json
import os
import re
import subprocess
import sys
from collections.abc import Callable
from functools import lru_cache
from pathlib import Path
from typing import Any

try:
    import check_release_inputs
except ModuleNotFoundError:  # package import from the repository-root unittest command
    from scripts import check_release_inputs


INVENTORY_DEFAULT = ".github/release-images.json"
REGISTRY_DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
FILE_HASH_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")


class PlanError(Exception):
    """An invalid planner invocation or unusable current inventory."""


def inspect_registry(reference: str) -> dict[str, Any]:
    """Resolve one registry reference through Buildx; tests replace this helper."""
    result = subprocess.run(
        [
            "docker", "buildx", "imagetools", "inspect", reference,
            "--format", "{{json .}}",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise RuntimeError(f"registry inspection failed for {reference}: {detail}")
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"registry returned invalid JSON for {reference}: {exc}") from exc
    if not isinstance(payload, dict):
        raise TypeError(f"registry returned a non-object for {reference}")
    return payload


def planned_tags(tag: str) -> list[str]:
    """Return the release tag plus latest for stable (non-hyphenated) tags."""
    tags = [tag]
    if "-" not in tag:
        tags.append("latest")
    return tags


def _run_git(root: Path, *args: str) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(root), *args],
        check=False,
        capture_output=True,
    )
    if result.returncode:
        message = result.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(message or f"git {' '.join(args)} failed")
    return result.stdout


def _resolve_commit(root: Path, revision: str) -> str:
    resolved = _run_git(root, "rev-parse", "--verify", f"{revision}^{{commit}}")
    return resolved.decode("ascii").strip()


def _ls_tree_entries(root: Path, revision: str, path: str) -> list[tuple[str, str, str, str]]:
    output = _run_git(root, "ls-tree", "-r", "-z", revision, "--", path)
    entries: list[tuple[str, str, str, str]] = []
    for raw in output.split(b"\0"):
        if not raw:
            continue
        metadata, name = raw.split(b"\t", 1)
        mode, object_type, oid = metadata.decode("ascii").split(" ", 2)
        entries.append((
            name.decode("utf-8", errors="surrogateescape"),
            mode,
            object_type,
            oid,
        ))
    return entries


@lru_cache(maxsize=4096)
def _identity(root: Path, revision: str, kind: str, path: str) -> Any:
    """Return exact Git mode/type/OID identities for an input selector."""
    if kind == "dir":
        output = _run_git(root, "ls-tree", "-d", "-r", "-z", revision, "--", path)
        prefix = path.rstrip("/")
        for raw in output.split(b"\0"):
            if not raw:
                continue
            metadata, name = raw.split(b"\t", 1)
            if name.decode("utf-8", errors="surrogateescape") == prefix:
                mode, object_type, oid = metadata.decode("ascii").split(" ", 2)
                return mode, object_type, oid
        return None

    if kind == "file":
        entries = _ls_tree_entries(root, revision, path)
        return next((entry[1:] for entry in entries if entry[0] == path), None)
    if kind == "glob":
        directory, _, pattern = path.rpartition("/")
        tree_path = directory or "."
        entries = _ls_tree_entries(root, revision, tree_path)
        matches = [
            (name[len(directory) + 1:] if directory else name, mode, obj_type, oid)
            for name, mode, obj_type, oid in entries
            if (not directory or name.startswith(directory + "/"))
            and "/" not in (name[len(directory) + 1:] if directory else name)
            and fnmatch.fnmatchcase(
                name[len(directory) + 1:] if directory else name, pattern)
        ]
        return tuple(sorted((name, mode, obj_type, oid) for
                            name, mode, obj_type, oid in matches))
    raise ValueError(f"unknown input selector kind: {kind}")


def _changed_selector(
    root: Path,
    before: str,
    after: str,
    kind: str,
    path: str,
) -> bool:
    return _identity(root, before, kind, path) != _identity(root, after, kind, path)


def _control_selectors(
    image: dict[str, Any],
    inventory: dict[str, Any],
    inventory_path: str,
) -> list[tuple[str, str]]:
    dockerfiles = sorted({
        str(entry["dockerfile"]) for entry in inventory["images"]
        if isinstance(entry, dict) and isinstance(entry.get("dockerfile"), str)
    })
    # Treat appearance/disappearance of any per-Dockerfile ignore sidecar as a
    # global context-policy change. This is intentionally conservative: §12
    # requires a newly added frontend sidecar to invalidate all image decisions.
    selectors = [
        ("file", str(image["dockerfile"])),
        ("file", ".dockerignore"),
        ("file", ".gitattributes"),
        ("file", ".github/workflows/release.yml"),
        ("file", inventory_path),
        ("file", "scripts/release_image_plan.py"),
        ("file", "scripts/check_release_inputs.py"),
    ]
    selectors.extend(("file", f"{dockerfile}.dockerignore") for dockerfile in dockerfiles)
    return selectors


def _first_changed_input(
    root: Path,
    before: str,
    after: str,
    image: dict[str, Any],
    inventory: dict[str, Any],
    inventory_path: str,
) -> str | None:
    try:
        for item in image.get("inputs", []):
            kind = item["kind"]
            path = item["path"]
            if _changed_selector(root, before, after, kind, path):
                return path
        for kind, path in _control_selectors(image, inventory, inventory_path):
            if _changed_selector(root, before, after, kind, path):
                return path
    except (OSError, RuntimeError, ValueError, KeyError) as exc:
        return f"object identity unavailable ({exc})"
    return None


def _record_index(record: Any) -> tuple[dict[str, dict[str, Any]], str | None]:
    if not isinstance(record, dict) or type(record.get("schema")) is not int \
            or record.get("schema") != 1:
        return {}, None
    release = record.get("release")
    if not isinstance(release, str) or not release:
        return {}, None
    images = record.get("images")
    if not isinstance(images, list):
        return {}, release
    indexed: dict[str, dict[str, Any]] = {}
    for item in images:
        if isinstance(item, dict) and isinstance(item.get("image"), str):
            if item["image"] in indexed:
                return {}, None
            indexed[item["image"]] = item
    return indexed, release


def _image_repository(reference: str) -> str:
    """Strip a tag or digest while preserving any registry port."""
    value = reference.split("@", 1)[0]
    last_component = value.rsplit("/", 1)[-1]
    if ":" in last_component:
        value = value.rsplit(":", 1)[0]
    return value


def _release_membership(
    record: Any,
    expected_tag: str,
) -> tuple[dict[str, set[str]], dict[str, dict[str, set[str]]], set[str]]:
    """Read validated repository references and package/service membership."""
    if not isinstance(record, dict) \
            or type(record.get("schema")) is not int or record.get("schema") != 1 \
            or record.get("release") != expected_tag:
        raise PlanError(f"release record does not identify {expected_tag}")
    compose_files = record.get("compose_files")
    if not isinstance(compose_files, dict) \
            or not isinstance(compose_files.get("platform"), dict) \
            or not isinstance(compose_files.get("packages"), dict):
        raise PlanError(f"release {expected_tag} has no platform/package membership")

    refs: dict[str, set[str]] = {}
    owners: dict[str, dict[str, set[str]]] = {}
    models = [("platform", compose_files["platform"])]
    models.extend(sorted(compose_files["packages"].items()))
    for owner, model in models:
        if not isinstance(owner, str) or not isinstance(model, dict) \
                or not isinstance(model.get("file"), str) \
                or not isinstance(model.get("sha256"), str) \
                or not FILE_HASH_RE.fullmatch(model["sha256"]) \
                or not isinstance(model.get("services"), dict):
            raise PlanError(f"release {expected_tag} has malformed {owner} Compose metadata")
        for service, reference in model["services"].items():
            if not isinstance(service, str) or not isinstance(reference, str) or not reference:
                raise PlanError(
                    f"release {expected_tag} has malformed {owner} service membership")
            repository = _image_repository(reference)
            refs.setdefault(repository, set()).add(reference)
            owners.setdefault(repository, {}).setdefault(reference, set()).add(
                f"{owner}/{service}")

    images = record.get("images")
    if not isinstance(images, list):
        raise PlanError(f"release {expected_tag} has no image build inventory")
    recorded_builds = {
        f"ghcr.io/tonylturner/{item['image']}"
        for item in images
        if isinstance(item, dict) and isinstance(item.get("image"), str)
    }
    first_party = {
        repository for repository in refs
        if repository.startswith("ghcr.io/tonylturner/rangerdanger-")
    }
    if first_party != recorded_builds:
        raise PlanError(
            f"release {expected_tag} package/platform membership differs from "
            "the authored build inventory")
    return refs, owners, first_party


def delta_image_candidates(
    *,
    previous_record: Any,
    new_record: Any,
    compose_images: list[str],
    since_tag: str,
    new_tag: str,
    include_upstream: bool = False,
) -> list[dict[str, Any]]:
    """Select new runtime images while using old/new package membership."""
    old_refs, old_owners, _ = _release_membership(previous_record, since_tag)
    new_refs, new_owners, new_first_party = _release_membership(new_record, new_tag)
    local_first_party = {
        repository for image in compose_images
        if (repository := _image_repository(image)).startswith(
            "ghcr.io/tonylturner/rangerdanger-")
    }
    if local_first_party != new_first_party:
        raise PlanError(
            "new Compose image union differs from release asset first-party membership")

    new_references = {
        reference for references in new_refs.values() for reference in references
    }
    local_references = set(compose_images)
    if local_references != new_references:
        raise PlanError(
            "new Compose image union differs from release asset membership "
            f"(compose-only={sorted(local_references - new_references)}, "
            f"asset-only={sorted(new_references - local_references)})")
    candidates: list[dict[str, Any]] = []
    seen: set[tuple[str, str]] = set()
    for reference in sorted(set(compose_images)):
        repository = _image_repository(reference)
        first_party = repository.startswith("ghcr.io/tonylturner/rangerdanger-")
        if first_party:
            if repository not in new_refs:
                raise PlanError(
                    f"Compose image {reference} is absent from the new release record")
            tag = reference.rsplit(":", 1)[-1] if "@" not in reference else ""
            if tag != new_tag:
                raise PlanError(
                    f"Compose image {reference} does not use release tag {new_tag}")
        elif reference not in new_references:
            raise PlanError(
                f"Compose image {reference} is absent from the new release record")
        if (reference, repository) in seen:
            continue
        seen.add((reference, repository))

        old_candidates = old_refs.get(repository, set())
        service_owners = new_owners.get(repository, {}).get(reference, set())
        matching_old = sorted(
            old_reference for old_reference in old_candidates
            if service_owners & old_owners.get(repository, {}).get(old_reference, set())
        )
        previous_reference = (
            matching_old[0] if matching_old else
            min(old_candidates) if old_candidates else reference
        )
        if not first_party and not include_upstream \
                and previous_reference == reference and old_candidates:
            continue
        candidates.append({
            "image": reference,
            "since": previous_reference,
            "repository": repository,
            "first_party": first_party,
            "old_member": bool(old_candidates),
            "owners": sorted(service_owners),
        })
    return candidates


def _delta_candidates_main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Select delta images from old/new release package membership")
    parser.add_argument("--since-record", required=True)
    parser.add_argument("--new-record", required=True)
    parser.add_argument("--since", required=True)
    parser.add_argument("--new", required=True)
    parser.add_argument("--compose-images", required=True)
    parser.add_argument("--include-upstream", action="store_true")
    parser.add_argument("--format", choices=("json", "tsv"), default="json")
    args = parser.parse_args(argv)
    try:
        previous_record = json.loads(
            Path(args.since_record).read_text(encoding="utf-8"))
        new_record = json.loads(Path(args.new_record).read_text(encoding="utf-8"))
        compose_images = [
            line.strip()
            for line in Path(args.compose_images).read_text(encoding="utf-8").splitlines()
            if line.strip()
        ]
        candidates = delta_image_candidates(
            previous_record=previous_record,
            new_record=new_record,
            compose_images=compose_images,
            since_tag=args.since,
            new_tag=args.new,
            include_upstream=args.include_upstream,
        )
    except (OSError, UnicodeError, json.JSONDecodeError, PlanError) as exc:
        print(f"release membership rejected: {exc}", file=sys.stderr)
        return 1
    if args.format == "json":
        print(json.dumps(candidates, separators=(",", ":")))
    else:
        for item in candidates:
            print("\t".join((
                item["image"],
                item["since"],
                item["repository"],
                "yes" if item["first_party"] else "no",
                "yes" if item["old_member"] else "no",
                ",".join(item["owners"]),
            )))
    return 0


def _load_previous_record(
    path: Path | None,
) -> tuple[dict[str, dict[str, Any]], str | None, Any, str]:
    if path is None or not path.exists():
        return {}, None, None, "no previous release record"
    try:
        record = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        return {}, None, None, f"previous release record is unreadable: {exc}"
    indexed, release = _record_index(record)
    if release is None:
        return {}, None, None, "previous release record is malformed or has no release tag"
    return indexed, release, record.get("policy_version"), ""


def _platform_from_descriptor(descriptor: dict[str, Any]) -> str | None:
    annotations = descriptor.get("annotations")
    if isinstance(annotations, dict) and (
        annotations.get("vnd.docker.reference.type") == "attestation-manifest"
    ):
        return None
    platform = descriptor.get("platform")
    if not isinstance(platform, dict):
        return None
    os_name = platform.get("os")
    architecture = platform.get("architecture")
    if os_name == "unknown" and architecture == "unknown":
        return None
    if not isinstance(os_name, str) or not isinstance(architecture, str):
        return None
    return f"{os_name}/{architecture}"


def _registry_identity(payload: dict[str, Any]) -> tuple[str, set[str]]:
    manifest = payload.get("manifest")
    if not isinstance(manifest, dict):
        raise TypeError("registry response has no manifest object")
    digest = manifest.get("digest")
    if not isinstance(digest, str) or not REGISTRY_DIGEST_RE.fullmatch(digest):
        raise ValueError("registry response has no valid lowercase manifest.digest")
    platforms: set[str] = set()
    if "manifests" in manifest:
        descriptors = manifest["manifests"]
        if not isinstance(descriptors, list):
            raise ValueError("registry manifest.manifests is not an array")
        for descriptor in descriptors:
            if isinstance(descriptor, dict):
                platform = _platform_from_descriptor(descriptor)
                if platform:
                    platforms.add(platform)
    else:
        image = payload.get("image")
        if not isinstance(image, dict):
            raise ValueError("single-manifest registry response has no image object")
        platform = _platform_from_descriptor({"platform": image})
        if not platform:
            raise ValueError(
                "single-manifest registry response image has no valid platform")
        platforms.add(platform)
    return digest, platforms


def _validate_record_entry(
    record: dict[str, Any] | None,
    image: dict[str, Any],
    repository: str,
    policy_version: int,
) -> str | None:
    if record is None:
        return "no per-image record in previous release"
    if type(record.get("policy_version")) is not int \
            or record.get("policy_version") != policy_version:
        return "previous record policy_version does not match inventory"
    if record.get("repository") != repository:
        return "previous record repository does not match inventory"
    digest = record.get("root_digest")
    if not isinstance(digest, str) or not REGISTRY_DIGEST_RE.fullmatch(digest):
        return "previous record has no valid root digest"
    if record.get("platforms") != image.get("platforms"):
        return "recorded platform list does not match inventory"
    revision = record.get("build_revision")
    if not isinstance(revision, str) or not COMMIT_RE.fullmatch(revision):
        return "previous record has no full build_revision commit SHA"
    if not isinstance(record.get("build_created"), str):
        return "previous record has no build_created"
    if not isinstance(record.get("source_release"), str):
        return "previous record has no source_release"
    return None


def _promote_failure(
    inspect: Callable[[str], dict[str, Any]],
    image_record: dict[str, Any],
    image: dict[str, Any],
    repository: str,
    previous_release: str,
) -> str | None:
    digest = image_record["root_digest"]
    try:
        source_digest, source_platforms = _registry_identity(
            inspect(f"{repository}@{digest}"))
    except Exception as exc:  # noqa: BLE001 -- registry errors fail closed into an image build
        return f"recorded digest cannot be resolved: {exc}"
    if source_digest != digest:
        return "registry digest response disagrees with recorded root digest"
    expected = set(image["platforms"])
    if source_platforms != expected:
        return ("recorded root platform set does not match inventory "
                f"(registry={sorted(source_platforms)}, expected={sorted(expected)})")
    try:
        tagged_digest, _ = _registry_identity(
            inspect(f"{repository}:{previous_release}"))
    except Exception as exc:  # noqa: BLE001 -- registry errors fail closed into an image build
        return f"previous release tag cannot be resolved: {exc}"
    if tagged_digest != digest:
        return ("previous release tag disagrees with recorded root digest "
                f"(tag={tagged_digest}, record={digest})")
    return None


def build_plan(
    *,
    root: Path,
    tag: str,
    commit: str,
    event: str,
    override: bool = False,
    previous_record_path: Path | None = None,
    inventory_path: Path | None = None,
    inspect: Callable[[str], dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Build the plan; ``inspect`` substitutes the one registry helper in tests."""
    root = root.resolve()
    inventory_rel = inventory_path or Path(INVENTORY_DEFAULT)
    inventory_abs = inventory_rel if inventory_rel.is_absolute() else root / inventory_rel
    try:
        inventory = json.loads(inventory_abs.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise PlanError(f"cannot read current inventory: {exc}") from exc
    if not isinstance(inventory, dict) or type(inventory.get("schema")) is not int \
            or inventory.get("schema") != 1 \
            or not isinstance(inventory.get("images"), list):
        raise PlanError("current inventory is not a schema 1 image list")
    if not COMMIT_RE.fullmatch(commit):
        raise PlanError("--commit must be a full lowercase Git commit SHA")
    try:
        target_commit = _resolve_commit(root, commit)
    except RuntimeError as exc:
        raise PlanError(f"cannot resolve release commit {commit!r}: {exc}") from exc
    try:
        _run_git(root, "check-ref-format", f"refs/tags/{tag}")
    except RuntimeError as exc:
        raise PlanError(f"--tag must be a valid Git tag name: {tag!r}") from exc
    if event not in ("tag_push", "dispatch"):
        raise PlanError(f"unsupported event {event!r}")

    records, previous_release, previous_policy, record_problem = _load_previous_record(
        previous_record_path)
    current_policy = inventory.get("policy_version")
    if type(current_policy) is not int:
        raise PlanError("inventory policy_version must be an integer")
    registry = inventory.get("registry")
    namespace = inventory.get("namespace")
    if not isinstance(registry, str) or not registry \
            or not isinstance(namespace, str) or not namespace:
        raise PlanError("inventory registry and namespace must be non-empty strings")
    inspect_call = inspect if inspect is not None else inspect_registry
    try:
        inventory_git_path = inventory_abs.relative_to(root).as_posix()
    except ValueError as exc:
        raise PlanError("inventory path must be inside the repository") from exc

    guard_errors = check_release_inputs.validate(root, inventory_rel)
    if guard_errors:
        details = "; ".join(violation.format() for violation in guard_errors)
        raise PlanError(f"release input guard rejected inventory/recipe: {details}")
    try:
        compose_files = check_release_inputs.release_compose_metadata(root, tag)
    except (OSError, ValueError) as exc:
        raise PlanError(f"cannot describe release Compose membership: {exc}") from exc

    planned: list[dict[str, Any]] = []
    for image in inventory["images"]:
        if not isinstance(image, dict) or not isinstance(image.get("image"), str):
            raise PlanError("current inventory contains a malformed image entry")
        name = image["image"]
        if not name:
            raise PlanError("current inventory contains an empty image name")
        if not isinstance(image.get("dockerfile"), str) \
                or not isinstance(image.get("target", ""), str):
            raise PlanError(f"{name}: dockerfile and target must be strings")
        platforms = image.get("platforms")
        if not isinstance(platforms, list) or not platforms \
                or not all(isinstance(platform, str) for platform in platforms) \
                or len(set(platforms)) != len(platforms):
            raise PlanError(f"{name}: platforms must be a non-empty unique string list")
        repository = f"{registry}/{namespace}/{name}"
        decision = "build"
        reason: str
        source_digest: str | None = None
        source_revision: str | None = None
        source_created: str | None = None
        source_release: str | None = None
        prior = records.get(name)

        if image.get("always_build") is True:
            reason = str(image.get("always_build_reason", "image is marked always_build"))
        elif override:
            reason = "rebuild_all override is in effect"
        elif event == "dispatch":
            reason = "workflow_dispatch runs never promote"
        elif record_problem:
            reason = record_problem
        elif previous_release is None:
            reason = "previous release tag is unavailable"
        elif prior is None:
            reason = "no per-image record in previous release"
        elif type(previous_policy) is not int or previous_policy != current_policy:
            reason = "previous release policy_version does not match inventory"
        else:
            problem = _validate_record_entry(
                prior, image, repository, current_policy)
            if problem:
                reason = problem
            else:
                try:
                    baseline = _resolve_commit(root, prior["build_revision"])
                    changed = _first_changed_input(
                        root, baseline, target_commit, image, inventory,
                        inventory_git_path)
                except (RuntimeError, ValueError, KeyError, TypeError) as exc:
                    changed = f"object identity unavailable ({exc})"
                if changed:
                    reason = f"reviewed input/control changed or unavailable: {changed}"
                else:
                    problem = _promote_failure(
                        inspect_call, prior, image, repository, previous_release)
                    if problem:
                        reason = problem
                    else:
                        decision = "promote"
                        reason = f"inputs unchanged since {prior['build_revision']}"
                        source_digest = prior["root_digest"]
                        source_revision = prior["build_revision"]
                        source_created = prior["build_created"]
                        source_release = prior["source_release"]

        planned.append({
            "image": name,
            "repository": repository,
            "dockerfile": image["dockerfile"],
            "target": image.get("target", ""),
            "platforms": image["platforms"],
            "decision": decision,
            "reason": reason,
            "source_digest": source_digest,
            "source_build_revision": source_revision,
            "source_build_created": source_created,
            "source_release": source_release,
            "always_build": bool(image.get("always_build", False)),
        })

    return {
        "release": tag,
        "commit": target_commit,
        "previous_release": previous_release,
        "override": bool(override),
        "policy_version": current_policy,
        "images": planned,
        "compose_files": compose_files,
    }


def _emit_github_output(plan: dict[str, Any], path: Path) -> None:
    images = plan["images"]
    matrix = {
        "include": [
            {
                "image": item["image"],
                "dockerfile": item["dockerfile"],
                "target": item["target"],
                "platforms": item["platforms"],
                "decision": item["decision"],
                "source_digest": item["source_digest"],
            }
            for item in images
            if item["image"] != "rangerdanger-frontend"
        ]
    }
    frontend_decision = next(
        (item["decision"] for item in images if item["image"] == "rangerdanger-frontend"),
        "build",
    )
    values = {
        "matrix": json.dumps(matrix, separators=(",", ":")),
        "promote_count": str(sum(item["decision"] == "promote" for item in images)),
        "build_count": str(sum(item["decision"] == "build" for item in images)),
        "frontend_decision": frontend_decision,
    }
    with path.open("a", encoding="utf-8") as output:
        for key, value in values.items():
            output.write(f"{key}={value}\n")


def main(argv: list[str] | None = None) -> int:
    arguments = list(sys.argv[1:] if argv is None else argv)
    if arguments and arguments[0] == "delta-candidates":
        return _delta_candidates_main(arguments[1:])
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--event", required=True, choices=("tag_push", "dispatch"))
    parser.add_argument("--override", action="store_true")
    parser.add_argument("--previous-record", type=Path)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args(arguments)
    try:
        plan = build_plan(
            root=Path.cwd(),
            tag=args.tag,
            commit=args.commit,
            event=args.event,
            override=args.override,
            previous_record_path=args.previous_record,
        )
        rendered = json.dumps(plan, indent=2) + "\n"
        if args.out:
            args.out.write_text(rendered, encoding="utf-8")
        else:
            sys.stdout.write(rendered)
        github_output = os.environ.get("GITHUB_OUTPUT")
        if github_output:
            _emit_github_output(plan, Path(github_output))
    except (PlanError, OSError, ValueError, RuntimeError) as exc:
        print(f"release image planner: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
