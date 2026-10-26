"""One strict section reader for offline package-content contracts."""

import re
from pathlib import Path

import yaml


SECTION_FILES = {
    "catalog.yml", "requirements.yml", "readiness.yml", "coverage.yml",
    "implementation.yml", "nodes.yml", "plan.yml",
}


class _UniqueLoader(yaml.SafeLoader):
    def construct_mapping(self, node, deep=False):
        keys = set()
        for key_node, _ in node.value:
            key = self.construct_object(key_node, deep=deep)
            if key in keys:
                raise ValueError(f"Duplicate curriculum key {key}")
            keys.add(key)
        return super().construct_mapping(node, deep=deep)


def read_curriculum(package_dir: Path) -> dict:
    manifest = yaml.safe_load((package_dir / "package.yml").read_text())
    directory = manifest["curriculum"]["dir"]
    if not re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*", directory):
        raise ValueError("Invalid curriculum directory")
    path = package_dir / directory
    if path.is_symlink() or not path.is_dir():
        raise ValueError("Curriculum must be a directory")
    files = sorted(path.iterdir())
    if not files:
        raise ValueError("Empty curriculum directory")
    merged = {}
    for file in files:
        if file.name not in SECTION_FILES or file.is_symlink() or not file.is_file():
            raise ValueError(f"Unknown curriculum file {file.name}")
        section = yaml.load(file.read_text(), Loader=_UniqueLoader)
        if not isinstance(section, dict) or not section:
            raise ValueError(f"Curriculum file {file.name} must contain a nonempty mapping")
        for key, value in section.items():
            if not isinstance(key, str):
                raise ValueError("Curriculum requires string keys")
            if key in merged:
                raise ValueError(f"Duplicate curriculum key {key}")
            merged[key] = value
    return merged
