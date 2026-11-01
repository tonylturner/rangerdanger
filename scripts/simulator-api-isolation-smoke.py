#!/usr/bin/env python3
"""G3 simulator API isolation for either package; offline inspection with --list."""
import argparse
import json
import os
from pathlib import Path
import signal
import sys
import uuid

from lib.simulator_api_isolation import Evidence, Gate, Transport, expand_matrix, load_inventory


def interrupted(signum, _frame):
    raise InterruptedError(f"signal {signum}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("package", choices=("us-dnp3-substation", "eu-iec104-substation"))
    parser.add_argument("--base-url", default="http://localhost:8088")
    parser.add_argument("--evidence-dir")
    parser.add_argument("--list", action="store_true", help="print tuples without Docker or HTTP")
    args = parser.parse_args()
    evidence = None
    try:
        if args.list:
            cat = load_inventory(args.package)
            print(json.dumps({p: expand_matrix(cat, args.package, p)
                              for p in ("weak", "improved")}, indent=2))
            return 0
        directory = args.evidence_dir
        if not directory:
            base = (Path(os.environ["EU_GATE_EVIDENCE_DIR"]) / "G3"
                    if args.package.startswith("eu-") and os.getenv("EU_GATE_EVIDENCE_DIR")
                    else Path("build/smoke-receipts") / ("g3-" + args.package))
            directory = base / uuid.uuid4().hex
        evidence = Evidence(directory, args.package)
        cat = load_inventory(args.package)
        signal.signal(signal.SIGTERM, interrupted)
        Gate(args.package, cat, Transport(args.base_url, evidence), evidence).run()
    except (Exception, KeyboardInterrupt) as exc:
        if evidence:
            evidence.event("exit-failure", error=str(exc))
        print(f"G3 FAIL: {exc}", file=sys.stderr)
        return 1
    print(f"G3 PASS: {args.package}; Weak restored; evidence: {directory}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
