"""Real reducer validation with temporary inputs; injected decoder transport."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "lib"))
import containd_readiness  # noqa: E402


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


HERE = Path(__file__).parent
fixture = load("g22_fixture", HERE / "make_g22_evidence.py")
evidence = load("g22_evidence", HERE.parents[1] / "lib/eu-g22-evidence.py")


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        fixture.seed(self.path)
        evidence.decode(self.path / "G22-weak.pcap", fixture.utc(1), fixture.utc(4),
                        self.path / "G22-packets.json")

    def mutate(self, name, change):
        path = self.path / f"G22-{name}.json"
        data = json.loads(path.read_text())
        change(data)
        path.write_text(json.dumps(data))

    def reduce(self, valid=True, status="0"):
        if valid:
            evidence.write_record(self.path, "eu-hardening-configurations", status)
        else:
            with self.assertRaises(ValueError):
                evidence.write_record(self.path, "eu-hardening-configurations", status)
        record = json.loads((self.path / "G22-chain.json").read_text())
        self.assertEqual(record["status"], "PASS" if valid else "FAIL")
        return record

    def test_complete_record_and_analog_age(self):
        record = self.reduce()
        self.assertEqual(record["improved_classification"], "connect-timeout")
        self.assertEqual(record["range_generation"], "range-42")
        current = record["cc_observations"]["consequence-cc"]["feeder-current"]
        self.assertEqual(current["receipt_age_seconds"], "3.600000000")
        self.assertFalse(record["raw"]["consequence-field"]["devices"]["breaker-1"]["protection_tripped"])
        self.assertIn("G22-packets.json.pdml", record["raw_inputs"])
        self.assertIn("G22-weak.pcap", record["raw_inputs"])

    def test_cached_open(self):
        self.mutate("consequence-cc", lambda s: next(
            p for p in s["points"] if p["id"] == "feeder-breaker-position"
        ).update(received_at=fixture.utc(0)))
        self.reduce(False)

    def test_bad_position_quality(self):
        for quality in ("iv", "nt", "sb", "bl", "ov"):
            with self.subTest(quality=quality):
                self.mutate("consequence-cc", lambda s: next(
                    p for p in s["points"] if p["id"] == "feeder-breaker-position"
                )["quality"].update(dict.fromkeys(("iv", "nt", "sb", "bl", "ov"), False)))
                self.mutate("consequence-cc", lambda s: next(
                    p for p in s["points"] if p["id"] == "feeder-breaker-position"
                )["quality"].update({quality: True}))
                self.reduce(False)

    def test_missing_point(self):
        self.mutate("consequence-cc", lambda s: s.update(
            points=[p for p in s["points"] if p["id"] != "feeder-current"]))
        self.reduce(False)

    def test_null_cc_receipt_is_failure_record(self):
        self.mutate("consequence-cc", lambda s: next(
            p for p in s["points"] if p["id"] == "feeder-current"
        ).update(received_at=None))
        self.reduce(False)

    def test_malformed_cc_receipt_is_failure_record(self):
        self.mutate("consequence-cc", lambda s: next(
            p for p in s["points"] if p["id"] == "feeder-current"
        ).update(received_at="bad-time"))
        self.reduce(False)

    def test_nanosecond_boundary_precision(self):
        self.assertEqual(evidence.epoch("2026-11-01T12:00:00.000000001Z")
                         - evidence.epoch("2026-11-01T12:00:00Z"),
                         evidence.Decimal("0.000000001"))

    def test_stale_solver_revision(self):
        self.mutate("consequence-field", lambda s: s["process"]["devices"]["feeder-breaker"].update(
            solved_revision=9))
        self.reduce(False)

    def test_stale_solver_truth(self):
        self.mutate("consequence-field", lambda s: s["process"].update(truth_stale=True))
        self.reduce(False)

    def test_wrong_range(self):
        self.mutate("consequence-cc", lambda s: s.update(range_generation="different"))
        self.reduce(False)

    def test_wrong_package(self):
        self.mutate("consequence-cc", lambda s: s.update(package_id="us-dnp3-substation"))
        self.reduce(False)

    def test_wrong_cache(self):
        self.mutate("consequence-cc", lambda s: s.update(cache_epoch="different"))
        self.reduce(False)

    def test_absent_file(self):
        (self.path / "G22-consequence-field.json").unlink()
        self.reduce(False)

    def test_incomplete_record(self):
        (self.path / "G22-events.jsonl").write_text("")
        self.reduce(False)

    def test_incomplete_packet_record(self):
        self.mutate("packets", lambda s: s["fc6"].pop("transaction"))
        self.reduce(False)

    def test_packet_stream_mismatch(self):
        self.mutate("packets", lambda s: s["execute"].update(stream="99"))
        self.reduce(False)

    def test_failed_walk_preserves_diagnostics(self):
        record = self.reduce(False, "1")
        self.assertIn("walk failed", record["errors"][0])
        self.assertIn("consequence-cc", record["raw"])

    def test_bad_json_is_failure_record(self):
        (self.path / "G22-consequence-cc.json").write_text("{")
        self.reduce(False)

    def test_restore_supply(self):
        self.mutate("restoration-field", lambda s: s["process"]["buses"]["s1"].update(live=False))
        self.reduce(False)

    def test_restore_cached_cc(self):
        self.mutate("restoration-cc", lambda s: next(
            p for p in s["points"] if p["id"] == "feeder-breaker-position"
        ).update(received_at=fixture.utc(0)))
        self.reduce(False)

    def test_no_policy_receipt(self):
        self.mutate("improved-apply", lambda s: s.clear())
        self.reduce(False)

    def test_failed_positive_control(self):
        self.mutate("improved-control-after", lambda s: s["link"].update(state="stopped"))
        self.reduce(False)

    def test_changed_retest_revision(self):
        self.mutate("improved-held-field", lambda s: s["devices"]["breaker-1"].update(revision=12))
        self.reduce(False)

    def test_refusal_not_denial(self):
        self.mutate("improved-command", lambda s: s.update(
            stderr="connect failed: dial tcp 10.30.30.20:2404: connection refused"))
        record = self.reduce(False)
        self.assertIn("refusal", record["errors"][0])

    def test_no_route_not_denial(self):
        self.mutate("improved-command", lambda s: s.update(stderr="no route to host"))
        self.assertIn("no-route", self.reduce(False)["errors"][0])

    def test_tool_error_not_denial(self):
        self.mutate("improved-command", lambda s: s.update(stderr="iec104cmd: not found"))
        self.assertIn("tool-error", self.reduce(False)["errors"][0])

    def test_generic_failure_not_denial(self):
        self.mutate("improved-command", lambda s: s.update(stderr="command failed"))
        self.reduce(False)

    def test_tampered_pcap(self):
        with (self.path / "G22-weak.pcap").open("ab") as out:
            out.write(b"invalid")
        self.reduce(False)

    def test_dropped_packets(self):
        (self.path / "G22-weak-tcpdump.log").write_text(
            "3 packets captured\n3 packets received by filter\n1 packets dropped by kernel\n")
        self.reduce(False)

    def test_decoder_transport_timeout(self):
        def run(*args, **kwargs):
            self.assertEqual(kwargs["timeout"], 45)
            raise subprocess.TimeoutExpired(args[0], 45)
        with self.assertRaises(subprocess.TimeoutExpired):
            evidence.decode(self.path / "G22-weak.pcap", "0", "5",
                            self.path / "timeout.json", run=run)
        self.assertFalse((self.path / "timeout.json").exists())

    def test_backend_receipt_matches_shared_semantics(self):
        with self.assertRaises(ValueError):
            containd_readiness.require_backend_protection({"containd": {"applyId": "a"}})


if __name__ == "__main__":
    unittest.main()
