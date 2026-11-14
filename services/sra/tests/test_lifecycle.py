"""Offline tests for SRA lifecycle retention checks."""
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import unittest

HARNESS = Path(__file__).resolve().parents[1] / 'harness'
sys.path.insert(0, str(HARNESS))
MODULE = HARNESS / 'lifecycle_checks.py'
spec = importlib.util.spec_from_file_location('sra_lifecycle_checks', MODULE)
lifecycle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lifecycle)


def certificate(der):
    encoded = base64.b64encode(der).decode()
    return '-----BEGIN CERTIFICATE-----\n' + encoded + '\n-----END CERTIFICATE-----'


CERT_ONE = certificate(b'leaf-cert-one')
CERT_TWO = certificate(b'leaf-cert-two')
KEY_ONE = '-----BEGIN PRIVATE KEY-----\nprivate-key-one\n-----END PRIVATE KEY-----'


def identity(cert=CERT_ONE, key=KEY_ONE, ca='ca-one', metadata='metadata-one'):
    return json.dumps({
        'id': {'cert': cert, 'key': key, 'ca': ca},
        'ztAPI': metadata,
        'ztAPIs': [metadata],
        'other': metadata,
    })


class LifecycleTests(unittest.TestCase):
    def test_ca_and_metadata_changes_do_not_change_retained_credentials(self):
        before = lifecycle.host_credential_fingerprints(identity())
        after = lifecycle.host_credential_fingerprints(
            identity(ca='refreshed-ca', metadata='refreshed-controller-metadata'))

        self.assertEqual(before['leaf_certificate_sha256'], hashlib.sha256(b'leaf-cert-one').hexdigest())
        self.assertEqual(before, after)
        lifecycle.assert_same_host_credentials(before, after)

    def test_json_formatting_and_property_order_do_not_change_fingerprints(self):
        parsed = json.loads(identity())
        reformatted = json.dumps(parsed, indent=2, sort_keys=True)

        self.assertEqual(lifecycle.host_credential_fingerprints(identity()),
                         lifecycle.host_credential_fingerprints(reformatted))

    def test_leaf_certificate_change_fails_retention_comparison(self):
        before = lifecycle.host_credential_fingerprints(identity())
        after = lifecycle.host_credential_fingerprints(identity(cert=CERT_TWO))

        self.assertNotEqual(before['leaf_certificate_sha256'], after['leaf_certificate_sha256'])
        with self.assertRaises(AssertionError):
            lifecycle.assert_same_host_credentials(before, after)

    def test_private_key_change_fails_retention_comparison(self):
        before = lifecycle.host_credential_fingerprints(identity())
        after = lifecycle.host_credential_fingerprints(identity(key='different private key'))

        self.assertNotEqual(before['private_key_sha256'], after['private_key_sha256'])
        with self.assertRaises(AssertionError):
            lifecycle.assert_same_host_credentials(before, after)

    def test_fingerprint_uses_only_leaf_certificate_from_chain(self):
        before = lifecycle.host_credential_fingerprints(identity())
        with_chain = lifecycle.host_credential_fingerprints(
            identity(cert=CERT_ONE + '\n' + certificate(b'changed-ca-certificate')))

        self.assertEqual(before, with_chain)

    def test_missing_or_invalid_identity_credentials_are_rejected(self):
        invalid = (
            '{}',
            json.dumps({'id': {}}),
            json.dumps({'id': {'cert': CERT_ONE, 'key': ''}}),
            json.dumps({'id': {'cert': 'not PEM', 'key': KEY_ONE}}),
            '{invalid json',
        )
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises((AssertionError, json.JSONDecodeError)):
                    lifecycle.host_credential_fingerprints(value)

    def test_wrapper_marker_requires_running_run_host_and_tracks_attempts(self):
        before = lifecycle.host_run_marker(
            'running', '1763140000',
            'ziti-edge-tunnel\x00run-host\x00--identity\x00/ziti-edge-tunnel/ot-host.json')
        after = lifecycle.host_run_marker(
            'running', '1763140000',
            'ziti-edge-tunnel\x00run-host\x00--identity\x00/ziti-edge-tunnel/ot-host.json')
        retried = lifecycle.host_run_marker(
            'running', '1763140001',
            'ziti-edge-tunnel\x00run-host\x00--identity\x00/ziti-edge-tunnel/ot-host.json')

        self.assertEqual(before, after)
        self.assertNotEqual(before, retried)
        with self.assertRaises(AssertionError):
            lifecycle.host_run_marker('enrolling', '1763140002',
                                      'ziti-edge-tunnel\x00enroll')


if __name__ == '__main__':
    unittest.main()
