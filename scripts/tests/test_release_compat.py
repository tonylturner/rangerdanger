import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class FragmentedSocket:
    def __init__(self, data):
        self.data = data
        self.sent = b""

    def recv(self, count):
        chunk, self.data = self.data[:1], self.data[1:]
        return chunk

    def sendall(self, data):
        self.sent += data


class ProxyProbeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        python = source.split("<<'RFB_PY'\n", 1)[1].split("\nRFB_PY", 1)[0]
        cls.rfb = {"__name__": "offline_fixture"}
        exec(compile(python, "rfb-probe", "exec"), cls.rfb)

    def test_rfb_vnc_auth_fragmented(self):
        sock = FragmentedSocket(b"RFB 003.008\n\x01\x02")
        self.assertEqual(self.rfb["negotiate"](sock), [2])
        self.assertEqual(sock.sent, b"RFB 003.008\n")

    def test_rfb_none_missing_auth_and_truncation_fail(self):
        for data in (b"RFB 003.008\n\x01\x01", b"RFB 003.008\n\x02\x01\x02",
                     b"RFB 003.008\n\x00", b"RFB 003.008\n\x01",
                     b"RFB 003.003\n\x01\x02"):
            with self.subTest(data=data), self.assertRaises(ValueError):
                self.rfb["negotiate"](FragmentedSocket(data))

    def test_proxy_probe_entry_points(self):
        source = (ROOT / "scripts/proxy-smoke.sh").read_text()
        for required in ("containd-ui-probe.py", ":3000/", "apps/vendor-jump/",
                         "10.20.20.10:80/", "sites-enabled/default", "base + \"programs\""):
            self.assertIn(required, source)
        self.assertIn("RD-22, non-blocking", source)


if __name__ == "__main__":
    unittest.main()
