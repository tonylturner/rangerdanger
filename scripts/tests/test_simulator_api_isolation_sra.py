"""SRA simulator isolation contracts, independent of the parallel package lane."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from scripts.lib import simulator_api_isolation as g3
from scripts.tests.test_simulator_api_isolation import FakeTransport, bodies, NOW

SRA = "us-sra-substation"


class SRAIsolationTests(unittest.TestCase):
    def catalogue(self):
        return copy.deepcopy(json.loads(g3.MATRIX.read_text())["packages"][SRA])

    def test_inventory_load_and_fail_closed_compose_review(self):
        cat = self.catalogue()
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp) / "lab-definitions/packages" / SRA
            base.mkdir(parents=True)
            (base / "manifest.json").write_text(json.dumps({"package": SRA, "services": cat["services"]}))
            for name in cat["reviewed_inputs"]:
                content = f"fixture {name}".encode()
                (base / name).write_bytes(content)
                cat["reviewed_inputs"][name] = hashlib.sha256(content).hexdigest()
            matrix = Path(tmp) / "matrix.json"
            matrix.write_text(json.dumps({"schema": 1, "packages": {SRA: cat}}))
            self.assertEqual(g3.load_inventory(SRA, tmp, matrix), cat)
            (base / "compose.source.yml").write_text("changed")
            with self.assertRaisesRegex(ValueError, "unclassified Compose change"):
                g3.load_inventory(SRA, tmp, matrix)

    def test_sources_aliases_and_verdicts(self):
        cat = self.catalogue()
        self.assertEqual(cat["sources"], ["kali", "vendor_jump"])
        self.assertFalse(any(d["service"] == "openplc" for d in cat["destinations"]))
        self.assertFalse({"openplc", "corp_ws", "eng_workstation"} & {s["key"] for s in cat["services"]})
        for policy, counts in (('weak', (76, 43, 33)), ('improved', (76, 3, 73))):
            rows = g3.expand_matrix(cat, SRA, policy)
            self.assertEqual((len(rows), sum(r["expected"] == "A" for r in rows),
                              sum(r["expected"] == "D" for r in rows)), counts)
            self.assertEqual(len({r["id"] for r in rows}), counts[0])
        vendor_https = next(d for d in cat["destinations"] if d["host"] == "10.30.30.20" and d["port"] == 443)
        self.assertEqual(vendor_https["overrides"], {"vendor_jump": {"weak": "A", "improved": "D"}})

    def test_controller_source_pin_and_both_policy_positive_control(self):
        cat = self.catalogue()
        controller = [d for d in cat["destinations"] if d["service"] == "sra_ziti_controller"]
        self.assertEqual(len(controller), 1)
        self.assertEqual((controller[0]["host"], controller[0]["port"]), ("10.25.25.40", 1280))
        self.assertEqual(controller[0]["control"]["service"], "sra_postgres")
        self.assertEqual(controller[0]["control"]["tool"], "nc")
        self.assertIsNone(controller[0]["control"]["path"])
        for policy in g3.POLICIES:
            rows = g3.expand_matrix(cat, SRA, policy)
            self.assertEqual({r["source"]: r["expected"] for r in rows
                              if (r["host"], r["port"]) == ("10.25.25.40", 1280)},
                             {"kali": "D", "vendor_jump": "A" if policy == "improved" else "D"})

    def test_controller_listener_inventory_rejects_unclassified_ports(self):
        cat = self.catalogue()
        # Actual kernel table shape: the controller shares one IPv6 wildcard
        # socket for all 1280 APIs; Docker DNS is namespace-local.
        raw = (" 0: 00000000000000000000000000000000:0500 "
               "00000000000000000000000000000000:0000 0A 0 0 0 0 0 100\n"
               " 1: 0B00007F:1234 00000000:0000 0A 0 0 0 0 0 101\n")
        loopbacks = []
        g3.check_listeners(cat, "sra_ziti_controller", raw,
                           record_loopback=lambda host, port: loopbacks.append((host, port)))
        self.assertEqual(loopbacks, [("127.0.0.11", 4660)])
        with self.assertRaisesRegex(ValueError, "unclassified listener"):
            g3.check_listeners(cat, "sra_ziti_controller", raw.replace(":0500", ":0501"))

    def test_controls_and_gate_never_request_openplc(self):
        cat = self.catalogue()
        with tempfile.TemporaryDirectory() as tmp:
            evidence = g3.Evidence(tmp, SRA)
            io = FakeTransport(cat, SRA)
            gate = g3.Gate(SRA, cat, io, evidence)
            gate.run()
            self.assertTrue(evidence.record["passed"])
            self.assertTrue(any(c[:2] == ("http", "/apps/fuxa-hmi/") for c in io.calls))
            self.assertFalse(any("openplc" in str(c) for c in io.calls))
            policies = [c[2]["config"] for c in io.calls if c[:2] == ("http", "/api/firewall/apply")]
            self.assertEqual(policies, ["weak", "improved", "weak"])
            self.assertEqual(sum(e["kind"] == "student-batch" for e in evidence.record["events"]), 4)

    def test_retained_device_and_physics_controls_are_not_optional(self):
        valid = bodies("us-dnp3-substation")
        g3.check_backend_controls(SRA, valid, 7, NOW + 2)
        del valid["/api/substation/state"]["devices"]["relay"]
        with self.assertRaisesRegex(ValueError, "incomplete US devices"):
            g3.check_backend_controls(SRA, valid, 7, NOW + 2)

    def test_cli_admits_sra(self):
        import importlib.util
        spec = importlib.util.spec_from_file_location("g3_sra_cli", g3.ROOT / "scripts/simulator-api-isolation-smoke.py")
        cli = importlib.util.module_from_spec(spec)
        with patch.dict("sys.modules", {"lib": __import__("scripts.lib", fromlist=["lib"]),
                                        "lib.simulator_api_isolation": g3}):
            spec.loader.exec_module(cli)
        with patch("sys.argv", ["g3", SRA, "--list"]), patch.object(cli, "load_inventory", return_value=self.catalogue()), \
                patch("builtins.print") as output:
            self.assertEqual(cli.main(), 0)
            self.assertEqual(len(json.loads(output.call_args.args[0])["weak"]), 76)
