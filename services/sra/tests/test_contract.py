"""Offline closure and image configuration checks alongside the runtime harness."""
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[3]
SRA = ROOT / 'services/sra'


class ContractTests(unittest.TestCase):
    def test_every_wrapper_declares_bounded_health_and_pinned_base(self):
        names = ('keycloak', 'guacamole', 'guacd', 'postgres', 'ziti-controller', 'ziti-router', 'ziti-host')
        for name in names:
            text = (ROOT / ('Dockerfile.sra-' + name)).read_text()
            for source in re.findall(r'^FROM (\S+)', text, re.M):
                self.assertRegex(source, r'^.+:[^@]+@sha256:[0-9a-f]{64}$')
            self.assertIn('HEALTHCHECK --interval=5s --timeout=3s', text)
            self.assertIn('--retries=24', text)
            self.assertIn('COPY services/sra/' + name + '/', text)

    def test_realm_has_complete_builtin_flows_and_required_otp_without_cookie(self):
        realm = json.loads((SRA / 'keycloak/realm.json').read_text())
        self.assertEqual(realm['keycloakVersion'], '26.8.2')
        flows = {f['alias']: f for f in realm['authenticationFlows']}
        self.assertTrue({'browser', 'clients', 'registration', 'reset credentials'} <= flows.keys())
        custom = flows[realm['browserFlow']]['authenticationExecutions']
        self.assertEqual([f['authenticator'] for f in custom], ['auth-username-password-form', 'auth-otp-form'])
        self.assertTrue(all(f['requirement'] == 'REQUIRED' for f in custom))
        self.assertEqual(realm['sslRequired'], 'none')
        self.assertEqual({u['username'] for u in realm['users']}, {'vendor', 'auditor', 'sra-admin'})
        self.assertEqual({g['name'] for g in realm['groups']}, {'sra-vendors', 'sra-auditors', 'sra-admins'})
        for user in realm['users']:
            self.assertEqual(user['requiredActions'], [])
            credential = next(c for c in user['credentials'] if c['type'] == 'otp')
            self.assertEqual(json.loads(credential['credentialData'])['secretEncoding'], 'BASE32')
            self.assertRegex(json.loads(credential['secretData'])['value'], r'^[A-Z2-7]+=*$')
        admin = next(u for u in realm['users'] if u['username'] == 'sra-admin')
        self.assertEqual(admin['groups'], ['/sra-admins'])

    def test_interrupted_seed_fails_closed_but_completed_seed_survives(self):
        # Use the actual marker functions with only the state root rewritten.
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            script = (SRA / 'common/state.sh').read_text().replace('/var/lib/sra', directory)
            result = subprocess.run(['sh', '-ec', script + '\nsra_check'], timeout=5)
            self.assertEqual(result.returncode, 0)
            (state / 'cleanup-complete').touch()
            result = subprocess.run(['sh', '-ec', script + '\nsra_check'], capture_output=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            (state / 'seed-complete').touch()
            result = subprocess.run(['sh', '-ec', script + '\nsra_check'], timeout=5)
            self.assertEqual(result.returncode, 0)

    def test_host_owns_enrollment_and_executes_tunneler_once(self):
        text = (SRA / 'ziti-host/entrypoint.sh').read_text()
        self.assertEqual(text.count('exec ziti-edge-tunnel run-host'), 1)
        self.assertIn('timeout 20 ziti-edge-tunnel enroll', text)
        self.assertIn('date +%s > /var/lib/sra/attempt-time', text)
        self.assertIn('delay=30', text)
        self.assertNotIn('/dev/net/tun', text)

    def test_host_health_rejects_stale_enrollment_and_missing_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            phase = root / 'phase'
            attempt = root / 'attempt-time'
            identity = root / 'ot-host.json'
            comm = root / 'comm'
            script = (SRA / 'ziti-host/health.sh').read_text()
            script = script.replace('/var/lib/sra', directory)
            script = script.replace('/ziti-edge-tunnel/ot-host.json', str(identity))
            script = script.replace('/proc/1/comm', str(comm))

            def healthy():
                return subprocess.run(['bash', '-c', script], capture_output=True,
                                      timeout=5).returncode == 0

            phase.write_text('enrolling\n')
            attempt.write_text(str(int(time.time())))
            self.assertTrue(healthy())
            attempt.write_text(str(int(time.time()) - 61))
            self.assertFalse(healthy())
            phase.write_text('running\n')
            comm.write_text('ziti-edge-tunne\n')
            self.assertFalse(healthy())
            identity.write_text('enrolled identity')
            self.assertTrue(healthy())
            comm.write_text('bash\n')
            self.assertFalse(healthy())
            phase.write_text('unknown\n')
            self.assertFalse(healthy())

    def test_vendor_addition_does_not_install_another_browser(self):
        text = (ROOT / 'Dockerfile.vendor-jump').read_text()
        self.assertIn('        oathtool \\', text)
        self.assertNotIn('firefox', text.lower())


if __name__ == '__main__':
    unittest.main()
