#!/usr/bin/env python3
"""Independent electrical/telemetry and serialized DOM assertions. No UI imports."""
import argparse
import datetime as dt
import json
import math
import re
from pathlib import Path

ORACLE = json.loads(Path(__file__).with_name('eu-state-oracle.json').read_text())
ROWS = {r['id']: r for r in ORACLE['rows']}
DEVICES = ['breaker-1', 'rmu-1', 'rmu-2', 'oltc-1', 'f2-breaker']
SECTIONS = ['S1', 'S2', 'S3']
VOLTAGES = ['rmu-1-source-kv', 'rmu-1-section-kv', 'rmu-2-source-kv']
PATHS = {'D': 'no supply', 'B': 'via CB1', 'F': 'via F2', 'BF': 'via CB1 + F2'}


def require(ok, message):
    if not ok:
        raise AssertionError(message)


def near(actual, expected, tolerance, label):
    require(isinstance(actual, (int, float)) and math.isfinite(actual)
            and abs(actual - expected) <= tolerance + 1e-6,
            f'{label}: {actual!r} != {expected!r} ±{tolerance}')


def timestamp(value):
    return dt.datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


def point_values(field):
    """Wire point inventory from protocol/device register definitions, not UI adapters."""
    values = {}
    for prefix, name in zip(['feeder', 'rmu-1', 'rmu-2', 'oltc-1'], DEVICES):
        d = field['devices'][name]
        if prefix != 'oltc-1':
            values[prefix + ('-breaker-position' if prefix == 'feeder' else '-position')] = d['position']
        for suffix, key in [('current', 'current_a'), ('voltage' if prefix == 'feeder' else 'bus-kv' if prefix == 'oltc-1' else 'section-kv', 'voltage_kv_ll')]:
            values[f'{prefix}-{suffix}'] = d['measurements'][key]
        if prefix.startswith('rmu'):
            values[f'{prefix}-source-kv'] = d['measurements']['source_kv_ll']
            values[f'{prefix}-remote'] = d['remote_enabled']
            values[f'{prefix}-earth'] = d['earth_closed']
            values[f'{prefix}-fault-passage'] = d['fault_passage']
        else:
            for suffix, key in [('p-mw', 'p_mw'), ('q-mvar', 'q_mvar'), ('pf', 'pf')]:
                values[f'{prefix}-{suffix}'] = d['measurements'][key]
    d = field['devices']['oltc-1']
    f2 = field['devices']['f2-breaker']
    values.update({'feeder-protection-trip': field['devices']['breaker-1']['protection_tripped'],
                   'f2-position': f2['position'], 'f2-protection-trip': f2['protection_tripped'],
                   'f2-current': f2['measurements']['current_a'], 'f2-p-mw': f2['measurements']['p_mw'],
                   'oltc-1-tap': {'value': d['tap'], 'transient': d['moving']},
                   'oltc-1-remote': d['remote_enabled'], 'oltc-1-auto': d['auto'],
                   'oltc-1-at-lower': d['tap'] == -8, 'oltc-1-at-upper': d['tap'] == 8,
                   'oltc-1-blocked': d['blocked'] or d['protection']['picked_up'],
                   'oltc-1-overcurrent-block': d['protection']['picked_up']})
    return values


def good(point):
    q = point['quality']
    return (point.get('value') is not None and all(q[k] is False for k in ['iv', 'nt', 'sb', 'bl'])
            and q['ov'] in (None, False) and not point.get('stale_reason'))


def check_interlock(case, before_field, before_operator, field, operator, command):
    require(command['point'] == case['point'] and command['operation'] == case['operation'],
            'interlock: wrong command attempt')
    require(command['result'] == 'rejected' and 'negative ACT-CON' in (command['reason'] or '')
            and command['con_at'] is not None and command['term_at'] is None
            and command['feedback'] is None, 'interlock: expected negative ACT-CON, not unknown/completed')
    before_points = {p['id']: p for p in before_operator['points']}
    points = {p['id']: p for p in operator['points']}
    for name, id in zip(DEVICES[:3] + ['f2-breaker'], ['feeder-breaker-position', 'rmu-1-position', 'rmu-2-position', 'f2-position']):
        before, after = before_field['devices'][name], field['devices'][name]
        require(all(before[k] == after[k] for k in ['generation', 'revision', 'position']),
                f'interlock: {name} moved or changed revision/generation')
        require(good(before_points[id]) and good(points[id])
                and before_points[id]['value'] == points[id]['value'] == after['position'],
                f'interlock: {id} changed or is not GOOD')
    for section, id in zip(SECTIONS, VOLTAGES):
        before = before_field['process']['buses'][section.lower()]
        after = field['process']['buses'][section.lower()]
        require(before['live'] == after['live']
                and (before['kv'] >= ORACLE['live_kv']) == (after['kv'] >= ORACLE['live_kv']) == after['live'],
                f'interlock: plant {section} changed state')
        require(good(before_points[id]) and good(points[id])
                and (before_points[id]['value'] >= ORACLE['live_kv'])
                == (points[id]['value'] >= ORACLE['live_kv']) == after['live'],
                f'interlock: CC {section} changed state or is not GOOD')


def check_data(row, field, operator, before, since, tap=0, auto=False, local=False):
    p = field['process']
    require(p['truth_stale'] is False, 'plant truth stale/unknown')
    require(p['solver_epoch'] == before['process']['solver_epoch'] and p['solve_seq'] > before['process']['solve_seq'], 'solver epoch changed or solve_seq did not advance')
    require(timestamp(p['solved_at']) >= timestamp(since), 'solve predates checkpoint')
    require(p['f2_tripped'] == (row['f2'] == 'tripped'), 'plant F2 state')
    f2 = field['devices']['f2-breaker']
    tripped = row['f2'] == 'tripped'
    require(f2['position'] == ('open' if tripped else 'closed')
            and f2['protection_tripped'] is tripped and f2['lockout'] is tripped,
            'plant F2 position/trip/lockout')
    near(f2['measurements']['current_a'], row['f2_current_a'],
         ORACLE['current_zero_a'] if row['f2_current_a'] == 0 else max(2, row['f2_current_a'] * .1), 'F2 current')
    near(f2['measurements']['p_mw'], row['f2_p_mw'], max(.05, abs(row['f2_p_mw']) * .1), 'F2 MW')
    require(p['fault_section'] == row.get('fault', 'none'), 'plant fault')
    bus20 = p['buses']['bus20']
    require(bus20['live'] is True and 18 <= bus20['kv'] <= 22, 'plant bus20/T1 must remain energized')
    near(field['devices']['oltc-1']['measurements']['voltage_kv_ll'], bus20['kv'], .1, 'T1 bus20 voltage')
    near(field['devices']['breaker-1']['measurements']['source_kv_ll'], bus20['kv'], .1, 'CB1 upstream voltage')
    cb = field['devices']['breaker-1']
    require(cb['lockout'] == row.get('lockout', False) and cb['protection_tripped'] == row.get('lockout', False), 'plant trip/lockout')
    for name, switch in zip(DEVICES, row['switches']):
        d = field['devices'][name]
        require(d['position'] == ('closed' if switch == 'C' else 'open'), f'plant {name} position')
    for name in DEVICES:
        d = field['devices'][name]
        m = d['measurements']
        delivery = p['devices'][d['device_id']]
        require(m['valid'] and delivery['delivered'] and delivery['solved_revision'] == d['revision'], f'{name}: invalid/undelivered revision')
        require(m['solver_epoch'] == p['solver_epoch'] and m['solve_seq'] > before['process']['solve_seq'], f'{name}: old measurement solve')
        require(timestamp(m['received_at']) >= timestamp(since), f'{name}: old measurement receipt')
        require(d['remote_enabled'] == (not local if name == 'rmu-1' else True), f'{name}: authority')
        if name.startswith('rmu'):
            require(not d['earth_closed'], f'{name}: earth closed')
    t = field['devices']['oltc-1']
    require(t['tap'] == tap and not t['moving'] and t['auto'] == auto, 'plant tap/motion/mode')
    for i, (section, supply) in enumerate(zip(SECTIONS, row['supply'])):
        bus = p['buses'][section.lower()]
        require(bus['live'] == (supply != 'D'), f'plant {section} live')
        kv = bus['kv']
        if supply == 'D':
            require(0 <= kv < ORACLE['live_kv'], f'{section}: dead bus kV {kv}')
        elif 'section_kv' in row:
            near(kv, row['section_kv'][i], 0.2, section)
        else:
            require(18 <= kv <= 22, f'{section}: live kV {kv}')
        d = field['devices']['rmu-1' if i < 2 else 'rmu-2']['measurements']
        near(d['source_kv_ll' if i != 1 else 'voltage_kv_ll'], kv, 0.1, f'{section} device/solver voltage')
    near(field['devices']['rmu-2']['measurements']['voltage_kv_ll'], p['buses']['s2']['kv'], 0.1, 'RMU2 S2 voltage')
    # Numbers are illustrative solver probes. Assert topology zeros separately;
    # allow 10% or 2 A on nonzero currents (load flow, not reporting tolerance).
    for name, amps in zip(DEVICES, row['currents_a']):
        actual = field['devices'][name]['measurements']['current_a']
        near(actual, amps, ORACLE['current_zero_a'] if amps == 0 else max(2, amps * .1), name + ' current')
    vt = field['devices']['breaker-1']['measurements']['voltage_kv_ll']
    near(vt, row['vt_kv'], .1 if row['vt_kv'] == 0 else .7, 'VT downstream of CB1')
    require(p['profile']['enabled'] is False and p['load_multiplier'] == 1 and p['source_pu'] == 1, 'reference profile/load/source')
    require(operator['link']['state'] == 'started' and operator['link']['gi']['state'] == 'complete', 'CC link/GI')
    points = {v['id']: v for v in operator['points']}
    expected = point_values(field)
    plc = {'plc-1-running': True, 'plc-1-input-valid': True, 'plc-1-setpoint-kv': 20.4,
           'plc-1-deadband-pct': 1, 'plc-1-delay-s': 30}
    require(len(operator['points']) == ORACLE['point_count']
            and set(points) == set(expected) | set(plc), 'CC 42-point inventory incomplete/duplicate')
    for id, value in expected.items():
        point = points[id]
        require(timestamp(point['received_at']) >= timestamp(since), f'{id}: retained CC receipt')
        if value is None:
            # Quiet NaN PF at zero power is explicitly IV, not communications loss.
            require(id.endswith('-pf') and point['quality']['iv'] and not point['quality']['nt']
                    and all(point['quality'][k] is False for k in ['sb', 'bl', 'ov'])
                    and point['stale_reason'] == 'measurement_invalid', f'{id}: expected zero-power IV')
        else:
            require(good(point), f'{id}: CC not GOOD')
            if point['kind'] == 'float':
                near(point['value'], value, point['reporting']['deadband'], id + ' reporting deadband')
            else:
                require(point['value'] == value, f'{id}: CC {point["value"]!r} != plant {value!r}')
    for id, value in plc.items():
        require(good(points[id]) and timestamp(points[id]['received_at']) >= timestamp(since), f'{id}: not fresh GOOD')
        near(points[id]['value'], value, .001, id)
    return expected


def check_dom(row, dom, view, expected, tap=0):
    require(dom['view'] == view and dom['visible'] and not dom['cached'], f'{view}: missing/hidden/cached root')
    require(dom['viewState'] == 'ok', f'{view}: SCADA banner not ok')
    require(len(dom['sections']) == 3, f'{view}: section cardinality')
    sections = {v['section']: v for v in dom['sections']}
    for i, (id, supply) in enumerate(zip(SECTIONS, row['supply'])):
        section = sections[id]
        state = 'DEAD' if supply == 'D' else 'LIVE'
        path = row['cc_paths'][i] if view == 'cc' else PATHS[supply]
        require(section['state'] == state and section['path'] == path, f'{view} {id}: state/path {section}')
        kv = float(section['kv'])
        near(kv, expected[VOLTAGES[i]], .1, f'{view} {id} DOM kV')
        require(0 <= kv < .1 if state == 'DEAD' else kv >= .1, f'{view} {id}: kV/live contradiction')
        require(state in section['text'] and path in section['text'], f'{view} {id}: visible state/path text')
    require(len(dom['devices']) == 5, f'{view}: device cardinality')
    devices = {v['device']: v for v in dom['devices']}
    for device, switch in zip(['CB1', 'RMU1', 'RMU2'], row['switches']):
        require(devices[device]['position'] == ('closed' if switch == 'C' else 'open') and devices[device]['quality'] == 'good', f'{view} {device}: position/quality')
    require(devices['T1']['quality'] == 'good' and devices['T1']['tap'] == str(tap), f'{view} T1: tap/quality')
    if 'oltc-1-auto' in expected:
        require(re.search(r'\b' + ('AUTO' if expected['oltc-1-auto'] else 'MANUAL') + r'\b', devices['T1']['text']), f'{view} T1: displayed mode')
    require(devices['F2']['position'] == ('open' if row['f2'] == 'tripped' else 'closed')
            and devices['F2']['quality'] == 'good', f'{view} F2 position/quality')
    require(dom['f2Line'] == ('DEAD' if row['supply'][2] == 'D' else 'LIVE'), f'{view} F2 lower-run state')
    telemetry = dom['f2Telemetry']
    require(('TRIPPED' if row['f2'] == 'tripped' else 'NORMAL') in telemetry, f'{view}: F2 trip text')
    for id, unit, tolerance in [('f2-current', 'A', 2.05), ('f2-p-mw', 'MW', .055)]:
        numbers = re.findall(r'(-?\d+(?:\.\d+)?)\s*' + unit + r'\b', telemetry)
        require(bool(numbers), f'{view}: F2 {unit} reading absent')
        near(float(numbers[-1]), expected[id], tolerance, f'{view}: displayed {id}')
    measurements = {v['point']: v for v in dom['measurements']}
    for id in ['oltc-1-current', 'oltc-1-bus-kv', 'feeder-current', 'feeder-voltage', 'rmu-1-current', 'rmu-2-current', 'rmu-2-section-kv']:
        require(id in measurements and measurements[id]['visible'] and measurements[id]['text'].strip(), f'{view} missing visible measurement {id}')
        voltage = id.endswith('-kv') or id == 'feeder-voltage'
        # Group text includes its fixed label; the last number+unit is the
        # displayed reading (not e.g. the "20 kV busbar" label).
        numbers = re.findall(r'(-?\d+(?:\.\d+)?)\s*' + ('kV' if voltage else 'A') + r'\b', measurements[id]['text'])
        require(bool(numbers), f'{view} {id}: missing numeric reading/unit')
        near(float(numbers[-1]), expected[id], .105 if voltage else 2.05, f'{view} {id}: displayed value')
        if voltage:
            require(measurements[id]['state'] == ('LIVE' if expected[id] >= .1 else 'DEAD'), f'{view} {id}: measurement state')
    if expected.get('rmu-1-remote') is False:
        require(any(a['point'] == 'rmu-1-remote' and 'LOCAL' in a['text'] for a in dom['authorities']), f'{view}: RMU1 LOCAL indication absent')
    if view == 'physical' and expected.get('feeder-protection-trip'):
        require('Protection TRIPPED' in dom['text'], 'physical: asserted trip indication absent')
    if view == 'physical' and row.get('lockout'):
        require('lockout' in dom['text'].lower(), 'physical lockout indication absent')
    if view == 'cc':
        require(dom['observationUncertainty'] == str(sum(v is None for v in expected.values())),
                'CC observation uncertainty differs from expected measurement_invalid PF count')
        # IDs intentionally unspecified by the DOM contract; verify visible wording.
        conditions = dom['conditions']
        require(conditions and all(v['id'] and v['text'] for v in conditions), 'CC summary missing stable condition IDs/text')
        text = ' '.join(v['text'].lower() for v in conditions)
        if expected.get('feeder-protection-trip'):
            require(any('protection trip' in v['text'].lower() and 'asserted' in v['text'].lower() for v in conditions), 'CC asserted trip summary absent')
        if row['f2'] == 'tripped':
            require(any(v['id'] == 'f2-protection-trip' and 'asserted' in v['text'].lower() for v in conditions),
                    'CC F2 asserted trip summary absent')
        abnormal = False
        for name, switch, normal in zip(['CB1', 'RMU1', 'RMU2'], row['switches'], 'CCO'):
            if switch != normal:
                abnormal = True
                require(name.lower() in text and ('open' if switch == 'O' else 'closed') in text, f'CC missing off-normal {name}')
        for section, supply in zip(SECTIONS, row['supply']):
            if supply == 'D':
                abnormal = True
                require(section.lower() in text and 'no supply' in text, f'CC missing no-supply {section}')
        require(not abnormal or all(v['id'] != 'none' for v in conditions), 'CC incorrectly says no abnormal conditions')
    else:
        rows = dom['comparisons']
        comparable = {id: v for id, v in expected.items() if id not in ['oltc-1-at-lower', 'oltc-1-at-upper']}
        require(len(rows) == len(comparable) and {r['point'] for r in rows} == set(comparable), 'comparison inventory incomplete/duplicate')
        for r in rows:
            status = 'INVALID' if expected[r['point']] is None else 'MATCH'
            require(r['status'] == status, f'comparison {r["point"]}: {r["status"]} != {status}')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('row', choices=ROWS)
    ap.add_argument('directory', type=Path)
    ap.add_argument('--tap', type=int, default=0)
    ap.add_argument('--auto', action='store_true')
    ap.add_argument('--local', action='store_true')
    ap.add_argument('--interlock', choices=ORACLE['interlocks'])
    ap.add_argument('--cleared', action='store_true', help='isolated S1 fault cleared; electrical row unchanged, lockout retained')
    args = ap.parse_args()
    read = lambda name: json.loads((args.directory / name).read_text())
    row = dict(ROWS[args.row])
    if args.cleared:
        require(args.row == 'S1-isolated', '--cleared requires the isolated S1 electrical row')
        row['fault'] = 'none'
    try:
        expected = check_data(row, read('field.json'), read('operator.json'), read('before-field.json'), (args.directory / 'since.txt').read_text().strip(), args.tap, args.auto, args.local)
        if args.interlock:
            case = ORACLE['interlocks'][args.interlock]
            require(args.row == case['predecessor'], 'interlock: wrong predecessor oracle')
            parent = args.directory.parent
            check_interlock(case, json.loads((parent / 'predecessor/field.json').read_text()),
                            json.loads((parent / 'predecessor/operator.json').read_text()),
                            read('field.json'), read('operator.json'),
                            json.loads((parent / 'interlock-command.json').read_text()))
        for view in ['cc', 'physical']:
            check_dom(row, read(f'{view}-dom.json'), view, expected, args.tap)
        result = {'plant': 'PASS', 'cc': 'PASS', 'dom': 'PASS'}
        if args.interlock:
            result['interlock'] = 'PASS: negative ACT-CON; plant/GOOD CC positions and section states unchanged'
    except (AssertionError, KeyError, ValueError, TypeError, OSError) as error:
        result = {'result': 'FAIL', 'error': str(error)}
        print(json.dumps(result))
        raise SystemExit(1)
    print(json.dumps(result))


if __name__ == '__main__':
    main()
