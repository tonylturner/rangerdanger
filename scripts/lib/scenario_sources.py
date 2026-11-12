"""Resolve consumer-owned scenario sources for offline readers."""
import os
import re
from pathlib import Path

import yaml


def scenario_files(package_dir, definitions_root):
    package_dir, root = Path(package_dir), Path(definitions_root)
    manifest = yaml.safe_load((package_dir / "package.yml").read_text())
    sources = manifest["scenarios"]
    if not isinstance(sources, list) or not sources:
        raise ValueError("scenarios must be a nonempty source list")

    def confined(path):
        if not path.absolute().is_relative_to(root.absolute()) or not path.resolve(strict=True).is_relative_to(root.resolve(strict=True)):
            raise ValueError(f"scenario path {path} escapes definitions root")
        return path

    files, owners = [], set()
    for source in sources:
        if not isinstance(source, dict) or set(source) - {"dir", "include"}:
            raise ValueError("invalid scenario source")
        directory = source.get("dir")
        if (not isinstance(directory, str) or not directory.strip()
                or Path(directory).is_absolute() or "\\" in directory
                or re.match(r"^[A-Za-z]:", directory)):
            raise ValueError("invalid relative scenario directory")
        # Normalize lexical '..' before checking containment.
        path = confined(Path(os.path.abspath(package_dir / directory)))
        if not path.is_dir():
            raise ValueError("scenario source must be a directory")
        include = source.get("include")
        selected = "include" in source
        if selected and (not isinstance(include, list) or not include
                         or any(not isinstance(i, str) or not re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*", i) for i in include)
                         or len(set(include)) != len(include)):
            raise ValueError("invalid scenario include list")
        found = set()
        for file in sorted(path.glob("*.yml")):
            confined(file)
            if not file.is_file():
                raise ValueError("scenario must be a regular file")
            doc = yaml.safe_load(file.read_text())
            sid = doc["id"]
            if selected and sid not in include:
                continue
            if sid in owners:
                raise ValueError(f"duplicate scenario id {sid}")
            owners.add(sid)
            found.add(sid)
            files.append(file)
        if selected and set(include) != found:
            raise ValueError("included scenario must resolve exactly once")
    return files


def read_scenarios(package_dir, definitions_root):
    return {doc["id"]: doc for file in scenario_files(package_dir, definitions_root)
            for doc in [yaml.safe_load(file.read_text())]}
