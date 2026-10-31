"""One strict section reader for offline package-content contracts."""

import math
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
    if "budgetAdjustments" in merged:
        validate_budget_adjustments(merged)
    return merged


def validate_budget_adjustments(model: dict) -> None:
    """Mirror the budget schema and references; Go also checks scenario options."""
    adjustments = model["budgetAdjustments"]
    if not isinstance(adjustments, list):
        raise ValueError("budgetAdjustments must be a list")
    teams = {t["name"]: t["capacity_hours"] for t in model.get("teams", [])}
    actions = {a["id"] for a in model.get("catalog", [])}
    sources = {r["id"] for r in model.get("readiness", [])}
    seen = set()
    for a in adjustments:
        if not isinstance(a, dict) or set(a) != {"source", "verdict", "teamCaps", "effort"}:
            raise ValueError("Invalid budget adjustment fields")
        if not isinstance(a["source"], str) or not isinstance(a["verdict"], str):
            raise ValueError("Budget source/verdict must be strings")
        key = (a["source"], a["verdict"])
        if a["source"] not in sources or not a["verdict"] or key in seen:
            raise ValueError("Invalid or duplicate budget source/verdict")
        seen.add(key)
        if not isinstance(a["teamCaps"], dict) or not isinstance(a["effort"], list):
            raise ValueError("Budget collections must be explicit")
        if not a["teamCaps"] and not a["effort"]:
            raise ValueError("Budget adjustment must have an effect")
        for team, cap in a["teamCaps"].items():
            if team not in teams or type(cap) is not int or not 0 <= cap <= teams[team]:
                raise ValueError("Invalid budget team cap")
        for e in a["effort"]:
            if not isinstance(e, dict) or set(e) != {"actions", "multiplier", "extraHours"}:
                raise ValueError("Invalid budget effort fields")
            ids, factor, extra = e["actions"], e["multiplier"], e["extraHours"]
            if (not isinstance(ids, list) or not ids
                    or any(not isinstance(i, str) or i not in actions for i in ids)
                    or len(set(ids)) != len(ids)):
                raise ValueError("Invalid budget effort actions")
            if (type(factor) not in (int, float) or not math.isfinite(factor) or factor < 1
                    or type(extra) is not int or extra < 0 or (factor == 1 and extra == 0)):
                raise ValueError("Invalid budget effort arithmetic")
