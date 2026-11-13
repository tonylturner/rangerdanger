#!/usr/bin/env python3
"""One-command disposable acceptance: python3 services/sra/harness/run.py.

Native arm64 integration followed by one amd64 build per image; no range
Compose, no host ports, no host UI. Every owned Docker resource is removed.
"""
import argparse
import json
import time
from pathlib import Path
from resources import Resources, docker, ROOT

NAMES = ('keycloak', 'guacamole', 'guacd', 'postgres', 'ziti-controller', 'ziti-router', 'ziti-host')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--native-only', action='store_true', help='omit the separately bounded amd64 builds')
    args = parser.parse_args()
    if docker('info', '--format', '{{.Architecture}}') != 'aarch64':
        raise RuntimeError('native arm64 host required')
    from preflight import main as enrollment_regression
    assert enrollment_regression() == 0
    with Resources() as r:
        images = {name: r.build(name) for name in NAMES}
        images['vendor'] = r.build('vendor', 'Dockerfile.vendor-jump')
        net = r.network()
        enrollment = r.volume('enrollment')
        recordings = r.volume('recordings')
        containers = {}
        cold = time.monotonic()

        def start(name, address, *extra):
            if name not in ('guacd', 'postgres'):
                extra = ('--cap-add', 'NET_ADMIN', '-e', 'GATEWAY=172.31.250.1', *extra)
            containers[name] = r.start(name, images[name], '--network', net, '--ip', address,
                                      *extra)
            return containers[name]

        start('keycloak', '172.31.250.10', '--memory', '1g', '-e', 'SRA_KEYCLOAK_ADDRESS=172.31.250.10', '-e', 'SRA_GUACAMOLE_ADDRESS=172.31.250.20')
        start('postgres', '172.31.250.31', '--memory', '256m', '-e', 'SRA_ROUTER_ADDRESS=172.31.250.50')
        ctrl = start('ziti-controller', '172.31.250.40', '--memory', '256m', '-e', 'SRA_CONTROLLER_ADDRESS=172.31.250.40', '-e', 'SRA_RTAC_ADDRESS=172.31.250.80', '-v', enrollment + ':/run/sra/enrollment')
        r.healthy(ctrl)
        router = start('ziti-router', '172.31.250.50', '--memory', '256m', '-e', 'SRA_CONTROLLER_ADDRESS=172.31.250.40', '-e', 'SRA_ROUTER_ADDRESS=172.31.250.50', '-v', enrollment + ':/run/sra/enrollment:ro')
        host = start('ziti-host', '172.31.250.60', '--memory', '128m', '-v', enrollment + ':/run/sra/enrollment:ro')
        r.healthy(router)
        start('guacd', '172.31.250.30', '--memory', '256m', '-v', recordings + ':/recordings')
        for name in ('keycloak', 'postgres', 'guacd', 'ziti-host'):
            r.healthy(containers[name])
        start('guacamole', '172.31.250.20', '--memory', '512m', '-e', 'SRA_GUACD_ADDRESS=172.31.250.30', '-e', 'SRA_POSTGRES_ADDRESS=172.31.250.31', '-e', 'SRA_KEYCLOAK_ADDRESS=172.31.250.10', '-e', 'SRA_GUACAMOLE_ADDRESS=172.31.250.20', '-v', recordings + ':/recordings:ro')
        r.healthy(containers['guacamole'])
        elapsed = round(time.monotonic() - cold, 2)
        print('ALL HEALTHY cold_seconds=' + str(elapsed), flush=True)
        assert elapsed <= 180, elapsed
        for container, certificate, expected in [
            (ctrl, '/ziti-controller/pki/intermediate/certs/server.chain.pem', '172.31.250.40'),
            (router, '/ziti-router/sra-router.server.chain.cert', '172.31.250.50'),
        ]:
            san = docker('exec', container, 'openssl', 'x509', '-in', certificate, '-noout', '-ext', 'subjectAltName')
            print(san, flush=True)
            assert 'IP Address:' + expected in san, san
        print(docker('exec', '-u', '2171', ctrl, 'ziti', 'edge', 'login', 'https://172.31.250.40:1280', '-u', 'admin', '-p', 'lab-ziti-password', '--ca', '/ziti-controller/pki/root/certs/root.cert'))
        target = r.start('target', images['vendor'], '--network', net, '--ip', '172.31.250.80', '--entrypoint', '/bin/bash', command=('-ec', 'useradd -m -s /bin/bash rangerdanger; echo rangerdanger:rangerdanger | chpasswd; ssh-keygen -A; mkdir -p /run/sshd; exec /usr/sbin/sshd -D -e'))
        vendor = r.start('vendor', images['vendor'], '--network', net, '--ip', '172.31.250.90', '--entrypoint', '/bin/sleep', command=('infinity',))
        docker('cp', str(ROOT / 'services/sra/keycloak/realm.json'), vendor + ':/tmp/realm.json')
        print(docker('exec', vendor, 'oathtool', '--version'))
        for name in ('cdp.py', 'browser_acceptance.py'):
            print(docker('cp', str(Path(__file__).with_name(name)), vendor + ':/tmp/' + name))
        print(docker('exec', '-u', 'abc', vendor, '/usr/bin/python3', '/tmp/browser_acceptance.py', 'session', timeout=180), flush=True)
        assert docker('exec', target, 'cat', '/tmp/rdsra-b-user') == 'rangerdanger'
        # Keep the vendor browser alive while restarting only the two reset targets.
        before = docker('exec', containers['guacd'], 'find', '/recordings', '-type', 'f')
        restart = time.monotonic()
        docker('restart', '-t', '10', containers['guacamole'], containers['guacd'], timeout=60)
        for name in ('guacamole', 'guacd'):
            r.healthy(containers[name], 60)
        reset_seconds = round(time.monotonic() - restart, 2)
        assert reset_seconds <= 60
        print('RESET healthy_seconds=' + str(reset_seconds), flush=True)
        assert before == docker('exec', containers['guacd'], 'find', '/recordings', '-type', 'f')
        from lifecycle_checks import check_recordings, inventory
        check_recordings(containers)
        print(json.dumps({'listeners': inventory(containers)}, indent=2), flush=True)
        print('AUDITOR GRANTS', docker('exec', containers['postgres'], 'psql', '-U', 'guacamole', '-d', 'guacamole', '-Atqc',
              "SELECT permission FROM guacamole_system_permission JOIN guacamole_entity USING(entity_id) WHERE name='auditor'"), flush=True)
        print(docker('exec', '-u', 'abc', vendor, '/usr/bin/python3', '/tmp/browser_acceptance.py', 'audit', timeout=180), flush=True)
        # Regression tests of first-boot markers and host startup with an unreachable controller.
        from lifecycle_checks import check_lifecycle
        check_lifecycle(r, containers, images, enrollment)
        if not args.native_only:
            for name in (*NAMES, 'vendor'):
                r.build(name, 'Dockerfile.vendor-jump' if name == 'vendor' else None, 'amd64')
        print('PASS: SRA acceptance', flush=True)


if __name__ == '__main__':
    main()
