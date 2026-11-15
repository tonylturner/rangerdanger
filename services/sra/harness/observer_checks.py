"""Student CLI authorization evidence from the disposable vendor desktop."""
import ipaddress
import json
import subprocess

from resources import ROOT, docker

PERMISSIONS = {
    'service.read', 'service-policy.read', 'identity.read',
    'edge-router-policy.read', 'service-edge-router-policy.read',
}


def cli(vendor, *args, refused=False):
    # The harness bypasses webtop init; use the same writable abc home as Chromium.
    command = ['docker', 'exec', '-u', 'abc', '-e', 'HOME=/tmp/sra-home', vendor,
               'ziti', 'edge', *args]
    result = subprocess.run(command, capture_output=True, text=True, timeout=20, cwd=ROOT)
    print('STUDENT ' + json.dumps(dict(command=command, exit_code=result.returncode,
                                     stdout=result.stdout, stderr=result.stderr)), flush=True)
    if refused:
        assert result.returncode == 1 and 'not authorized:' in result.stderr, result
        assert '401 Unauthorized' in result.stderr, result
    else:
        assert result.returncode == 0, result
    return result.stdout


def observer_identity(controller):
    data = json.loads(docker('exec', '-u', '2171', controller, 'ziti', 'edge', 'list',
                            'identities', 'name="sra-observer"', '-j'))['data']
    assert len(data) == 1, data
    identity = data[0]
    assert identity['isAdmin'] is False, identity
    assert set(identity['permissions']) == PERMISSIONS, identity
    assert not identity['roleAttributes'], identity
    return identity['id']


def read_and_refuse(vendor):
    cli(vendor, 'login', '172.31.250.40:1280', '-u', 'sra-observer', '-p', 'rangerdanger', '-y')
    policies = cli(vendor, 'list', 'service-policies')
    assert all(name in policies for name in ('sra-dial', 'sra-bind')), policies
    identities = cli(vendor, 'list', 'identities')
    assert all(name in identities for name in ('sra-observer', 'ot-host', 'sra-router')), identities
    routers = cli(vendor, 'list', 'edge-router-policies')
    assert 'sra-identities' in routers, routers
    advisor = cli(vendor, 'policy-advisor', 'services', 'rtac-ssh')
    assert 'OKAY : ot-host' in advisor and 'OKAY : sra-router' in advisor, advisor
    assert 'ERROR:' not in advisor, advisor
    assert 'sra-service-routers' in cli(vendor, 'list', 'service-edge-router-policies')
    before = json.loads(cli(vendor, 'list', 'service-policies', '-j'))['data']
    cli(vendor, 'delete', 'service-policy', 'sra-dial', refused=True)
    # A 401 alone might mean an expired session. Prove this same session still
    # reads and that the attempted mutation left both policies untouched.
    after = json.loads(cli(vendor, 'list', 'service-policies', '-j'))['data']
    assert before == after, (before, after)
    return 'observer reads and advisor succeed; refused write leaves session and policies intact'


def controller_listeners(sockets):
    external = []
    for item in sockets:
        address = ipaddress.ip_address(item['address'])
        address = getattr(address, 'ipv4_mapped', None) or address
        if not address.is_loopback:
            external.append(item)
    assert external and all(item['protocol'] == 'tcp' and item['port'] == 1280
                            for item in external), sockets
    return json.dumps(sockets, sort_keys=True)
