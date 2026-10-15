#!/usr/bin/env bash
# Exercise the fault injector without tracing or signalling any host process.
set -eu
python3 -B - "$(dirname "${BASH_SOURCE[0]}")/freeze-plc-scan.py" <<'PY'
import ctypes
import importlib.util
import struct
import sys
import unittest
from unittest.mock import Mock, mock_open, patch

spec = importlib.util.spec_from_file_location('freeze_scan', sys.argv.pop())
scan = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scan)

class FreezeScanTests(unittest.TestCase):
    def setUp(self):
        self.trace = Mock()
        for target, value in [
            ('glob.glob', Mock(return_value=['/proc/66/comm'])),
            ('wait_stop', Mock()), ('trace', self.trace),
            ('time.sleep', Mock()), ('signal.signal', Mock()),
            ('signal.alarm', Mock()),
        ]:
            p = patch.object(scan, target, value) if '.' not in target else patch('freeze_scan.'+target, value)
            # dotted patches resolve the module from sys.modules below
            p.start()
            self.addCleanup(p.stop)
        p = patch('builtins.open', mock_open(read_data='openplc\n'))
        p.start()
        self.addCleanup(p.stop)
        self.libc = Mock()
        p = patch.object(scan, 'libc', self.libc)
        p.start()
        self.addCleanup(p.stop)
        def syscall_info(request, pid, size, info):
            data = bytearray(128)
            data[0] = 1
            struct.pack_into('I', data, 4, 0xC00000B7)
            struct.pack_into('Q', data, 24, 115)
            ctypes.memmove(info, bytes(data), len(data))
            return 80
        self.libc.ptrace.side_effect = syscall_info

    def test_only_scan_thread_and_detach(self):
        scan.main()
        self.assertEqual([c.args for c in self.trace.call_args_list],
                         [(0x4206,66,1),(0x4207,66),(24,66),(17,66)])
        scan.time.sleep.assert_called_once_with(18)

    def test_detach_after_syscall_info_failure(self):
        self.libc.ptrace.side_effect = OSError('unsupported')
        with self.assertRaises(OSError):
            scan.main()
        self.trace.assert_called_with(17,66)

    def test_never_detach_an_unowned_thread(self):
        self.trace.side_effect = OSError('seize denied')
        with self.assertRaises(OSError):
            scan.main()
        self.assertEqual(self.trace.call_count, 1)

    def test_no_ambiguous_runtime(self):
        scan.glob.glob.return_value = ['/proc/66/comm','/proc/67/comm']
        with self.assertRaises(RuntimeError):
            scan.main()
        self.trace.assert_not_called()

sys.modules['freeze_scan'] = scan
unittest.main()
PY
