"""Offline management-plane probe contract tests; no Docker calls."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('management', ROOT / 'scripts/lib/eu-management-probes.py')
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


class ManagementProbes(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((ROOT / 'lab-definitions/packages/eu-iec104-substation/manifest.json').read_text())

    def test_policy_matrix(self):
        for service in self.manifest['services']:
            for policy in ('weak', 'improved'):
                rows = probe.targets(self.manifest, service, policy)
                engine = [r for r in rows if r['port'] == 8081]
                self.assertEqual(len(engine), 7)
                self.assertTrue(all(not r['expected'] for r in engine))
                self.assertTrue(all(r['expected'] == (policy == 'weak') for r in rows if r['port'] != 8081))

    def test_desktops_hard_check_containd(self):
        for key in ('vendor_jump', 'eng_workstation'):
            service = next(s for s in self.manifest['services'] if s['key'] == key)
            rows = probe.targets(self.manifest, service, 'improved')
            self.assertTrue(any(r['host'] == '10.99.99.2' and r['port'] == 8080 and not r['expected'] for r in rows))

    def test_fail_closed(self):
        expected = [dict(host='10.10.10.2', port=8081, expected=False)]
        for actual in ([], [dict(expected[0], error='no tool')], [dict(expected[0], host='wrong', open=False)]):
            with self.assertRaises(ValueError):
                probe.check_rows(expected, actual)
        self.assertFalse(probe.check_rows(expected, [dict(expected[0], open=True)])[0]['passed'])
        self.assertTrue(probe.check_rows(expected, [dict(expected[0], open=False)])[0]['passed'])

    def test_shipped_policy_flags(self):
        for name in ('weak', 'improved'):
            policy = json.loads((ROOT / f'lab-definitions/packages/eu-iec104-substation/firewall/{name}.json').read_text())
            for iface in policy['interfaces']:
                if iface['name'] == 'lan3':
                    self.assertEqual(iface['access'], dict.fromkeys(('mgmt', 'http', 'https', 'ssh'), True))
                elif name == 'improved':
                    self.assertEqual(iface['access'], dict.fromkeys(('mgmt', 'http', 'https', 'ssh'), False))
                else:
                    self.assertEqual(iface['access'], {})


if __name__ == '__main__':
    unittest.main()
