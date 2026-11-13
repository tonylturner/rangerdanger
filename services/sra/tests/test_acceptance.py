"""Acceptance aggregation and the unchanged pending authorization contract."""
import contextlib
import hashlib
import io
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

SRA = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SRA / 'harness'))
from results import Results
import run as runner
import preflight
import resources


class AcceptanceTests(unittest.TestCase):
    def test_failure_does_not_hide_later_items_and_exit_remains_nonzero(self):
        results = Results()
        with contextlib.redirect_stdout(io.StringIO()) as output:
            results.run('playback', lambda: self.fail('authorization pending'))
            results.run('lifecycle', lambda: 'tested')
            self.assertEqual(results.finish(), 1)
        self.assertEqual([item['passed'] for item in results.items], [False, True])
        self.assertIn('FAIL playback', output.getvalue())
        self.assertIn('PASS lifecycle', output.getvalue())

    def test_missing_prerequisite_is_explicit_failure_not_success(self):
        results = Results()
        with contextlib.redirect_stdout(io.StringIO()):
            results.run('dependent', lambda: results.require('missing'))
        self.assertFalse(results.items[0]['passed'])
        self.assertIn('cannot execute', results.items[0]['detail'])

    def test_timeout_with_byte_stderr_does_not_hide_later_items(self):
        results = Results()
        def timeout():
            raise subprocess.TimeoutExpired('build', 900, stderr=b'emulator \xff timeout')
        with contextlib.redirect_stdout(io.StringIO()):
            results.run('build', timeout)
            results.run('later-build', lambda: 'tested')
            self.assertEqual(results.finish(), 1)
        self.assertEqual([item['passed'] for item in results.items], [False, True])
        self.assertIn('emulator', results.items[0]['detail'])

    def test_cleanup_timeout_is_reported_after_remaining_resources_are_removed(self):
        owned = resources.Resources()
        owned.volumes = ['rdsra-b-one', 'rdsra-b-two']
        owned.images = ['rdsra-b-image']
        with patch.object(resources, 'docker', side_effect=[subprocess.TimeoutExpired('rm', 45), '', '']) as docker, \
                contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(RuntimeError, 'cleanup failed'):
                owned.__exit__(None, None, None)
            self.assertEqual(docker.call_count, 3)
            self.assertEqual(docker.call_args.args, ('image', 'rm', 'rdsra-b-image'))

    def test_playback_failure_still_builds_each_amd64_image_once_and_cleans(self):
        def native(results, owned):
            results.run('auditor-playback', lambda: self.fail('authorization pending'))
            results.run('later-lifecycle', lambda: 'tested')
        with patch.object(runner, 'Resources') as factory, \
                patch.object(runner, 'docker', return_value='aarch64'), \
                patch.object(runner, 'run_native', side_effect=native), \
                patch.object(preflight, 'main', return_value=0), \
                patch.object(sys, 'argv', ['run.py']), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(), 1)
            calls = factory.return_value.build.call_args_list
            amd64 = [call for call in calls if len(call.args) == 3 and call.args[2] == 'amd64']
            self.assertEqual([call.args[0] for call in amd64], [*runner.NAMES, 'vendor'])
            factory.return_value.__exit__.assert_called_once_with(None, None, None)

    def test_build_timeout_records_duration_without_retry(self):
        owned = resources.Resources()
        with patch.object(resources, 'docker', side_effect=['', subprocess.TimeoutExpired('build', 900)]) as docker:
            with self.assertRaises(subprocess.TimeoutExpired):
                owned.build('keycloak', arch='amd64')
        self.assertIn('rdsra-b-keycloak-amd64', owned.timings)
        self.assertEqual(docker.call_count, 2)

    def test_seed_and_playback_assertions_remain_exactly_unchanged(self):
        text = (SRA / 'harness/browser_acceptance.py').read_text()
        block = text[text.index('        recordings = [h for h in history'):text.index('\n\n\nif __name__')]
        self.assertEqual(hashlib.sha256(block.encode()).hexdigest(),
                         '37ebcb3b904b7a29d0c844e6d05067a5f72cec5f862a51119495efdb22809120')
        self.assertEqual(hashlib.sha256((SRA / 'postgres/seed.sql').read_bytes()).hexdigest(),
                         '45086e69538bc54f46d868812a970b4212cb16eaf23f222964ade6fea6f13bb0')


if __name__ == '__main__':
    unittest.main()
