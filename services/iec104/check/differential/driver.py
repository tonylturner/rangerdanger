# SPDX-License-Identifier: Apache-2.0
"""First-party raw scenario peer, never an independent decode oracle.

Wire constants come from the repository's own probes/captures. tshark and
the comparator independently decide conformance.
"""

import argparse
import ast
from contextlib import ExitStack
import json
from pathlib import Path
import socket
import select
import struct
import time
import urllib.request

from scenario import scenario


def http(url, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("HTTP action deadline")
    with urllib.request.urlopen(url, timeout=min(2, remaining)) as response:
        return response.read().decode()


def barrier(url, predicate, deadline):
    while time.monotonic() < deadline:
        try:
            value = json.loads(http(url, deadline))
        except (OSError, ValueError):
            value = None
        if value is not None and predicate(value):
            print(json.dumps({"barrier": url, "state": value}), flush=True)
            return value
        time.sleep(0.02)
    raise TimeoutError("barrier did not complete: " + url)


class Peer:
    def __init__(self, sock, deadline, *, server=False):
        self.sock = sock
        self.deadline = deadline
        self.tx = self.rx = 0
        self.records = []
        self.server = server
        self.sent_count = self.read_count = 0
        self.associations = []

    def budget(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("raw peer action deadline")
        self.sock.settimeout(min(5, remaining))

    def send(self, frame):
        self.budget()
        self.sock.sendall(frame)
        self.sent_count += 1

    def exact(self, count):
        data = b""
        while len(data) < count:
            self.budget()
            chunk = self.sock.recv(count - len(data))
            if not chunk:
                raise EOFError("peer closed before barrier")
            data += chunk
        return data

    def read(self, acknowledge=True):
        assert self.exact(1) == b"\x68"
        size = self.exact(1)[0]
        assert 4 <= size <= 253
        data = self.exact(size)
        self.records.append(data.hex())
        self.read_count += 1
        if not data[0] & 1:
            ns = int.from_bytes(data[:2], "little") >> 1
            assert ns == self.rx, (ns, self.rx)
            self.rx = (self.rx + 1) % 32768
            if acknowledge:
                self.send(b"\x68\x04\x01\x00" + struct.pack("<H", self.rx * 2))
        elif data[0] == 0x43:
            self.send(bytes.fromhex("680483000000"))
        return data

    def until(self, predicate):
        while True:
            data = self.read()
            if predicate(data):
                return data

    def u(self, act, con):
        self.send(bytes([104, 4, act, 0, 0, 0]))
        self.until(lambda data: data == bytes([con, 0, 0, 0]))

    def asdu(self, type_id, cot, ioa, payload, ca=1):
        data = bytes([type_id, 1, cot, 0]) + struct.pack("<H", ca)
        data += ioa.to_bytes(3, "little") + payload
        self.send(bytes([104, 4 + len(data)]) + struct.pack("<HH", self.tx * 2, self.rx * 2) + data)
        self.tx = (self.tx + 1) % 32768

    def expect(self, type_id, cot, ioa=None, value=None):
        def matches(data):
            if data[0] & 1 or len(data) < (14 if value is not None else 13):
                return False
            a = data[4:]
            return (a[0] == type_id and a[2] == cot
                    and (ioa is None or int.from_bytes(a[6:9], "little") == ioa)
                    and (value is None or a[9] == value))
        return self.until(matches)

    def connection_limit(self):
        """STARTDT barriers hold eight sessions open before dialing nine."""
        host = self.sock.getpeername()
        with ExitStack() as stack:
            held = [self]
            for _ in range(7):
                sock = stack.enter_context(socket.create_connection(host, timeout=5))
                peer = Peer(sock, self.deadline)
                peer.u(7, 11)
                held.append(peer)
            ninth = stack.enter_context(socket.create_connection(host, timeout=5))
            Peer(ninth, self.deadline).budget()
            # No send/close race: first measure whether the server closes
            # an over-limit TCP connection without any APCI stimulus.
            assert ninth.recv(1) == b"", "over-limit connection sent APCI data"
            print(json.dumps({"ninth": "tcp-connected-then-eof", "apci": []}), flush=True)
            # Prove rejection did not displace any of the eight incumbents.
            for peer in held:
                peer.u(67, 131)
            for peer in reversed(held[1:]):
                peer.u(19, 35)

    def associate(self, step, message, *, sent):
        direction = "server_to_client" if self.server == sent else "client_to_server"
        self.associations.append({"step": step, "message": message, "session": 0,
                                  "direction": direction,
                                  "index": (self.sent_count if sent else self.read_count) - 1})

    def zero_object(self, type_id, step):
        cot = 3 if self.server else 6
        asdu = bytes([type_id, 0, cot, 0, 1, 0])
        self.send(bytes([104, 10]) + struct.pack("<HH", self.tx * 2, self.rx * 2) + asdu)
        self.tx = (self.tx + 1) % 32768
        self.associate(step, "input", sent=True)
        # Exact threshold S barrier before any application reply or FIN.
        assert self.read() == b"\x01\x00" + struct.pack("<H", self.tx * 2)
        if self.server:
            self.u(67, 131)  # proves the CC ignored the report and stayed connected
        elif type_id == 100:
            self.budget()
            assert self.sock.recv(1) == b"", "zero-object GI sent an ASDU instead of FIN"
        else:
            expected_rx = self.rx
            reply = self.read()
            assert reply == struct.pack("<HH", expected_rx * 2, self.tx * 2) + bytes([type_id, 0, 0x6f, 0, 1, 0]), reply.hex()
            self.associate(step, "reply", sent=False)

    def action(self, action, fixture, step=0):
        self.deadline = time.monotonic() + action["timeout_s"]
        op, args = action["op"], action["args"]
        if op == "start":
            self.u(7, 11)
        elif op == "stop":
            self.u(19, 35)
        elif op == "test":
            self.u(67, 131)
        elif op == "window":
            self.window()
        elif op == "connection-limit":
            self.connection_limit()
        elif op == "zero-object":
            self.zero_object(args["type"], step)
        elif op == "cycles":
            # The existing Modbus fixture supplies constant 28.9 A / 20 kV
            # registers. A received, acknowledged image is the cycle barrier;
            # no wall-clock sampling window or sleep defines frame counts.
            expected = bytes.fromhex("0d0201000100")
            expected += (2001).to_bytes(3, "little") + struct.pack("<fB", 28.9, 0)
            expected += (2002).to_bytes(3, "little") + struct.pack("<fB", 20, 0)
            for cycle in range(args["count"]):
                data = self.until(lambda frame: not frame[0] & 1)
                assert data[4:] == expected, data.hex()
                print(json.dumps({"cycle": cycle + 1, "asdu": data[4:].hex()}), flush=True)
        elif op == "gi":
            first = len(self.records)
            self.asdu(100, 6, 0, b"\x14")
            self.expect(100, 7)
            self.expect(100, 10)
            gi = [bytes.fromhex(record)[4:] for record in self.records[first:]
                  if not bytes.fromhex(record)[0] & 1 and bytes.fromhex(record)[6] == 20]
            assert sorted((a[0], a[1]) for a in gi) == [(1, 1), (3, 1)], gi
        elif op == "negative":
            # Read has no element after IOA; clock sync has a complete CP56.
            # Fixed, valid public wire layouts, not codec/application support.
            payload = {102: b"", 103: bytes.fromhex("0000000c170a1a")}.get(
                args["type"], bytes([args["value"]]))
            self.asdu(args["type"], args["cot"], args["ioa"], payload, args["ca"])
            self.expect(args["type"], 64 | args["expected_cot"], args["ioa"])
        elif op == "sbo":
            self.asdu(46, 6, 3001, bytes([128 | args["value"]]))
            self.expect(46, 7, 3001, 128 | args["value"])
            self.asdu(46, 6, 3001, bytes([args["value"]]))
            self.expect(46, 7, 3001, args["value"])
            self.expect(31, 11, 1001)
            self.expect(46, 10, 3001, args["value"])
        elif op == "fixture":
            result = ast.literal_eval(http(fixture + ":8081/" + args["query"], self.deadline))
            print(json.dumps({"fixture": result}), flush=True)
        elif op == "expect":
            self.expect(args["type"], args["cot"], args["ioa"], args["value"])
        else:
            raise ValueError("unsupported client action: " + op)
        print(json.dumps({"completed": op, "tx": self.tx, "rx": self.rx}), flush=True)

    def window(self):
        """Fill the configured k=12 with four-frame GIs, withholding N(R)."""
        def request():
            asdu = bytes.fromhex("64010600010000000014")
            self.send(bytes([104, 4 + len(asdu)]) + struct.pack("<HH", self.tx * 2, 0) + asdu)
            self.tx = (self.tx + 1) % 32768

        for _ in range(3):
            request()
            count = 0
            while count < 4:
                count += int(not self.read(acknowledge=False)[0] & 1)
        assert self.rx == 12
        request()
        # w=1 acknowledges the fourth request even while the output k window
        # is full. That acknowledgement is the processing barrier.
        while True:
            frame = self.read(acknowledge=False)
            assert frame[0] & 1, "RTU sent more than k unacknowledged I frames"
            if frame[0] == 1 and int.from_bytes(frame[2:4], "little") >> 1 == self.tx:
                break
        # Explicit negative observation budget, not a startup sleep.
        assert not select.select([self.sock], [], [], 0.2)[0], "k window did not block"
        self.send(b"\x68\x04\x01\x00" + struct.pack("<H", self.rx * 2))
        count = 0
        while count < 4:
            count += int(not self.read(acknowledge=False)[0] & 1)
        assert self.rx == 16
        self.send(b"\x68\x04\x01\x00" + struct.pack("<H", self.rx * 2))


def raw_server(case, address, result):
    deadline = time.monotonic() + case["timeout_s"]
    with socket.socket() as listener:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind((address, 2404))
        listener.listen(1)
        listener.settimeout(case["timeout_s"])
        print("LISTENING", flush=True)
        sock, _ = listener.accept()
    with sock:
        peer = Peer(sock, deadline, server=True)
        assert peer.read() == bytes.fromhex("07000000")
        peer.send(bytes.fromhex("68040b000000"))
        peer.until(lambda data: not data[0] & 1 and data[4] == 100)

        def acknowledged(type_id, cot, ioa, payload):
            peer.asdu(type_id, cot, ioa, payload)
            target = peer.tx
            peer.until(lambda data: data[0] == 1 and int.from_bytes(data[2:4], "little") >> 1 == target)

        acknowledged(100, 7, 0, b"\x14")
        acknowledged(3, 20, 1001, b"\x02")
        acknowledged(1, 20, 1002, b"\x00")
        acknowledged(100, 10, 0, b"\x14")
        for step, action in enumerate(case["actions"]):
            peer.deadline = min(deadline, time.monotonic() + action["timeout_s"])
            args = action["args"]
            if action["op"] == "zero-object":
                peer.zero_object(args["type"], step)
                continue
            if action["op"] != "server-reports":
                raise ValueError("unsupported scripted-server action")
            for _ in range(args["count"]):
                payload = bytes([args["value"]])
                # Known fixed timestamp; IV is an exact compared status bit.
                if args["timed_invalid"]:
                    payload += bytes.fromhex("0000800c170a1a")
                acknowledged(31 if args["timed_invalid"] else 3, 3, 1001, payload)
        result.write_text(json.dumps({"passed": True, "tx": peer.tx, "rx": peer.rx, "associations": peer.associations}))
        print("SCENARIO COMPLETE", flush=True)
        # Runner stops CC after completion. Do not close early and provoke a
        # reconnect whose timing would create a spurious additional session.
        peer.budget()
        assert sock.recv(1) == b""
        Path("/e/peer-closed").touch()
        # Docker removes an endpoint's veth when its owner exits, even while
        # tcpdump shares the namespace. Keep the owner alive until flush.
        deadline = time.monotonic() + 15
        while not Path("/e/capture-stopped").exists():
            if time.monotonic() >= deadline:
                raise TimeoutError("capture shutdown barrier")
            time.sleep(0.02)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("client", "server", "ready", "cc-ready"))
    parser.add_argument("--scenario", default="/e/scenario.json")
    parser.add_argument("--prefix", required=True)
    args = parser.parse_args()
    target = args.prefix + ".10"
    fixture = "http://" + args.prefix + ".20"
    if args.mode == "ready":
        barrier(fixture + ":8080/ready",
                lambda state: state["read_replies"] >= 3 and state["clock_polls"] >= 1,
                time.monotonic() + 20)
        return
    if args.mode == "cc-ready":
        barrier("http://" + args.prefix + ".30:8080/api/state",
                lambda state: state["link"]["gi"]["state"] == "complete"
                and len(state["points"]) == 2
                and all(not any(point["quality"].values()) for point in state["points"]),
                time.monotonic() + 20)
        return
    case = scenario(args.scenario)
    result = Path("/e/peer-result.json")
    try:
        if args.mode == "server":
            raw_server(case, target, result)
        else:
            with socket.create_connection((target, 2404), timeout=5) as sock:
                peer = Peer(sock, time.monotonic() + case["timeout_s"])
                for step, action in enumerate(case["actions"]):
                    peer.action(action, fixture, step)
            result.write_text(json.dumps({"passed": True, "records": peer.records, "associations": peer.associations}))
    except BaseException as error:
        result.write_text(json.dumps({"passed": False, "error": str(error)}))
        raise


if __name__ == "__main__":
    main()
