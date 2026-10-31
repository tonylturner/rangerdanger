"""Write a synthetic IEC 104 pcap for the EU gate's tshark tests.

Usage: python3 -I make_iec104_pcap.py OUT FRAME [FRAME ...]

FRAME is SRC>DST=ASDU[;ASDU...]; each ASDU (one APDU in the same TCP
segment) is TYPE/COT/CA/IOA/ELEMENT, the element bytes written as hex. The
destination port is 2404 unless the frame starts with "reply:", which makes
2404 the source port.
"""

import socket
import struct
import sys


def apdu(spec, send_seq):
    type_id, cot, ca, ioa, element = spec.split("/")
    asdu = struct.pack("<BBBBH", int(type_id), 1, int(cot), 0, int(ca))
    asdu += int(ioa).to_bytes(3, "little") + bytes.fromhex(element)
    control = struct.pack("<HH", send_seq << 1, 0)
    body = control + asdu
    return bytes([0x68, len(body)]) + body


def frame(spec, index, tcp_seq):
    reply = spec.startswith("reply:")
    if reply:
        spec = spec[len("reply:"):]
    route, asdus = spec.split("=", 1)
    src, dst = route.split(">")
    payload = b"".join(apdu(item, index + n) for n, item in enumerate(asdus.split(";")))
    sport, dport = (2404, 40000) if reply else (40000, 2404)
    ip = struct.pack(
        "!BBHHHBBH4s4s", 0x45, 0, 40 + len(payload), index, 0, 64, 6, 0,
        socket.inet_aton(src), socket.inet_aton(dst),
    )
    tcp = struct.pack("!HHIIBBHHH", sport, dport, tcp_seq, 0, 0x50, 0x18, 65535, 0, 0)
    ethernet = bytes.fromhex("0000000000020000000000010800")
    return ethernet + ip + tcp + payload, len(payload)


def main(argv):
    if len(argv) < 3:
        raise SystemExit(__doc__)
    with open(argv[1], "wb") as out:
        out.write(struct.pack("<IHHIIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, 1))
        tcp_seq = 1000
        for index, spec in enumerate(argv[2:]):
            data, length = frame(spec, index, tcp_seq)
            out.write(struct.pack("<IIII", index + 1, 0, len(data), len(data)) + data)
            tcp_seq += length


if __name__ == "__main__":
    main(sys.argv)
