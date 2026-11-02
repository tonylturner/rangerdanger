"""Synthetic mixed-hop G22 fixtures; no live network or protocol library."""

import importlib.util
from pathlib import Path
import socket
import struct
import sys

spec = importlib.util.spec_from_file_location(
    "iec_fixture", Path(__file__).with_name("make_iec104_pcap.py"))
iec_fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(iec_fixture)
frame = iec_fixture.frame


def modbus(unit=1, address=0, value=1, transaction=7):
    return struct.pack("!HHHBBHH", transaction, 0, 6, unit, 6, address, value)


def modbus_frame(payload, reply=False, wrong_source=False):
    src, dst = ("10.30.30.20", "10.40.40.20")
    sport, dport = 41000, 502
    if reply:
        src, dst, sport, dport = dst, src, dport, sport
    if wrong_source:
        src = "10.20.20.10"
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 40 + len(payload), 3, 0,
                     64, 6, 0, socket.inet_aton(src), socket.inet_aton(dst))
    tcp = struct.pack("!HHIIBBHHH", sport, dport, 1000, 0, 0x50, 0x18, 65535, 0, 0)
    return bytes.fromhex("0000000000020000000000010800") + ip + tcp + payload


def generate(path, case):
    source = "10.60.60.20" if case == "wrong-source" else "10.10.10.50"
    ca = 2 if case == "wrong-ca" else 1
    ioa = 3002 if case == "wrong-ioa" else 3001
    select = f"46/6/{ca}/{ioa}/{'82' if case == 'close' else '81'}"
    execute = f"46/6/{ca}/{ioa}/{'02' if case == 'close' else '01'}"
    if case == "cross-asdu":
        select = "46/6/2/3001/81;46/6/1/3002/81"
    first, length = frame(f"{source}>10.30.30.20={select}", 0, 1000)
    second, _ = frame(f"{source}>10.30.30.20={execute}", 1, 1000 + length)
    if case == "different-stream":
        second = second[:34] + struct.pack("!H", 40001) + second[36:]
    payload = modbus(unit=2 if case == "wrong-unit" else 1,
                     address=1 if case == "wrong-address" else 0,
                     value=2 if case == "wrong-value" else 1)
    if case == "cross-pdu":
        payload = modbus(unit=2) + modbus(address=1) + modbus(value=2)
    third = modbus_frame(payload, case == "response", case == "wrong-direction")
    packets = [(1, first), (2, second), (3, third)]
    if case == "multi":
        both, _ = frame(f"{source}>10.30.30.20={select};{execute}", 0, 1000)
        packets = [(1, both), (3, third)]
    if case == "no-select":
        packets = packets[1:]
    if case == "no-execute":
        packets = [packets[0], packets[2]]
    if case == "reversed":
        packets = [(1, first), (2, third), (3, second)]
    if case == "timestamp-reversed":
        packets = [(2, first), (1, second), (3, third)]
    if case == "duplicates":
        packets = [(1, first), (1, first), (2, second), (2, second), (3, third), (3, third)]
    if case == "malformed":
        bad = modbus_frame(b"\x00\x01\x00\x00\x00\x02\x01\x06")
        bad = bad[:34] + struct.pack("!H", 41001) + bad[36:]
        packets.append((4, bad))
    with open(path, "wb") as out:
        out.write(struct.pack("<IHHIIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, 1))
        for stamp, data in packets:
            out.write(struct.pack("<IIII", stamp, 100000, len(data), len(data)) + data)
    if case == "truncated":
        with open(path, "rb+") as out:
            out.truncate(out.seek(0, 2) - 3)


if __name__ == "__main__":
    generate(*sys.argv[1:])
