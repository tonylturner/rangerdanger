#!/usr/bin/env python3
"""Freeze only the real PLC scan thread, at its unlocked sleep syscall.

Runs in an owned, network-less diagnostic container sharing the PLC PID
namespace, with SYS_PTRACE. No PLC code, registers or listeners are replaced.
The Modbus worker threads continue normally. Detach on termination; also bound
this fault to 20 seconds so a lost gate cannot leave the runtime suspended.
"""
import ctypes
import glob
import json
import os
import signal
import struct
import time

libc = ctypes.CDLL(None, use_errno=True)
libc.ptrace.restype = ctypes.c_long


def trace(request, pid, data=0):
    result = libc.ptrace(request, pid, None, ctypes.c_void_p(data))
    if result == -1:
        raise OSError(ctypes.get_errno(), os.strerror(ctypes.get_errno()))
    return result


def wait_stop(pid):
    waited, status = os.waitpid(pid, 0x40000000)  # __WALL: traced thread
    if waited != pid or not os.WIFSTOPPED(status):
        raise RuntimeError(f"scan thread did not stop: {waited}, {status}")


def main():
    pids = []
    for path in glob.glob('/proc/[0-9]*/comm'):
        try:
            with open(path) as comm:
                if comm.read().strip() == 'openplc':
                    pids.append(int(path.split('/')[2]))
        except FileNotFoundError:  # unrelated short-lived healthcheck exited
            continue
    if len(pids) != 1:
        raise RuntimeError(f"expected one real OpenPLC runtime: {pids}")
    pid = pids[0]
    attached = False

    def terminate(*_):
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, terminate)
    signal.signal(signal.SIGINT, terminate)
    signal.signal(signal.SIGALRM, terminate)
    signal.alarm(20)
    try:
        trace(0x4206, pid, 1)  # PTRACE_SEIZE, TRACESYSGOOD (no group SIGSTOP)
        attached = True
        trace(0x4207, pid)  # PTRACE_INTERRUPT: only the scan thread
        wait_stop(pid)
        # Stop at clock_nanosleep entry, after main.cpp unlocks bufferLock.
        # Interrupting at an arbitrary instruction could freeze the mutex and
        # would test device_unreachable, not heartbeat staleness.
        for _ in range(100):
            trace(24, pid)  # PTRACE_SYSCALL
            wait_stop(pid)
            info = ctypes.create_string_buffer(128)
            result = libc.ptrace(0x420E, pid, ctypes.c_void_p(128), ctypes.byref(info))
            if result == -1:
                raise OSError(ctypes.get_errno(), "PTRACE_GET_SYSCALL_INFO")
            arch = struct.unpack_from('I', info.raw, 4)[0]
            number = struct.unpack_from('Q', info.raw, 24)[0]
            # Inspect the tracee's kernel ABI, not the diagnostic image's ABI:
            # Docker Desktop can run an amd64 PLC on an arm64 kernel.
            syscall = {0xC000003E: 230, 0xC00000B7: 115}[arch]
            if info.raw[0] == 1:
                print(json.dumps({"event": "syscall_entry", "arch": arch,
                                  "number": number}), flush=True)
            if info.raw[0] == 1 and number == syscall:
                print(json.dumps({"event": "scan_frozen", "pid": pid,
                                  "syscall": syscall, "at": time.time()}), flush=True)
                open('/tmp/scan-frozen', 'w').write(str(pid))
                time.sleep(18)
                return
        raise RuntimeError("scan did not enter its unlocked clock_nanosleep")
    finally:
        if attached:
            trace(17, pid)  # PTRACE_DETACH resumes the real thread
            print(json.dumps({"event": "scan_resumed", "pid": pid}), flush=True)


if __name__ == '__main__':
    main()
