import io
import json
from urllib.error import HTTPError, URLError

import pytest
from eu_support import profile, truths
from eu_wire import TruthClient, TruthError, acknowledgement, validate_truth


@pytest.mark.parametrize('device_id', ['feeder-breaker', 'f2-breaker', 'rmu-1', 'rmu-2', 'oltc-1'])
def test_truth_exact_per_kind_shape(tmp_path, device_id):
    device = next(d for d in profile(tmp_path).devices if d.id == device_id)
    value = truths()[device_id]
    assert validate_truth(value, device) == value
    assert validate_truth(value, device) is not value
    for field in value:
        bad = dict(value)
        del bad[field]
        with pytest.raises(TruthError):
            validate_truth(bad, device)
    with pytest.raises(TruthError):
        validate_truth(dict(value, unknown=False), device)
    for field, item in value.items():
        if type(item) is bool:
            with pytest.raises(TruthError):
                validate_truth(dict(value, **{field: 1}), device)


@pytest.mark.parametrize('field,value', [
    ('schema', 1), ('schema', 2), ('schema', 3.0), ('device_id', 'breaker-1'), ('kind', 'rmu'),
    ('generation', -1), ('generation', 2**32), ('generation', True),
    ('revision', 2**64), ('revision', 1.5), ('event_seq', None),
    ('changed_at', '2026-10-15'), ('changed_at', '2026-10-15T12:00:00+00:00'),
    ('changed_at', '2026-99-15T12:00:00Z'), ('changed_at', '20261015T120000Z'),
    ('position', 'intermediate'), ('position', None), ('remote_enabled', 'true'),
])
def test_truth_rejects_invalid_types_id_ranges_and_time(tmp_path, field, value):
    device = profile(tmp_path).devices[0]
    with pytest.raises(TruthError):
        validate_truth(dict(truths()[device.id], **{field: value}), device)


@pytest.mark.parametrize('changes', [
    dict(tap=-9), dict(tap=9), dict(tap=True), dict(target_tap=1),
    dict(tap=float('nan')), dict(moving=True, target_tap=2),
    dict(moving=True, target_tap=0), dict(auto=1), dict(armed=None),
])
def test_invalid_oltc_truth(tmp_path, changes):
    device = next(d for d in profile(tmp_path).devices if d.id == 'oltc-1')
    with pytest.raises(TruthError):
        validate_truth(dict(truths()[device.id], **changes), device)


@pytest.mark.parametrize('status,body,delivered,retry,revision', [
    (200, dict(accepted=True, solved_revision=7, revision=7), True, False, 7),
    (200, dict(accepted=True, solved_revision=7, revision=8), True, True, 8),
    (409, dict(accepted=False, reason='stale_truth'), False, True, None),
    (409, dict(accepted=False, reason='replay'), False, False, 7),
    (400, dict(accepted=False, reason='invalid'), False, False, 7),
    (200, dict(accepted=True, revision=7), False, False, 7),
    (200, dict(accepted=True, solved_revision=6, revision=7), False, False, 7),
    (200, dict(accepted=True, solved_revision=7, revision=6), False, False, 7),
    (200, dict(accepted=1, solved_revision=7, revision=7), False, False, 7),
    (200, dict(accepted=True, solved_revision=7, revision=True), False, False, 7),
    (200, dict(accepted=True, solved_revision=7.0, revision=7), False, False, 7),
    (409, dict(accepted=0, reason='stale_truth'), False, False, 7),
    (200, None, False, False, 7), (200, [], False, False, 7),
])
def test_ack_strict_two_revisions(status, body, delivered, retry, revision):
    record, reread = acknowledgement(status, body, 7)
    assert record == dict(delivered=delivered, solved_revision=7, revision=revision)
    assert reread is retry


class Response(io.BytesIO):
    status = 200


def test_http_client_paths_timeout_and_strict_truth(tmp_path):
    config = profile(tmp_path)
    device = config.devices[0]
    calls = []
    def opener(request, timeout):
        calls.append((request, timeout))
        return Response(json.dumps(truths()[device.id]).encode())
    client = TruthClient(config, opener)
    assert client.get_truth(device)['schema'] == 3
    request, timeout = calls[-1]
    assert request.full_url == device.truth_url + '/api/truth'
    assert request.method == 'GET'
    assert timeout == .5
    payload = dict(schema=3, revision=1)
    client.post_measurements(device, payload)
    request, _ = calls[-1]
    assert request.full_url == device.truth_url + '/api/measurements'
    assert request.method == 'POST'
    assert json.loads(request.data) == payload


@pytest.mark.parametrize('error', [URLError('down'), TimeoutError('deadline'),
                                   OSError('down')])
def test_client_network_loss_is_truth_error(tmp_path, error):
    config = profile(tmp_path)
    def opener(*args, **kwargs):
        raise error
    client = TruthClient(config, opener)
    with pytest.raises(TruthError):
        client.get_truth(config.devices[0])
    with pytest.raises(TruthError):
        client.post_measurements(config.devices[0], {})


def test_client_reads_http_conflict_body(tmp_path):
    config = profile(tmp_path)
    def opener(*args, **kwargs):
        raise HTTPError('http://host', 409, 'Conflict', {},
                        io.BytesIO(b'{"accepted":false,"reason":"stale_truth"}'))
    client = TruthClient(config, opener)
    assert client.post_measurements(config.devices[0], {}) == (
        409, dict(accepted=False, reason='stale_truth'))
    with pytest.raises(TruthError):
        client.get_truth(config.devices[0])
