#!/usr/bin/env python3
"""Package-aware, read-only TCP probes from each EU service's network namespace.

Reuse the running Kali image with --pull=never; no added networks, routes, tools
or privileged access. Exec/tool/JSON errors fail the gate rather than imply deny.
"""
import json
from pathlib import Path
import subprocess
import sys

SOCKET_PROBE = '''
import concurrent.futures,json,socket,sys

def probe(target):
    row=dict(target)
    try:
        with socket.create_connection((row['host'],row['port']),timeout=2):
            row['open']=True
    except (ConnectionRefusedError,TimeoutError):
        row['open']=False
    except OSError as error:
        row['error']=str(error)
    return row
with concurrent.futures.ThreadPoolExecutor(max_workers=32) as pool:
    print(json.dumps(list(pool.map(probe,json.loads(sys.argv[1])))))
'''


def targets(manifest, service, policy):
    firewall = next(s for s in manifest['services'] if s['key'] == 'firewall')
    rows = [dict(host=i['ipv4'], port=8081, expected=False)
            for i in firewall['interfaces']]
    # The weak management exposure is intentional; improved closes INPUT on
    # zone ingress even when a desktop addresses the firewall's management IP.
    if service['key'] == 'kali' or any(i['network'].startswith('desk_')
                                      for i in service['interfaces']):
        for host in ['10.10.10.2'] if service['key'] == 'kali' else ['10.20.20.2', '10.99.99.2']:
            rows += [dict(host=host, port=p, expected=policy == 'weak')
                     for p in (8080, 8443, 2222)]
    return rows


def check_rows(expected, actual):
    if len(expected) != len(actual):
        raise ValueError('incomplete probe results')
    for target, row in zip(expected, actual):
        if any(row.get(k) != v for k, v in target.items()):
            raise ValueError('probe identity mismatch')
        if type(row.get('open')) is not bool or row.get('error'):
            raise ValueError('probe execution failed: ' + repr(row))
    return [dict(row, passed=row['open'] == row['expected']) for row in actual]


def main(manifest_path, policy):
    manifest = json.loads(Path(manifest_path).read_text())
    if manifest['package'] != 'eu-iec104-substation' or policy not in ('weak', 'improved'):
        raise ValueError('EU management gate requires an EU package and named policy')
    kali = next(s for s in manifest['services'] if s['key'] == 'kali')
    image = subprocess.check_output(['docker', 'inspect', '-f', '{{.Image}}', kali['container']], text=True).strip()
    zone_networks = {n['key'] for n in manifest['networks'] if n.get('zone')}
    passed = failed = 0
    for service in manifest['services']:
        if service['key'] == 'firewall' or not any(i['network'] in zone_networks for i in service['interfaces']):
            continue
        expected = targets(manifest, service, policy)
        result = subprocess.run(['docker', 'run', '--rm', '--pull=never',
                                 '--network', 'container:' + service['container'],
                                 '--entrypoint', 'python3', image, '-c', SOCKET_PROBE,
                                 json.dumps(expected)], text=True, capture_output=True, timeout=25, check=True)
        for row in check_rows(expected, json.loads(result.stdout)):
            passed += row['passed']
            failed += not row['passed']
            print(json.dumps(dict(service=service['key'], policy=policy, **row)), flush=True)
    print(f'MANAGEMENT {policy}: {passed} PASS {failed} FAIL', flush=True)
    return bool(failed)


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
