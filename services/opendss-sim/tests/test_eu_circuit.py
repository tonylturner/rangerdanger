import importlib.util
import json

import pytest
from eu_circuit import EUFeederSolver, fault_paths
from eu_controls import ProcessControls
from eu_support import PROCESS_DIR, profile, truths


@pytest.fixture
def solver(tmp_path):
    value = EUFeederSolver(profile(tmp_path))
    value.compile_circuit()
    return value


def test_reference_cases_match_active_circuit_and_v3_requirements(solver):
    spec = importlib.util.spec_from_file_location('reference_probe', PROCESS_DIR / 'reference_probe.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    cases = module.reference_cases()
    assert cases == json.loads((PROCESS_DIR / 'reference-cases.json').read_text())
    assert cases['status'] == 'passed'
    assert all(cases['checks'].values())
    assert cases['source_1_10_settle_tap'] < 0
    assert cases['triple_load'][0]['bus20_kv'] < 19.8
    assert cases['triple_load'][-1]['in_band']
    assert len(cases['triple_load']) - 1 <= 3
    result = solver.solve(truths(), ProcessControls())
    assert result['converged']
    assert result['buses']['bus20']['kv'] == pytest.approx(cases['nominal']['bus20_kv'], abs=1e-7)
    assert solver.solution_frequency_hz == 50


@pytest.mark.parametrize('load,source,taps', [
    (1.0, 1.0, [-1, 0, 1]), (3.0, 1.0, [0, 1]),
    (1.0, .90, list(range(9))), (1.0, 1.10, list(range(0, -8, -1))),
])
def test_runtime_solver_matches_independent_operating_point_probe(solver, load, source, taps):
    spec = importlib.util.spec_from_file_location('reference_probe', PROCESS_DIR / 'reference_probe.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    probe = module.Probe(load, source)
    vector = truths()
    for tap in taps:
        vector['oltc-1'].update(tap=tap, target_tap=tap)
        actual = solver.solve(vector, ProcessControls(load_multiplier=load, source_pu=source))
        assert actual['buses']['bus20']['kv'] == pytest.approx(
            probe.sample(tap)['bus20_kv'], abs=1e-4)

@pytest.mark.parametrize('cb,r1,r2,f2,fault,expected', [
    (True, True, False, True, 's1', {'feeder-breaker'}),
    (True, True, False, True, 's2', {'feeder-breaker', 'rmu-1'}),
    (False, False, True, True, 's1', set()),
    (False, False, True, True, 's2', {'f2', 'rmu-2'}),
    (True, True, True, True, 's1', {'feeder-breaker', 'rmu-1', 'rmu-2', 'f2'}),
    (True, True, True, True, 's2', {'feeder-breaker', 'rmu-1', 'rmu-2', 'f2'}),
    (False, True, True, True, 's1', {'rmu-1', 'rmu-2', 'f2'}),
    (False, True, False, True, 's2', set()),
    (False, False, True, False, 's2', set()),
])
def test_fault_paths_table_and_ring(cb, r1, r2, f2, fault, expected):
    assert fault_paths(dict(zip(('feeder-breaker', 'rmu-1', 'rmu-2', 'f2'),
                               (cb, r1, r2, f2))), {fault}) == expected


@pytest.mark.parametrize('fault,rmu_fault', [('S1', False), ('S2', True)])
def test_bolted_fault_path_current_from_dss(solver, fault, rmu_fault):
    result = solver.solve(truths(), ProcessControls(fault_section=fault))
    assert result['converged']
    assert result['measurements']['feeder-breaker']['fault_current']
    assert result['measurements']['feeder-breaker']['current_a'] > 1000
    assert result['measurements']['rmu-1']['fault_current'] is rmu_fault
    assert not result['measurements']['rmu-2']['fault_current']
    assert result['buses'][fault.lower()]['kv'] < .01


def test_isolation_backfeed_and_f2_trip_vector(solver):
    vector = truths()
    vector['feeder-breaker']['position'] = 'open'
    vector['rmu-1']['position'] = 'open'
    vector['rmu-2']['position'] = 'closed'
    healthy = solver.solve(vector, ProcessControls(fault_section='S1'))
    assert healthy['buses']['s2']['live'] and healthy['buses']['s3']['live']
    assert not healthy['buses']['s1']['live']
    assert not healthy['f2_trip']
    fault = solver.solve(vector, ProcessControls(fault_section='S2'))
    assert fault['f2_trip']
    assert fault['measurements']['rmu-2']['fault_current']
    assert fault['measurements']['rmu-2']['current_a'] > 1000
    dead = solver.solve(vector, ProcessControls(fault_section='S2', f2_tripped=True))
    assert not dead['buses']['s3']['live']
    assert not dead['buses']['s2']['live']
    assert not dead['f2_trip']


def test_ring_solves_meshed_network_and_side_voltages(solver):
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    radial = solver.solve(truths(), ProcessControls())
    ring = solver.solve(vector, ProcessControls())
    assert ring['converged']
    assert all(b['live'] for b in ring['buses'].values())
    assert ring['buses']['s2']['kv'] > radial['buses']['s2']['kv']
    for key, source, section in [('feeder-breaker', 'bus20', 's1'),
                                 ('rmu-1', 's1', 's2'), ('rmu-2', 's3', 's2')]:
        assert ring['measurements'][key]['source_kv_ll'] == ring['buses'][source]['kv']
        assert ring['measurements'][key]['voltage_kv_ll'] == ring['buses'][section]['kv']
    assert ring['measurements']['oltc-1']['source_kv_ll'] > 100


def test_f2_latch_does_not_invent_a_dead_s3_if_ring_backfeeds_it(solver):
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    backfed = solver.solve(vector, ProcessControls(f2_tripped=True))
    assert backfed['converged'] and backfed['buses']['s3']['live']

@pytest.mark.parametrize('earth_device', ['rmu-1', 'rmu-2'])
def test_defensive_earth_switch_on_energised_s2_is_a_fault(solver, earth_device):
    vector = truths()
    vector[earth_device]['earth_closed'] = True
    result = solver.solve(vector, ProcessControls())
    assert result['measurements']['feeder-breaker']['fault_current']
    assert result['measurements']['rmu-1']['fault_current']
    assert result['buses']['s2']['kv'] < .01


def test_dead_earth_section_does_not_fault_healthy_feeder(solver):
    vector = truths()
    vector['rmu-1']['position'] = 'open'
    vector['rmu-2']['earth_closed'] = True
    result = solver.solve(vector, ProcessControls())
    assert result['buses']['s1']['live'] and result['buses']['s3']['live']
    assert not result['buses']['s2']['live']
    assert not any(m['fault_current'] for m in result['measurements'].values())


def test_completed_tap_only_and_repeated_fault_clearing(solver):
    vector = truths()
    vector['oltc-1'].update(moving=True, target_tap=1)
    baseline = solver.solve(vector, ProcessControls())
    assert baseline['tap'] == 0
    for _ in range(2):
        assert solver.solve(vector, ProcessControls(fault_section='S1'))['buses']['s1']['kv'] < .01
        cleared = solver.solve(vector, ProcessControls())
        assert cleared['buses']['bus20']['kv'] == pytest.approx(baseline['buses']['bus20']['kv'], abs=1e-5)
