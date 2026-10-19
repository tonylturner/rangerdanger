#!/usr/bin/env python3
"""Compare workshop results with the mounted YAML, not golden auto-pass totals."""
import argparse
import json
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
EXECUTED_TYPES = {"command", "sequence", "firewall", "check", "probe"}


def expected_table(directory):
    table = {}
    for path in sorted(directory.glob("*.yml")):
        doc = yaml.safe_load(path.read_text())
        scenario = doc["id"]
        if scenario in table:
            raise ValueError(f"duplicate YAML scenario {scenario}")
        steps = []
        for step in doc["steps"]:
            action = step.get("action")
            if action is None:
                kind = "manual"
            elif action["type"] == "decision":
                kind = "decision"
            elif action["type"] in EXECUTED_TYPES:
                kind = "executed"
            else:
                raise ValueError(f"{path}: unknown action {action['type']}")
            steps.append({"title": step["title"], "kind": kind})
        if not steps:
            raise ValueError(f"{path}: empty steps")
        table[scenario] = steps
    if not table:
        raise ValueError("no YAML scenarios")
    return table


def validate(response, table):
    if not isinstance(response, dict) or not isinstance(response.get("scenarios"), list):
        raise ValueError("expected JSON suite object with scenarios")
    scenarios = response["scenarios"]
    ids = [s["scenario_id"] for s in scenarios]
    if len(set(ids)) != len(ids) or set(ids) != set(table):
        raise ValueError(f"scenario inventory mismatch: expected {list(table)}, got {ids}")
    total = passed = failed = auto = reset_failures = 0
    lines = ["scenario | executed | manual | decision (manual/decision are NOT verified)"]
    for scenario in scenarios:
        sid = scenario["scenario_id"]
        steps = scenario["steps"]
        if not isinstance(steps, list) or len(steps) != len(table[sid]):
            raise ValueError(f"{sid}: step count mismatch")
        counts = {"executed": 0, "manual": 0, "decision": 0}
        scenario_passed = True
        for idx, (result, expected) in enumerate(zip(steps, table[sid])):
            if type(result.get("step_index")) is not int or result["step_index"] != idx:
                raise ValueError(f"{sid}: step index mismatch")
            if result.get("step_title") != expected["title"]:
                raise ValueError(f"{sid} step {idx}: title mismatch")
            expected_auto = expected["kind"] != "executed"
            if type(result.get("auto_pass")) is not bool or result["auto_pass"] != expected_auto:
                raise ValueError(f"{sid} step {idx}: auto_pass does not match YAML")
            if type(result.get("passed")) is not bool:
                raise ValueError(f"{sid} step {idx}: invalid passed")
            counts[expected["kind"]] += 1
            total += 1
            auto += int(expected_auto)
            passed += int(result["passed"])
            failed += int(not result["passed"])
            scenario_passed = scenario_passed and result["passed"]
        if type(scenario.get("reset_ok")) is not bool:
            raise ValueError(f"{sid}: invalid reset_ok")
        reset_failures += int(not scenario["reset_ok"])
        expected_passed = scenario_passed and scenario["reset_ok"]
        if type(scenario.get("passed")) is not bool or scenario["passed"] != expected_passed:
            raise ValueError(f"{sid}: inconsistent scenario verdict")
        lines.append(f'{sid} | {counts["executed"]} | {counts["manual"]} | {counts["decision"]}')
    for field, actual in (("total_tests", total), ("passed", passed), ("failed", failed),
                          ("auto_passed", auto), ("reset_failures", reset_failures)):
        if type(response.get(field)) is not int or response[field] != actual:
            raise ValueError(f"{field}: reported {response.get(field)}, counted {actual}")
    if failed or reset_failures:
        raise ValueError(f"failed steps={failed}; reset failures={reset_failures}")
    lines.append(f"passed={passed}/{total}; auto-passed={auto} (not execution coverage)")
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenarios", type=Path, default=ROOT / "lab-definitions/scenarios")
    args = parser.parse_args()
    print(validate(json.load(sys.stdin), expected_table(args.scenarios)))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, yaml.YAMLError) as exc:
        print(f"suite accounting: {exc}", file=sys.stderr)
        sys.exit(1)
