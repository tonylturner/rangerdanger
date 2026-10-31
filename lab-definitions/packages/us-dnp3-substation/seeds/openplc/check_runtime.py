"""Require the seeded local program's Modbus status and heartbeat at startup."""
import socket
import struct
import time

HOST = "127.0.0.1"
PORT = 502
ATTEMPTS = 30
POLL_INTERVAL = 0.5


def receive(sock, size):
    data = b""
    while len(data) < size:
        chunk = sock.recv(size - len(data))
        if not chunk:
            raise RuntimeError("short Modbus response")
        data += chunk
    return data


def read_status():
    with socket.create_connection((HOST, PORT), timeout=POLL_INTERVAL) as sock:
        sock.settimeout(POLL_INTERVAL)
        sock.sendall(struct.pack(">HHHBBHH", 1, 0, 6, 1, 3, 1024, 4))
        header = receive(sock, 7)
        if header != struct.pack(">HHHB", 1, 0, 11, 1):
            raise RuntimeError("invalid Modbus response header")
        body = receive(sock, 10)
        if body[:2] != b"\x03\x08":
            raise RuntimeError("invalid feeder status response")
        return struct.unpack(">4H", body[2:])


def main():
    previous = None
    last_error = None
    deadline = time.monotonic() + ATTEMPTS * POLL_INTERVAL
    while time.monotonic() < deadline:
        try:
            # Holding registers 1024-1027 are %MW0-%MW3 (see README.md).
            _close_request, status, close_output, heartbeat = read_status()
            if status & 0x000F != 0x000F:
                raise RuntimeError("feeder interlock status is incomplete")
            if close_output != 0:
                raise RuntimeError("close output is unexpectedly energized")
            if previous is not None and heartbeat != previous:
                print("US OpenPLC seed heartbeat advanced")
                return
            previous = heartbeat
            last_error = RuntimeError("seed heartbeat has not advanced")
        except (OSError, RuntimeError, struct.error) as error:
            last_error = error  # Early register reads may be incomplete until the first scan.
        time.sleep(min(POLL_INTERVAL, max(0, deadline - time.monotonic())))
    message = "US OpenPLC seed heartbeat did not advance within 15 seconds"
    if last_error is not None:
        message += f": {last_error}"
    raise RuntimeError(message) from last_error


if __name__ == "__main__":
    main()
