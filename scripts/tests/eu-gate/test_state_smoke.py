"""Offline contract fixture/mutation tests. No Playwright/Vite/browser writes."""
import copy
import importlib.util
import itertools
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('check', ROOT / 'scripts/lib/eu-state-check.py')
check = importlib.util.module_from_spec(spec)
# No checkout __pycache__ artifacts when the offline suite imports the evaluator.
import sys
sys.dont_write_bytecode = True
spec.loader.exec_module(check)
FIXTURE = json.loads(Path(__file__).with_name('fixtures').joinpath('eu-state-dom.json').read_text())


def fixture(row, view):
    dom = copy.deepcopy(FIXTURE)
    dom['view'] = view
    dom['text'] = 'Protection TRIPPED · LOCKOUT' if row.get('lockout') else ''
    dom['authorities'] = []
    dom['devices'][3]['text'] = 'Tap 0 · GOOD · MANUAL'
    expected = dict(zip(check.VOLTAGES, row.get('section_kv', [0 if s == 'D' else 19.8 for s in row['supply']])))
    expected.update({'oltc-1-bus-kv': 20, 'feeder-voltage': row['vt_kv'],
                     'oltc-1-current': row['currents_a'][3], 'feeder-current': row['currents_a'][0],
                     'rmu-1-current': row['currents_a'][1], 'rmu-2-current': row['currents_a'][2],
                     'rmu-2-section-kv': expected['rmu-1-section-kv']})
    for i, section in enumerate(dom['sections']):
        section.update(state='DEAD' if row['supply'][i] == 'D' else 'LIVE',
                       path=row['cc_paths'][i] if view == 'cc' else check.PATHS[row['supply'][i]],
                       kv=str(expected[check.VOLTAGES[i]]))
        section['text'] = f'{section["section"]} · {section["state"]} {section["path"]} {section["kv"]} kV'
    for d, s in zip(dom['devices'], row['switches']):
        d['position'] = 'closed' if s == 'C' else 'open'
    dom['devices'][4]['position'] = 'unknown' if view == 'cc' else 'open' if row['f2'] == 'tripped' else 'closed'
    dom['f2Line'] = 'UNKNOWN' if view == 'cc' else 'DEAD' if row['supply'][2] == 'D' else 'LIVE'
    for m in dom['measurements']:
        m['text'] = f'{expected[m["point"]]:.2f} {"kV" if m["point"].endswith("-kv") or m["point"] == "feeder-voltage" else "A"} · GOOD'
        if m['point'].endswith('-kv') or m['point'] == 'feeder-voltage':
            m['state'] = 'LIVE' if expected[m['point']] >= .1 else 'DEAD'
    dom['conditions'] = []
    for d, s, normal in zip(['CB1', 'RMU1', 'RMU2'], row['switches'], 'CCO'):
        if s != normal:
            dom['conditions'].append({'id': d, 'text': f'{d}: {"OPEN" if s == "O" else "CLOSED"} (normal {"CLOSED" if normal == "C" else "OPEN"})'})
    for section, supply in zip(check.SECTIONS, row['supply']):
        if supply == 'D':
            dom['conditions'].append({'id': section, 'text': f'{section}: no supply'})
    if not dom['conditions']:
        dom['conditions'] = copy.deepcopy(FIXTURE['conditions'])
    if row.get('lockout'):
        dom['conditions'] = [c for c in dom['conditions'] if c['id'] != 'none']
        dom['conditions'].append({'id': 'feeder-protection-trip', 'text': 'Protection trip: asserted'})
    if view == 'physical':
        dom['comparisons'] = [{'point': id, 'status': 'MATCH'} for id in expected]
    return dom, expected


def data_fixture(row):
    """Synthetic wire snapshots: explicit device registers, not UI fixtures."""
    now = '2026-10-18T18:00:05Z'
    kv = row.get('section_kv', [0 if s == 'D' else 19.8 for s in row['supply']])
    field = {'devices': {}, 'process': {
        'truth_stale': False, 'solver_epoch': 'fixture', 'solve_seq': 2, 'solved_at': now,
        'fault_section': row.get('fault', 'none'), 'f2_tripped': row['f2'] == 'tripped',
        'profile': {'enabled': False}, 'source_pu': 1, 'load_multiplier': 1,
        'buses': {f's{i+1}': {'kv': v, 'live': v >= .1} for i, v in enumerate(kv)},
        'devices': {},
    }}
    field['process']['buses']['bus20'] = {'kv': 20, 'live': True}
    for i, name in enumerate(check.DEVICES):
        device_id = 'feeder-breaker' if i == 0 else name
        amps = row['currents_a'][i]
        d = {'device_id': device_id, 'generation': 7, 'revision': 1, 'remote_enabled': True,
             'protection': {'picked_up': False}, 'position': 'closed' if i < 3 and row['switches'][i] == 'C' else 'open',
             'earth_closed': False, 'fault_passage': False,
             'measurements': {'valid': True, 'solver_epoch': 'fixture', 'solve_seq': 2,
                              'received_at': now, 'current_a': amps,
                              'voltage_kv_ll': [row['vt_kv'], kv[1], kv[1], 20][i],
                              'source_kv_ll': [20, kv[0], kv[2], 110][i],
                              'p_mw': amps / 30, 'q_mvar': amps / 100, 'pf': .95 if amps else None}}
        field['devices'][name] = d
        field['process']['devices'][device_id] = {'delivered': True, 'solved_revision': 1}
    field['devices']['breaker-1'].update(lockout=row.get('lockout', False), protection_tripped=row.get('lockout', False))
    field['devices']['oltc-1'].update(tap=0, moving=False, auto=False, blocked=False)
    values = check.point_values(field)
    values.update({'plc-1-running': True, 'plc-1-input-valid': True,
                   'plc-1-setpoint-kv': 20.4, 'plc-1-deadband-pct': 1, 'plc-1-delay-s': 30})
    operator = {'link': {'state': 'started', 'gi': {'state': 'complete'}}, 'points': []}
    for id, value in values.items():
        operator['points'].append({
            'id': id, 'value': value if value is not None else 0, 'received_at': now,
            'kind': 'float' if type(value) is float else 'status',
            'quality': {'iv': value is None, 'nt': False, 'sb': False, 'bl': False, 'ov': False},
            'stale_reason': 'measurement_invalid' if value is None else None,
            'reporting': {'deadband': .1},
        })
    before = {'process': {'solver_epoch': 'fixture', 'solve_seq': 1}}
    return field, operator, before, '2026-10-18T18:00:00Z'


class StateSmoke(unittest.TestCase):
    def test_all_21_plant_and_cc_vectors(self):
        for row in check.ORACLE['rows']:
            with self.subTest(row=row['id']):
                check.check_data(row, *data_fixture(row))

    def test_all_21_four_way_checkpoints(self):
        for row in check.ORACLE['rows']:
            expected = check.check_data(row, *data_fixture(row))
            for view in ['cc', 'physical']:
                with self.subTest(row=row['id'], view=view):
                    dom, _ = fixture(row, view)
                    if view == 'physical':
                        dom['comparisons'] = [{'point': id, 'status': 'INVALID' if v is None else 'MATCH'}
                                              for id, v in expected.items() if id not in ['oltc-1-at-lower', 'oltc-1-at-upper']]
                    check.check_dom(row, dom, view, expected)

    def test_data_mutations_fail(self):
        row = check.ROWS['CCO-normal']
        mutations = [
            lambda f, o: f['process'].update(truth_stale=True),
            lambda f, o: f['process'].update(solve_seq=1),
            lambda f, o: f['process'].update(solver_epoch='new'),
            lambda f, o: f['process'].update(f2_tripped=True),
            lambda f, o: f['process']['devices']['rmu-1'].update(solved_revision=0),
            lambda f, o: f['devices']['rmu-1']['measurements'].update(valid=False),
            lambda f, o: f['devices']['breaker-1']['measurements'].update(voltage_kv_ll=0),
            lambda f, o: f['devices']['rmu-1']['measurements'].update(current_a=0),
            lambda f, o: f['devices']['rmu-1'].update(position='open'),
            lambda f, o: o['points'][0].update(value='open'),
            lambda f, o: o['points'][0]['quality'].update(iv=True),
            lambda f, o: o['points'][0].update(received_at='2026-10-18T17:00:00Z'),
        ]
        for i, mutation in enumerate(mutations):
            with self.subTest(mutation=i):
                f, o, before, since = data_fixture(row)
                mutation(f, o)
                with self.assertRaises(AssertionError):
                    check.check_data(row, f, o, before, since)

    def test_oracle_shape_and_electrical_reachability(self):
        rows = check.ORACLE['rows']
        self.assertEqual(len(rows), 21)
        self.assertEqual(len(check.ROWS), 21)
        self.assertEqual({(r['switches'], r['f2']) for r in rows[:16]},
                         {(''.join(s), f) for s in itertools.product('OC', repeat=3) for f in ['normal', 'tripped']})
        for r in rows[:16]:
            for i in range(3):
                b = all(s == 'C' for s in r['switches'][:i+1])
                f = r['f2'] == 'normal' and all(s == 'C' for s in r['switches'][i+1:])
                self.assertEqual(r['supply'][i], 'BF' if b and f else 'B' if b else 'F' if f else 'D')
            self.assertEqual(len(r['currents_a']), 4)
            self.assertEqual(len(r['cc_paths']), 3)

    def test_list_is_offline_and_complete(self):
        result = subprocess.run(['bash', str(ROOT / 'scripts/eu-state-smoke.sh'), '--list'], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        for r in check.ORACLE['rows'][:16]:
            name = 'CCC-normal-interlock' if r['id'] == 'CCC-normal' else r['id']
            self.assertIn(name + ':', result.stdout)
        self.assertIn('CCC-normal-interlock: interlock', result.stdout)
        self.assertNotIn('CCC-normal: UNSUPPORTED', result.stdout)
        for name in ['S3-fault', 'PLC-setpoint', 'S1-pre-trip']:
            self.assertIn(name + ': UNSUPPORTED', result.stdout)

    def test_interlock_rejected_without_state_change(self):
        field, operator, _, _ = data_fixture(check.ROWS['OCC-normal'])
        command = {'point': 'feeder-breaker-command', 'operation': 'close', 'result': 'rejected',
                   'reason': 'execute rejected (negative ACT-CON)',
                   'con_at': '2026-10-18T18:00:05Z', 'term_at': None, 'feedback': None}
        check.check_interlock(check.ORACLE['interlocks']['CCC-normal-interlock'],
                              field, operator, copy.deepcopy(field), copy.deepcopy(operator), command)

    def test_interlock_mutations_fail(self):
        before_field, before_operator, _, _ = data_fixture(check.ROWS['OCC-normal'])
        mutations = [
            lambda f, o, c: c.update(result='unknown'),
            lambda f, o, c: c.update(result='completed'),
            lambda f, o, c: c.update(reason='timeout'),
            lambda f, o, c: c.update(point='rmu-1-command'),
            lambda f, o, c: c.update(con_at=None),
            lambda f, o, c: c.update(term_at='2026-10-18T18:00:06Z'),
            lambda f, o, c: c.update(feedback={'value': 'closed'}),
            lambda f, o, c: f['devices']['breaker-1'].update(position='closed'),
            lambda f, o, c: f['devices']['breaker-1'].update(revision=2),
            lambda f, o, c: f['devices']['breaker-1'].update(generation=8),
            lambda f, o, c: o['points'][0].update(value='closed'),
            lambda f, o, c: o['points'][0]['quality'].update(iv=True),
        ]
        for section, point in zip(check.SECTIONS, check.VOLTAGES):
            mutations.append(lambda f, o, c, s=section: f['process']['buses'][s.lower()].update(live=False, kv=0))
            mutations.append(lambda f, o, c, id=point: next(p for p in o['points'] if p['id'] == id).update(value=0))
        for i, mutate in enumerate(mutations):
            with self.subTest(mutation=i):
                field, operator = copy.deepcopy(before_field), copy.deepcopy(before_operator)
                command = {'point': 'feeder-breaker-command', 'operation': 'close', 'result': 'rejected',
                           'reason': 'execute rejected (negative ACT-CON)',
                           'con_at': '2026-10-18T18:00:05Z', 'term_at': None, 'feedback': None}
                mutate(field, operator, command)
                with self.assertRaises(AssertionError):
                    check.check_interlock(check.ORACLE['interlocks']['CCC-normal-interlock'],
                                          before_field, before_operator, field, operator, command)
        bad_before = copy.deepcopy(before_operator)
        bad_before['points'][0]['quality']['nt'] = True
        with self.assertRaises(AssertionError):
            check.check_interlock(check.ORACLE['interlocks']['CCC-normal-interlock'],
                                  before_field, bad_before, before_field, before_operator, command)

    def test_failed_case_stops_without_retry_and_resets(self):
        run_spec = importlib.util.spec_from_file_location('run', ROOT / 'scripts/lib/eu-state-run.py')
        run = importlib.util.module_from_spec(run_spec)
        run_spec.loader.exec_module(run)
        import io
        calls = []
        def worker(case, directory, pending, timeout):
            calls.append((case, timeout))
            return {'case': case, 'exit_code': 0 if case == 'cleanup' else 1}
        class Response(io.BytesIO):
            pass
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(run, 'ROOT', Path(tmp)), \
             patch.object(run, 'preflight'), \
             patch.object(run, 'worker', side_effect=worker), \
             patch.object(run.signal, 'signal'), \
             patch.object(run.urllib.request, 'urlopen', side_effect=lambda *a, **k: Response(b'{}')), \
             patch.object(sys, 'argv', ['smoke', '--case', 'switching', '--pending-dom']):
            self.assertEqual(run.main(), 1)
        self.assertEqual([case for case, _ in calls], ['OOO-normal', 'cleanup'])
        self.assertEqual(calls[1][1], 110)

    def test_timeout_terminates_only_owned_process_group(self):
        run_spec = importlib.util.spec_from_file_location('run', ROOT / 'scripts/lib/eu-state-run.py')
        run = importlib.util.module_from_spec(run_spec)
        run_spec.loader.exec_module(run)
        process = Mock(pid=43210)
        process.wait.side_effect = [subprocess.TimeoutExpired('owned worker', 1), -15]
        with tempfile.TemporaryDirectory() as tmp, \
             patch.object(run.subprocess, 'Popen', return_value=process) as popen, \
             patch.object(run.os, 'killpg') as kill:
            result = run.worker('baseline', Path(tmp) / 'evidence', True, 1)
        self.assertEqual(result['exit_code'], 124)
        self.assertTrue(popen.call_args.kwargs['start_new_session'])
        kill.assert_called_once_with(43210, run.signal.SIGTERM)

    def test_static_baseline_fixture(self):
        check.check_dom(check.ROWS['CCO-normal'], FIXTURE, 'cc', {
            'rmu-1-source-kv': 19.8, 'rmu-1-section-kv': 19.7, 'rmu-2-source-kv': 19.9,
            'oltc-1-bus-kv': 20, 'feeder-voltage': 19.91, 'rmu-2-section-kv': 19.7,
            'oltc-1-current': 72.3, 'feeder-current': 61.4, 'rmu-1-current': 18.7, 'rmu-2-current': 0})

    def test_all_21_rows_both_dom_views(self):
        for row in check.ORACLE['rows']:
            for view in ['cc', 'physical']:
                with self.subTest(row=row['id'], view=view):
                    dom, expected = fixture(row, view)
                    check.check_dom(row, dom, view, expected)

    def test_dom_contract_mutations_fail(self):
        row = check.ROWS['COO-tripped']
        mutations = [lambda d: d.update(cached=True), lambda d: d.update(visible=False),
                     lambda d: d['sections'][1].update(state='LIVE'),
                     lambda d: d['sections'][1].update(path='via CB1'),
                     lambda d: d['sections'][1].update(kv='0.099', state='LIVE'),
                     lambda d: d['sections'][1].update(text='S2 LIVE'),
                     lambda d: d['devices'][0].update(position='open'),
                     lambda d: d['devices'][0].update(quality='unknown'),
                     lambda d: d['devices'][3].update(tap='1'),
                     lambda d: d['devices'][4].update(position='closed'),
                     lambda d: d.update(f2Line='LIVE'),
                     lambda d: d['measurements'][3].update(state='DEAD'),
                     lambda d: d['measurements'][0].update(visible=False),
                     lambda d: d['measurements'][0].update(text='9999.0 A · GOOD'),
                     lambda d: d['comparisons'][0].update(status='UNKNOWN'),
                     lambda d: d['comparisons'].pop()]
        for i, mutation in enumerate(mutations):
            with self.subTest(mutation=i):
                dom, expected = fixture(row, 'physical')
                mutation(dom)
                with self.assertRaises(AssertionError):
                    check.check_dom(row, dom, 'physical', expected)

    def test_empty_or_missing_abnormal_summary_fails(self):
        row = check.ROWS['OOC-normal']
        for conditions in [[], FIXTURE['conditions'], [{'id': 'offnormal', 'text': 'CB1 OPEN'}]]:
            dom, expected = fixture(row, 'cc')
            dom['conditions'] = conditions
            with self.assertRaises(AssertionError):
                check.check_dom(row, dom, 'cc', expected)

    def test_zero_power_pf_expected_invalid_not_unknown(self):
        row = check.ROWS['OOO-tripped']
        dom, expected = fixture(row, 'physical')
        expected['feeder-pf'] = None
        dom['comparisons'].append({'point': 'feeder-pf', 'status': 'INVALID'})
        check.check_dom(row, dom, 'physical', expected)
        dom['comparisons'][-1]['status'] = 'UNKNOWN'
        with self.assertRaises(AssertionError):
            check.check_dom(row, dom, 'physical', expected)


if __name__ == '__main__':
    unittest.main()
