"""Role-specific browser checks, with no browser or network in unit tests."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import MagicMock, patch

SRA = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SRA / 'harness'))
spec = importlib.util.spec_from_file_location('sra_browser_checks', SRA / 'harness/browser_acceptance.py')
browser_checks = importlib.util.module_from_spec(spec)
with patch.object(Path, 'read_text', return_value='{"users": []}'):
    spec.loader.exec_module(browser_checks)


class BrowserRoleTests(unittest.TestCase):
    def test_auditor_requires_history_with_empty_recording_logs(self):
        for history, permitted in [
            ([{'logs': {}}], True),
            ([{'logs': {'recording': {}}}], False),
            ([], False),
            ([{}], False),
        ]:
            with self.subTest(history=history), patch.object(Path, 'read_text', return_value=json.dumps(history)):
                if permitted:
                    browser_checks.main('auditor-no-playback')
                else:
                    with self.assertRaises(AssertionError):
                        browser_checks.main('auditor-no-playback')

    def test_playback_logs_in_as_admin_and_uses_only_closed_history(self):
        browser = MagicMock()
        history = [
            {'identifier': 'open', 'endDate': None, 'logs': {'live': {}}},
            {'identifier': 'closed', 'endDate': 1, 'logs': {'recording': {}}},
        ]
        with patch.object(browser_checks, 'CDP', return_value=browser), \
                patch.object(browser_checks, 'clear_login') as clear, \
                patch.object(browser_checks, 'login', return_value={'authToken': 'admin-token'}) as login, \
                patch.object(browser_checks, 'api', return_value=history) as api, \
                contextlib.redirect_stdout(io.StringIO()):
            browser_checks.main('playback')
        clear.assert_called_once_with(browser)
        login.assert_called_once_with(browser, 'sra-admin')
        api.assert_called_once_with(browser, 'session/data/postgresql/history/connections', 'admin-token')
        browser.call.assert_called_once_with('Page.navigate', url=browser_checks.BASE + '#/settings/postgresql/recording/closed/recording')
        self.assertTrue(any('.guac-player-seek' in call.args[0] for call in browser.wait.call_args_list))
        self.assertTrue(any('.guac-player-play' in call.args[0] for call in browser.js.call_args_list))

    def test_playback_rejects_history_without_a_closed_recording(self):
        with patch.object(browser_checks, 'CDP'), patch.object(browser_checks, 'clear_login'), \
                patch.object(browser_checks, 'login', return_value={'authToken': 'admin-token'}), \
                patch.object(browser_checks, 'api', return_value=[{'endDate': None, 'logs': {'live': {}}}]):
            with self.assertRaises(AssertionError):
                browser_checks.main('playback')


if __name__ == '__main__':
    unittest.main()
