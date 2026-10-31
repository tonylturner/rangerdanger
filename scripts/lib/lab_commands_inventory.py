"""Offline package discovery and the shared description command grammar."""
import json
import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
US = "us-dnp3-substation"
GRAMMAR = json.loads((ROOT / "frontend/lib/command-grammar.json").read_text())
CMD_TOOL_RE = re.compile(GRAMMAR["command"])
RUN_RE = re.compile(GRAMMAR["run"])
CLI_RE = re.compile(GRAMMAR["cli"])
COPY_RE = re.compile(GRAMMAR["copy"])
MARKER_RE = re.compile(GRAMMAR["marker"], re.I)
FENCE_RE = re.compile(
    r"^:::(?:hint(?:\s+.+)?|guided|technical|decision\s+.+|"
    r"(?:findings-panel|plan-coverage|track-picker|generate-traffic-button|"
    r"validation-report|icon)(?:\s+.+)?)$")


def packages(root=ROOT):
    result = sorted(p.parent.name for p in
                    (Path(root) / "lab-definitions/packages").glob("*/package.yml"))
    if not result:
        raise ValueError("no package definitions found")
    return result


def package(root, package_id):
    base = Path(root) / "lab-definitions/packages" / package_id
    doc = yaml.safe_load((base / "package.yml").read_text())
    if doc["id"] != package_id:
        raise ValueError(f"{base}: package identity mismatch")
    topo = yaml.safe_load((base / doc["topology"]).read_text())
    manifest = json.loads((base / "manifest.json").read_text())
    if manifest["package"] != package_id:
        raise ValueError(f"{package_id}: manifest package identity mismatch")
    nodes = {s["node"]: s["container"] for s in manifest["services"] if s.get("node")}
    for node in topo["nodes"]:
        if nodes.get(node["id"]) != node["container"]:
            raise ValueError(f"{package_id}: topology/manifest mismatch: {node['id']}")
    curriculum = yaml.safe_load((base / doc["curriculum"]["dir"] / "nodes.yml").read_text())
    ips = {n["id"]: n.get("ip", "") for n in topo["nodes"]}
    for rule in curriculum["nodeInference"]:
        rule["ip"] = re.sub(r"\$\{ip:([^}]+)}", lambda m: ips[m[1]], rule["ip"])
    return base, doc, nodes, curriculum


def scenario_files(root=ROOT, package_id=US):
    base, doc, _, _ = package(root, package_id)
    files = sorted((base / doc["scenarios"]).glob("*.yml"))
    if not files:
        raise ValueError(f"{package_id}: no scenario YAML files")
    return files


def marker(text):
    sources = GRAMMAR["sources"]
    found = ""
    for match in MARKER_RE.finditer(text):
        found = sources.get(match[1].lower(), found)
    return found


def description_commands(text, inherited=""):
    """Mirror splitDescription fences; one shared vocabulary/source grammar.

    Return command, line, source, UI class. Hints/tracks recurse with their
    inherited source; ignored structured bodies must not create phantom Runs.
    """
    lines = text.split("\n")
    result, prose = [], []
    source, i = inherited, 0
    while i < len(lines):
        raw, trimmed = lines[i], lines[i].strip()
        if FENCE_RE.match(trimmed):
            source = marker("\n".join(prose)) or source
            prose = []
            start, opening = i + 1, trimmed
            i += 1
            while i < len(lines) and lines[i].strip() != ":::":
                i += 1
            if re.match(r"^:::(?:hint(?:\s|$)|guided$|technical$)", opening):
                for cmd, line, src, kind in description_commands(
                        "\n".join(lines[start:i]), source):
                    result.append((cmd, start + line, src, kind))
        elif re.match(r"^\s+\S", raw) and (
                CMD_TOOL_RE.match(trimmed) or CLI_RE.match(trimmed) or COPY_RE.match(trimmed)):
            source = marker("\n".join(prose)) or source
            prose = []
            command, line = trimmed, i
            kind = "cli" if CLI_RE.match(trimmed) else "copy" if COPY_RE.match(trimmed) or not RUN_RE.match(trimmed) else "exec"
            while command.endswith("\\") and i + 1 < len(lines) and CMD_TOOL_RE.match(trimmed):
                i += 1
                command = command[:-1].rstrip() + " " + lines[i].strip()
            result.append((command, line, source, kind))
        else:
            prose.append(raw)
        i += 1
    return result


def extract_step_commands(desc):
    return [(cmd, line) for cmd, line, _, _ in description_commands(desc)]


def infer_node_per_command(desc, line):
    return marker("\n".join(desc.splitlines()[:line]))


def extract(root=ROOT, target="", package_id=US):
    _, _, nodes, curriculum = package(root, package_id)
    rows, found = [], False
    for path in scenario_files(root, package_id):
        doc = yaml.safe_load(path.read_text())
        if not isinstance(doc, dict) or not isinstance(doc.get("steps"), list) or not doc.get("id"):
            raise ValueError(f"{path}: invalid scenario")
        scenario = doc["id"]
        if target and scenario != target:
            continue
        found = True
        default = curriculum["nodeMap"].get(doc["id"], {}).get("primary")
        default = default or (doc.get("nodes") or [""])[0]
        for idx, step in enumerate(doc["steps"], 1):
            desc = step.get("description") or ""
            inferred = next((r["node"] for r in curriculum["nodeInference"]
                             if r["ip"] in desc or re.search(r["pattern"], desc, re.I)), "")
            fallback = step.get("node") or inferred or default
            occurrences = {}
            for command, line, source, kind in description_commands(desc):
                occurrences[command] = occurrences.get(command, 0) + 1
                node = "fw-1" if kind == "cli" else source or fallback
                if node != "host" and node not in nodes:
                    raise ValueError(f"{package_id}/{scenario}/{step['id']}: unknown source {node}")
                preceding = "\n".join(desc.splitlines()[:line])
                entry = "shell" if node == "fw-1" and kind != "cli" and (
                    package_id != US or re.search(r"`(?:shell|bash|exit)`", preceding)) else ""
                prompt = "cli" if kind == "cli" or (node == "fw-1" and not entry) else "shell"
                skip = (kind == "copy" or node == "host" or
                        re.search(r"/api/firewall/apply|^docker |^ssh\b", command) or
                        re.match(r"^(?:import|delete|set|commit|confirm|vi)\b", command))
                rows.append(dict(
                    package=package_id, scenario=scenario, step=idx, step_id=step["id"],
                    title=step["title"], policy=step.get("expected_config") or "weak",
                    container=nodes.get(node, ""), node=node, command=command,
                    prompt=prompt, entry=entry if prompt == "shell" else "",
                    action="SKIP" if skip else "RUN", occurrence=occurrences[command]))
    if not found:
        raise ValueError(f"unknown scenario: {target}")
    return rows


def inventory(rows, root=ROOT, package_id=US):
    counts = {}
    for path in scenario_files(root, package_id):
        scenario = yaml.safe_load(path.read_text())["id"]
        if scenario in counts:
            raise ValueError(f"{package_id}: duplicate scenario {scenario}")
        counts[scenario] = {"RUN": 0, "SKIP": 0}
    for row in rows:
        counts[row["scenario"]][row["action"]] += 1
    return counts
