#!/usr/bin/env python3
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: Apache-2.0
"""Count actual CC receive callbacks, not cache/journal entries (which dedupe)."""
import datetime
import re
import sys


def check(lines):
    frames = []
    for line in lines:
        match = re.search(r"^(\S+) cc rx ASDU type=\S+\((\d+)\) cot=\S+\((\d+)\)", line)
        if match:
            at, kind, cot = match.groups()
            frames.append((datetime.datetime.fromisoformat(at.replace("Z", "+00:00")), int(kind), int(cot)))
    for cot in (3, 11):
        count = sum(kind == 31 and cause == cot for _, kind, cause in frames)
        assert count == 2, f"expected exactly two breaker reports with COT {cot} (open, close), got {count}"
    cycles = [at for at, kind, cot in frames if kind == 13 and cot == 1]
    assert len(cycles) >= 3, f"expected at least three packed cyclic images, got {len(cycles)}"
    gaps = [(b - a).total_seconds() for a, b in zip(cycles, cycles[1:])]
    assert all(1.5 < gap < 3 for gap in gaps), f"duplicate or missing 2000 ms cyclic image: gaps={gaps}"
    print(f"PASS two spontaneous + two COT 11 reports; {len(cycles)} cyclic images, gaps={gaps}")


if __name__ == "__main__":
    check(sys.stdin.read().splitlines()[int(sys.argv[1]):])
