#!/usr/bin/env python3
"""Check reviewed image input envelopes against the first-party Dockerfiles.

This deliberately reads only the small Dockerfile subset used by this
repository. It is not a general Dockerfile or Compose parser.
"""

from __future__ import annotations

import argparse
import fnmatch
import json
import re
import shlex
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


FIRST_PARTY_PREFIX = "ghcr.io/tonylturner/"
IMAGE_SUFFIX = "rangerdanger-"
ARCHIVE_SUFFIXES = (
    ".tar",
    ".tar.gz",
    ".tgz",
    ".tar.bz2",
    ".tbz2",
    ".tar.xz",
    ".txz",
    ".tar.zst",
)


@dataclass(frozen=True)
class Violation:
    path: str
    line: int
    image: str | None
    message: str

    def format(self) -> str:
        owner = self.image or "inventory"
        return f"{self.path}:{self.line}: {owner}: {self.message}"


@dataclass(frozen=True)
class Instruction:
    name: str
    value: str
    line: int


@dataclass
class Stage:
    index: int
    alias: str | None
    base: str
    line: int
    parents: set[int]
    copy_stages: set[int]
    local_sources: list[tuple[str, int]]
    recipe_issues: list[Violation]
    arg_definitions: set[str]
    run_env_values: list[tuple[str, int, str]]


def _read_instructions(path: Path) -> tuple[list[Instruction], list[Violation]]:
    """Join Dockerfile continuations while retaining the first physical line."""
    instructions: list[Instruction] = []
    violations: list[Violation] = []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        return [], [Violation(str(path), 1, None, f"cannot read Dockerfile: {exc}")]

    pending = ""
    start = 1
    for number, physical in enumerate(lines, start=1):
        stripped = physical.strip()
        if not pending and (not stripped or stripped.startswith("#")):
            continue
        if not pending:
            start = number
        continuation = stripped.endswith("\\")
        piece = stripped[:-1].rstrip() if continuation else stripped
        pending = f"{pending} {piece}".strip()
        if continuation:
            continue
        match = re.match(r"^([A-Za-z]+)\s+(.*)$", pending)
        if not match:
            violations.append(Violation(path.as_posix(), start, None,
                                        "unsupported Dockerfile instruction syntax"))
        else:
            instructions.append(Instruction(match.group(1).upper(),
                                            match.group(2).strip(), start))
        pending = ""
    if pending:
        violations.append(Violation(path.as_posix(), start, None,
                                    "unterminated Dockerfile continuation"))
    return instructions, violations


def _instruction_tokens(value: str) -> list[str]:
    """Tokenize the shell-like form; JSON-array COPY/ADD is handled separately."""
    if value.startswith("["):
        parsed = json.loads(value)
        if not isinstance(parsed, list) or not all(isinstance(item, str) for item in parsed):
            raise ValueError("JSON instruction must be an array of strings")
        return parsed
    return shlex.split(value, posix=True)


def _parse_copy(instruction: Instruction) -> tuple[list[str], str | None]:
    tokens = _instruction_tokens(instruction.value)
    from_stage: str | None = None
    sources: list[str] = []
    index = 0
    while index < len(tokens) and tokens[index].startswith("--"):
        option = tokens[index]
        if option.startswith("--from="):
            from_stage = option.partition("=")[2]
        elif option == "--from":
            index += 1
            if index >= len(tokens):
                raise ValueError("--from requires a value")
            from_stage = tokens[index]
        elif option.startswith("--"):
            # COPY/ADD options do not add context inputs; the source syntax
            # remains bounded. Unknown options are left to Docker's own parser.
            pass
        index += 1
    operands = tokens[index:]
    if len(operands) < 2:
        raise ValueError("COPY/ADD requires at least one source and a destination")
    sources = operands[:-1]
    return sources, from_stage


def _normal_source(source: str) -> str:
    source = source.replace("\\", "/")
    while source.startswith("./"):
        source = source[2:]
    return source.rstrip("/") or "."


def _valid_glob(pattern: str) -> bool:
    if pattern.count("*") != 1 or any(char in pattern for char in "?[]"):
        return False
    directory, _, name = pattern.rpartition("/")
    return bool(name) and "*" in name and "/" not in name and not directory.startswith("/")


def _input_covers(inputs: list[dict[str, Any]], source: str) -> bool:
    normalized = _normal_source(source)
    for entry in inputs:
        kind = entry.get("kind")
        declared = str(entry.get("path", "")).rstrip("/")
        if kind == "file" and normalized == declared:
            return True
        if kind == "dir":
            if declared == "." or normalized == declared \
                    or normalized.startswith(declared + "/"):
                return True
        if kind == "glob":
            directory, _, pattern = declared.rpartition("/")
            source_directory, _, source_name = normalized.rpartition("/")
            if source_directory == directory and fnmatch.fnmatchcase(source_name, pattern):
                return True
    return False


def _source_patterns_supported(
    instruction: Instruction,
    sources: list[str],
    path: str,
    image: str,
) -> list[Violation]:
    violations: list[Violation] = []
    for source in sources:
        if "$" in source:
            violations.append(Violation(
                path, instruction.line, image,
                f"{instruction.name} source {source!r} uses variable interpolation; "
                "express the input differently or mark the image always_build",
            ))
        if any(char in source for char in "?[]") or source.count("*") > 1:
            violations.append(Violation(
                path, instruction.line, image,
                f"{instruction.name} source pattern {source!r} is outside the "
                "file/dir/single-segment glob model; express the input differently "
                "or mark the image always_build",
            ))
        elif "*" in source and not _valid_glob(source):
            violations.append(Violation(
                path, instruction.line, image,
                f"{instruction.name} source pattern {source!r} is outside the "
                "file/dir/single-segment glob model; express the input differently "
                "or mark the image always_build",
            ))
    return violations


def _check_add_source(
    instruction: Instruction,
    sources: list[str],
    path: str,
    image: str,
) -> list[Violation]:
    violations: list[Violation] = []
    for source in sources:
        lowered = source.lower()
        is_url_or_git = (
            "://" in source
            or source.startswith("git@")
            or source.startswith("github.com:")
            or source.startswith("github.com/")
            or source.startswith("git+")
            or "#" in source
        )
        if is_url_or_git:
            violations.append(Violation(
                path, instruction.line, image,
                f"ADD source {source!r} is a URL or Git reference; express the input "
                "differently or mark the image always_build",
            ))
        elif lowered.endswith(ARCHIVE_SUFFIXES):
            violations.append(Violation(
                path, instruction.line, image,
                f"ADD archive source {source!r} is outside the input model; "
                "express the input differently or mark the image always_build",
            ))
    return violations


def _resolve_stages(
    instructions: list[Instruction],
    dockerfile: str,
    image: str,
) -> tuple[list[Stage], dict[str, int], list[Violation]]:
    stages: list[Stage] = []
    aliases: dict[str, int] = {}
    violations: list[Violation] = []
    for instruction in instructions:
        if instruction.name == "FROM":
            try:
                words = _instruction_tokens(instruction.value)
            except (ValueError, json.JSONDecodeError) as exc:
                violations.append(Violation(dockerfile, instruction.line, image,
                                            f"cannot parse FROM: {exc}"))
                continue
            words = [word for word in words if not word.startswith("--")]
            if not words:
                violations.append(Violation(dockerfile, instruction.line, image,
                                            "FROM has no base"))
                continue
            base = words[0]
            alias = None
            if len(words) >= 3 and words[1].upper() == "AS":
                alias = words[2]
            if base.isdecimal():
                violations.append(Violation(
                    dockerfile, instruction.line, image,
                    "numeric stage references are unsupported; express the input "
                    "differently or mark the image always_build",
                ))
            stage = Stage(len(stages), alias, base, instruction.line,
                          set(), set(), [], [], set(), [])
            if base.lower() in aliases:
                stage.parents.add(aliases[base.lower()])
            stages.append(stage)
            if alias:
                aliases[alias.lower()] = stage.index
            continue
        if not stages:
            continue
        stage = stages[-1]
        if instruction.name == "ONBUILD":
            stage.recipe_issues.append(Violation(
                dockerfile, instruction.line, image,
                "ONBUILD changes inherited recipe behavior; express the input "
                "differently or mark the image always_build",
            ))
        elif instruction.name == "ARG":
            stage.arg_definitions.add(instruction.value.split("=", 1)[0].strip())
        elif instruction.name in ("COPY", "ADD"):
            try:
                sources, from_stage = _parse_copy(instruction)
            except (ValueError, json.JSONDecodeError) as exc:
                stage.recipe_issues.append(Violation(
                    dockerfile, instruction.line, image,
                    f"cannot parse {instruction.name}: {exc}"))
                continue
            stage.recipe_issues.extend(_source_patterns_supported(
                instruction, sources, dockerfile, image))
            if instruction.name == "ADD":
                stage.recipe_issues.extend(_check_add_source(
                    instruction, sources, dockerfile, image))
            if from_stage is not None:
                ref = from_stage.lower()
                if from_stage.isdecimal():
                    stage.recipe_issues.append(Violation(
                        dockerfile, instruction.line, image,
                        "numeric stage references are unsupported; express the input "
                        "differently or mark the image always_build",
                    ))
                elif ref in aliases:
                    stage.copy_stages.add(aliases[ref])
                else:
                    stage.recipe_issues.append(Violation(
                        dockerfile, instruction.line, image,
                        f"unknown --from={from_stage!r}; named contexts and external "
                        "stage inputs are not bounded by this model; express the "
                        "input differently or mark the image always_build",
                    ))
            else:
                stage.local_sources.extend(
                    (_normal_source(source), instruction.line) for source in sources)
        elif instruction.name in ("ENV", "RUN"):
            stage.run_env_values.append(
                (instruction.name, instruction.line, instruction.value))
            if instruction.name == "RUN":
                for mount in re.findall(
                    r"(?:^|\s)--mount(?:=|\s+)([^\s]+)", instruction.value):
                    options = {
                        key.lower(): value
                        for part in mount.split(",")
                        for key, sep, value in [part.partition("=")]
                        if sep
                    }
                    if options.get("type", "").lower() == "bind" or "from" in options:
                        reason = ("type=bind" if options.get("type", "").lower() == "bind"
                                  else "from=")
                        stage.recipe_issues.append(Violation(
                            dockerfile, instruction.line, image,
                            f"RUN --mount with {reason} is not bounded; express the "
                            "input differently or mark the image always_build",
                        ))
    return stages, aliases, violations


def _reachable_stages(
    stages: list[Stage],
    aliases: dict[str, int],
    target: str,
    dockerfile: str,
    image: str,
) -> tuple[set[int], list[Violation]]:
    violations: list[Violation] = []
    if not stages:
        return set(), [Violation(dockerfile, 1, image, "Dockerfile has no FROM stage")]
    if not target:
        selected = len(stages) - 1
    elif target.lower() in aliases:
        selected = aliases[target.lower()]
    else:
        return set(), [Violation(
            dockerfile, 1, image, f"target {target!r} does not resolve to a Dockerfile stage")]
    reachable: set[int] = set()

    def visit(index: int) -> None:
        if index in reachable:
            return
        reachable.add(index)
        stage = stages[index]
        for parent in stage.parents | stage.copy_stages:
            visit(parent)

    visit(selected)
    return reachable, violations


def _compose_first_party_images(compose_path: Path) -> tuple[set[str], dict[str, int]]:
    """Read only service-level image fields; intentionally not a YAML parser."""
    services_indent: int | None = None
    service_indent: int | None = None
    image_indent: int | None = None
    current_service: str | None = None
    image_lines: dict[str, int] = {}
    try:
        lines = compose_path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return set(), {}
    for number, line in enumerate(lines, start=1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip(" "))
        text = line.strip()
        if text == "services:":
            services_indent = indent
            service_indent = None
            continue
        if services_indent is None:
            continue
        if indent <= services_indent:
            services_indent = None
            current_service = None
            continue
        if service_indent is None or indent <= service_indent:
            if text.endswith(":") and not text.startswith("-"):
                current_service = text[:-1].strip("'\"")
                service_indent = indent
                image_indent = None
                continue
        if current_service is not None and text.startswith("image:"):
            if image_indent is None:
                image_indent = indent
            if indent == image_indent:
                value = text.partition(":")[2].strip().strip("'\"")
                if value.startswith(FIRST_PARTY_PREFIX):
                    suffix = value[len(FIRST_PARTY_PREFIX):].split(":", 1)[0]
                    if suffix.startswith(IMAGE_SUFFIX):
                        image_lines[suffix] = number
    return set(image_lines), image_lines


def _named_context_violations(root: Path, images: list[dict[str, Any]]) -> list[Violation]:
    """Reject Buildx named contexts declared outside Dockerfile syntax."""
    workflow = root / ".github/workflows/release.yml"
    try:
        lines = workflow.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError):
        return []
    violations: list[Violation] = []
    for number, line in enumerate(lines, start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        context_match = re.search(
            r"(?:^|\s)context\s*:\s*['\"]?([^\s'\"#|]+)", stripped)
        uses_external_context = context_match and context_match.group(1) not in (".", "./")
        if uses_external_context or re.search(
            r"\bbuild-contexts\s*:|--build-context(?:\s|=)", stripped):
            for entry in images:
                image = entry.get("image")
                if isinstance(image, str):
                    violations.append(Violation(
                        ".github/workflows/release.yml", number, image,
                        "named or non-root Buildx contexts are not bounded; express "
                        "the input differently or mark the image always_build",
                    ))
    return violations


def _line_for_image(path: Path, image: str) -> int:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return 1
    for number, line in enumerate(lines, start=1):
        if f'"image": "{image}"' in line or f'"image":"{image}"' in line:
            return number
    return 1


def validate(root: Path, inventory_path: Path) -> list[Violation]:
    """Return every envelope, Dockerfile, target, and Compose inventory violation."""
    root = root.resolve()
    inventory_abs = inventory_path if inventory_path.is_absolute() else root / inventory_path
    try:
        raw = json.loads(inventory_abs.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        return [Violation(inventory_abs.as_posix(), 1, None,
                          f"cannot read inventory: {exc}")]

    violations: list[Violation] = []
    if not isinstance(raw, dict) or type(raw.get("schema")) is not int \
            or raw.get("schema") != 1:
        return [Violation(inventory_abs.as_posix(), 1, None,
                          "inventory must be a schema 1 JSON object")]
    images = raw.get("images")
    if not isinstance(images, list):
        return [Violation(inventory_abs.as_posix(), 1, None,
                          "inventory images must be an array")]
    if not isinstance(raw.get("registry"), str) or not raw["registry"] \
            or not isinstance(raw.get("namespace"), str) or not raw["namespace"]:
        violations.append(Violation(inventory_abs.as_posix(), 1, None,
                                    "registry and namespace must be non-empty strings"))
    if type(raw.get("policy_version")) is not int:
        violations.append(Violation(inventory_abs.as_posix(), 1, None,
                                    "policy_version must be an integer"))
    seen: set[str] = set()
    for entry in images:
        image = entry.get("image") if isinstance(entry, dict) else None
        if not isinstance(image, str) or not image:
            violations.append(Violation(inventory_abs.as_posix(), 1, None,
                                        "image entry must have a non-empty image name"))
            continue
        line = _line_for_image(inventory_abs, image)
        if image in seen:
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "duplicate image entry"))
        seen.add(image)
        platforms = entry.get("platforms")
        if not isinstance(platforms, list) or not platforms \
                or not all(isinstance(platform, str) for platform in platforms) \
                or len(set(platforms)) != len(platforms):
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "platforms must be a non-empty unique string list"))
        if type(entry.get("always_build")) is not bool:
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "always_build must be a boolean"))
        dockerfile_value = entry.get("dockerfile")
        if not isinstance(dockerfile_value, str) or not dockerfile_value:
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "dockerfile must be a non-empty path"))
            continue
        dockerfile_rel = Path(dockerfile_value)
        if dockerfile_rel.is_absolute() or ".." in dockerfile_rel.parts:
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        f"unsafe Dockerfile path {dockerfile_value!r}"))
            continue
        dockerfile_abs = root / dockerfile_rel
        try:
            dockerfile_abs.relative_to(root)
        except ValueError:
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "Dockerfile escapes repository root"))
            continue
        if not dockerfile_abs.is_file():
            violations.append(Violation(dockerfile_value, 1, image,
                                        "Dockerfile does not exist"))
            continue

        inputs = entry.get("inputs", [])
        if not isinstance(inputs, list):
            violations.append(Violation(inventory_abs.as_posix(), line, image,
                                        "inputs must be an array"))
            inputs = []
        for item in inputs:
            if not isinstance(item, dict) or item.get("kind") not in ("file", "dir", "glob"):
                violations.append(Violation(inventory_abs.as_posix(), line, image,
                                            f"invalid input entry {item!r}"))
                continue
            kind = item["kind"]
            value = item.get("path")
            if not isinstance(value, str) or not value or Path(value).is_absolute() \
                    or ".." in Path(value).parts:
                violations.append(Violation(inventory_abs.as_posix(), line, image,
                                            f"invalid input path {value!r}"))
                continue
            if kind == "glob":
                if not _valid_glob(value):
                    violations.append(Violation(
                        inventory_abs.as_posix(), line, image,
                        f"glob {value!r} must contain one * in one path segment"))
                    continue
                directory, _, pattern = value.rpartition("/")
                parent = root / directory if directory else root
                matches = list(parent.glob(pattern)) if parent.is_dir() else []
                if not matches:
                    violations.append(Violation(inventory_abs.as_posix(), line, image,
                                                f"glob input {value!r} has no matches"))
            else:
                target = root / value
                if kind == "file" and not target.is_file():
                    violations.append(Violation(inventory_abs.as_posix(), line, image,
                                                f"file input {value!r} does not exist"))
                elif kind == "dir" and not target.is_dir():
                    violations.append(Violation(inventory_abs.as_posix(), line, image,
                                                f"directory input {value!r} does not exist"))

        instructions, parse_issues = _read_instructions(dockerfile_abs)
        violations.extend(Violation(v.path, v.line, image, v.message)
                          for v in parse_issues)
        stages, aliases, recipe_issues = _resolve_stages(
            instructions, dockerfile_value, image)
        violations.extend(recipe_issues)
        target = entry.get("target") or ""
        reachable, target_issues = _reachable_stages(
            stages, aliases, str(target), dockerfile_value, image)
        violations.extend(target_issues)
        if target_issues:
            continue
        for stage_index in sorted(reachable):
            stage = stages[stage_index]
            violations.extend(stage.recipe_issues)
            for source, source_line in stage.local_sources:
                if not _input_covers(inputs, source):
                    violations.append(Violation(
                        dockerfile_value, source_line, image,
                        f"uncovered local COPY/ADD source {source!r}; declare it in "
                        "inputs or mark the image always_build",
                    ))

        if not entry.get("always_build", False):
            reachable_args = set().union(
                *(stages[index].arg_definitions for index in reachable))
            for stage_index in reachable:
                stage = stages[stage_index]
                for instruction_name, source_line, value in stage.run_env_values:
                    used = {
                        name for name in reachable_args
                        if re.search(
                            rf"(?<!\\)\$(?:{re.escape(name)}(?![A-Za-z0-9_])|"
                            rf"\{{{re.escape(name)}(?:[:?+\-][^}}]*)?\}})",
                            value,
                        )
                    }
                    if used:
                        violations.append(Violation(
                            dockerfile_value, source_line, image,
                            f"{instruction_name} consumes ARG(s) {', '.join(sorted(used))}; "
                            "express the input differently or mark the image always_build",
                        ))

    violations.extend(_named_context_violations(
        root, [entry for entry in images if isinstance(entry, dict)]))
    compose_path = root / "docker-compose.release.yml"
    compose_images, compose_lines = _compose_first_party_images(compose_path)
    inventory_names = {
        entry.get("image") for entry in images if isinstance(entry, dict)
        and isinstance(entry.get("image"), str)
    }
    for image in sorted(compose_images - inventory_names):
        line = compose_lines.get(image, 1)
        violations.append(Violation("docker-compose.release.yml", line, image,
                                    "first-party Compose image is missing from inventory"))
    for image in sorted(inventory_names - compose_images):
        violations.append(Violation(
            inventory_abs.as_posix(), _line_for_image(inventory_abs, image), image,
            "inventory image is not a first-party image in docker-compose.release.yml"))
    return violations


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", default=".github/release-images.json")
    parser.add_argument("--root", default=".")
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()
    violations = validate(root, Path(args.inventory))
    if violations:
        for violation in violations:
            print(violation.format(), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
