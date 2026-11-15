#!/usr/bin/env python3
"""Regression: enrollment must be owned by the wrapper, not run-host.

Run: python3 services/sra/harness/preflight.py
The pinned run-host exits 0 without an identity. Both cases must retain that
observation: pre-enrollment health belongs to the wrapper's live retry loop.
"""
from __future__ import annotations

import json
import subprocess
import sys
import time

HOST_IMAGE = (
    'openziti/ziti-host:1.20.0@sha256:'
    'f0ef49efeb36413782b6d8642bb94dbb5a56812535d9d4b4f46d803658595b80'
)
OBSERVATION_SECONDS = 2


def docker(*args: str) -> str:
    result = subprocess.run(['docker', *args], check=True, capture_output=True,
                            text=True, timeout=180 if args[0] == 'create' else 20)
    return (result.stdout + (result.stderr if args[0] == 'logs' else '')).strip()


def command(case: str) -> list[str]:
    options = {
        'empty-directory': '-I /tmp/sra-empty',
        'missing-identity': '-i /tmp/sra-empty/ot-host.json',
    }
    return [
        'create', '--name', f'rdsra-b-preflight-{case}', '--network', 'none',
        '--entrypoint', '/bin/bash', HOST_IMAGE, '-ec',
        'mkdir /tmp/sra-empty; exec ziti-edge-tunnel run-host ' + options[case],
    ]


def satisfies_readiness(state: dict) -> bool:
    """Successful process exit is NOT readiness for a required running server."""
    return state['Running'] and state['Status'] == 'running'


def observe(case: str) -> dict:
    name = f'rdsra-b-preflight-{case}'
    # A collision aborts before the try: never delete a pre-existing container.
    docker(*command(case))
    try:
        started = time.monotonic()
        docker('start', name)
        time.sleep(OBSERVATION_SECONDS)
        state = json.loads(docker('inspect', name))[0]['State']
        return {
            'case': case,
            'observation_seconds': round(time.monotonic() - started, 3),
            'running': state['Running'],
            'container_exit_code': state['ExitCode'],
            'started_at': state['StartedAt'],
            'finished_at': state['FinishedAt'],
            'contract_satisfied': satisfies_readiness(state),
            'logs': docker('logs', name),
        }
    finally:
        docker('rm', '-fv', name)


def image_present() -> bool:
    try:
        docker('image', 'inspect', HOST_IMAGE)
    except subprocess.CalledProcessError:
        return False
    return True


def main() -> int:
    architecture = docker('info', '--format', '{{.Architecture}}')
    if architecture != 'aarch64':
        raise RuntimeError(f'run this preflight on the contracted native arm64 host, got {architecture}')
    owned_image = not image_present()
    try:
        results = [observe(case) for case in ('empty-directory', 'missing-identity')]
    finally:
        if owned_image and image_present():
            docker('image', 'rm', HOST_IMAGE)
    passed = all(not result['running'] and result['container_exit_code'] == 0 for result in results)
    print(json.dumps({'image': HOST_IMAGE, 'architecture': architecture,
                      'observations': results, 'enrollment_ownership_regression_passed': passed}, indent=2))
    if not passed:
        print('Upstream run-host pre-enrollment behavior changed; review wrapper ownership.', file=sys.stderr)
    return 0 if passed else 1


if __name__ == '__main__':
    sys.exit(main())
