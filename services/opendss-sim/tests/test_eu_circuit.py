import importlib.util
import json
from dataclasses import replace

import pytest
from eu_circuit import EUFeederSolver
from eu_controls import ProcessControls
from eu_support import PROCESS_DIR, profile, truths


@pytest.fixture
def solver(tmp_path):
    value = EUFeederSolver(profile(tmp_path))
    value.compile_circuit()
    return value


def test_reference_cases_match_active_circuit_and_schema3_requirements(solver):
    spec = importlib.util.spec_from_file_location('reference_probe', PROCESS_DIR / 'reference_probe.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    cases = module.reference_cases()
    assert cases == json.loads((PROCESS_DIR / 'reference-cases.json').read_text())
    assert cases['status'] == 'passed'
    assert all(cases['checks'].values())
    assert cases['nominal']['feeder_voltage_kv'] > cases['nominal']['s1_kv']
    assert cases['source_1_10_settle_tap'] < 0
    assert cases['triple_load'][0]['bus20_kv'] < cases['band_kv'][0]
    assert cases['triple_load'][-1]['in_band']
    assert len(cases['triple_load']) - 1 <= 3
    result = solver.solve(truths(), ProcessControls(profile_enabled=False))
    assert result['converged']
    assert result['buses']['bus20']['kv'] == pytest.approx(cases['nominal']['bus20_kv'], abs=1e-7)
    assert result['measurements']['feeder-breaker']['voltage_kv_ll'] == pytest.approx(
        cases['nominal']['feeder_voltage_kv'], abs=1e-7)
    assert result['measurements']['rmu-1']['source_kv_ll'] == pytest.approx(
        cases['nominal']['s1_kv'], abs=1e-7)
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
        actual = solver.solve(vector, ProcessControls(profile_enabled=False, load_multiplier=load, source_pu=source))
        assert actual['buses']['bus20']['kv'] == pytest.approx(
            probe.sample(tap)['bus20_kv'], abs=1e-4)

@pytest.mark.parametrize('fault,rmu_fault', [('S1', False), ('S2', True)])
def test_bolted_fault_path_current_from_dss(solver, fault, rmu_fault):
    result = solver.solve(truths(), ProcessControls(profile_enabled=False, fault_section=fault))
    assert result['converged']
    assert result['measurements']['feeder-breaker']['current_a'] >= 400
    assert result['measurements']['feeder-breaker']['current_a'] > 1000
    assert (result['measurements']['rmu-1']['current_a'] >= 300) is rmu_fault
    assert result['measurements']['rmu-2']['current_a'] < 1
    assert result['buses'][fault.lower()]['kv'] < .01


def test_isolation_backfeed_and_f2_trip_vector(solver):
    vector = truths()
    vector['feeder-breaker']['position'] = 'open'
    vector['rmu-1']['position'] = 'open'
    vector['rmu-2']['position'] = 'closed'
    healthy = solver.solve(vector, ProcessControls(profile_enabled=False, fault_section='S1'))
    assert healthy['buses']['s2']['live'] and healthy['buses']['s3']['live']
    assert not healthy['buses']['s1']['live']
    assert healthy['measurements']['f2-breaker']['current_a'] < 400
    fault = solver.solve(vector, ProcessControls(profile_enabled=False, fault_section='S2'))
    assert fault['measurements']['f2-breaker']['current_a'] >= 400
    assert fault['measurements']['rmu-2']['current_a'] > 1000
    vector['f2-breaker'].update(position='open', protection_tripped=True)
    dead = solver.solve(vector, ProcessControls(profile_enabled=False, fault_section='S2'))
    assert not dead['buses']['s3']['live']
    assert not dead['buses']['s2']['live']
    assert dead['measurements']['f2-breaker']['current_a'] < 1


def test_ring_solves_meshed_network_and_side_voltages(solver):
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    radial = solver.solve(truths(), ProcessControls(profile_enabled=False))
    ring = solver.solve(vector, ProcessControls(profile_enabled=False))
    assert ring['converged']
    assert all(b['live'] for b in ring['buses'].values())
    assert ring['buses']['s2']['kv'] > radial['buses']['s2']['kv']
    for key, source, section in [('feeder-breaker', 'bus20', 'f1'),
                                 ('rmu-1', 's1', 's2'), ('rmu-2', 's3', 's2')]:
        assert ring['measurements'][key]['source_kv_ll'] == ring['buses'][source]['kv']
        expected = solver._bus(section) if section == 'f1' else ring['buses'][section]
        assert ring['measurements'][key]['voltage_kv_ll'] == expected['kv']
    assert ring['measurements']['oltc-1']['source_kv_ll'] > 100


@pytest.mark.parametrize('breaker,rmu1,rmu2,live', [
    ('closed', 'closed', 'open', True),
    ('open', 'closed', 'open', False),
    ('open', 'closed', 'closed', True),
])
def test_cb1_line_side_liveness_preserves_switch_interlock_sides(
        solver, breaker, rmu1, rmu2, live):
    vector = truths()
    vector['feeder-breaker']['position'] = breaker
    vector['rmu-1']['position'] = rmu1
    vector['rmu-2']['position'] = rmu2
    result = solver.solve(vector, ProcessControls(profile_enabled=False))
    cb1 = result['measurements']['feeder-breaker']

    assert result['buses']['s1']['live'] is live
    assert solver._bus('f1')['live'] is result['buses']['s1']['live']
    assert cb1['section_live'] is result['buses']['s1']['live']
    assert cb1['source_live'] is result['buses']['bus20']['live']
    if breaker == 'open' and rmu2 == 'closed':
        # G15's "reverse feed S1 through tie" state leaves both CB1 sides live.
        # The unchanged switch interlock therefore rejects a close as live_live.
        assert cb1['source_live'] and cb1['section_live']


def test_open_f2_does_not_invent_a_dead_s3_if_ring_backfeeds_it(solver):
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    vector['f2-breaker'].update(position='open', protection_tripped=True)
    backfed = solver.solve(vector, ProcessControls(profile_enabled=False))
    assert backfed['converged'] and backfed['buses']['s3']['live']

@pytest.mark.parametrize('earth_device', ['rmu-1', 'rmu-2'])
def test_defensive_earth_switch_on_energised_s2_is_a_fault(solver, earth_device):
    vector = truths()
    vector[earth_device]['earth_closed'] = True
    result = solver.solve(vector, ProcessControls(profile_enabled=False))
    assert result['measurements']['feeder-breaker']['current_a'] >= 400
    assert result['measurements']['rmu-1']['current_a'] >= 300
    assert result['buses']['s2']['kv'] < .01


def test_dead_earth_section_does_not_fault_healthy_feeder(solver):
    vector = truths()
    vector['rmu-1']['position'] = 'open'
    vector['rmu-2']['earth_closed'] = True
    result = solver.solve(vector, ProcessControls(profile_enabled=False))
    assert result['buses']['s1']['live'] and result['buses']['s3']['live']
    assert not result['buses']['s2']['live']
    assert all(m['current_a'] < 300 for m in result['measurements'].values())


def test_completed_tap_only_and_repeated_fault_clearing(solver):
    vector = truths()
    vector['oltc-1'].update(moving=True, target_tap=1)
    baseline = solver.solve(vector, ProcessControls(profile_enabled=False))
    assert baseline['tap'] == 0
    for _ in range(2):
        assert solver.solve(vector, ProcessControls(profile_enabled=False, fault_section='S1'))['buses']['s1']['kv'] < .01
        cleared = solver.solve(vector, ProcessControls(profile_enabled=False))
        assert cleared['buses']['bus20']['kv'] == pytest.approx(baseline['buses']['bus20']['kv'], abs=1e-5)


def test_three_phase_power_sign_and_terminal_selection(solver):
    controls = ProcessControls(profile_enabled=False)
    normal = solver.solve(truths(), controls)['measurements']
    for device in ('feeder-breaker', 'rmu-1', 'oltc-1'):
        assert normal[device]['p_mw'] > 0
        assert normal[device]['q_mvar'] > 0
    assert normal['oltc-1']['p_mw'] == pytest.approx(2.34, abs=.05)
    assert normal['feeder-breaker']['p_mw'] == pytest.approx(2.01, abs=.05)
    # Check the actual metering terminal and 3-phase kW/kvar conversion.
    for device, element, terminal, sign in [
        ('feeder-breaker', 'Line.CB1', 1, 1),
        ('f2-breaker', 'Line.F2', 1, 1),
        ('oltc-1', 'Transformer.T1', 2, -1),
    ]:
        solver._dss.Circuit.SetActiveElement(element)
        values = solver._dss.CktElement.Powers()
        offset = (terminal - 1) * solver._dss.CktElement.NumConductors() * 2
        assert normal[device]['p_mw'] == pytest.approx(
            sign * sum(values[offset:offset + 6:2]) / 1000)
        assert normal[device]['q_mvar'] == pytest.approx(
            sign * sum(values[offset + 1:offset + 6:2]) / 1000)
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    vector['f2-breaker'].update(position='open', protection_tripped=True)
    reverse = solver.solve(vector, ProcessControls(
        profile_enabled=False))['measurements']['rmu-2']
    assert reverse['p_mw'] < 0 and reverse['q_mvar'] < 0


@pytest.mark.parametrize('device', ['feeder-breaker', 'f2-breaker', 'rmu-1', 'rmu-2'])
def test_open_switch_has_exact_zero_power_even_with_energised_sides(solver, device):
    vector = truths()
    vector['rmu-2']['position'] = 'closed'
    vector[device]['position'] = 'open'
    sample = solver.solve(vector, ProcessControls(profile_enabled=False))['measurements'][device]
    assert sample['section_live'] and sample['source_live']
    assert sample['p_mw'] == 0.0 and sample['q_mvar'] == 0.0


def test_solved_at_strictly_increases_for_fast_solves_and_wall_clock_rollback(tmp_path):
    now = [1792270800.]
    solver = EUFeederSolver(profile(tmp_path), clock=lambda: now[0])
    solver.compile_circuit()
    controls = ProcessControls(profile_enabled=False)
    first = solver.solve(truths(), controls)['solved_at']
    second = solver.solve(truths(), controls)['solved_at']
    now[0] -= 60
    third = solver.solve(truths(), controls)['solved_at']
    assert first < second < third


def test_profile_composes_with_controls_and_does_not_compound_loads(tmp_path):
    config = profile(tmp_path)
    config = replace(config, profile=replace(config.profile, seed=1))
    solver = EUFeederSolver(config, clock=lambda: 1792270800.)
    solver.compile_circuit()
    controls = ProcessControls(load_multiplier=2, source_pu=.95)
    sample = solver.solve(truths(), controls)
    for name, category in [('Residential', 'residential'), ('Commercial', 'commercial'),
                            ('Industrial', 'industrial')]:
        solver._dss.Loads.Name(name)
        expected = solver._base_loads[name][0] * sample['profile']['load_factors'][category]
        assert solver._dss.Loads.kW() == pytest.approx(expected)
    assert solver._dss.Solution.LoadMult() == 2
    solver._dss.Vsources.Name('Source')
    assert solver._dss.Vsources.PU() == pytest.approx(.95 * sample['profile']['source_pu'])
    again = solver.solve(truths(), controls)
    assert again['buses']['bus20']['kv'] == pytest.approx(sample['buses']['bus20']['kv'], abs=1e-5)
