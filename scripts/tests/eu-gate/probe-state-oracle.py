"""Optional offline OpenDSS oracle re-probe; requires the process Python 3.13 venv.

No UI imports, device HTTP calls, range or protection timers. The ordinary
eu-gate suite remains stdlib-only; run this explicitly when the model changes.
"""
import json
from pathlib import Path
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'services/opendss-sim'))

from eu_circuit import EUFeederSolver
from eu_controls import ProcessControls
from eu_profile import DeviceProfile, EUProfile, ProfileSettings


def main():
    process = ROOT / 'lab-definitions/packages/eu-iec104-substation/process'
    config = json.loads((process / 'profile.json').read_text())
    config.pop('schema')
    config['circuit_file'] = process / 'feeder.dss'
    config['devices'] = tuple(DeviceProfile(**d) for d in config['devices'])
    config['profile'] = ProfileSettings(**config['profile'])
    oracle = json.loads((ROOT / 'scripts/lib/eu-state-oracle.json').read_text())
    for row in oracle['rows']:
        solver = EUFeederSolver(EUProfile(**config))
        solver.compile_circuit()
        truths = {
            name: {'position': 'closed' if switch == 'C' else 'open', 'earth_closed': False}
            for name, switch in zip(['feeder-breaker', 'rmu-1', 'rmu-2'], row['switches'])
        }
        truths['f2-breaker'] = {'position': 'open' if row['f2'] == 'tripped' else 'closed',
                                'protection_tripped': row['f2'] == 'tripped'}
        truths['oltc-1'] = {'tap': 0}
        result = solver.solve(truths, ProcessControls(
            fault_section=row.get('fault', 'none'), profile_enabled=False))
        assert result['converged'], row['id']
        for section, supply in zip(['s1', 's2', 's3'], row['supply']):
            assert result['buses'][section]['live'] == (supply != 'D'), (row['id'], section)
        for key, column in [('current_a', 'f2_current_a'), ('p_mw', 'f2_p_mw')]:
            actual = result['measurements']['f2-breaker'][key]
            assert abs(actual - row[column]) <= .001, (row['id'], key, actual, row[column])
    print(f"PASS: {len(oracle['rows'])} independent OpenDSS F2/section vectors")


if __name__ == '__main__':
    main()
