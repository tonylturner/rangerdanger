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
    dom['observationUncertainty'] = '0'
    dom['devices'][3]['text'] = 'Tap 0 · GOOD · MANUAL'
    expected = check.point_values(data_fixture(row)[0])
    expected.update({'plc-1-running': True, 'plc-1-input-valid': True,
                     'plc-1-setpoint-kv': 20.4, 'plc-1-deadband-pct': 1, 'plc-1-delay-s': 30})
    dom['observationUncertainty'] = str(sum(v is None for v in expected.values()))
    for i, section in enumerate(dom['sections']):
        section.update(state='DEAD' if row['supply'][i] == 'D' else 'LIVE',
                       path=row['cc_paths'][i] if view == 'cc' else check.PATHS[row['supply'][i]],
                       kv=str(expected[check.VOLTAGES[i]]))
        section['text'] = f'{section["section"]} · {section["state"]} {section["path"]} {section["kv"]} kV'
    for d, s in zip(dom['devices'], row['switches']):
        d['position'] = 'closed' if s == 'C' else 'open'
    dom['devices'][4]['position'] = 'open' if row['f2'] == 'tripped' else 'closed'
    dom['f2Line'] = 'DEAD' if row['supply'][2] == 'D' else 'LIVE'
    dom['f2Telemetry'] = f'F2 · indication only: {"TRIPPED" if row["f2"] == "tripped" else "NORMAL"} · I {row["f2_current_a"]:.1f} A · P {row["f2_p_mw"]:.2f} MW'
    for m in dom['measurements']:
        m['text'] = f'{expected[m["point"]]:.2f} {"kV" if m["point"].endswith("-kv") or m["point"] == "feeder-voltage" else "A"} · GOOD'
        if m['point'].endswith('-kv') or m['point'] == 'feeder-voltage':
            m['state'] = 'LIVE' if expected[m['point']] >= .1 else 'DEAD'
    add_presentation(dom, expected)
    if view == 'physical':
        dom['comparisons'] = [{'point': id, 'status': 'INVALID' if v is None else 'MATCH'}
                              for id, v in check.comparison_values(expected).items()]
    return dom, expected


def add_presentation(dom, expected):
    """Extend the historical text-node fixture with the new presentation contract."""
    rules = check.ORACLE['presentation']
    alarms, off = [], []
    for id, node in rules['supply_nodes'].items():
        if expected[id] is not None and 0 <= expected[id] < .1:
            alarms.append({'id': node['id'], 'text': f'{node["label"]}: loss of supply'})
    for id, (low, high) in rules['voltage_limits'].items():
        if expected[id] is not None and expected[id] >= .1 and not low <= expected[id] <= high:
            alarms.append({'id': id + '-voltage-limit', 'text': f'{id}: voltage limit'})
    for id in rules['asserted_alarms'] + rules['false_alarms']:
        if expected[id] is (id in rules['asserted_alarms']):
            label = 'Protection trip' if id == 'feeder-protection-trip' else 'F2 protection trip' if id == 'f2-protection-trip' else id
            alarms.append({'id': id, 'text': f'{label}: asserted'})
    for device, spec in rules['switches'].items():
        value = expected[spec['point']]
        if value != spec['normal']:
            off.append({'id': spec['point'] + '-off-normal',
                        'text': f'{spec["label"]}: {value.upper()} (normal {spec["normal"].upper()})'})
    if expected['oltc-1-auto'] is False:
        off.append({'id': 'oltc-1-auto-off-normal', 'text': 'T1: MANUAL (normal AUTO)'})
    for device, id in rules['authorities'].items():
        if expected[id] is False:
            off.append({'id': id, 'text': f'{"T1 OLTC" if device == "T1" else device} authority: LOCAL'})
    for d in dom['devices']:
        device = d['device']
        position_bad = device in rules['switches'] and expected[rules['switches'][device]['point']] != rules['switches'][device]['normal']
        local = device in rules['authorities'] and expected[rules['authorities'][device]] is False
        d['offNormal'] = str(position_bad or local or device == 'T1' and expected['oltc-1-auto'] is False).lower()
    dom['conditions'] = [dict(c, kind='alarm') for c in alarms] + [dict(c, kind='off-normal') for c in off]
    if not dom['conditions']:
        dom['conditions'] = [{'id': 'none', 'text': 'No alarms in GOOD observations'}]
    headline = f'{len(alarms)} {"alarm" if len(alarms) == 1 else "alarms"} · {len(off)} off-normal' if alarms or off else 'No alarms in GOOD observations'
    dom['abnormalStrip'] = {'visible': True, 'beforeSchematic': True, 'alarmCount': str(len(alarms)),
                            'offNormalCount': str(len(off)), 'text': headline}


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
        amps = row['f2_current_a'] if name == 'f2-breaker' else row['currents_a'][i]
        d = {'device_id': device_id, 'generation': 7, 'revision': 1, 'remote_enabled': True,
             'protection': {'picked_up': False}, 'position': 'closed' if i < 3 and row['switches'][i] == 'C' else 'open',
             'earth_closed': False, 'fault_passage': False,
             'measurements': {'valid': True, 'solver_epoch': 'fixture', 'solve_seq': 2,
                              'received_at': now, 'current_a': amps,
                              'voltage_kv_ll': [row['vt_kv'], kv[1], kv[1], 20, kv[2]][i],
                              'source_kv_ll': [20, kv[0], kv[2], 110, 20][i],
                              'p_mw': amps / 30, 'q_mvar': amps / 100, 'pf': .95 if amps else None}}
        field['devices'][name] = d
        field['process']['devices'][device_id] = {'delivered': True, 'solved_revision': 1}
    field['devices']['breaker-1'].update(lockout=row.get('lockout', False), protection_tripped=row.get('lockout', False))
    field['devices']['f2-breaker'].update(position='open' if row['f2'] == 'tripped' else 'closed',
                                         lockout=row['f2'] == 'tripped', protection_tripped=row['f2'] == 'tripped')
    field['devices']['f2-breaker']['measurements']['p_mw'] = row['f2_p_mw']
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
    def test_alarm_oracle_covers_all_kinds_and_busbar_loss(self):
        _, expected = fixture(check.ROWS['CCO-normal'], 'cc')
        self.assertEqual(check.expected_alarms(expected), set())
        for id in check.PRESENTATION['asserted_alarms'] + check.PRESENTATION['false_alarms']:
            with self.subTest(point=id):
                values = dict(expected)
                values[id] = id in check.PRESENTATION['asserted_alarms']
                self.assertEqual(check.expected_alarms(values), {id})
        for id, node in check.PRESENTATION['supply_nodes'].items():
            for value in [0, .000004, .099]:
                with self.subTest(point=id, value=value):
                    self.assertEqual(check.expected_alarms(dict(expected, **{id: value})), {node['id']})
            for value in [None, -1, float('nan'), float('inf')]:
                self.assertEqual(check.expected_alarms(dict(expected, **{id: value})), set())
        for id in check.PRESENTATION['voltage_limits']:
            self.assertEqual(check.expected_alarms(dict(expected, **{id: 18})), {id + '-voltage-limit'})

    def test_presentation_oracle_matches_protocol(self):
        points = json.loads((ROOT / 'lab-definitions/packages/eu-iec104-substation/protocols/points.json').read_text())['points']
        self.assertEqual(check.PRESENTATION['voltage_limits'],
                         {p['id']: [p['limits']['low'], p['limits']['high']] for p in points if p.get('limits')})

    def test_strip_mutations_fail_for_every_oracle_row(self):
        for row in check.ORACLE['rows']:
            mutations = [
                lambda d: d.update(abnormalStrip=None),
                lambda d: d['abnormalStrip'].update(visible=False),
                lambda d: d['abnormalStrip'].update(beforeSchematic=False),
                lambda d: d['abnormalStrip'].update(alarmCount='999'),
                lambda d: d['abnormalStrip'].update(offNormalCount='999'),
                lambda d: d['abnormalStrip'].update(text='Portal API: available · 0 active alarms'),
                lambda d: d['conditions'].pop(),
            ]
            for i, mutate in enumerate(mutations):
                with self.subTest(row=row['id'], mutation=i):
                    dom, expected = fixture(row, 'cc')
                    mutate(dom)
                    with self.assertRaises(AssertionError):
                        check.check_dom(row, dom, 'cc', expected)
            dom, expected = fixture(row, 'cc')
            alarms = [c for c in dom['conditions'] if c['kind'] == 'alarm']
            if alarms:
                dom['conditions'] = dom['conditions'][len(alarms):] + alarms
                with self.assertRaisesRegex(AssertionError, 'alarms first'):
                    check.check_dom(row, dom, 'cc', expected)
            for device in ['CB1', 'RMU1', 'RMU2', 'F2', 'T1']:
                for view in ['cc', 'physical']:
                    dom, expected = fixture(row, view)
                    node = next(d for d in dom['devices'] if d['device'] == device)
                    for wrong in [None, 'false' if node['offNormal'] == 'true' else 'true']:
                        with self.subTest(row=row['id'], view=view, device=device, wrong=wrong):
                            bad = copy.deepcopy(dom)
                            next(d for d in bad['devices'] if d['device'] == device)['offNormal'] = wrong
                            with self.assertRaisesRegex(AssertionError, 'off-normal indication'):
                                check.check_dom(row, bad, view, expected)

    def test_each_dead_node_requires_its_own_loss_of_supply_wording(self):
        row = check.ROWS['OOO-tripped']
        for id, node in check.PRESENTATION['supply_nodes'].items():
            label = node['label']
            dom, expected = fixture(row, 'cc')
            expected['oltc-1-bus-kv'] = 0
            next(m for m in dom['measurements'] if m['point'] == 'oltc-1-bus-kv').update(text='0.00 kV GOOD', state='DEAD')
            add_presentation(dom, expected)
            check.check_dom(row, dom, 'cc', expected)
            alarm = next(c for c in dom['conditions'] if c['id'] == node['id'])
            for wording in [f'{label}: no supply', 'Another node: loss of supply']:
                with self.subTest(point=id, wording=wording):
                    alarm['text'] = wording
                    with self.assertRaisesRegex(AssertionError, 'loss-of-supply alarm'):
                        check.check_dom(row, dom, 'cc', expected)

    def test_manual_auto_and_local_checkpoints_both_views(self):
        row = check.ROWS['CCO-normal']
        for auto, local in itertools.product([False, True], repeat=2):
            for view in ['cc', 'physical']:
                dom, expected = fixture(row, view)
                expected.update({'oltc-1-auto': auto, 'rmu-1-remote': not local})
                next(d for d in dom['devices'] if d['device'] == 'T1')['text'] = 'Tap 0 · GOOD · ' + ('AUTO' if auto else 'MANUAL')
                dom['authorities'] = [{'point': 'rmu-1-remote', 'text': 'RMU1 authority: LOCAL'}] if local else []
                add_presentation(dom, expected)
                self.assertEqual(dom['abnormalStrip']['offNormalCount'], str(int(not auto) + int(local)))
                check.check_dom(row, dom, view, expected)

    def test_capture_serializes_strip_order_counts_and_off_normal(self):
        # Exercise the real evaluate callback without Chrome or a second capture implementation.
        script = r'''
import assert from 'node:assert/strict';
import { captureDom } from './frontend/e2e/eu-state-dom.mjs';
const element = (dataset = {}, value = '', localName = 'span') => ({
  nodeType: 1, localName, dataset,
  childNodes: [{nodeType: 3, nodeValue: value}],
  getAttribute: name => dataset[name.replace(/^data-/, '').replace(/-([a-z])/g, (_, c) => c.toUpperCase())] ?? null,
  getClientRects: () => [{}],
});
const device = element({device: 'CB1', offNormal: 'true', position: 'open', quality: 'good'}, 'OPEN · GOOD');
device.childNodes.push(element({}, 'IEC tooltip must not be visible plant text', 'title'));
const condition = element({condition: 'S1-no-supply', conditionKind: 'alarm'}, 'S1 residential: loss of supply');
const strip = element({alarmCount: '1', offNormalCount: '1'}, '1 alarm · 1 off-normal');
strip.compareDocumentPosition = () => 4;
const schematic = element();
const root = element({cached: 'false'});
root.querySelector = selector => ({
  '[aria-label="Abnormal conditions"]': strip, '[aria-label="Feeder schematic"]': schematic,
})[selector];
root.querySelectorAll = selector => ({
  '[data-device]': [device], '[aria-label="Abnormal conditions"] [data-condition]': [condition],
})[selector] ?? [];
globalThis.document = {querySelector: () => element({viewState: 'ok'})};
globalThis.getComputedStyle = () => ({visibility: 'visible'});
globalThis.location = {href: 'offline:test'};
let dom = captureDom(root, 'cc');
assert.deepEqual(dom.abnormalStrip, {visible: true, beforeSchematic: true, alarmCount: '1', offNormalCount: '1', text: '1 alarm · 1 off-normal'});
assert.equal(dom.devices[0].offNormal, 'true');
assert.equal(dom.devices[0].text, 'OPEN · GOOD');
assert.deepEqual(dom.conditions, [{id: condition.dataset.condition, kind: 'alarm', text: 'S1 residential: loss of supply'}]);
strip.compareDocumentPosition = () => 2;
assert.equal(captureDom(root, 'cc').abnormalStrip.beforeSchematic, false);
root.querySelector = () => null;
assert.equal(captureDom(root, 'physical').abnormalStrip, null);
'''
        result = subprocess.run(['node', '--input-type=module', '-e', script], cwd=ROOT,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_oracle_fault_uses_plant_vocabulary(self):
        # data_fixture copies the oracle into the plant, so it cannot catch a
        # spelling the live process API never reports (eu_controls.py upper-cases).
        for row in check.ORACLE['rows']:
            with self.subTest(row=row['id']):
                self.assertIn(row.get('fault', 'none'), {'none', 'S1', 'S2'})

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
                                              for id, v in check.comparison_values(expected).items()]
                    dom['observationUncertainty'] = str(sum(v is None for v in expected.values()))
                    check.check_dom(row, dom, view, expected)

    def test_data_mutations_fail(self):
        row = check.ROWS['CCO-normal']
        mutations = [
            lambda f, o: f['process'].update(truth_stale=True),
            lambda f, o: f['process'].update(solve_seq=1),
            lambda f, o: f['process'].update(solver_epoch='new'),
            lambda f, o: f['process'].update(f2_tripped=True),
            lambda f, o: f['process'].update(f2_tripped=None),
            lambda f, o: f['devices']['f2-breaker'].update(position='open'),
            lambda f, o: f['devices']['f2-breaker'].update(protection_tripped=True),
            lambda f, o: f['devices']['f2-breaker'].update(lockout=True),
            lambda f, o: f['devices']['f2-breaker']['measurements'].update(current_a=0),
            lambda f, o: f['devices']['f2-breaker']['measurements'].update(p_mw=0),
            lambda f, o: f['devices']['f2-breaker']['measurements'].update(valid=False),
            lambda f, o: f['process']['devices']['f2-breaker'].update(solved_revision=0),
            lambda f, o: f['process']['devices']['rmu-1'].update(solved_revision=0),
            lambda f, o: f['devices']['rmu-1']['measurements'].update(valid=False),
            lambda f, o: f['devices']['breaker-1']['measurements'].update(voltage_kv_ll=0),
            lambda f, o: f['devices']['rmu-1']['measurements'].update(current_a=0),
            lambda f, o: f['devices']['rmu-1'].update(position='open'),
            lambda f, o: o['points'][0].update(value='open'),
            lambda f, o: o['points'][0]['quality'].update(iv=True),
            lambda f, o: o['points'][0].update(received_at='2026-10-18T17:00:00Z'),
            lambda f, o: o['points'].pop(),
        ]
        for i, mutation in enumerate(mutations):
            with self.subTest(mutation=i):
                f, o, before, since = data_fixture(row)
                mutation(f, o)
                with self.assertRaises(AssertionError):
                    check.check_data(row, f, o, before, since)
        for id in ['f2-position', 'f2-protection-trip', 'f2-current', 'f2-p-mw']:
            for change in [{'quality': {'iv': True, 'nt': False, 'sb': False, 'bl': False, 'ov': False}},
                           {'received_at': '2026-10-18T17:00:00Z'},
                           {'value': 'open' if id == 'f2-position' else True if id == 'f2-protection-trip' else 100}]:
                with self.subTest(point=id, change=change):
                    f, o, before, since = data_fixture(row)
                    next(p for p in o['points'] if p['id'] == id).update(change)
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
                self.assertEqual(r['cc_paths'][i], check.PATHS[r['supply'][i]])
            self.assertEqual(len(r['currents_a']), 4)
            self.assertEqual(len(r['cc_paths']), 3)
            if r['f2'] == 'tripped':
                self.assertEqual((r['f2_current_a'], r['f2_p_mw']), (0, 0))

    def test_f2_inventory_and_wire_mapping(self):
        field, operator, _, _ = data_fixture(check.ROWS['CCO-normal'])
        self.assertEqual(len(field['devices']), 5)
        self.assertEqual(len(operator['points']), 42)
        protocol = json.loads((ROOT / 'lab-definitions/packages/eu-iec104-substation/protocols/points.json').read_text())
        points = {p['id']: p for p in protocol['points']}
        for id, ioa in [('f2-position', 1501), ('f2-protection-trip', 1502),
                        ('f2-current', 2501), ('f2-p-mw', 2502)]:
            self.assertEqual(points[id]['ioa'], ioa)
            self.assertEqual(points[id]['source']['device'], 'f2-breaker')

    def test_list_is_offline_and_complete(self):
        result = subprocess.run(['bash', str(ROOT / 'scripts/eu-state-smoke.sh'), '--list'], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        for r in check.ORACLE['rows'][:16]:
            name = 'CCC-normal-interlock' if r['id'] == 'CCC-normal' else r['id']
            self.assertIn(name + ':', result.stdout)
        self.assertIn('CCC-normal-interlock: interlock', result.stdout)
        self.assertIn('f2-cycle: supported', result.stdout)
        self.assertNotIn('CCC-normal: UNSUPPORTED', result.stdout)
        for name in ['S3-fault', 'PLC-setpoint', 'S1-pre-trip']:
            self.assertIn(name + ': UNSUPPORTED', result.stdout)

    def test_dom_cannot_be_disabled(self):
        result = subprocess.run(['bash', str(ROOT / 'scripts/eu-state-smoke.sh'), '--pending-dom'],
                                capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 2)
        self.assertIn('unrecognized arguments: --pending-dom', result.stderr)

    def test_cli_requires_both_dom_views(self):
        row = check.ROWS['CCO-tripped']
        field, operator, before, since = data_fixture(row)
        expected = check.check_data(row, field, operator, before, since)
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            snapshots = {'field.json': field, 'operator.json': operator, 'before-field.json': before}
            for view in ['cc', 'physical']:
                dom, _ = fixture(row, view)
                dom['observationUncertainty'] = str(sum(v is None for v in expected.values()))
                if view == 'physical':
                    dom['comparisons'] = [{'point': id, 'status': 'INVALID' if v is None else 'MATCH'}
                                          for id, v in check.comparison_values(expected).items()]
                snapshots[f'{view}-dom.json'] = dom
            for name, value in snapshots.items():
                (directory / name).write_text(json.dumps(value))
            (directory / 'since.txt').write_text(since)
            command = ['python3', str(ROOT / 'scripts/lib/eu-state-check.py'), row['id'], tmp]
            result = subprocess.run(command, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(json.loads(result.stdout)['dom'], 'PASS')
            for view in ['cc', 'physical']:
                dom = snapshots[f'{view}-dom.json']
                (directory / f'{view}-dom.json').write_text(json.dumps(dict(dom, viewState='degraded')))
                result = subprocess.run(command, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 1)
                self.assertIn('SCADA banner not ok', result.stdout)
                (directory / f'{view}-dom.json').write_text(json.dumps(dom))
            (directory / 'physical-dom.json').unlink()
            result = subprocess.run(command, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(json.loads(result.stdout)['result'], 'FAIL')

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
        def worker(case, directory, timeout):
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
             patch.object(sys, 'argv', ['smoke', '--case', 'switching']):
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
            result = run.worker('baseline', Path(tmp) / 'evidence', 1)
        self.assertEqual(result['exit_code'], 124)
        self.assertTrue(popen.call_args.kwargs['start_new_session'])
        kill.assert_called_once_with(43210, run.signal.SIGTERM)

    def test_static_baseline_fixture(self):
        _, expected = fixture(check.ROWS['CCO-normal'], 'cc')
        expected.update({
            'rmu-1-source-kv': 19.8, 'rmu-1-section-kv': 19.7, 'rmu-2-source-kv': 19.9,
            'f2-current': 11.25, 'f2-p-mw': .332494,
            'oltc-1-bus-kv': 20, 'feeder-voltage': 19.91, 'rmu-2-section-kv': 19.7,
            'oltc-1-current': 72.3, 'feeder-current': 61.4, 'rmu-1-current': 18.7, 'rmu-2-current': 0,
            'oltc-1-auto': False})
        # Raw fixture preserves the real SVG sibling concatenation, not spaces
        # hand-inserted between Tap/GOOD/MANUAL. The checker must stay strict.
        with self.assertRaisesRegex(AssertionError, 'cc T1: displayed mode'):
            check.check_dom(check.ROWS['CCO-normal'], FIXTURE, 'cc', expected)
        captured = subprocess.run(['node', str(Path(__file__).with_name('test_state_dom_capture.mjs'))],
                                  capture_output=True, text=True, timeout=10)
        self.assertEqual(captured.returncode, 0, captured.stdout + captured.stderr)
        dom = json.loads(captured.stdout)
        # The archived capture fixture predates off-normal hooks and the strip.
        # These fields are exercised against the real capture callback below.
        add_presentation(dom, expected)
        check.check_dom(check.ROWS['CCO-normal'], dom, 'cc', expected)
        auto_dom = copy.deepcopy(dom)
        next(d for d in auto_dom['devices'] if d['device'] == 'T1')['text'] = next(
            d['text'] for d in dom['devices'] if d['device'] == 'T1').replace('MANUAL', 'AUTO')
        auto_expected = dict(expected, **{'oltc-1-auto': True})
        add_presentation(auto_dom, auto_expected)
        check.check_dom(check.ROWS['CCO-normal'], auto_dom, 'cc', auto_expected)

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
                     lambda d: d['devices'][4].update(quality='unknown'),
                     lambda d: d.update(f2Telemetry='F2 indication only: NORMAL · I 0 A · P 0 MW'),
                     lambda d: d.update(f2Telemetry='F2 indication only: TRIPPED · I 20 A · P 0 MW'),
                     lambda d: d.update(f2Telemetry='F2 indication only: TRIPPED · I 0 A · P 1 MW'),
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

    def test_steady_banner_and_pf_uncertainty(self):
        row = check.ROWS['OOO-tripped']
        expected = check.check_data(row, *data_fixture(row))
        self.assertEqual(sum(v is None for v in expected.values()), 2)
        for view in ['cc', 'physical']:
            dom, _ = fixture(row, view)
            dom['observationUncertainty'] = '2'
            if view == 'physical':
                dom['comparisons'] = [{'point': id, 'status': 'INVALID' if v is None else 'MATCH'}
                                      for id, v in check.comparison_values(expected).items()]
            check.check_dom(row, dom, view, expected)
            for state in ['lost', 'recovering', 'degraded', 'not-established', 'unavailable', None]:
                with self.subTest(view=view, state=state), self.assertRaises(AssertionError):
                    check.check_dom(row, dict(dom, viewState=state), view, expected)
            if view == 'cc':
                with self.assertRaises(AssertionError):
                    check.check_dom(row, dict(dom, observationUncertainty='0'), view, expected)
                dom['conditions'] = [c for c in dom['conditions'] if c['id'] != 'f2-protection-trip']
                with self.assertRaises(AssertionError):
                    check.check_dom(row, dom, view, expected)

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
        dom['observationUncertainty'] = str(sum(v is None for v in expected.values()))
        pf_row = next(r for r in dom['comparisons'] if r['point'] == 'feeder-pf')
        pf_row['status'] = 'INVALID'
        check.check_dom(row, dom, 'physical', expected)
        pf_row['status'] = 'UNKNOWN'
        with self.assertRaises(AssertionError):
            check.check_dom(row, dom, 'physical', expected)


if __name__ == '__main__':
    unittest.main()
