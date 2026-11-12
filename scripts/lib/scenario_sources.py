"""Resolve a package's confined scenario directory for offline readers."""
import os
import re
from pathlib import Path

import yaml


def scenario_files(package_dir, definitions_root):
    package_dir, root = Path(package_dir), Path(definitions_root)
    manifest = yaml.safe_load((package_dir / "package.yml").read_text())
    directory = manifest["scenarios"]
    if (not isinstance(directory, str) or not directory.strip()
            or Path(directory).is_absolute() or "\\" in directory
            or re.match(r"^[A-Za-z]:", directory)):
        raise ValueError("invalid relative scenario directory")

    def confined(path):
        if not path.absolute().is_relative_to(root.absolute()) or not path.resolve(strict=True).is_relative_to(root.resolve(strict=True)):
            raise ValueError(f"scenario path {path} escapes definitions root")
        return path

    # Normalize lexical '..' before checking containment.
    path = confined(Path(os.path.abspath(package_dir / directory)))
    if not path.is_dir():
        raise ValueError("scenarios must be a directory")
    files, owners = [], set()
    for file in sorted(path.glob("*.yml")):
        confined(file)
        if not file.is_file():
            raise ValueError("scenario must be a regular file")
        doc = yaml.safe_load(file.read_text())
        sid = doc["id"]
        if sid in owners:
            raise ValueError(f"duplicate scenario id {sid}")
        owners.add(sid)
        files.append(file)
    return files


def read_scenarios(package_dir, definitions_root):
    return {doc["id"]: doc for file in scenario_files(package_dir, definitions_root)
            for doc in [yaml.safe_load(file.read_text())]}
