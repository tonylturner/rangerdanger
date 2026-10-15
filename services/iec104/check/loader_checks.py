#!/usr/bin/env python3
"""Strict loader regression tests, run without network in the test image."""

import copy
import json
import subprocess
import tempfile

with open("/check/points.test.json") as f:
    original = json.load(f)


def run(data, valid=False):
    with tempfile.NamedTemporaryFile(mode="w") as f:
        json.dump(data, f)
        f.flush()
        result = subprocess.run(
            [
                "iec104-cc",
                "--config",
                f.name,
                "--bind",
                "192.0.2.1",
                "--target",
                "127.0.0.1:2404",
                "--http",
                "127.0.0.1:8080",
            ],
            check=False,
            capture_output=True,
            text=True,
        )
    output = result.stdout + result.stderr
    assert result.returncode == 1, output
    assert ("fatal: --bind" if valid else "config error:") in output, output


cases = [
    lambda d: d.update(schema=1),
    lambda d: d["points"][0]["source"].update(address=11),
    lambda d: d["points"][0]["source"].update(table="holding"),
    lambda d: d["points"][2]["source"].update(address=10),
    lambda d: d["points"][0]["source"].update(format="u16"),
    lambda d: d["points"][2]["source"].update(format="bogus"),
    lambda d: d["commands"][0].update(feedback="feeder-current"),
    lambda d: d["commands"][0].update(feedback="rmu-1-position"),
    lambda d: d["devices"][4]["heartbeat"].update(address=1030),
    lambda d: d["points"][18]["source"]["transient"].update(address=15),
    lambda d: d["points"][1].update(ioa=1001),
    lambda d: d["devices"][1].update(id="feeder-breaker"),
    lambda d: d["commands"][1].update(id="feeder-breaker-command"),
    lambda d: d["points"][0].update(asdu="M_ST_TB_1"),
    lambda d: d["points"][2]["source"].update(scale=0.1),
    lambda d: d["devices"][0]["measurements_valid"].update(address=11),
]
run(original, True)
signed = copy.deepcopy(original)
signed["points"][2]["source"].update(format="i16", scale=0.1)
run(signed, True)
for change in cases:
    data = copy.deepcopy(original)
    change(data)
    run(data)
print(f"PASS {len(cases) + 2} loader cases")
