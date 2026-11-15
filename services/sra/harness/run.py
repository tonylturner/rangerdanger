#!/usr/bin/env python3
"""Run all acceptance items once; print timed PASS/FAIL lines and clean up.

python3 services/sra/harness/run.py
Native arm64 integration; add --amd64 when Dockerfiles change.
No range Compose, host ports or host UI. Any failed item makes exit status 1.
"""
import argparse
import sys
from resources import Resources, docker
from results import Results
from native import NAMES, run_native


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--amd64', action='store_true', help='also attempt one amd64 build per image after Dockerfile changes')
    args = parser.parse_args()
    if docker('info', '--format', '{{.Architecture}}') != 'aarch64':
        raise RuntimeError('native arm64 host required')
    from preflight import main as enrollment_regression
    results, resources = Results(), Resources()
    try:
        def regression():
            assert enrollment_regression() == 0
        results.run('enrollment-ownership-regression', regression)
        for name in (*NAMES, 'vendor'):
            results.run('arm64-build:' + name, lambda name=name:
                        resources.build(name, 'Dockerfile.vendor-jump' if name == 'vendor' else None))
        results.run('network', resources.network)
        results.run('enrollment-volume', lambda: resources.volume('enrollment'))
        results.run('recordings-volume', lambda: resources.volume('recordings'))
        run_native(results, resources)
        if args.amd64:
            for name in (*NAMES, 'vendor'):
                results.run('amd64-build:' + name, lambda name=name:
                            resources.build(name, 'Dockerfile.vendor-jump' if name == 'vendor' else None, 'amd64'))
    finally:
        results.run('cleanup', lambda: resources.__exit__(None, None, None))
    return results.finish()


if __name__ == '__main__':
    sys.exit(main())
