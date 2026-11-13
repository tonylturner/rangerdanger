"""Native disposable chain, browser checks and independent lifecycle items."""
import json
from pathlib import Path
import time
from resources import docker, ROOT
from lifecycle_checks import (check_recordings, closed_history, snapshot, check_retained,
                              interrupted, offline_host, inventory)

NAMES = ('keycloak', 'guacamole', 'guacd', 'postgres', 'ziti-controller', 'ziti-router', 'ziti-host')


def run_native(results, resources):
    containers = {}

    def image(name):
        return results.require('arm64-build:' + name)

    def chain():
        net = results.require('network')
        enrollment = results.require('enrollment-volume')
        recordings = results.require('recordings-volume')
        cold = time.monotonic()

        def start(name, address, *extra):
            if name not in ('guacd', 'postgres'):
                extra = ('--cap-add', 'NET_ADMIN', '-e', 'GATEWAY=172.31.250.1', *extra)
            containers[name] = resources.start(name, image(name), '--network', net, '--ip', address, *extra)
            return containers[name]

        ctrl = start('ziti-controller', '172.31.250.40', '--memory', '256m',
                     '-e', 'SRA_CONTROLLER_ADDRESS=172.31.250.40', '-e', 'SRA_RTAC_ADDRESS=172.31.250.80',
                     '-v', enrollment + ':/run/sra/enrollment')
        start('keycloak', '172.31.250.10', '--memory', '1g',
              '-e', 'SRA_KEYCLOAK_ADDRESS=172.31.250.10', '-e', 'SRA_GUACAMOLE_ADDRESS=172.31.250.20')
        start('postgres', '172.31.250.31', '--memory', '256m', '-e', 'SRA_ROUTER_ADDRESS=172.31.250.50')
        resources.healthy(ctrl)
        router = start('ziti-router', '172.31.250.50', '--memory', '256m',
                       '-e', 'SRA_CONTROLLER_ADDRESS=172.31.250.40', '-e', 'SRA_ROUTER_ADDRESS=172.31.250.50',
                       '-v', enrollment + ':/run/sra/enrollment:ro')
        start('ziti-host', '172.31.250.60', '--memory', '128m', '-v', enrollment + ':/run/sra/enrollment:ro')
        resources.healthy(router)
        start('guacd', '172.31.250.30', '--memory', '256m', '-v', recordings + ':/recordings')
        for name in ('keycloak', 'postgres', 'guacd', 'ziti-host'):
            resources.healthy(containers[name])
        start('guacamole', '172.31.250.20', '--memory', '512m', '-e', 'SRA_GUACD_ADDRESS=172.31.250.30',
              '-e', 'SRA_POSTGRES_ADDRESS=172.31.250.31', '-e', 'SRA_KEYCLOAK_ADDRESS=172.31.250.10',
              '-e', 'SRA_GUACAMOLE_ADDRESS=172.31.250.20', '-v', recordings + ':/recordings:ro')
        resources.healthy(containers['guacamole'])
        elapsed = round(time.monotonic() - cold, 2)
        assert elapsed <= 180, elapsed
        return f'all seven healthy in {elapsed}s (limit 180s)'

    results.run('cold-start', chain)

    def certificate(name, path, address):
        san = docker('exec', containers[name], 'openssl', 'x509', '-in', path, '-noout', '-ext', 'subjectAltName')
        assert 'IP Address:' + address in san, san
        return san

    results.run('controller-IP-SAN', lambda: certificate('ziti-controller',
                '/ziti-controller/pki/intermediate/certs/server.chain.pem', '172.31.250.40'))
    results.run('router-IP-SAN', lambda: certificate('ziti-router',
                '/ziti-router/sra-router.server.chain.cert', '172.31.250.50'))
    results.run('verified-ziti-login', lambda: docker('exec', '-u', '2171', containers['ziti-controller'],
                'ziti', 'edge', 'login', 'https://172.31.250.40:1280', '-u', 'admin', '-p', 'lab-ziti-password',
                '--ca', '/ziti-controller/pki/root/certs/root.cert'))

    def desktops():
        net = results.require('network')
        target = resources.start('target', image('vendor'), '--network', net, '--ip', '172.31.250.80',
                                 '--entrypoint', '/bin/bash', command=('-ec',
                                 'useradd -m -s /bin/bash rangerdanger; echo rangerdanger:rangerdanger | chpasswd; '
                                 'ssh-keygen -A; mkdir -p /run/sshd; exec /usr/sbin/sshd -D -e'))
        vendor = resources.start('vendor', image('vendor'), '--network', net, '--ip', '172.31.250.90',
                                 '--entrypoint', '/bin/sleep', command=('infinity',))
        docker('cp', str(ROOT / 'services/sra/keycloak/realm.json'), vendor + ':/tmp/realm.json')
        for name in ('cdp.py', 'browser_acceptance.py'):
            docker('cp', str(Path(__file__).with_name(name)), vendor + ':/tmp/' + name)
        return vendor, target

    results.run('harness-desktops', desktops)

    def browser(mode):
        vendor, _ = results.require('harness-desktops')
        return docker('exec', '-u', 'abc', vendor, '/usr/bin/python3', '/tmp/browser_acceptance.py', mode, timeout=180)

    results.run('vendor-oathtool', lambda: docker('exec', results.require('harness-desktops')[0], 'oathtool', '--version'))
    for name, mode in [('chromium-abc-default-seccomp-URL', 'browser'), ('vendor-password-TOTP-SAML', 'vendor-login'),
                       ('vendor-exactly-one-connection', 'one-connection'), ('database-password-refused', 'password-refused')]:
        results.run(name, lambda mode=mode: browser(mode))

    def ssh():
        browser('ssh-session')
        assert docker('exec', results.require('harness-desktops')[1], 'cat', '/tmp/rdsra-b-user') == 'rangerdanger'

    results.run('brokered-SSH-command', ssh)
    results.run('recordings-web-readable', lambda: check_recordings(containers))

    def reset():
        before = snapshot(containers)
        started = time.monotonic()
        docker('restart', '-t', '10', containers['guacamole'], containers['guacd'], timeout=60)
        for name in ('guacamole', 'guacd'):
            resources.healthy(containers[name], 60)
        elapsed = time.monotonic() - started
        assert elapsed <= 60, elapsed
        check_retained(before, containers)
        return f'healthy in {elapsed:.2f}s; identities and recordings retained'

    results.run('reset-healthy-within-60s-and-retention', reset)
    results.run('reset-old-token-rejected', lambda: browser('token-rejected'))
    results.run('reset-no-active-history', lambda: closed_history(containers))
    results.run('auditor-history-no-connections', lambda: browser('audit-history'))
    results.run('auditor-playback', lambda: browser('playback'))

    # None of these items depends on playback authorization.
    results.run('pre-restart-state', lambda: snapshot(containers))
    for name in ('ziti-controller', 'ziti-router', 'ziti-host', 'postgres', 'keycloak', 'guacd', 'guacamole'):
        def restart(name=name):
            docker('restart', '-t', '10', containers[name], timeout=30)
            resources.healthy(containers[name], 60)
        results.run('plain-restart:' + name, restart)
    results.run('plain-restart-identities-and-recordings-retained',
                lambda: check_retained(results.require('pre-restart-state'), containers))
    for name in ('ziti-controller', 'guacd', 'ziti-router', 'keycloak', 'guacamole', 'postgres'):
        results.run('interrupted-marker:' + name,
                    lambda name=name: interrupted(resources, containers, {name: image(name)}, name))
    results.run('host-controller-unreachable-retry-and-recovery',
                lambda: offline_host(resources, containers, {'ziti-host': image('ziti-host')}, results.require('network')))
    results.run('listener-inventory', lambda: json.dumps(inventory(containers), sort_keys=True))
