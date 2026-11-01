"""Offline RTAC gate conformance: no Docker, sockets or live HTTP."""
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from unittest.mock import Mock, patch


ROOT = Path(__file__).resolve().parents[2]
LIB = ROOT / "scripts/lib"
SPEC = importlib.util.spec_from_file_location("us_rtac_check", LIB / "us-rtac-check.py")
g = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(g)
# The host-only receipt reader is imported by Gate.run(), never in containers.
sys.path.insert(0, str(LIB))
TOKEN = "0123456789abcdef0123456789abcdef"


def addresses(ips):
    return [{"ifname": "leg" + str(i), "address": "02:00:00:00:00:%02x" % (i + 1),
             "flags": ["UP", "LOWER_UP"],
             "addr_info": [{"family": "inet", "local": ip}]} for i, ip in enumerate(ips)]


def snapshot():
    rows = addresses(g.LEGS)
    ot_if = rows[0]["ifname"]
    route = lambda dst: {"dst": dst, "gateway": g.GATEWAY, "dev": ot_if}
    values = {"/proc/sys/net/ipv4/conf/" + i + "/forwarding": "0"
              for i in ["all", "default", "lo"] + [r["ifname"] for r in rows]}
    values["/proc/sys/net/ipv4/ip_forward"] = "0"
    return {"time": {"utc": "2026-11-01T00:00:00+00:00", "monotonic": 1},
            "addresses": rows, "forwarding": values, "forward_chain": "-P FORWARD DROP\n",
            "input_chain": "-P INPUT ACCEPT\n-A INPUT -i leg1 -j DROP\n",
            "field_route": [route("10.40.40.0/24")],
            "default_route": [route("default")],
            "route_get": {"10.40.40." + str(i): [dict(route("10.40.40." + str(i)),
                                                   prefsrc=g.OT)]
                          for i in (20, 21, 22, 23)}}


def links():
    result = g.assert_interfaces(addresses(g.LEGS))
    # Deliberately not ethN and not the RTAC's interface enumeration.
    result[g.SOURCE] = {"ifname": "plc-ot", "mac": "02:00:00:00:00:30"}
    result[g.DEST] = {"ifname": "dss-net", "mac": "02:00:00:00:00:50"}
    return result


def event(kind, when, **fields):
    return dict(event=kind, time={"utc": "range-clock", "monotonic": when}, **fields)


def frame(stage, seq, when, kind="frame", link_map=None):
    link_map = link_map or links()
    src = g.SOURCE if stage == "neg" else g.PHYSICS
    raw = g.make_frame(src, link_map[src]["mac"],
                       link_map[g.OT if stage == "neg" else g.DEST]["mac"], TOKEN, stage, seq)
    result = event(kind, when, hex=raw.hex(), decoded=g.decode_frame(raw, TOKEN))
    if kind == "frame":
        result["packet_type"] = 0
    return result


def transit():
    ingress = [event("ready", 1, iface="leg0")]
    arrivals = [event("ready", 1, iface="dss-net"), frame("pre", 0, 3)]
    sent = [frame("pre", 0, 2, "sent")]
    for seq in range(3):
        sent.append(frame("neg", seq, 4 + seq * 2, "sent"))
        ingress.append(frame("neg", seq, 5 + seq * 2))
    sent.append(frame("post", 0, 10, "sent"))
    arrivals += [frame("post", 0, 11), event("done", 20, packets=2, dropped=0)]
    ingress.append(event("done", 20, packets=3, dropped=0))
    return ingress, arrivals, sent


class HostAssertionsTests(unittest.TestCase):
    def test_valid_alternate_interface_order(self):
        data = snapshot()
        data["addresses"].reverse()
        self.assertEqual(g.assert_host(data)[g.OT]["ifname"], "leg0")

    def test_missing_duplicate_or_down_leg(self):
        for mutation in ("missing", "duplicate", "down", "same_if"):
            rows = addresses(g.LEGS)
            if mutation == "missing":
                rows.pop()
            elif mutation == "duplicate":
                rows.append(copy.deepcopy(rows[0]))
            elif mutation == "down":
                rows[0]["flags"] = []
            else:
                rows[1]["ifname"] = rows[0]["ifname"]
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                g.assert_interfaces(rows)

    def test_forwarding_one_or_unreadable_or_missing(self):
        for path in snapshot()["forwarding"]:
            for value in ("1", "?", None):
                data = snapshot()
                if value is None:
                    del data["forwarding"][path]
                else:
                    data["forwarding"][path] = value
                with self.subTest(path=path, value=value), self.assertRaises(ValueError):
                    g.assert_host(data)

    def test_extra_interface_forwarding_also_checked(self):
        data = snapshot()
        data["forwarding"]["/proc/sys/net/ipv4/conf/new0/forwarding"] = "1"
        with self.assertRaisesRegex(ValueError, "forwarding_not_zero"):
            g.assert_host(data)

    def test_forward_policy_accept_rule_or_jump_fails(self):
        for text in ("", "-P FORWARD ACCEPT\n", "-P FORWARD DROP\n-A FORWARD -j ACCEPT\n",
                     "-P FORWARD DROP\n-A FORWARD -j CUSTOM\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                g.assert_forward_chain(text)

    def test_field_input_drop_not_shadowed_by_disjoint_interface(self):
        g.assert_field_input("-P INPUT ACCEPT\n-A INPUT -i lo -j ACCEPT\n"
                             "-A INPUT -i leg1 -j DROP\n", "leg1")

    def test_missing_conditional_or_shadowed_input_drop_fails(self):
        target = "-A INPUT -i leg1 -j DROP\n"
        for prior in ("-A INPUT -j ACCEPT", "-A INPUT -j CUSTOM",
                      "-A INPUT -m conntrack --ctstate ESTABLISHED -j ACCEPT",
                      "-A INPUT -i leg1 -p tcp -j ACCEPT",
                      "-A INPUT ! -i leg0 -j ACCEPT", "-A INPUT -i leg+ -j ACCEPT",
                      "-A INPUT -i", "-N CUSTOM"):
            with self.subTest(prior=prior), self.assertRaises(ValueError):
                g.assert_field_input("-P INPUT ACCEPT\n" + prior + "\n" + target, "leg1")
        for body in ("", "-P INPUT ACCEPT\n", "-P INPUT DROP\n",
                     "-P INPUT ACCEPT\n-A INPUT -i leg1 -p tcp -j DROP\n"):
            with self.subTest(body=body), self.assertRaises(ValueError):
                g.assert_field_input(body, "leg1")

    def test_field_route_direct_wrong_gateway_device_or_multipath_fails(self):
        for key, value in (("gateway", None), ("gateway", "10.40.40.2"),
                           ("dev", "leg1"), ("nexthops", [{"dev": "leg1"}]),
                           ("type", "blackhole")):
            data = snapshot()
            data["field_route"][0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                g.assert_host(data)

    def test_multiple_or_missing_field_or_default_routes_fail(self):
        for key in ("field_route", "default_route"):
            for count in (0, 2):
                data = snapshot()
                data[key] *= count
                with self.subTest(key=key, count=count), self.assertRaises(ValueError):
                    g.assert_host(data)

    def test_default_wrong_gateway_or_interface_fails(self):
        for key, value in (("gateway", "10.99.99.1"), ("dev", "leg3")):
            data = snapshot()
            data["default_route"][0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                g.assert_host(data)

    def test_every_device_more_specific_route_and_source_checked(self):
        for address in snapshot()["route_get"]:
            for key, value in (("gateway", None), ("dev", "leg1"),
                               ("prefsrc", g.FIELD)):
                data = snapshot()
                data["route_get"][address][0][key] = value
                with self.subTest(address=address, key=key), self.assertRaises(ValueError):
                    g.assert_host(data)


class FrameTests(unittest.TestCase):
    def test_checksum_known_vector_and_odd_length(self):
        self.assertEqual(g.checksum(bytes.fromhex("0001f203f4f5f6f7")), 0x220d)
        self.assertEqual(g.checksum(b"\x01"), 0xfeff)

    def test_frame_round_trip_all_tags(self):
        for stage in ("pre", "neg", "post"):
            for seq in range(3):
                decoded = frame(stage, seq, 1)["decoded"]
                self.assertEqual((decoded["stage"], decoded["seq"]), (stage, seq))
                self.assertTrue(decoded["udp_checksum_valid"])
                self.assertTrue(decoded["ip_checksum_valid"])

    def test_corrupted_ip_udp_payload_or_truncation_fails(self):
        original = bytes.fromhex(frame("neg", 0, 1)["hex"])
        for offset in (14, 24, 40, 43, len(original) - 1):
            raw = bytearray(original)
            raw[offset] ^= 1
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                g.decode_frame(bytes(raw), TOKEN)
        for length in (0, 14, 20, 41, len(original) - 1):
            with self.subTest(length=length), self.assertRaises(ValueError):
                g.decode_frame(original[:length], TOKEN)

    def test_zero_udp_checksum_fails(self):
        raw = bytearray.fromhex(frame("neg", 0, 1)["hex"])
        raw[40:42] = b"\0\0"
        with self.assertRaisesRegex(ValueError, "udp_header"):
            g.decode_frame(bytes(raw), TOKEN)

    def test_wrong_token_fails(self):
        with self.assertRaisesRegex(ValueError, "invalid_tag"):
            g.decode_frame(bytes.fromhex(frame("neg", 0, 1)["hex"]), "another-token")

    def test_fragment_or_wrong_protocol_fails_even_with_valid_checksum(self):
        for offset, value in ((20, 0x20), (23, 6), (22, 0)):
            raw = bytearray.fromhex(frame("neg", 0, 1)["hex"])
            raw[offset] = value
            raw[24:26] = b"\0\0"
            raw[24:26] = struct.pack("!H", g.checksum(bytes(raw[14:34])))
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                g.decode_frame(bytes(raw), TOKEN)


class TransitAssertionsTests(unittest.TestCase):
    def check(self, ingress, arrivals, sent):
        g.assert_transit_evidence(ingress, arrivals, sent, TOKEN, links())

    def test_valid_zero_arrival_evidence(self):
        self.check(*transit())

    def test_missing_readiness_or_completion_fails(self):
        for which in (0, 1):
            for index in (0, -1):
                data = list(transit())
                data[which].pop(index)
                with self.subTest(which=which, index=index), self.assertRaises(ValueError):
                    self.check(*data)

    def test_no_ingress_or_pre_or_post_control_fails(self):
        for stage in ("pre", "neg", "post"):
            data = list(transit())
            which = 0 if stage == "neg" else 1
            data[which][:] = [e for e in data[which]
                             if e.get("decoded", {}).get("stage") != stage]
            with self.subTest(stage=stage), self.assertRaises(ValueError):
                self.check(*data)

    def test_invalid_capture_fails_even_if_no_negative_arrival(self):
        data = transit()
        data[0][1]["error"] = "invalid_udp_checksum"
        with self.assertRaisesRegex(ValueError, "invalid_captured_frame"):
            self.check(*data)

    def test_capture_overflow_or_missing_statistics_cannot_prove_absence(self):
        for which in (0, 1):
            for dropped in (None, 1):
                data = transit()
                data[which][-1]["dropped"] = dropped
                with self.subTest(which=which, dropped=dropped), self.assertRaisesRegex(
                        ValueError, "observer_dropped"):
                    self.check(*data)

    def test_forwarded_frame_with_no_reply_is_exposure(self):
        data = transit()
        data[1].insert(2, frame("neg", 0, 6))
        with self.assertRaisesRegex(ValueError, "transit_exposure"):
            self.check(*data)

    def test_duplicates_and_wrong_sequence_fail(self):
        for mutation in ("duplicate", "wrong_seq", "sent"):
            data = transit()
            if mutation == "duplicate":
                data[0].insert(2, copy.deepcopy(data[0][1]))
            elif mutation == "wrong_seq":
                data[0][2] = copy.deepcopy(data[0][1])
            else:
                data[2].pop()
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.check(*data)

    def test_capture_checksum_revalidated_not_trusted(self):
        data = transit()
        raw = bytearray.fromhex(data[0][1]["hex"])
        raw[-1] ^= 1
        data[0][1]["hex"] = raw.hex()
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.check(*data)

    def test_wrong_source_mac_or_destination_mac_fails(self):
        for address in (g.SOURCE, g.OT):
            changed = links()
            changed[address]["mac"] = "02:00:00:00:ff:ff"
            data = transit()
            data[0][1] = frame("neg", 0, 5, link_map=changed)
            data[2][1] = frame("neg", 0, 4, kind="sent", link_map=changed)
            with self.subTest(address=address), self.assertRaises(ValueError):
                self.check(*data)

    def test_outgoing_capture_does_not_prove_ingress(self):
        data = transit()
        data[0][1]["packet_type"] = 4
        with self.assertRaisesRegex(ValueError, "not_ingress"):
            self.check(*data)

    def test_wrong_interface_fails(self):
        data = transit()
        data[0][0]["iface"] = "leg2"
        with self.assertRaisesRegex(ValueError, "observer_interface"):
            self.check(*data)

    def test_ttl_change_or_decoded_claim_mismatch_fails(self):
        for mutation in ("ttl", "claim"):
            data = transit()
            raw = bytearray.fromhex(data[0][1]["hex"])
            if mutation == "ttl":
                raw[22] = 63
                raw[24:26] = b"\0\0"
                raw[24:26] = struct.pack("!H", g.checksum(bytes(raw[14:34])))
                data[0][1].update(hex=raw.hex(), decoded=g.decode_frame(bytes(raw), TOKEN))
            else:
                data[0][1]["decoded"]["src"] = "10.30.30.99"
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.check(*data)

    def test_negative_not_bracketed_by_controls_fails(self):
        data = transit()
        data[1][1]["time"]["monotonic"] = 7
        with self.assertRaisesRegex(ValueError, "outside_control_window"):
            self.check(*data)


class FakeTransport:
    def __init__(self):
        self.calls = []
        self.capture = {}
        self.tick = 1
        self.fail_probe = False
        self.fail_restore = False
        self.bad_receipt = False
        self.bad_before = False
        self.bad_after = False
        self.wrong_package = False
        self.cleanup_error = False
        self.snapshots = 0

    def http(self, path, data=None):
        self.calls.append(("http", path, data))
        if path == "/api/range":
            return {"package": "eu-iec104-substation" if self.wrong_package else
                    "us-dnp3-substation", "phase": "ready",
                    "containd": {"applyId": "range", "protection": "protecting"}}
        if self.fail_restore:
            raise ValueError("http_status_409")
        return {"containd": {"applyId": "restore", "protection":
                            "degraded" if self.bad_receipt else "protecting"}}

    def command(self, container, mode, cfg=None):
        self.calls.append(("command", container, mode, cfg))
        if mode == "snapshot":
            self.snapshots += 1
            data = snapshot()
            if ((self.bad_before and self.snapshots == 1) or
                    (self.bad_after and self.snapshots == 2)):
                data["forward_chain"] = "-P FORWARD ACCEPT\n"
            return data
        if mode == "links":
            ip = g.SOURCE if container == g.PLC else g.DEST
            entry = links()[ip]
            return [{"ifname": entry["ifname"], "address": entry["mac"], "flags": ["UP"],
                     "addr_info": [{"family": "inet", "local": ip}]}]
        if self.fail_probe:
            raise ValueError("raw_socket_permission_denied")
        stage = cfg["stage"]
        result = []
        for seq in range(3 if stage == "neg" else 1):
            self.tick += 1
            result.append(frame(stage, seq, self.tick, "sent"))
            self.tick += 1
            target = g.RTAC if stage == "neg" else g.DSS
            self.capture[target].append(frame(stage, seq, self.tick))
        return result

    def observe(self, container, iface, token):
        self.calls.append(("observe", container, iface, token))
        self.capture[container] = [event("ready", self.tick, iface=iface)]
        return container

    def wait_event(self, collector, predicate):
        self.calls.append(("wait", collector))
        g.require(predicate(self.capture[collector]), "observer_readiness_or_frame_timeout")

    def finish(self, collector):
        self.tick += 1
        self.capture[collector].append(event("done", self.tick, packets=5, dropped=0))
        return self.capture[collector]

    def cleanup(self):
        self.calls.append(("cleanup",))
        if self.cleanup_error:
            raise ValueError("cleanup_error")


class GateTests(unittest.TestCase):
    def run_gate(self, fake):
        with tempfile.TemporaryDirectory() as directory:
            evidence = g.Evidence(directory, TOKEN)
            code = g.Gate(fake, evidence, TOKEN).run()
            saved = json.loads((evidence.directory / "us-rtac.json").read_text())
        return code, saved

    def test_full_offline_flow_then_cleanup_then_weak_restore(self):
        fake = FakeTransport()
        code, record = self.run_gate(fake)
        self.assertEqual(code, 0, record)
        self.assertEqual(record["result"], "PASS")
        self.assertEqual(fake.snapshots, 2)
        kinds = [e["kind"] for e in record["events"]]
        self.assertEqual(kinds, ["host_before_proven", "probe_links", "transit_proven",
                                 "host_after_proven", "observers_cleaned", "weak_restored"])
        self.assertEqual(fake.calls[-2:], [("cleanup",),
                                          ("http", "/api/firewall/apply", {"config": "weak"})])
        sends = [c for c in fake.calls if c[:1] == ("command",) and c[2] == "send"]
        self.assertEqual([s[3]["stage"] for s in sends], ["pre", "neg", "post"])
        self.assertEqual([s[1] for s in sends], [g.RTAC, g.PLC, g.RTAC])
        self.assertEqual(sends[1][3]["dst_mac"], links()[g.OT]["mac"])
        # No lifecycle commands or network mutations, and only mandated cleanup POST.
        for call in fake.calls:
            if call[0] == "command":
                self.assertIn(call[2], ("snapshot", "links", "send"))
            if call[0] == "http" and call[2] is not None:
                self.assertEqual(call, ("http", "/api/firewall/apply", {"config": "weak"}))

    def test_raw_permission_failure_still_checks_after_and_restores(self):
        fake = FakeTransport()
        fake.fail_probe = True
        code, record = self.run_gate(fake)
        self.assertEqual(code, 1)
        self.assertEqual(fake.snapshots, 2)
        self.assertIn("raw_socket_permission_denied", record["errors"][0])
        self.assertEqual(fake.calls[-1][1], "/api/firewall/apply")

    def test_host_failure_before_or_after_never_passes(self):
        for attr in ("bad_before", "bad_after"):
            fake = FakeTransport()
            setattr(fake, attr, True)
            code, record = self.run_gate(fake)
            with self.subTest(attr=attr):
                self.assertEqual(code, 1)
                self.assertEqual(record["result"], "FAIL")
                self.assertIn("forward_chain", record["errors"][0])
                self.assertEqual(fake.calls[-1][2], {"config": "weak"})

    def test_restore_error_or_bad_receipt_never_passes_or_retries(self):
        for attr in ("fail_restore", "bad_receipt"):
            fake = FakeTransport()
            setattr(fake, attr, True)
            code, record = self.run_gate(fake)
            with self.subTest(attr=attr):
                self.assertEqual(code, 1)
                self.assertIn("weak_restore_failed", record["errors"][-1])
                self.assertEqual(sum(c[:2] == ("http", "/api/firewall/apply")
                                     for c in fake.calls), 1)

    def test_cleanup_failure_fails_but_restores(self):
        fake = FakeTransport()
        fake.cleanup_error = True
        code, record = self.run_gate(fake)
        self.assertEqual(code, 1)
        self.assertIn("cleanup_error", record["errors"])
        self.assertEqual(fake.calls[-1][2], {"config": "weak"})

    def test_wrong_range_fails_without_changing_policy(self):
        fake = FakeTransport()
        fake.wrong_package = True
        code, record = self.run_gate(fake)
        self.assertEqual(code, 1)
        self.assertIn("weak_restore_not_safe", record["errors"][-1])
        self.assertEqual(fake.calls, [("http", "/api/range", None), ("cleanup",)])

    def test_unprotecting_ready_receipt_fails_and_restores(self):
        fake = FakeTransport()
        original = fake.http

        def http(path, data=None):
            result = original(path, data)
            if path == "/api/range":
                result["containd"]["protection"] = "degraded"
            return result
        fake.http = http
        code, record = self.run_gate(fake)
        self.assertEqual(code, 1)
        self.assertIn("not protecting", record["errors"][0])
        self.assertEqual(fake.snapshots, 0)
        self.assertEqual(fake.calls[-1][2], {"config": "weak"})

    def test_probe_and_after_failures_both_retained(self):
        fake = FakeTransport()
        fake.fail_probe = fake.bad_after = True
        code, record = self.run_gate(fake)
        self.assertEqual(code, 1)
        self.assertTrue(any(e.get("message") == "raw_socket_permission_denied"
                            for e in record["events"]))
        self.assertIn("forward_chain", record["errors"][0])


class TransportTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.evidence = g.Evidence(self.directory.name, TOKEN)
        self.transport = g.Transport(self.evidence, "http://fixture")

    def test_nonzero_docker_exit_preserves_raw_and_fails(self):
        with patch.object(g.subprocess, "run", return_value=subprocess.CompletedProcess(
                [], 127, b"", b"iptables missing")) as run:
            with self.assertRaisesRegex(ValueError, "tool_error"):
                self.transport.command(g.RTAC, "snapshot")
        self.assertEqual(run.call_args.kwargs["timeout"], 12)
        self.assertEqual(next(self.evidence.directory.glob("*.stderr")).read_bytes(),
                         b"iptables missing")

    def test_docker_timeout_preserves_partial_outputs_and_fails(self):
        with patch.object(g.subprocess, "run", side_effect=subprocess.TimeoutExpired(
                ["docker"], 12, output=b"partial", stderr=b"diagnostic")):
            with self.assertRaisesRegex(ValueError, "tool_timeout"):
                self.transport.command(g.RTAC, "snapshot")
        self.assertEqual(next(self.evidence.directory.glob("*.stdout")).read_bytes(), b"partial")

    def test_missing_docker_fails_distinctly(self):
        with patch.object(g.subprocess, "run", side_effect=FileNotFoundError("docker missing")):
            with self.assertRaisesRegex(ValueError, "tool_unavailable"):
                self.transport.command(g.RTAC, "snapshot")
        self.assertIn("docker missing", self.evidence.record["events"][-1]["error"])

    def test_http_409_retained_and_never_retried(self):
        error = urllib.error.HTTPError("http://fixture", 409, "conflict", {},
                                       io.BytesIO(b'{"error":"busy"}'))
        with patch.object(g.urllib.request, "urlopen", side_effect=error) as urlopen:
            with self.assertRaisesRegex(ValueError, "http_status_409"):
                self.transport.http("/api/firewall/apply", {"config": "weak"})
        self.assertEqual(urlopen.call_count, 1)
        self.assertTrue(error.fp.closed)
        self.assertEqual(urlopen.call_args.kwargs["timeout"], 5)
        self.assertEqual(next(self.evidence.directory.glob("*.body")).read_bytes(),
                         b'{"error":"busy"}')

    def test_http_tool_error_is_distinct_from_denial(self):
        with patch.object(g.urllib.request, "urlopen", side_effect=urllib.error.URLError("refused")):
            with self.assertRaisesRegex(ValueError, "http_transport_error"):
                self.transport.http("/api/range")

    def test_collector_missing_readiness_or_error_fails(self):
        collector = {"process": Mock()}
        collector["process"].poll.return_value = 1
        with patch.object(self.transport, "events", return_value=[]):
            with self.assertRaisesRegex(ValueError, "observer_exited_early"):
                self.transport.wait_event(collector, bool)
        with patch.object(self.transport, "events", return_value=[{"error": "checksum"}]):
            with self.assertRaisesRegex(ValueError, "invalid_captured_frame"):
                self.transport.wait_event(collector, bool)

    def test_observer_stderr_retained_on_nonzero_exit(self):
        proc = Mock()
        proc.wait.return_value = 1
        with patch.object(g.subprocess, "Popen", return_value=proc):
            collector = self.transport.observe(g.RTAC, "leg0", TOKEN)
        collector["err"].write(b"raw socket permission denied")
        collector["err"].flush()
        with self.assertRaisesRegex(ValueError, "observer_tool_error"):
            self.transport.finish(collector)
        with self.assertRaisesRegex(ValueError, "cleanup_failed"):
            self.transport.cleanup()
        self.assertIn("permission denied", next(
            self.evidence.directory.glob("*capture.stderr")).read_text())

    def test_partial_capture_line_is_not_parsed_as_readiness(self):
        path = self.evidence.directory / "fixture.jsonl"
        path.write_text('{"event":"ready"}\n{"event":')
        self.assertEqual(self.transport.events({"path": path}), [{"event": "ready"}])

    def test_helper_argv_only_streams_source_no_install_or_mutation(self):
        argv = self.transport.argv(g.RTAC, "snapshot", {})
        self.assertEqual(argv[:8], ["docker", "exec", "-i", g.RTAC, "python3", "-u", "-c",
                                    Path(g.__file__).read_text()])
        self.assertEqual(argv[8:], ["--helper", "snapshot", "{}"])

    def test_observer_poll_timeout_is_bounded(self):
        collector = {"process": Mock()}
        collector["process"].poll.return_value = None
        with patch.object(self.transport, "events", return_value=[]), \
                patch.object(g.time, "monotonic", side_effect=[0, 1, 5]), \
                patch.object(g.time, "sleep"):
            with self.assertRaisesRegex(ValueError, "observer_readiness_or_frame_timeout"):
                self.transport.wait_event(collector, bool)

    def test_observer_cleanup_timeout_fails_and_kills_only_owned_process(self):
        proc = Mock()
        proc.wait.side_effect = [subprocess.TimeoutExpired("docker", 22), 0]
        with patch.object(g.subprocess, "Popen", return_value=proc):
            self.transport.observe(g.RTAC, "leg0", TOKEN)
        with self.assertRaisesRegex(ValueError, "cleanup_failed"):
            self.transport.cleanup()
        proc.kill.assert_called_once_with()
        self.assertEqual([c.kwargs["timeout"] for c in proc.wait.call_args_list], [22, 2])


class HelperTests(unittest.TestCase):
    def test_snapshot_reads_exact_host_controls_without_mutations(self):
        data = snapshot()
        outputs = [json.dumps(data["addresses"]), data["forward_chain"], data["input_chain"],
                   json.dumps(data["field_route"]), json.dumps(data["default_route"])]
        outputs += [json.dumps(data["route_get"]["10.40.40." + str(i)])
                    for i in (20, 21, 22, 23)]
        calls = [subprocess.CompletedProcess([], 0, output, "") for output in outputs]
        with patch.object(g.subprocess, "run", side_effect=calls) as run, \
                patch.object(g.glob, "glob", return_value=[
                    p for p in data["forwarding"] if p.endswith("/forwarding")]), \
                patch.object(g.Path, "read_text", return_value="0"), \
                contextlib.redirect_stderr(io.StringIO()):
            result = g.helper_snapshot()
        self.assertEqual(g.assert_host(result), g.assert_host(data))
        expected = [("ip", "-j", "-4", "addr", "show"),
                    ("iptables", "-S", "FORWARD"), ("iptables", "-S", "INPUT"),
                    ("ip", "-j", "-4", "route", "show", "table", "main", "exact",
                     "10.40.40.0/24"),
                    ("ip", "-j", "-4", "route", "show", "default")]
        expected += [("ip", "-j", "-4", "route", "get", "10.40.40." + str(i))
                     for i in (20, 21, 22, 23)]
        self.assertEqual([c.args[0] for c in run.call_args_list], expected)
        self.assertTrue(all(c.kwargs["timeout"] == 3 for c in run.call_args_list))

    def test_observer_readiness_decodes_incoming_and_ignores_unrelated_and_outgoing(self):
        raw = bytes.fromhex(frame("neg", 0, 1)["hex"])
        sock = Mock()
        sock.__enter__ = Mock(return_value=sock)
        sock.__exit__ = Mock(return_value=False)
        sock.getsockopt.return_value = struct.pack("=II", 3, 0)
        sock.recvfrom.side_effect = [(b"unrelated", ("leg0", 0, 0)),
                                     (raw, ("leg0", 0, 4)), (raw, ("leg0", 0, 0))]
        # stamp() also reads monotonic, so give the loop a deterministic budget.
        with patch.object(g.socket, "socket", return_value=sock), \
                patch.object(g.socket, "AF_PACKET", 17, create=True), \
                patch.object(g, "stamp", return_value={"monotonic": 1}), \
                patch.object(g.time, "monotonic", side_effect=[0, 1, 2, 3, 19]), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            g.helper("observe", {"iface": "leg0", "token": TOKEN})
        events = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual([e["event"] for e in events], ["ready", "frame", "done"])
        self.assertEqual(events[1]["decoded"]["seq"], 0)
        self.assertEqual(events[-1]["dropped"], 0)
        sock.getsockopt.assert_called_once_with(263, 6, 8)
        sock.bind.assert_called_once_with(("leg0", 0))
        sock.settimeout.assert_called_once_with(0.2)

    def test_invalid_frame_keeps_raw_bytes_and_error_in_capture(self):
        raw = bytearray.fromhex(frame("neg", 0, 1)["hex"])
        raw[-1] ^= 1
        sock = Mock()
        sock.__enter__ = Mock(return_value=sock)
        sock.__exit__ = Mock(return_value=False)
        sock.getsockopt.return_value = struct.pack("=II", 1, 0)
        sock.recvfrom.return_value = (bytes(raw), ("leg0", 0, 0))
        with patch.object(g.socket, "socket", return_value=sock), \
                patch.object(g.socket, "AF_PACKET", 17, create=True), \
                patch.object(g, "stamp", return_value={"monotonic": 1}), \
                patch.object(g.time, "monotonic", side_effect=[0, 1, 19]), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            g.helper("observe", {"iface": "leg0", "token": TOKEN})
        events = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(events[1]["hex"], raw.hex())
        self.assertEqual(events[1]["error"], "invalid_udp_checksum")

    def test_missing_raw_socket_permission_fails(self):
        with patch.object(g.socket, "AF_PACKET", 17, create=True), \
                patch.object(g.socket, "socket", side_effect=PermissionError("denied")):
            with self.assertRaises(PermissionError):
                g.helper("observe", {"iface": "leg0", "token": TOKEN})

    def test_snapshot_nonzero_iptables_exit_retains_stderr(self):
        calls = [
            subprocess.CompletedProcess([], 0, json.dumps(addresses(g.LEGS)), ""),
            subprocess.CompletedProcess([], 1, "", "iptables: permission denied")]
        with patch.object(g.subprocess, "run", side_effect=calls), \
                patch.object(g.glob, "glob", return_value=[]), \
                patch.object(g.Path, "read_text", return_value="0"), \
                contextlib.redirect_stderr(io.StringIO()) as output:
            with self.assertRaisesRegex(ValueError, "container_tool_error"):
                g.helper_snapshot()
        self.assertIn("permission denied", output.getvalue())
        self.assertIn('"exit_code": 1', output.getvalue())

    def test_snapshot_unreadable_forwarding_is_fatal(self):
        with patch.object(g.subprocess, "run", return_value=subprocess.CompletedProcess(
                [], 0, json.dumps(addresses(g.LEGS)), "")), \
                patch.object(g.glob, "glob", return_value=[]), \
                patch.object(g.Path, "read_text", side_effect=PermissionError("unreadable")), \
                contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(PermissionError):
                g.helper_snapshot()

    def test_helper_send_is_bound_to_discovered_interface_and_exact_mac(self):
        sock = Mock()
        sock.__enter__ = Mock(return_value=sock)
        sock.__exit__ = Mock(return_value=False)
        sock.send.side_effect = lambda raw: len(raw)
        cfg = {"iface": "plc-ot", "src": g.SOURCE, "src_mac": links()[g.SOURCE]["mac"],
               "dst_mac": links()[g.OT]["mac"], "token": TOKEN, "stage": "neg"}
        with patch.object(g.socket, "socket", return_value=sock), \
                patch.object(g.socket, "AF_PACKET", 17, create=True), \
                patch.object(g.time, "sleep"), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            g.helper("send", cfg)
        emitted = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(len(emitted), 3)
        self.assertEqual([e["decoded"]["seq"] for e in emitted], [0, 1, 2])
        self.assertEqual(emitted[0]["decoded"]["dst_mac"], "020000000001")
        sock.bind.assert_called_once_with(("plc-ot", 0))
        sock.settimeout.assert_called_once_with(1)

    def test_helper_syntax_is_python36_compatible(self):
        import ast
        # The source is transported in full, including host-only code.
        ast.parse(Path(g.__file__).read_text(), feature_version=(3, 6))


if __name__ == "__main__":
    unittest.main()
