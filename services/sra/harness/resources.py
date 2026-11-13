"""Disposable Docker resources, deliberately independent of repository Compose."""
import ipaddress
import json
from pathlib import Path
import subprocess
import time

ROOT = Path(__file__).resolve().parents[3]


def docker(*args, timeout=45):
    try:
        result = subprocess.run(['docker', *args], check=True, capture_output=True,
                                text=True, timeout=timeout, cwd=ROOT)
    except subprocess.CalledProcessError as exc:
        print(exc.stdout, exc.stderr, flush=True)
        raise
    return result.stdout.strip() if args[0] != 'logs' else result.stdout + result.stderr


class Resources:
    def __init__(self):
        self.containers = []
        self.volumes = []
        self.networks = []
        self.images = []
        self.timings = {}
        self.sizes = {}

    def build(self, name, recipe=None, arch='arm64'):
        tag = 'rdsra-b-' + name + '-' + arch
        # Never overwrite even another harness's in-progress output.
        if tag + ':latest' in docker('image', 'ls', '--format', '{{.Repository}}:{{.Tag}}').splitlines():
            raise RuntimeError('image already exists: ' + tag)
        start = time.monotonic()
        recipe = recipe or 'Dockerfile.sra-' + name
        self.images.append(tag)
        docker('buildx', 'build', '--platform', 'linux/' + arch, '--load',
               '-t', tag, '-f', recipe, '.', timeout=900)
        self.timings[tag] = round(time.monotonic() - start, 2)
        self.sizes[tag] = int(docker('image', 'inspect', tag, '--format', '{{.Size}}'))
        print(f'BUILD {tag}: {self.timings[tag]}s {self.sizes[tag]} bytes', flush=True)
        return tag

    def network(self):
        subnet = ipaddress.ip_network('172.31.250.0/24')
        ids = docker('network', 'ls', '-q').split()
        for item in json.loads(docker('network', 'inspect', *ids)) if ids else []:
            for config in item['IPAM'].get('Config') or []:
                if config.get('Subnet'):
                    existing = ipaddress.ip_network(config['Subnet'])
                    if existing.version == 4 and subnet.overlaps(existing):
                        raise RuntimeError(f'network overlaps {item["Name"]}: {existing}')
        name = 'rdsra-b-net'
        docker('network', 'create', '--internal', '--subnet', str(subnet), name)
        self.networks.append(name)
        return name

    def volume(self, suffix):
        name = 'rdsra-b-' + suffix
        if name in docker('volume', 'ls', '-q').split():
            raise RuntimeError('volume already exists: ' + name)
        docker('volume', 'create', name)
        self.volumes.append(name)
        return name

    def start(self, name, image, *args, command=()):
        name = 'rdsra-b-' + name
        # A create collision must never transfer ownership; a failed start must.
        docker('create', '--name', name, *args, image, *command)
        self.containers.append(name)
        docker('start', name)
        return name

    def healthy(self, name, limit=180):
        start = time.monotonic()
        while time.monotonic() - start < limit:
            state = json.loads(docker('inspect', name))[0]['State']
            if not state['Running']:
                raise RuntimeError(f'{name} exited: {state}\n{docker("logs", name)}')
            if state.get('Health', {}).get('Status') == 'healthy':
                duration = round(time.monotonic() - start, 2)
                print(f'HEALTHY {name}: {duration}s', flush=True)
                return duration
            time.sleep(1)
        raise TimeoutError(f'{name} did not become healthy: {docker("logs", name)}')

    def __enter__(self):
        return self

    def __exit__(self, kind, error, traceback):
        failures = []
        for name in reversed(self.containers):
            if kind:
                try:
                    print('LOGS ' + name + '\n' + docker('logs', '--tail', '50', name), flush=True)
                except subprocess.SubprocessError as exc:
                    print('LOGS unavailable for ' + name + ': ' + str(exc), flush=True)
            try:
                docker('rm', '-fv', name)
            except subprocess.SubprocessError as exc:
                failures.append(str(exc))
        for resource, names in [('volume', self.volumes), ('network', self.networks), ('image', self.images)]:
            for name in reversed(names):
                try:
                    docker(resource, 'rm', name)
                except subprocess.CalledProcessError as exc:
                    if resource != 'image' or 'No such image' not in exc.stderr:
                        failures.append(str(exc))
        print(json.dumps({'build_seconds': self.timings, 'image_bytes': self.sizes, 'cleanup_errors': failures}))
        if failures:
            raise RuntimeError('cleanup failed: ' + '; '.join(failures))
