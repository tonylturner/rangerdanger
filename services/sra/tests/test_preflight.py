"""Offline tests for the disposable upstream-contract reproducer."""
import importlib.util
import contextlib
import io
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

MODULE = Path(__file__).resolve().parents[1] / 'harness/preflight.py'
spec = importlib.util.spec_from_file_location('sra_preflight', MODULE)
preflight = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preflight)


class PreflightTests(unittest.TestCase):
    def test_stderr_container_logs_are_retained(self):
        result = subprocess.CompletedProcess([], 0, 'stdout\n', 'stderr\n')
        with patch.object(preflight.subprocess, 'run', return_value=result):
            self.assertEqual(preflight.docker('logs', 'rdsra-b-test'), 'stdout\nstderr')

    def test_successful_exit_does_not_mean_ready(self):
        self.assertFalse(preflight.satisfies_readiness(
            {'Running': False, 'Status': 'exited', 'ExitCode': 0}))

    def test_failed_exit_does_not_mean_ready(self):
        self.assertFalse(preflight.satisfies_readiness(
            {'Running': False, 'Status': 'exited', 'ExitCode': 1}))

    def test_live_process_satisfies_process_contract(self):
        self.assertTrue(preflight.satisfies_readiness(
            {'Running': True, 'Status': 'running', 'ExitCode': 0}))

    def test_probe_is_pinned_and_isolated(self):
        for case in ('empty-directory', 'missing-identity'):
            args = preflight.command(case)
            self.assertEqual(args[args.index('--network') + 1], 'none')
            self.assertEqual(args[args.index('--name') + 1], 'rdsra-b-preflight-' + case)
            self.assertRegex(preflight.HOST_IMAGE, r'^openziti/ziti-host:1\.20\.0@sha256:[0-9a-f]{64}$')
            self.assertNotIn('-p', args)
            self.assertNotIn('-v', args)

    def test_create_collision_never_deletes_existing_container(self):
        with patch.object(preflight, 'docker', side_effect=RuntimeError('name in use')) as docker:
            with self.assertRaisesRegex(RuntimeError, 'name in use'):
                preflight.observe('empty-directory')
            self.assertEqual(docker.call_count, 1)
            self.assertEqual(docker.call_args.args[0], 'create')

    def test_owned_container_is_removed_on_observation_failure(self):
        with patch.object(preflight, 'docker', side_effect=['created', RuntimeError('start failed'), 'removed']) as docker:
            with self.assertRaisesRegex(RuntimeError, 'start failed'):
                preflight.observe('empty-directory')
            self.assertEqual(docker.call_args.args, ('rm', '-fv', 'rdsra-b-preflight-empty-directory'))

    def test_only_newly_pulled_image_is_removed(self):
        result = {'running': False, 'container_exit_code': 0}
        for preexisting in (True, False):
            with self.subTest(preexisting=preexisting):
                with patch.object(preflight, 'image_present', side_effect=[preexisting, True] if not preexisting else [True]), \
                        patch.object(preflight, 'observe', return_value=result), \
                        patch.object(preflight, 'docker', return_value='aarch64') as docker, \
                        contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(preflight.main(), 0)
                    removals = [c for c in docker.call_args_list if c.args[:2] == ('image', 'rm')]
                    self.assertEqual(len(removals), 0 if preexisting else 1)


if __name__ == '__main__':
    unittest.main()
