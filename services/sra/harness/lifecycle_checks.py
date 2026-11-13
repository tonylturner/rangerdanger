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
    print('PASS: recordings nonempty and readable as web runtime UID 1001', sessions, flush=True)


def check_lifecycle(r, containers, images, enrollment):
    guacd = containers['guacd']
    assert docker('exec', containers['postgres'], 'psql', '-U', 'guacamole', '-d', 'guacamole', '-Atqc', 'SELECT count(*) FROM guacamole_connection_history WHERE end_date IS NULL') == '0'
    # Container-local markers survive ordinary restarts. Binds must not be wiped.
    marker = docker('exec', containers['ziti-controller'], 'stat', '-c', '%Y', '/var/lib/sra/seed-complete')
    jwt = docker('exec', containers['ziti-controller'], 'sha256sum', '/run/sra/enrollment/router.jwt', '/run/sra/enrollment/ot-host.jwt')
    before = docker('exec', guacd, 'find', '/recordings', '-type', 'f')
    for name in ('ziti-controller', 'ziti-router', 'ziti-host', 'postgres', 'keycloak', 'guacd', 'guacamole'):
        docker('restart', '-t', '10', containers[name], timeout=30)
        r.healthy(containers[name], 60)
    assert marker == docker('exec', containers['ziti-controller'], 'stat', '-c', '%Y', '/var/lib/sra/seed-complete')
    assert jwt == docker('exec', containers['ziti-controller'], 'sha256sum', '/run/sra/enrollment/router.jwt', '/run/sra/enrollment/ot-host.jwt')
    assert before == docker('exec', guacd, 'find', '/recordings', '-type', 'f')
    for name in ('ziti-controller', 'guacd'):
        mounts = []
        info = json.loads(docker('inspect', containers[name]))[0]
        for mount in info['Mounts']:
            if mount['Destination'] in ('/recordings', '/run/sra/enrollment'):
                mounts += ['-v', mount['Name'] + ':' + mount['Destination']]
        bad = r.start('interrupted-' + name, images[name], '--network', 'none', *mounts,
                      '--entrypoint', '/bin/sh', command=('-ec', 'touch /var/lib/sra/cleanup-complete; exec /opt/sra/' + name + '/entrypoint.sh'))
        wait_exit(bad)
    assert before == docker('exec', guacd, 'find', '/recordings', '-type', 'f')
    isolated = r.start('isolated-host', images['ziti-host'], '--network', 'none', '--memory', '128m', '-v', enrollment + ':/run/sra/enrollment:ro')
    r.healthy(isolated, 15)
    first = docker('exec', isolated, 'cat', '/var/lib/sra/attempt-time')
    time.sleep(23)
    r.healthy(isolated, 5)
    assert first != docker('exec', isolated, 'cat', '/var/lib/sra/attempt-time')
    assert docker('exec', isolated, 'cat', '/var/lib/sra/phase') == 'enrolling'
    ctrl = containers['ziti-controller']
    docker('pause', ctrl)
    try:
        time.sleep(7)
        r.healthy(containers['ziti-host'], 5)
    finally:
        docker('unpause', ctrl)
    print('PASS: first-boot interruption, plain restart, bind retention, offline enrollment retry', flush=True)


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
