"""Schema-3 EU fixtures shared by process tests (not runtime code)."""

import copy
import json
from pathlib import Path

from eu_profile import load_profile

SERVICE_DIR = Path(__file__).resolve().parents[1]
PROCESS_DIR = SERVICE_DIR.parents[1] / 'lab-definitions/packages/eu-iec104-substation/process'


def profile_file(tmp_path, urls=None):
    config = json.loads((PROCESS_DIR / 'profile.json').read_text())
    config['profile']['enabled'] = False
    config['circuit_file'] = str(PROCESS_DIR / 'feeder.dss')
    for device in config['devices']:
        if urls:
            device['truth_url'] = urls[device['id']]
    path = tmp_path / 'profile.json'
    path.write_text(json.dumps(config))
    return path


def profile(tmp_path):
    return load_profile(profile_file(tmp_path))


def truths():
    base = dict(schema=3, generation=98231, revision=1, event_seq=1,
                changed_at='2026-10-15T12:00:00.000Z', remote_enabled=True)
    return {
        'feeder-breaker': dict(base, device_id='feeder-breaker', kind='breaker',
                              position='closed', lockout=False, protection_tripped=False),
        'rmu-1': dict(base, device_id='rmu-1', kind='rmu', position='closed',
                      earth_closed=False, fault_passage=False),
        'rmu-2': dict(base, device_id='rmu-2', kind='rmu', position='open',
                      earth_closed=False, fault_passage=False),
        'oltc-1': dict(base, device_id='oltc-1', kind='oltc', tap=0, target_tap=0,
                       moving=False, auto=True, blocked=False, armed=False),
    }


class MemoryClient:
    def __init__(self):
        self.truths = truths()
        self.gets = []
        self.posts = []
        self.unreachable = set()
        self.fail_posts = set()
        self.responses = {}
        self.protect = False
        self.after_post = None

    def get_truth(self, device):
        self.gets.append(device.id)
        if device.id in self.unreachable:
            raise OSError('truth unavailable')
        return copy.deepcopy(self.truths[device.id])

    def post_measurements(self, device, payload):
        self.posts.append(copy.deepcopy(payload))
        if device.id in self.fail_posts:
            raise OSError('delivery unavailable')
        if self.responses.get(device.id):
            return self.responses[device.id].pop(0)
        truth = self.truths[device.id]
        revision = payload['revision']
        # Zero-delay peer: tests revision-changing acknowledgements, not field timing.
        if self.protect:
            if (device.kind == 'breaker' and truth['position'] == 'closed'
                    and payload['current_a'] >= 400):
                truth.update(position='open', lockout=True, protection_tripped=True)
                truth['revision'] += 1
            if (device.kind == 'rmu' and not truth['fault_passage']
                    and payload['current_a'] >= 300):
                truth['fault_passage'] = True
                truth['revision'] += 1
        if self.after_post:
            self.after_post(device, payload)
        return 200, dict(accepted=True, solved_revision=revision, revision=truth['revision'])
