"""Ownership and failure-path cleanup for the disposable harness."""
import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

MODULE = Path(__file__).resolve().parents[1] / 'harness/resources.py'
spec = importlib.util.spec_from_file_location('sra_resources', MODULE)
resources = importlib.util.module_from_spec(spec)
spec.loader.exec_module(resources)


class ResourceTests(unittest.TestCase):
    def test_collision_does_not_transfer_ownership(self):
        owned = resources.Resources()
        with patch.object(resources, 'docker', side_effect=subprocess.CalledProcessError(1, 'create')):
            with self.assertRaises(subprocess.CalledProcessError):
                owned.start('test', 'image')
        self.assertEqual(owned.containers, [])

    def test_failed_start_is_owned_and_removed(self):
        owned = resources.Resources()
        with patch.object(resources, 'docker', side_effect=['id', subprocess.CalledProcessError(1, 'start'), '', '']) as docker:
            with contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(subprocess.CalledProcessError):
                    with owned:
                        owned.start('test', 'image')
            self.assertEqual(docker.call_args.args, ('rm', '-fv', 'rdsra-b-test'))

    def test_log_failure_does_not_prevent_cleanup(self):
        owned = resources.Resources()
        owned.containers = ['rdsra-b-test']
        with patch.object(resources, 'docker', side_effect=[subprocess.CalledProcessError(1, 'logs'), '']) as docker:
            with contextlib.redirect_stdout(io.StringIO()):
                owned.__exit__(RuntimeError, RuntimeError('test'), None)
            self.assertEqual(docker.call_args.args, ('rm', '-fv', 'rdsra-b-test'))


if __name__ == '__main__':
    unittest.main()
