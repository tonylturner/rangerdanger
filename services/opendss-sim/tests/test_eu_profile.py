import json
import os
import subprocess
import sys

import pytest
from eu_profile import ProfileError, load_profile
from eu_support import PROCESS_DIR, SERVICE_DIR, profile_file


def test_package_profile_freezes_schema_devices_and_endpoints(tmp_path):
    config = json.loads((PROCESS_DIR / 'profile.json').read_text())
    assert config['schema'] == 3
    assert config['circuit_file'] == '/profiles/feeder.dss'
    assert [(d['id'], d['kind'], d['truth_url']) for d in config['devices']] == [
        ('feeder-breaker', 'breaker', 'http://10.50.50.30:8080'),
        ('rmu-1', 'rmu', 'http://10.50.50.31:8080'),
        ('rmu-2', 'rmu', 'http://10.50.50.32:8080'),
        ('oltc-1', 'oltc', 'http://10.50.50.33:8080'),
    ]
    result = load_profile(profile_file(tmp_path))
    assert (result.frequency_hz, result.hv_kv, result.mv_kv) == (50, 110, 20)
    assert len(result.elements) == 8
    assert not (PROCESS_DIR / 'reference-candidate.dss').exists()


def test_image_copies_every_eu_runtime_module():
    dockerfile = (SERVICE_DIR / 'Dockerfile').read_text()
    for module in SERVICE_DIR.glob('eu_*.py'):
        assert f'services/opendss-sim/{module.name}' in dockerfile


@pytest.mark.parametrize('mutation', [
    lambda c: c.update(schema=1), lambda c: c.update(schema=2),
    lambda c: c.update(schema=3.0),
    lambda c: c.update(primary_kv=20), lambda c: c.update(frequency_hz=60),
    lambda c: c.update(devices=c['devices'][:3]),
    lambda c: c['devices'][1].update(kind='breaker'),
    lambda c: c['devices'][1].update(id='feeder-breaker', kind='breaker'),
    lambda c: c['devices'][1].update(truth_url='https://host:8080'),
    lambda c: c['devices'][1].update(truth_url='http://host:8080/api/truth'),
    lambda c: c['devices'][1].update(truth_url='http://user@host:8080'),
    lambda c: c['devices'][1].update(truth_url='http://host:99999'),
    lambda c: c['elements'].update(f2='Line.Missing; Solve'),
    lambda c: c['elements'].update(f2=c['elements']['rmu-1']),
    lambda c: c.update(solve_interval_ms=999),
    lambda c: c.update(request_timeout_ms=1000),
    lambda c: c.update(truth_stale_after_ms=2000),
    lambda c: c.update(circuit_file='/does-not-exist.dss'),
    lambda c: c['profile'].update(enabled=1),
    lambda c: c['profile'].update(seed=True),
    lambda c: c['profile'].update(timezone='not/a/timezone'),
    lambda c: c['profile'].update(noise_sigma=-1),
    lambda c: c['profile'].update(noise_sigma=float('nan')),
    lambda c: c['profile'].update(noise_sigma=10**1000),
    lambda c: c['profile'].update(noise_tau_s=0),
    lambda c: c['profile'].update(source_tau_s=0),
    lambda c: c['profile'].update(source_pu_min=1.01),
    lambda c: c['profile'].update(source_pu_max=.99),
    lambda c: c['profile'].update(extra=True),
    lambda c: c['f2_protection'].update(pickup_a=0),
    lambda c: c['f2_protection'].update(delay_ms=-1),
    lambda c: c['f2_protection'].update(delay_ms=1.5),
    lambda c: c['f2_protection'].update(instantaneous_a=300),
    lambda c: c['f2_protection'].update(instantaneous_a=None),
    lambda c: c['f2_protection'].update(extra=True),
])
def test_invalid_profiles_rejected(tmp_path, mutation):
    path = profile_file(tmp_path)
    config = json.loads(path.read_text())
    mutation(config)
    path.write_text(json.dumps(config))
    with pytest.raises(ProfileError):
        load_profile(path)


def test_invalid_profile_fails_service_import(tmp_path):
    path = tmp_path / 'invalid.json'
    path.write_text('{"schema":1}')
    environment = dict(os.environ, OPENDSS_PROFILE=str(path))
    result = subprocess.run([sys.executable, '-c', 'import main'], cwd=SERVICE_DIR,
                            env=environment, capture_output=True, text=True)
    assert result.returncode != 0
    assert 'profile fields mismatch' in result.stderr
