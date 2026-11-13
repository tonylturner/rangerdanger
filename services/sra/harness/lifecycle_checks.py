"""Restart, first-boot interruption, enrollment liveness and listener evidence."""
import json
import socket
import time
from resources import docker


def wait_exit(name, limit=10):
    end = time.monotonic() + limit
    while time.monotonic() < end:
        state = json.loads(docker('inspect', name))[0]['State']
        if not state['Running']:
            assert state['ExitCode'] != 0, state
            return
        time.sleep(.2)
    raise TimeoutError('interrupted seed did not fail closed: ' + name)


def check_recordings(containers):
    guacd = containers['guacd']
    web = containers['guacamole']
    recordings = docker('exec', guacd, 'find', '/recordings', '-type', 'f').splitlines()
    sessions = [p for p in recordings if p.endswith('/session')]
    assert sessions, recordings
    for path in sessions:
        docker('exec', web, 'setpriv', '--reuid=1001', '--regid=1001', '--init-groups', 'sh', '-ec', 'test -s "$1"; cat "$1" >/dev/null', 'check', path)
    return 'recordings nonempty and readable as web UID 1001: ' + ', '.join(sessions)


def closed_history(containers):
    assert docker('exec', containers['postgres'], 'psql', '-U', 'guacamole', '-d', 'guacamole', '-Atqc', 'SELECT count(*) FROM guacamole_connection_history WHERE end_date IS NULL') == '0'


def snapshot(containers):
    files = {
        'ziti-controller': ['/run/sra/enrollment/router.jwt', '/run/sra/enrollment/ot-host.jwt',
                            '/ziti-controller/pki/root/certs/root.cert'],
        'ziti-router': ['/ziti-router/sra-router.cert', '/ziti-router/sra-router.key'],
        'ziti-host': ['/ziti-edge-tunnel/ot-host.json'],
        'keycloak': ['/opt/keycloak/data/import/sra-realm.json'],
    }
    result = {name: docker('exec', containers[name], 'sha256sum', *paths)
              for name, paths in files.items()}
    for name in ('ziti-controller', 'ziti-router', 'guacd', 'guacamole', 'postgres'):
        result[name + '-marker'] = docker('exec', containers[name], 'stat', '-c', '%Y', '/var/lib/sra/seed-complete')
    result['recordings'] = docker('exec', containers['guacd'], 'find', '/recordings', '-type', 'f')
    return result


def check_retained(before, containers):
    after = snapshot(containers)
    assert before == after, {name: (before.get(name), after.get(name))
                             for name in before if before.get(name) != after.get(name)}


def interrupted(r, containers, images, name):
    before = snapshot(containers)
    mounts = []
    info = json.loads(docker('inspect', containers[name]))[0]
    for mount in info['Mounts']:
        if mount['Destination'] in ('/recordings', '/run/sra/enrollment'):
            mounts += ['-v', mount['Name'] + ':' + mount['Destination']]
    bad = r.start('interrupted-' + name, images[name], '--network', 'none', *mounts,
                  '--entrypoint', '/bin/sh', command=('-ec', 'touch /var/lib/sra/cleanup-complete; exec /opt/sra/' + name + '/entrypoint.sh'))
    wait_exit(bad)
    assert 'interrupted initialization' in docker('logs', bad)
    check_retained(before, containers)


def offline_host(r, containers, images, net):
    ctrl = containers['ziti-controller']
    docker('exec', '-u', '2171', ctrl, 'ziti', 'edge', 'create', 'identity', 'retry-host',
           '--role-attributes', 'sra-hosts', '--jwt-output-file', '/run/sra/enrollment/retry-host.jwt')
    jwt = docker('exec', ctrl, 'cat', '/run/sra/enrollment/retry-host.jwt')
    volume = r.volume('retry-enrollment')
    helper = r.start('retry-seed', images['ziti-host'], '--network', 'none', '--user', '0',
                     '-v', volume + ':/run/sra/enrollment', '--entrypoint', '/bin/sleep', command=('infinity',))
    docker('exec', helper, '/bin/sh', '-ec',
           'printf %s "$1" > /run/sra/enrollment/ot-host.jwt; chmod 644 /run/sra/enrollment/ot-host.jwt',
           'seed', jwt)
    docker('pause', ctrl)
    try:
        isolated = r.start('retry-host', images['ziti-host'], '--network', net, '--ip', '172.31.250.61',
                           '--cap-add', 'NET_ADMIN', '-e', 'GATEWAY=172.31.250.1', '--memory', '128m',
                           '-v', volume + ':/run/sra/enrollment:ro')
        r.healthy(isolated, 15)
        first = docker('exec', isolated, 'cat', '/var/lib/sra/attempt-time')
        deadline = time.monotonic() + 60
        while docker('exec', isolated, 'cat', '/var/lib/sra/attempt-time') == first:
            assert time.monotonic() < deadline, 'no second enrollment attempt'
            state = json.loads(docker('inspect', isolated))[0]['State']
            assert state['Running'] and state['Health']['Status'] == 'healthy', state
            time.sleep(1)
        assert docker('exec', isolated, 'cat', '/var/lib/sra/phase') == 'enrolling'
        r.healthy(isolated, 5)
        r.healthy(containers['ziti-host'], 5)
    finally:
        docker('unpause', ctrl)
    deadline = time.monotonic() + 90
    while docker('exec', isolated, 'cat', '/var/lib/sra/phase') != 'running':
        assert time.monotonic() < deadline, 'did not enroll when controller returned'
        time.sleep(1)
    r.healthy(isolated, 10)
    docker('exec', isolated, 'test', '-s', '/ziti-edge-tunnel/ot-host.json')
    assert docker('exec', isolated, 'cat', '/proc/1/comm').startswith('ziti-edge-tunne')
    return 'healthy during two enrollment attempts; enrolled and execed run-host after controller returned'


def inventory(containers):
    output = {}
    # Read kernel tables and inode-to-process ownership, including images without ss.
    script = '''for table in tcp tcp6 udp udp6; do echo TABLE:$table; cat /proc/net/$table; done
for pid in /proc/[0-9]*; do
  test -r "$pid/comm" || continue
  echo PID:${pid##*/}:$(cat "$pid/comm")
  ls -l "$pid/fd" 2>/dev/null || :
done'''
    for name, container in containers.items():
        user = {'keycloak': '1000', 'guacamole': '1001', 'guacd': '1000',
                'postgres': 'postgres', 'ziti-controller': '2171',
                'ziti-router': '2171', 'ziti-host': '65534'}[name]
        text = docker('exec', '-u', user, container, 'sh', '-c', script)
        sockets, owners = {}, {}
        table, process = '', ''
        for line in text.splitlines():
            if line.startswith('TABLE:'):
                table = line.split(':')[1]
            elif line.startswith('PID:'):
                table = ''
                process = line[4:]
            elif table and len(line.split()) > 9 and ':' in line.split()[0]:
                fields = line.split()
                if table.startswith('tcp') and fields[3] != '0A':
                    continue
                address, port = fields[1].split(':')
                raw = bytes.fromhex(address)
                if table.endswith('6'):
                    raw = b''.join(raw[i:i + 4][::-1] for i in range(0, 16, 4))
                    address = socket.inet_ntop(socket.AF_INET6, raw)
                else:
                    address = socket.inet_ntop(socket.AF_INET, raw[::-1])
                sockets[fields[9]] = {'address': address, 'port': int(port, 16), 'protocol': table.rstrip('6')}
            elif 'socket:[' in line:
                inode = line.split('socket:[')[1].split(']')[0]
                owners.setdefault(inode, set()).add(process)
        output[name] = [dict(value, processes=sorted(owners.get(inode, []))) for inode, value in sockets.items()]
    return output
