import asyncio
import json
import threading
from contextlib import ExitStack, contextmanager
from dataclasses import replace
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest
from eu_app import create_eu_app
from eu_circuit import EUFeederSolver
from eu_controls import ProcessControls
from eu_profile import ProtectionSettings, load_profile
from eu_support import MemoryClient, profile, profile_file
from eu_truth_loop import TruthSolveLoop


def make_loop(tmp_path, **kwargs):
    config = profile(tmp_path)
    solver = EUFeederSolver(config, **kwargs.pop('solver_kwargs', {}))
    solver.compile_circuit()
    return TruthSolveLoop(config, solver, client=kwargs.pop('client', MemoryClient()), **kwargs)


def tick(loop):
    asyncio.run(loop.run_tick())
    return loop.state.electrical()


def test_one_vector_per_tick_per_device_measurements(tmp_path):
    loop = make_loop(tmp_path)
    state = tick(loop)
    assert state['schema'] == 3 and state['solve_seq'] == 1
    assert not state['truth_stale']
    assert len(loop.client.gets) == len(loop.client.posts) == 4
    for payload in loop.client.posts:
        assert set(payload) == {
            'schema', 'device_id', 'generation', 'revision', 'solver_epoch', 'solve_seq',
            'solved_at', 'current_a', 'voltage_kv_ll', 'source_kv_ll',
            'section_live', 'source_live', 'p_mw', 'q_mvar',
        }
        assert payload['schema'] == 3
        assert payload['generation'] == loop.client.truths[payload['device_id']]['generation']
        assert payload['revision'] == loop.client.truths[payload['device_id']]['revision']
        assert payload['solve_seq'] == state['solve_seq']
        assert payload['solved_at'] == state['solved_at']
        assert state['devices'][payload['device_id']]['delivered']
    assert loop.state.health()['status'] == 'ok'


def test_peer_io_is_concurrent_and_read_before_any_delivery(tmp_path):
    class ConcurrentClient(MemoryClient):
        def __init__(self):
            super().__init__()
            self.read_barrier = threading.Barrier(4)
            self.post_barrier = threading.Barrier(4)

        def get_truth(self, device):
            self.read_barrier.wait(timeout=2)
            return super().get_truth(device)

        def post_measurements(self, device, payload):
            assert len(self.gets) == 4
            self.post_barrier.wait(timeout=2)
            return super().post_measurements(device, payload)

    loop = make_loop(tmp_path, client=ConcurrentClient())
    assert all(d['delivered'] for d in tick(loop)['devices'].values())


@pytest.mark.parametrize('fault,rmu_fault', [('fault_s1', False), ('fault_s2', True)])
def test_protection_ack_rereads_all_and_resolves_once(tmp_path, fault, rmu_fault):
    client = MemoryClient()
    client.protect = True
    loop = make_loop(tmp_path, client=client)
    result = asyncio.run(loop.action(dict(action=fault)))
    assert loop.solve_seq == 2
    assert len(client.gets) == 8
    assert len(client.posts) == 8
    first = {p['device_id']: p for p in client.posts[:4]}
    second = {p['device_id']: p for p in client.posts[4:]}
    assert first['feeder-breaker']['current_a'] >= 400
    assert (first['rmu-1']['current_a'] >= 300) is rmu_fault
    assert client.truths['rmu-1']['fault_passage'] is rmu_fault
    assert client.truths['feeder-breaker']['lockout']
    assert second['feeder-breaker']['revision'] == 2
    assert second['feeder-breaker']['current_a'] < 1
    assert not result['buses']['s1']['live']
    assert result['buses']['s3']['live']
    for record in result['devices'].values():
        assert record['delivered'] and record['solved_revision'] == record['revision']


def test_409_stale_truth_rereads_full_vector_at_most_once(tmp_path):
    client = MemoryClient()
    client.responses['rmu-1'] = [(409, dict(accepted=False, reason='stale_truth'))] * 2
    loop = make_loop(tmp_path, client=client)
    state = tick(loop)
    assert loop.solve_seq == 2
    assert len(client.gets) == len(client.posts) == 8
    assert not state['devices']['rmu-1']['delivered']
    assert state['devices']['rmu-2']['delivered']
    assert loop.state.health()['status'] == 'degraded'


def test_revision_bump_on_both_passes_does_not_start_a_third_solve(tmp_path):
    client = MemoryClient()
    client.responses['oltc-1'] = [(200, dict(accepted=True, solved_revision=1, revision=2)),
                                  (200, dict(accepted=True, solved_revision=2, revision=3))]
    def change(device, payload):
        if device.id == 'rmu-2':
            client.truths['oltc-1']['revision'] = 2
    client.after_post = change
    loop = make_loop(tmp_path, client=client)
    state = tick(loop)
    assert loop.solve_seq == 2
    assert {k: state['devices']['oltc-1'][k] for k in
            ('delivered', 'solved_revision', 'revision')} == dict(
                delivered=True, solved_revision=2, revision=3)
    assert loop.state.health()['status'] == 'degraded'


@pytest.mark.parametrize('failure', ['transport', 'replay', 'invalid', 'malformed'])
def test_partial_delivery_does_not_hide_healthy_peers_or_retry(tmp_path, failure):
    client = MemoryClient()
    if failure == 'transport':
        client.fail_posts.add('rmu-2')
    else:
        client.responses['rmu-2'] = [
            (409, dict(accepted=False, reason='replay')) if failure == 'replay'
            else (400, dict(accepted=False, reason='invalid')) if failure == 'invalid'
            else (200, dict(accepted=True, revision=1))]
    loop = make_loop(tmp_path, client=client)
    state = tick(loop)
    assert loop.solve_seq == 1
    assert len(client.posts) == 4
    assert not state['devices']['rmu-2']['delivered']
    assert all(d['delivered'] for k, d in state['devices'].items() if k != 'rmu-2')
    assert all(b['kv'] > 0 for b in state['buses'].values())
    assert loop.state.health()['status'] == 'degraded'


def test_truth_loss_immediately_stale_retains_last_solve_and_recovers(tmp_path):
    now = [10.0]
    loop = make_loop(tmp_path, clock=lambda: now[0])
    last = tick(loop)
    loop.client.unreachable.add('rmu-1')
    stale = tick(loop)
    assert stale['truth_stale']
    for key in ('buses', 'tap', 'solve_seq', 'solved_at'):
        assert stale[key] == last[key]
    assert len(loop.client.posts) == 4
    now[0] += 4
    assert loop.state.truth_stale()
    loop.client.unreachable.clear()
    recovered = tick(loop)
    assert not recovered['truth_stale']
    assert recovered['solve_seq'] == 2


def test_no_assumed_positions_before_initial_truth(tmp_path):
    loop = make_loop(tmp_path)
    loop.client.unreachable.add('feeder-breaker')
    state = tick(loop)
    assert state['truth_stale']
    assert state['tap'] is None and state['solved_at'] is None
    assert all(b == dict(kv=None, live=None) for b in state['buses'].values())
    assert not loop.client.posts and loop.solve_seq == 0


def test_truth_loss_during_correction_does_not_solve_assumed_vector(tmp_path):
    loop = make_loop(tmp_path)
    loop.client.protect = True

    def lose_truth(device, payload):
        loop.client.unreachable.add('rmu-2')

    loop.client.after_post = lose_truth
    state = asyncio.run(loop.action(dict(action='fault_s1')))
    assert loop.solve_seq == 1
    assert len(loop.client.posts) == 4
    assert state['truth_stale']
    assert not any(d['delivered'] for d in state['devices'].values())


def test_malformed_truth_and_solver_failure_preserve_last_values(tmp_path):
    loop = make_loop(tmp_path)
    last = tick(loop)
    loop.client.truths['rmu-2']['schema'] = 1
    assert tick(loop)['truth_stale']
    assert len(loop.client.posts) == 4
    loop.client.truths['rmu-2']['schema'] = 3
    loop.solver.solve = lambda *_: dict(converged=False)
    state = tick(loop)
    assert state['buses'] == last['buses']
    assert state['solve_seq'] == last['solve_seq']
    assert not any(v['delivered'] for v in state['devices'].values())
    assert loop.state.health()['last_converged'] is False


def test_generation_restart_sequence_epoch_and_snapshot_copy(tmp_path):
    loop = make_loop(tmp_path)
    state = tick(loop)
    state['buses']['s1']['kv'] = -1
    assert loop.state.electrical()['buses']['s1']['kv'] > 0
    loop.client.truths['rmu-1'].update(generation=98300, revision=0)
    tick(loop)
    restart = next(p for p in loop.client.posts[4:] if p['device_id'] == 'rmu-1')
    assert restart['generation'] == 98300 and restart['revision'] == 0
    assert restart['solve_seq'] == 2
    second = make_loop(tmp_path)
    tick(second)
    assert loop.solver_epoch != second.solver_epoch
    assert second.client.posts[0]['solve_seq'] == 1


def test_f2_trip_reset_retrip_and_latched_until_reset(tmp_path):
    now = [1792270800.]
    loop = make_loop(tmp_path, solver_kwargs=dict(clock=lambda: now[0]))
    loop.client.protect = True
    loop.client.truths['feeder-breaker']['position'] = 'open'
    loop.client.truths['rmu-1']['position'] = 'open'
    loop.client.truths['rmu-2']['position'] = 'closed'
    state = asyncio.run(loop.action(dict(action='fault_s2')))
    assert not state['f2_tripped']  # First solve picks up, immediate FPI correction cannot trip.
    assert state['f2_protection']['picked_up']
    assert loop.client.truths['rmu-2']['fault_passage']
    now[0] += 1.001
    state = tick(loop)
    assert state['f2_tripped'] and not state['buses']['s3']['live']
    assert state['f2_protection']['last_trip_current_a'] >= 400
    assert not state['f2_protection']['picked_up']  # Post-open correction has no current.
    retrip = asyncio.run(loop.action(dict(action='reset_f2')))
    assert not retrip['f2_tripped']  # Reset starts a fresh pickup timer.
    now[0] = datetime.fromisoformat(retrip['solved_at'].replace('Z', '+00:00')).timestamp() + 1
    retrip = tick(loop)
    assert retrip['f2_tripped'] and not retrip['buses']['s3']['live']
    cleared = asyncio.run(loop.action(dict(action='clear_fault')))
    assert cleared['f2_tripped'] and not cleared['buses']['s3']['live']
    reset = asyncio.run(loop.action(dict(action='reset_f2')))
    assert not reset['f2_tripped']
    assert reset['buses']['s3']['live'] and reset['buses']['s2']['live']


def test_second_pass_f2_transition_uses_no_third_solve(tmp_path):
    # Both passes see overcurrent, one second apart; the second trips F2.
    times = iter([1792270800., 1792270800., 1792270801., 1792270801.,
                  1792270802., 1792270802.])
    loop = make_loop(tmp_path, solver_kwargs=dict(clock=lambda: next(times)))
    client = loop.client
    client.truths['feeder-breaker']['position'] = 'open'
    client.truths['rmu-1']['position'] = 'open'
    client.truths['rmu-2']['position'] = 'closed'
    client.responses['rmu-2'] = [(409, dict(accepted=False, reason='stale_truth'))]
    state = asyncio.run(loop.action(dict(action='fault_s2')))
    assert loop.solve_seq == 2
    assert loop.controls.f2_tripped and state['f2_tripped']
    assert loop.state.health()['status'] == 'degraded'
    settled = tick(loop)
    assert loop.solve_seq == 3 and not settled['buses']['s3']['live']
    assert loop.state.health()['status'] == 'ok'


@pytest.mark.parametrize('settings,fault,expected', [
    (ProtectionSettings(5, 0, 3000), 'none', True),
    (ProtectionSettings(10000, 1000, 20000), 'S2', False),
])
def test_f2_operates_from_amperes_not_requested_fault_or_topology(tmp_path, settings, fault, expected):
    config = replace(profile(tmp_path), f2_protection=settings)
    solver = EUFeederSolver(config)
    solver.compile_circuit()
    client = MemoryClient()
    client.truths['feeder-breaker']['position'] = 'open'
    client.truths['rmu-1']['position'] = 'open'
    client.truths['rmu-2']['position'] = 'closed'
    loop = TruthSolveLoop(config, solver, client=client)
    loop.controls.fault_section = fault
    state = tick(loop)
    assert state['f2_tripped'] is expected
    if expected:
        assert state['f2_protection']['last_trip_current_a'] >= settings.pickup_a
    else:
        assert not state['f2_protection']['picked_up']


async def invoke(app, method, path, body=None, raw=None):
    messages = []
    sent = False
    async def receive():
        nonlocal sent
        if not sent:
            sent = True
            return dict(type='http.request', body=raw if raw is not None else json.dumps(body).encode(), more_body=False)
        return dict(type='http.disconnect')
    async def send(message):
        messages.append(message)
    await app(dict(type='http', asgi=dict(version='3.0', spec_version='2.3'),
                   http_version='1.1', method=method, scheme='http', path=path,
                   raw_path=path.encode(), query_string=b'', headers=[],
                   client=('127.0.0.1', 1), server=('127.0.0.1', 2)), receive, send)
    status = next(m['status'] for m in messages if m['type'] == 'http.response.start')
    data = next(m['body'] for m in messages if m['type'] == 'http.response.body')
    return status, json.loads(data)


def app_with_memory(tmp_path):
    app = create_eu_app(str(profile_file(tmp_path)))
    app.state.eu_solver.compile_circuit()
    app.state.eu_loop.client = MemoryClient()
    return app


@pytest.mark.parametrize('action,value,field,expected', [
    ('fault_s1', None, 'fault_section', 'S1'), ('fault_s2', None, 'fault_section', 'S2'),
    ('clear_fault', None, 'fault_section', 'none'), ('reset_f2', None, 'f2_tripped', False),
    ('reset', None, 'load_multiplier', 1.0), ('set_load', .2, 'load_multiplier', .2),
    ('set_load', 3.0, 'load_multiplier', 3.0), ('set_source', .90, 'source_pu', .90),
    ('set_source', 1.10, 'source_pu', 1.10),
])
def test_process_action_api_each_action_and_boundaries(tmp_path, action, value, field, expected):
    app = app_with_memory(tmp_path)
    app.state.eu_loop.controls = ProcessControls(fault_section='S2', load_multiplier=2)
    body = dict(action=action)
    if value is not None:
        body['value'] = value
    status, state = asyncio.run(invoke(app, 'POST', '/api/process/actions', body))
    assert status == 200
    assert state[field] == expected
    assert state['schema'] == 3
    assert set(state) == {'schema', 'profile_id', 'solver_epoch', 'solve_seq', 'solved_at',
                          'truth_stale', 'fault_section', 'f2_tripped', 'load_multiplier',
                          'source_pu', 'tap', 'buses', 'devices', 'profile', 'f2_protection'}
    if action == 'reset':
        assert (state['fault_section'], state['f2_tripped'], state['source_pu']) == ('none', False, 1.0)


@pytest.mark.parametrize('body', [
    {}, [], None, dict(action='unknown'), dict(action='set_load'),
    dict(action='set_load', value=.1999), dict(action='set_load', value=3.001),
    dict(action='set_source', value=.8999), dict(action='set_source', value=1.1001),
    dict(action='set_load', value=True), dict(action='set_load', value='3'),
    dict(action='set_load', value=10**1000),
    dict(action='set_load', value=float('nan')), dict(action='set_load', value=float('inf')),
    dict(action='clear_fault', value=0), dict(action='reset', extra=True),
])
def test_invalid_process_actions_return_400_without_mutation(tmp_path, body):
    app = app_with_memory(tmp_path)
    before = app.state.eu_loop.controls.snapshot()
    status, _ = asyncio.run(invoke(app, 'POST', '/api/process/actions', body))
    assert status == 400
    assert app.state.eu_loop.controls.snapshot() == before
    assert app.state.eu_loop.solve_seq == 0


def test_unknown_before_truth_health_and_legacy_rejection(tmp_path):
    app = app_with_memory(tmp_path)
    status, state = asyncio.run(invoke(app, 'GET', '/api/electrical'))
    assert status == 200 and state['schema'] == 3 and state['truth_stale']
    assert state['tap'] is None
    status, health = asyncio.run(invoke(app, 'GET', '/api/health'))
    assert status == 200 and health['status'] == 'degraded'
    assert asyncio.run(invoke(app, 'POST', '/api/update-state'))[0] == 405
    assert asyncio.run(invoke(app, 'POST', '/api/process/actions', raw=b'{'))[0] == 400


def test_process_reset_does_not_assume_a_field_tap_reset(tmp_path):
    app = app_with_memory(tmp_path)
    app.state.eu_loop.client.truths['oltc-1'].update(tap=5, target_tap=5)
    status, state = asyncio.run(invoke(app, 'POST', '/api/process/actions',
                                     dict(action='reset')))
    assert status == 200 and state['tap'] == 5
    assert state['load_multiplier'] == state['source_pu'] == 1.0


def assert_schema3(state):
    assert set(state) == {
        'schema', 'profile_id', 'solver_epoch', 'solve_seq', 'solved_at', 'truth_stale',
        'fault_section', 'f2_tripped', 'load_multiplier', 'source_pu', 'tap', 'buses',
        'devices', 'profile', 'f2_protection'}
    assert state['schema'] == 3
    assert set(state['profile']) == {'enabled', 'load_factors', 'source_pu'}
    assert set(state['profile']['load_factors']) == {'residential', 'commercial', 'industrial'}
    assert set(state['f2_protection']) == {'picked_up', 'picked_up_since', 'last_trip_current_a'}
    for record in state['devices'].values():
        assert set(record) == {
            'delivered', 'solved_revision', 'revision', 'current_a', 'p_mw', 'q_mvar'}


def test_electrical_exact_keys_initial_and_solved_values_match_last_post(tmp_path):
    app = app_with_memory(tmp_path)
    _, initial = asyncio.run(invoke(app, 'GET', '/api/electrical'))
    assert_schema3(initial)
    assert initial['profile']['enabled'] is False
    assert initial['profile']['source_pu'] is None
    assert all(n is None for n in initial['profile']['load_factors'].values())
    assert initial['f2_protection'] == {
        'picked_up': False, 'picked_up_since': None, 'last_trip_current_a': None}
    assert all(record[k] is None for record in initial['devices'].values()
               for k in ('current_a', 'p_mw', 'q_mvar'))
    tick(app.state.eu_loop)
    _, solved = asyncio.run(invoke(app, 'GET', '/api/electrical'))
    assert_schema3(solved)
    for payload in app.state.eu_loop.client.posts:
        for key in ('current_a', 'p_mw', 'q_mvar'):
            assert solved['devices'][payload['device_id']][key] == payload[key]


def test_profile_action_disable_and_reset_preserve_deterministic_mode(tmp_path):
    app = app_with_memory(tmp_path)
    for enabled in (True, False):
        status, state = asyncio.run(invoke(
            app, 'POST', '/api/process/actions', dict(action='set_profile', enabled=enabled)))
        assert status == 200 and state['profile']['enabled'] is enabled
    status, state = asyncio.run(invoke(
        app, 'POST', '/api/process/actions', dict(action='reset')))
    assert status == 200
    assert state['profile'] == {
        'enabled': False, 'load_factors': dict.fromkeys(
            ('residential', 'commercial', 'industrial'), 1.0), 'source_pu': 1.0}


@pytest.mark.parametrize('body', [
    dict(action='set_profile'), dict(action='set_profile', enabled=1),
    dict(action='set_profile', enabled=None), dict(action='set_profile', enabled='false'),
    dict(action='set_profile', enabled=False, extra=True),
])
def test_profile_action_rejects_invalid_shape_without_mutation(tmp_path, body):
    app = app_with_memory(tmp_path)
    before = app.state.eu_loop.controls.profile_enabled
    status, _ = asyncio.run(invoke(app, 'POST', '/api/process/actions', body))
    assert status == 400 and app.state.eu_loop.controls.profile_enabled is before


@pytest.mark.parametrize('failure', ['truth', 'solve'])
def test_f2_timer_invalidated_when_truth_or_solve_unavailable(tmp_path, failure):
    now = [1792270800.]
    loop = make_loop(tmp_path, solver_kwargs=dict(clock=lambda: now[0]))
    loop.client.truths['feeder-breaker']['position'] = 'open'
    loop.client.truths['rmu-1']['position'] = 'open'
    loop.client.truths['rmu-2']['position'] = 'closed'
    picked = asyncio.run(loop.action(dict(action='fault_s2')))
    assert picked['f2_protection']['picked_up']
    original = loop.solver.solve
    if failure == 'truth':
        loop.client.unreachable.add('rmu-2')
    else:
        loop.solver.solve = lambda *_: dict(converged=False)
    failed = tick(loop)
    assert not failed['f2_protection']['picked_up']
    loop.client.unreachable.clear()
    loop.solver.solve = original
    now[0] += 2
    recovered = tick(loop)
    assert not recovered['f2_tripped']
    assert recovered['f2_protection']['picked_up_since'] == recovered['solved_at']


def test_lifespan_starts_autonomous_solver_and_stops_cleanly(tmp_path):
    app = create_eu_app(str(profile_file(tmp_path)))
    app.state.eu_loop.client = MemoryClient()

    async def exercise():
        async with app.router.lifespan_context(app):
            async def wait_for_delivery():
                while app.state.eu_state.health()['status'] != 'ok':
                    await asyncio.sleep(.001)
            await asyncio.wait_for(wait_for_delivery(), timeout=5)
            assert app.state.eu_loop.solve_seq == 1
            assert len(app.state.eu_loop.client.posts) == 4

    asyncio.run(exercise())


@contextmanager
def http_device(client, device_id):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            assert self.path == '/api/truth'
            self.respond(200, client.truths[device_id])
        def do_POST(self):
            assert self.path == '/api/measurements'
            payload = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            client.posts.append(payload)
            self.respond(200, dict(accepted=True, solved_revision=payload['revision'], revision=payload['revision']))
        def respond(self, status, body):
            encoded = json.dumps(body).encode()
            self.send_response(status)
            self.send_header('Content-Length', str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
        def log_message(self, *_):
            pass
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f'http://127.0.0.1:{server.server_port}'
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def test_real_http_four_independent_device_origins(tmp_path):
    client = MemoryClient()
    with ExitStack() as stack:
        urls = {k: stack.enter_context(http_device(client, k)) for k in client.truths}
        config = load_profile(profile_file(tmp_path, urls))
        solver = EUFeederSolver(config)
        solver.compile_circuit()
        loop = TruthSolveLoop(config, solver)
        state = tick(loop)
    assert len(client.posts) == 4
    assert all(v['delivered'] for v in state['devices'].values())
    assert loop.state.health()['status'] == 'ok'
