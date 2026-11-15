"""Fail-closed observer authorization and student-command oracles."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'harness'))
import observer_checks as observer


class ObserverTests(unittest.TestCase):
    def test_identity_is_non_admin_with_exact_permissions_and_no_overlay_role(self):
        identity = dict(id='observer-id', isAdmin=False, roleAttributes=[],
                        permissions=sorted(observer.PERMISSIONS))
        for mutation in ({}, {'isAdmin': True}, {'permissions': ['service.read']},
                         {'permissions': [*observer.PERMISSIONS, 'service.create']},
                         {'roleAttributes': ['sra-dialers']}):
            with self.subTest(mutation=mutation), \
                    patch.object(observer, 'docker', return_value=json.dumps(
                        {'data': [dict(identity, **mutation)]})):
                if mutation:
                    with self.assertRaises(AssertionError):
                        observer.observer_identity('controller')
                else:
                    self.assertEqual(observer.observer_identity('controller'), 'observer-id')

    def test_cli_retains_both_output_streams_and_runs_as_vendor_user(self):
        result = subprocess.CompletedProcess([], 0, 'table\n', 'diagnostic\n')
        with patch.object(observer.subprocess, 'run', return_value=result) as run, \
                contextlib.redirect_stdout(io.StringIO()) as evidence:
            self.assertEqual(observer.cli('vendor', 'list', 'identities'), 'table\n')
        record = json.loads(evidence.getvalue().removeprefix('STUDENT '))
        self.assertEqual((record['stdout'], record['stderr']), ('table\n', 'diagnostic\n'))
        command = run.call_args.args[0]
        self.assertEqual(command[:5], ['docker', 'exec', '-u', 'abc', '-e'])
        self.assertEqual(command[-4:], ['ziti', 'edge', 'list', 'identities'])
        self.assertEqual(run.call_args.kwargs['timeout'], 20)

    def test_write_requires_authorization_denial_not_just_nonzero_exit(self):
        for code, error, accepted in [
            (1, 'not authorized: please log in (401 Unauthorized)', True),
            (0, 'not authorized: please log in (401 Unauthorized)', False),
            (1, 'no identities found in CLI config', False),
            (1, 'connection refused', False),
        ]:
            with self.subTest(error=error, code=code), \
                    patch.object(observer.subprocess, 'run', return_value=
                                 subprocess.CompletedProcess([], code, '', error)), \
                    contextlib.redirect_stdout(io.StringIO()):
                if accepted:
                    observer.cli('vendor', 'delete', 'service-policy', 'sra-dial', refused=True)
                else:
                    with self.assertRaises(AssertionError):
                        observer.cli('vendor', 'delete', 'service-policy', 'sra-dial', refused=True)

    def test_write_is_followed_by_same_session_read_and_unchanged_policy_check(self):
        responses = ['', 'sra-dial sra-bind', 'sra-observer ot-host sra-router',
                     'sra-identities', 'OKAY : ot-host\nOKAY : sra-router',
                     'sra-service-routers', '{"data":[{"id":"dial"}]}', '',
                     '{"data":[{"id":"dial"}]}']
        with patch.object(observer, 'cli', side_effect=responses) as cli:
            observer.read_and_refuse('vendor')
        self.assertEqual(cli.call_args_list[-2].args,
                         ('vendor', 'delete', 'service-policy', 'sra-dial'))
        self.assertTrue(cli.call_args_list[-2].kwargs['refused'])
        self.assertEqual(cli.call_args_list[-1].args,
                         ('vendor', 'list', 'service-policies', '-j'))
        responses[-1] = '{"data":[]}'
        with patch.object(observer, 'cli', side_effect=responses):
            with self.assertRaises(AssertionError):
                observer.read_and_refuse('vendor')

    def test_controller_listener_inventory_rejects_extra_external_listener(self):
        sockets = [dict(address='::', port=1280, protocol='tcp'),
                   dict(address='127.0.0.11', port=45000, protocol='tcp'),
                   dict(address='::ffff:127.0.0.2', port=45001, protocol='udp')]
        observer.controller_listeners(sockets)
        for extra in (dict(address='::', port=8441, protocol='tcp'),
                      dict(address='0.0.0.0', port=1280, protocol='udp')):
            with self.subTest(extra=extra), self.assertRaises(AssertionError):
                observer.controller_listeners([*sockets, extra])
