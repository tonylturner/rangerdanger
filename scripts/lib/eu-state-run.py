#!/usr/bin/env python3
"""Bounded live worker lifecycle; all evidence survives failure, no operation retries."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
ORACLE = json.loads(Path(__file__).with_name('eu-state-oracle.json').read_text())
CASES = ['baseline'] + [r['id'] for r in ORACLE['rows'][:16] if r['id'] not in ORACLE['unsupported']] + ['s1-cycle', 'rmu-local', 'oltc']


def wait_owned(process, timeout):
    try:
        return process.wait(timeout=timeout)
    except (subprocess.TimeoutExpired, KeyboardInterrupt):
        # Only this invocation's process group; never shared Chrome/Docker processes.
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=2)
        return 124


def worker(case, directory, pending, timeout):
    directory.mkdir(parents=True)
    start = time.monotonic()
    with (directory / 'run.log').open('w') as log:
        process = subprocess.Popen(['bash', str(ROOT / 'scripts/eu-state-smoke.sh'), '--worker', case, str(directory), str(int(pending))], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        code = wait_owned(process, timeout)
    result = {'case': case, 'exit_code': code, 'seconds': round(time.monotonic() - start, 2), 'dom': 'PENDING lane A' if pending else 'enabled'}
    (directory / 'outcome.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result), flush=True)
    if code:
        print((directory / 'run.log').read_text()[-12000:], file=sys.stderr)
    return result


def preflight(pending):
    for tool in ['bash', 'curl', 'jq', 'python3', 'docker'] + ([] if pending else ['node']):
        if not shutil.which(tool):
            raise RuntimeError(f'missing host tool {tool}')
    api = os.environ.get('RANGERDANGER_API', 'http://127.0.0.1:8088').rstrip('/')
    with urllib.request.urlopen(api + '/api/range', timeout=5) as response:
        state = json.load(response)
    if state['package'] != 'eu-iec104-substation' or state['phase'] != 'ready':
        raise RuntimeError(f'refusing wrong/non-ready package: {state}')
    if not pending:
        process = subprocess.Popen(['node', str(ROOT / 'frontend/e2e/eu-state-dom.mjs'), '--preflight'], start_new_session=True)
        if wait_owned(process, 20):
            raise RuntimeError('system Chrome preflight failed or timed out')


def main():
    started = time.monotonic()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--list', action='store_true', help='no network, Docker or browser access')
    parser.add_argument('--case', action='append', default=[], help='repeatable case name, or switching for all reachable switching rows')
    parser.add_argument('--pending-dom', action='store_true', help='explicitly mark both DOM assertions pending lane A; never a full PASS')
    args = parser.parse_args()
    if args.list:
        for name in CASES:
            print(f'{name}: supported')
        for name, why in ORACLE['unsupported'].items():
            print(f'{name}: UNSUPPORTED — {why}')
        return 0
    cases = []
    for name in args.case or CASES:
        expanded = [r for r in CASES if r.endswith(('-normal', '-tripped'))] if name == 'switching' else [name]
        for case in expanded:
            if case not in CASES:
                parser.error(f'unsupported/unknown case {case}: {ORACLE["unsupported"].get(case, "use --list")}')
            if case not in cases:
                cases.append(case)
    preflight(args.pending_dom)
    directory = ROOT / 'build/eu-state-smoke' / dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S.%fZ')
    directory.mkdir(parents=True)
    print(f'Evidence: {directory}', flush=True)
    # Pre-mutation export; reset also clears workshop captures/credentials and reapplies weak.
    api = os.environ.get('RANGERDANGER_API', 'http://127.0.0.1:8088').rstrip('/')
    for name in ['field', 'operator']:
        with urllib.request.urlopen(api + f'/api/{name}/state', timeout=5) as response:
            (directory / f'initial-{name}.json').write_bytes(response.read())
    results = []
    cleanup = None
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    try:
        for case in cases:
            # Reserve reset (110s) plus process-termination grace inside 30 min.
            remaining = 1800 - 120 - (time.monotonic() - started)
            if remaining <= 0:
                raise RuntimeError('whole-run deadline exhausted')
            result = worker(case, directory / case, args.pending_dom, min(300 if case == 'oltc' else 180, remaining))
            results.append(result)
            if result['exit_code']:
                break  # Unknown, timeout or assertion failure: no replay, no later commands.
    finally:
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        cleanup = worker('cleanup', directory / 'cleanup', args.pending_dom, 110)
        report = {'cases': results, 'cleanup': cleanup, 'seconds': round(time.monotonic() - started, 2)}
        (directory / 'outcomes.json').write_text(json.dumps(report, indent=2) + '\n')
        print(f'Runtime: {report["seconds"]}s; evidence: {directory}', flush=True)
    return 1 if len(results) != len(cases) or any(r['exit_code'] for r in results + [cleanup]) else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f'FAIL {error}', file=sys.stderr)
        sys.exit(1)
