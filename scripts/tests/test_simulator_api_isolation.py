"""G3 offline contract tests. Never invoke Docker or contact the range."""
import copy
from datetime import timezone
import http.client
import http.server
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch
import urllib.error

import yaml

from scripts.lib import simulator_api_isolation as g3

US = "us-dnp3-substation"
EU = "eu-iec104-substation"
NOW = 1793534400.0


def iso(seconds=0):
    return g3.datetime.fromtimestamp(NOW + seconds, timezone.utc).isoformat()


def observed(row, outcome="connected", source="10.10.10.50"):
    return dict(row, outcome=outcome, local=[source, 32123], started_at=NOW,
                ended_at=NOW + 2.5, elapsed=2.5)


def bodies(package, seq=1):
    if package == US:
        state = {"devices": {s: {"state": True} for s in ("relay", "recloser", "regulator", "capbank")},
                 "device_comms": {s: True for s in ("relay", "recloser", "regulator", "capbank")},
                 "physics": {"stale": False, "solved_at": iso(seq), "last_good_at": iso(seq)}}
        return {"/api/substation/state": state, "/api/substation/health": dict(state, status="ok")}
    cat = g3.load_inventory(EU)
    return {
        "/api/field/state": {
            "devices": {s: {"state": True} for s in
                        ("breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1")},
            "clock": {"status": "locked"},
            "process": {"truth_stale": False, "solver_epoch": "epoch",
                        "solve_seq": seq, "solved_at": iso(seq)}},
        "/api/operator/state": {"package_id": EU, "range_generation": 7,
                                "link": {"state": "started", "last_rx_at": iso(seq)},
                                "rendered_at": iso(seq), "points": [{"id": "point"}]},
        "/api/historian/points": [{"id": p, "last": {"ingested_at": iso(seq)}}
                                 for p in cat["point_ids"]],
    }


class CatalogueTests(unittest.TestCase):
    def test_cardinality_and_verdict_counts(self):
        for package, counts in ((US, ((156, 90, 66), (156, 2, 154))),
                                (EU, ((90, 25, 65), (90, 2, 88)))):
            cat = g3.load_inventory(package)
            for policy, (count, allow, deny) in zip(g3.POLICIES, counts):
                rows = g3.expand_matrix(cat, package, policy)
                self.assertEqual(len(rows), count)
                self.assertEqual(sum(r["expected"] == "A" for r in rows), allow)
                self.assertEqual(sum(r["expected"] == "D" for r in rows), deny)
                self.assertEqual(len({r["id"] for r in rows}), count)
                self.assertEqual(rows, g3.expand_matrix(cat, package, policy))

    def test_source_sets(self):
        self.assertEqual(g3.load_inventory(EU)["sources"], ["kali", "vendor_jump", "eng_workstation"])
        self.assertEqual(g3.load_inventory(US)["sources"],
                         ["kali", "corp_ws", "vendor_jump", "eng_workstation"])

    def test_exact_alias_inventory(self):
        eu = g3.load_inventory(EU)
        us = g3.load_inventory(US)
        actual_eu = {(d["host"], d["port"]) for d in eu["destinations"]}
        expected_eu = {("10.99.99.21", 8080), ("10.30.30.20", 2404)}
        for field, process, mgmt in ((20, 30, 22), (21, 31, 24), (22, 32, 25),
                                    (23, 33, 26), (24, 34, 28)):
            expected_eu.update(((f"10.40.40.{field}", 502), (f"10.50.50.{process}", 8080),
                                (f"10.99.99.{mgmt}", 8080)))
        expected_eu.update((host, port) for host, ports in (
            ("10.50.50.20", (8080,)), ("10.99.99.23", (8080,)),
            ("10.30.30.50", (8080, 502)), ("10.99.99.27", (8080, 502)),
            ("10.30.30.30", (8080, 502)), ("10.99.99.30", (8080, 502)),
            ("10.70.70.40", (8080, 8081)), ("10.99.99.40", (8080,))) for port in ports)
        self.assertEqual(actual_eu, expected_eu)
        expected_us = {(host, p) for host in ("10.30.30.20", "10.40.40.10",
                                             "10.50.50.10", "10.99.99.20")
                       for p in (8080, 502, 20000, 443)}
        expected_us.update((f"10.40.40.{ip}", p) for ip in range(20, 24) for p in (8080, 502, 20000))
        expected_us.update((f"10.30.30.{ip}", p) for ip in (40, 50) for p in (8080, 502))
        expected_us.update({("10.50.50.20", 8080), ("10.30.30.10", 1881), ("10.99.99.50", 1881)})
        expected_us.update((host, p) for host in ("10.30.30.30", "10.99.99.30") for p in (8080, 502))
        self.assertEqual({(d["host"], d["port"]) for d in us["destinations"]}, expected_us)

    def test_policy_exceptions(self):
        for package, host, port, source, expected in (
            (EU, "10.30.30.30", 8080, "eng_workstation", "A"),
            (EU, "10.70.70.40", 8080, "kali", "A"),
            (EU, "10.30.30.30", 8080, "vendor_jump", "D"),
            (US, "10.30.30.20", 443, "vendor_jump", "A"),
            (US, "10.30.30.20", 443, "corp_ws", "D")):
            rows = g3.expand_matrix(g3.load_inventory(package), package, "improved")
            self.assertEqual(next(r for r in rows if (r["host"], r["port"], r["source"]) ==
                                  (host, port, source))["expected"], expected)

    def test_compose_modes_manifest_and_namespace_aliases(self):
        for package in (US, EU):
            cat = g3.load_inventory(package)
            manifest = {s["key"]: s for s in cat["services"]}
            for mode in ("source", "release"):
                compose = yaml.safe_load((g3.ROOT / "lab-definitions/packages" / package /
                                          f"compose.{mode}.yml").read_text())["services"]
                self.assertEqual(set(compose), set(manifest))
                for name, service in compose.items():
                    self.assertEqual(service["container_name"], manifest[name]["container"])
                    expected = {i["network"]: i["ipv4"] for i in manifest[name]["interfaces"]}
                    self.assertEqual({n: v["ipv4_address"] for n, v in
                                      service.get("networks", {}).items()}, expected)
                if package == US:
                    self.assertEqual(compose["hmi_poller"]["network_mode"], "service:fuxa_hmi")

    def test_unknown_package_policy_empty_and_duplicate(self):
        with self.assertRaises(ValueError):
            g3.load_inventory("other")
        for policy in ("hardened", "", "Improved"):
            with self.assertRaises(ValueError):
                g3.expand_matrix(g3.load_inventory(US), US, policy)
        with self.assertRaises(ValueError):
            g3.expand_matrix({"sources": [], "destinations": []}, US, "weak")

    def test_inventory_rejects_unreviewed_mutations(self):
        original = json.loads(g3.MATRIX.read_text())
        mutations = [
            lambda c: c["destinations"].append(copy.deepcopy(c["destinations"][0])),
            lambda c: c["sources"].append("invented"),
            lambda c: c["destinations"][0]["expected"].update(other="D"),
            lambda c: c["destinations"][0].update(host="10.99.99.199"),
            lambda c: c["destinations"].clear(),
            lambda c: c["services"].pop(),
            lambda c: c["reviewed_inputs"].update({"compose.source.yml": "0" * 64}),
        ]
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "matrix.json"
            for mutate in mutations:
                matrix = copy.deepcopy(original)
                mutate(matrix["packages"][EU])
                path.write_text(json.dumps(matrix))
                with self.assertRaises(ValueError):
                    g3.load_inventory(EU, matrix_path=path)

    def test_no_plc_dnp3_or_invented_eu_corporate(self):
        for package in (US, EU):
            cat = g3.load_inventory(package)
            self.assertFalse(any(d["port"] == 20000 and d["service"].startswith("openplc")
                                 for d in cat["destinations"]))
        self.assertNotIn("corp_ws", g3.load_inventory(EU)["sources"])


class ProbeTests(unittest.TestCase):
    def setUp(self):
        self.row = g3.expand_matrix(g3.load_inventory(US), US, "improved")[0]

    def test_connected_and_timeout(self):
        for verdict, outcome in (("A", "connected"), ("D", "timeout")):
            row = dict(self.row, expected=verdict)
            result = g3.check_rows([row], [observed(row, outcome)], "10.10.10.50")
            self.assertTrue(result[0]["passed"])

    def test_refusal_no_route_and_http_errors_never_deny(self):
        for outcome in ("refused", "transport-error", "connected"):
            row = observed(self.row, outcome)
            for status in (401, 403, 404, 405):
                row["http_status"] = status
                self.assertFalse(g3.check_rows([self.row], [row], "10.10.10.50")[0]["passed"])

    def test_evidence_rejects_partial_duplicate_reordered_wrong_source(self):
        other = dict(self.row, id="other")
        expected = [self.row, other]
        for actual in ([], [observed(self.row)], [observed(self.row)] * 2,
                       [observed(other), observed(self.row)]):
            with self.assertRaises(ValueError):
                g3.check_rows(expected, actual, "10.10.10.50")
        for changes in ({"source": "corp_ws"}, {"host": "10.0.0.1"}, {"local": ["10.99.99.10", 1]},
                        {"local": ["0.0.0.0", 0]}, {"outcome": "unknown"}, {"elapsed": -1},
                        {"elapsed": float("nan")}, {"started_at": None}):
            with self.assertRaises(ValueError):
                g3.check_rows([self.row], [dict(observed(self.row), **changes)], "10.10.10.50")

    def test_timeout_must_belong_to_probe_and_use_full_budget(self):
        row = dict(observed(self.row, "timeout"), elapsed=.2)
        with self.assertRaises(ValueError):
            g3.check_rows([self.row], [row], "10.10.10.50")

    def test_cached_socket_evidence_outside_control_window_fails(self):
        with self.assertRaisesRegex(ValueError, "predates"):
            g3.check_rows([self.row], [observed(self.row)], "10.10.10.50",
                          not_before=NOW + 10)

    def test_student_program_unbound_and_bounded(self):
        self.assertNotIn(".bind(", g3.SOCKET_PROGRAM)
        self.assertIn("getsockname", g3.SOCKET_PROGRAM)
        self.assertIn("max_workers=8", g3.SOCKET_PROGRAM)
        self.assertIn("settimeout(2.5)", g3.SOCKET_PROGRAM)
        self.assertNotIn(".send", g3.SOCKET_PROGRAM)

    def test_injected_socket_errors_are_classified_without_binding(self):
        for error, outcome in ((TimeoutError(), "timeout"), (ConnectionRefusedError(), "refused"),
                               (OSError(101, "Network is unreachable"), "transport-error")):
            with patch.object(g3.socket, "socket") as factory:
                sock = factory.return_value.__enter__.return_value
                sock.connect.side_effect = error
                sock.getsockname.return_value = ("10.10.10.50", 12345)
                self.assertEqual(g3.socket_probe(self.row)["outcome"], outcome)
                sock.settimeout.assert_called_once_with(2.5)
                sock.bind.assert_not_called()

    def test_real_loopback_connect_and_refusal(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen(1)
            target = dict(self.row, host="127.0.0.1", port=listener.getsockname()[1], expected="A")
            row = g3.socket_probe(target)
            self.assertTrue(g3.check_rows([target], [row], "127.0.0.1")[0]["passed"])
            peer, _ = listener.accept()
            peer.close()
        self.assertEqual(g3.socket_probe(target)["outcome"], "refused")

    def test_remote_program_uses_same_probe(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen(1)
            row = dict(self.row, host="127.0.0.1", port=listener.getsockname()[1], expected="A")
            result = subprocess.run(["python3", "-c", g3.SOCKET_PROGRAM, json.dumps([row])],
                                    text=True, capture_output=True, timeout=10, check=True)
            self.assertTrue(g3.check_rows([row], json.loads(result.stdout), "127.0.0.1")[0]["passed"])
            peer, _ = listener.accept()
            peer.close()


class ControlTests(unittest.TestCase):
    def check(self, package, value, previous=None, now=NOW + 2):
        return g3.check_backend_controls(package, value, 7, now, previous,
                                         g3.load_inventory(EU).get("point_ids", ()))

    def test_fresh_advancing_application_bodies(self):
        for package in (US, EU):
            marker = self.check(package, bodies(package))
            self.check(package, bodies(package, 2), marker)

    def test_cached_and_stale_bodies(self):
        for package in (US, EU):
            marker = self.check(package, bodies(package))
            with self.assertRaises(ValueError):
                self.check(package, bodies(package), marker)
            with self.assertRaises(ValueError):
                self.check(package, bodies(package), now=NOW + 60)

    def test_timestamp_requires_explicit_timezone(self):
        with self.assertRaisesRegex(ValueError, "no timezone"):
            g3.fresh("2026-11-01T12:00:00", NOW)

    def test_us_missing_devices_comms_stale(self):
        for mutate in (lambda b: b["/api/substation/state"]["devices"].pop("relay"),
                       lambda b: b["/api/substation/health"]["device_comms"].update(relay=False),
                       lambda b: b["/api/substation/state"]["physics"].update(stale=True)):
            value = bodies(US)
            mutate(value)
            with self.assertRaises(ValueError):
                self.check(US, value)

    def test_eu_missing_devices_clock_wrong_generation_historian(self):
        mutations = (
            lambda b: b["/api/field/state"]["devices"].pop("f2-breaker"),
            lambda b: b["/api/field/state"].update(clock=None),
            lambda b: b["/api/field/state"]["process"].update(truth_stale=True),
            lambda b: b["/api/operator/state"].update(range_generation=8),
            lambda b: b["/api/operator/state"]["link"].update(state="disconnected"),
            lambda b: b["/api/historian/points"].pop(),
            lambda b: b["/api/historian/points"][0]["last"].update(ingested_at=iso(-60)),
        )
        for mutate in mutations:
            value = bodies(EU)
            mutate(value)
            with self.assertRaises(ValueError):
                self.check(EU, value)

    def test_proc_tables_missing_unknown_and_alias(self):
        cat = g3.load_inventory(EU)
        raw = "  0: 1500630A:1F90 00000000:0000 0A\n"
        self.assertEqual(g3.listener_ports(raw), [("10.99.0.21", 8080)])
        with self.assertRaises(ValueError):
            g3.check_listeners(cat, "control_centre", raw)
        for raw in ("", "  0: 00000000:1234 00000000:0000 0A\n"):
            with self.assertRaises(ValueError):
                g3.check_listeners(cat, "control_centre", raw)
        g3.check_listeners(cat, "control_centre", " 0: 1563630A:1F90 0:0 0A\n")
        self.assertEqual(g3.listener_ports(" 0: " + "0" * 32 + ":1F90 0:0 0A\n"), [("::", 8080)])

    def test_kernel_tcp_table_headers_queues_and_non_listening_states(self):
        raw = """  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt uid
  0: 1563630A:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0
  1: 1563630A:C001 0A63630A:1F90 01 00000000:00000000 00:00000000 00000000 0
  sl  local_address                         remote_address                        st
  2: 00000000000000000000000000000000:01F6 00000000000000000000000000000000:0000 0A
"""
        self.assertEqual(g3.listener_ports(raw), [("10.99.99.21", 8080), ("::", 502)])

    def test_widened_bind_on_known_port_is_an_unclassified_alias(self):
        cat = g3.load_inventory(EU)
        for host in ("00000000", "00000000000000000000000000000000"):
            with self.assertRaisesRegex(ValueError, "unclassified listener alias"):
                g3.check_listeners(cat, "feeder_breaker",
                                   f" 0: {host}:1F90 0:0 0A\n 1: 1428280A:01F6 0:0 0A\n")


class FakeTransport:
    def __init__(self, cat, package):
        self.cat, self.package = cat, package
        self.calls = []
        self.seq = 0
        self.clock = NOW
        self.fail = None
        self.identity = {"package": package, "phase": "ready", "generation": 7}

    def http(self, path, body=None, json_body=True):
        self.calls.append(("http", path, body))
        if path == "/api/range":
            return self.identity
        if path == "/api/firewall/apply":
            if self.fail:
                self.fail(body)
            return {"containd": {"applyId": "receipt", "protection": "protecting"}}
        if path.startswith("/apps/"):
            return "<html>OpenPLC</html>" if "openplc" in path else "<html>FUXA</html>"
        if path in ("/api/field/state", "/api/substation/state"):
            self.seq += 1
            self.clock += 1
        value = bodies(self.package, self.seq)[path]
        return json.loads(json.dumps(value).replace(iso(self.seq), iso(self.clock - NOW)))

    def exec(self, container, argv, timeout=15):
        self.calls.append(("exec", container, argv))
        if argv[0] == "cat":
            service = next(s["key"] for s in self.cat["services"] if s["container"] == container)
            aliases = {(d["host"], d["port"]) for d in self.cat["destinations"]
                       if d["service"] == service}
            return "".join(f" 0: {socket.inet_aton(host)[::-1].hex()}:{p:04X} 0:0 0A\n"
                           for host, p in aliases)
        if argv[0] == "wget":
            return "{}"
        if argv[0] == "sh":
            return ""
        if argv[2] == g3.HTTP_PROGRAM:
            return json.dumps({"status": 200, "body": "{}"})
        if argv[2] == g3.PREFLIGHT_PROGRAM:
            return json.dumps({"ready": True, "hostname": container, "time": self.clock})
        if argv[2] == g3.SOCKET_PROGRAM:
            rows = json.loads(argv[3])
            source = next(s for s in self.cat["services"] if s["container"] == container)
            address = next(i["ipv4"] for i in source["interfaces"])
            actual = [dict(observed(r, "connected" if r["expected"] == "A" else "timeout",
                                    address), started_at=self.clock, ended_at=self.clock + 2.5)
                      for r in rows]
            self.clock += 3
            return json.dumps(actual)
        return str(self.clock)


class OrchestrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.cat = g3.load_inventory(US)
        self.evidence = g3.Evidence(self.temp.name, US)
        self.io = FakeTransport(self.cat, US)
        self.gate = g3.Gate(US, self.cat, self.io, self.evidence)

    def test_full_both_packages_order_controls_cleanup_and_counts(self):
        for package in (US, EU):
            with self.subTest(package=package):
                cat = g3.load_inventory(package)
                io = FakeTransport(cat, package)
                evidence = g3.Evidence(self.temp.name, package)
                g3.Gate(package, cat, io, evidence).run()
                policies = [c[2]["config"] for c in io.calls if c[:2] == ("http", "/api/firewall/apply")]
                self.assertEqual(policies, ["weak", "improved", "weak"])
                self.assertTrue(evidence.record["passed"])
                stages = [(e["policy"], e["stage"]) for e in evidence.record["events"]
                          if e["kind"] == "controls-complete"]
                self.assertEqual(stages, [("weak", "before"), ("weak", "after"),
                                          ("improved", "before"), ("improved", "after")])
                batches = [e for e in evidence.record["events"] if e["kind"] == "student-batch"]
                self.assertEqual(sum(e["observed_count"] for e in batches), 312 if package == US else 180)
                self.assertEqual(evidence.record["events"][-1]["kind"], "restored")
                self.assertFalse(any("reset" in str(c) for c in io.calls))
                for call in io.calls:
                    if call[0] == "exec":
                        self.assertIn(call[2][0], ("python3", "cat", "wget", "sh"))
                        self.assertNotIn("route", call[2])
                        self.assertNotIn("--privileged", call[2])
                    elif call[2] is not None:
                        self.assertEqual(call[1], "/api/firewall/apply")
                        self.assertIn(call[2]["config"], ("weak", "improved"))

    def test_wrong_package_phase_generation_never_mutates(self):
        for identity in ({"package": EU, "phase": "ready", "generation": 7},
                         {"package": US, "phase": "starting", "generation": 7},
                         {"package": US, "phase": "ready", "generation": 0}):
            self.io.identity = identity
            with self.assertRaises(ValueError):
                self.gate.run()
        self.assertFalse(any(c[1] == "/api/firewall/apply" for c in self.io.calls))

    def test_generation_change_is_detected_before_mutation(self):
        self.gate.identity()
        self.io.identity["generation"] = 8
        with self.assertRaises(ValueError):
            self.gate.apply("improved")

    def test_range_and_peer_envelope_json_shape_fail_closed(self):
        for identity in (None, [], "ready"):
            self.io.identity = identity
            with self.assertRaisesRegex(ValueError, "identity/phase"):
                self.gate.identity()
        destination = self.cat["destinations"][0]
        for envelope in (None, [], {}, {"status": "200", "body": "{}"},
                         {"status": 200, "body": []}):
            with patch.object(self.io, "exec", return_value=json.dumps(envelope)):
                with self.assertRaisesRegex(ValueError, "HTTP peer failed"):
                    self.gate.peer_control(destination)

    def test_missing_corporate_python_fails_preflight_then_restores(self):
        original = self.io.exec

        def execute(container, argv, **kwargs):
            if container == self.gate.container("corp_ws"):
                raise ValueError("missing python3")
            return original(container, argv, **kwargs)

        self.io.exec = execute
        with self.assertRaisesRegex(ValueError, "missing python3"):
            self.gate.run()
        policies = [c[2]["config"] for c in self.io.calls if c[:2] == ("http", "/api/firewall/apply")]
        self.assertEqual(policies, ["weak"])
        self.assertFalse(self.evidence.record["passed"])

    def test_portal_frontend_fallback_is_not_a_positive(self):
        original = self.io.http

        def http(path, *args, **kwargs):
            return "<html>RangerDanger</html>" if path.startswith("/apps/") else original(
                path, *args, **kwargs)

        self.io.http = http
        with self.assertRaisesRegex(ValueError, "not the upstream"):
            self.gate.controls("weak", "before")

    def test_all_failure_sites_restore_and_never_pass(self):
        for method in ("apply", "controls", "probe"):
            with self.subTest(method=method):
                self.setUp()
                original = getattr(self.gate, method)
                calls = 0

                def fail_once(*args, **kwargs):
                    nonlocal calls
                    calls += 1
                    if calls == 1:
                        raise ValueError("injected failure")
                    return original(*args, **kwargs)

                with patch.object(self.gate, method, side_effect=fail_once):
                    with self.assertRaisesRegex(ValueError, "injected"):
                        self.gate.run()
                self.assertFalse(self.evidence.record["passed"])
                self.assertEqual(self.evidence.record["events"][-1]["kind"], "restored")

    def test_after_controls_run_on_probe_failure(self):
        with patch.object(self.gate, "probe", side_effect=ValueError("probe")):
            with self.assertRaises(ValueError):
                self.gate.run()
        # TCP peer controls also call probe, so use the orchestration boundary.
        with patch.object(self.gate, "controls") as controls:
            with patch.object(self.gate, "probe", side_effect=ValueError("student")):
                with self.assertRaises(ValueError):
                    self.gate.run()
            self.assertEqual(controls.call_args_list[1].args, ("weak", "after"))

    def test_control_poll_only_waits_for_valid_unchanged_marker(self):
        self.gate.marker = ("same",)
        with patch.object(g3, "check_backend_controls", side_effect=[
                ("same",), ("new",), ("new",)]), patch.object(g3.time, "sleep") as sleep:
            self.gate.controls("weak", "before")
        sleep.assert_called_once_with(.5)
        self.assertEqual(self.gate.marker, ("new",))

    def test_control_poll_cannot_pass_cached_forever(self):
        self.gate.marker = ("same",)
        with patch.object(g3, "check_backend_controls", return_value=("same",)), \
                patch.object(g3.time, "sleep") as sleep:
            with self.assertRaisesRegex(ValueError, "did not advance"):
                self.gate.controls("weak", "before")
        self.assertEqual(sleep.call_count, 5)

    def test_unhealthy_application_is_not_polled_or_retried(self):
        with patch.object(g3, "check_backend_controls", side_effect=ValueError("unhealthy")) as check, \
                patch.object(g3.time, "sleep") as sleep:
            with self.assertRaisesRegex(ValueError, "unhealthy"):
                self.gate.controls("weak", "before")
        self.assertEqual(check.call_count, 1)
        sleep.assert_not_called()

    def test_keyboard_interrupt_restores_before_propagating(self):
        with patch.object(self.gate, "controls"):
            with patch.object(self.gate, "probe", side_effect=KeyboardInterrupt):
                with self.assertRaises(KeyboardInterrupt):
                    self.gate.run()
        self.assertEqual(self.evidence.record["events"][-1]["kind"], "restored")
        self.assertFalse(self.evidence.record["passed"])

    def test_post_control_window_rejects_future_socket_evidence(self):
        self.gate.student_rows = [{"ended_at": NOW + 100}]
        with self.assertRaisesRegex(ValueError, "extends beyond"):
            self.gate.controls("weak", "after")

    def test_nonprotecting_receipt_fails_and_restores(self):
        original = self.io.http
        count = 0

        def http(path, body=None, **kwargs):
            nonlocal count
            if path == "/api/firewall/apply":
                count += 1
                if count == 1:
                    return {"containd": {"applyId": "id", "protection": "pending"}}
            return original(path, body, **kwargs)

        self.io.http = http
        with self.assertRaisesRegex(ValueError, "not protecting"):
            self.gate.run()
        self.assertEqual(self.evidence.record["events"][-1]["kind"], "restored")

    def test_improved_409_is_not_retried_weak_is_restored(self):
        def fail(body):
            if body["config"] == "improved":
                raise urllib.error.HTTPError("url", 409, "conflict", {}, None)
        self.io.fail = fail
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.gate.run()
        failure.exception.close()
        policies = [c[2]["config"] for c in self.io.calls if c[:2] == ("http", "/api/firewall/apply")]
        self.assertEqual(policies, ["weak", "improved", "weak"])

    def test_restoration_failure_propagates(self):
        def fail(body):
            if body["config"] == "weak" and self.io.seq > 0:
                raise ValueError("restore failed")
        self.io.fail = fail
        with self.assertRaisesRegex(RuntimeError, "Weak restoration failed"):
            self.gate.run()
        self.assertFalse(self.evidence.record["passed"])

    def test_malformed_truncated_results_fail(self):
        for raw in ("[", "[]", "null", "{}"):
            with patch.object(self.io, "exec", return_value=raw):
                with self.assertRaises((ValueError, TypeError)):
                    self.gate.probe("kali", g3.expand_matrix(self.cat, US, "weak")[:2])

    def test_exact_student_exec_container(self):
        rows = g3.expand_matrix(self.cat, US, "weak")
        for source in self.cat["sources"]:
            self.gate.probe(source, [r for r in rows if r["source"] == source])
        for call, source in zip(self.io.calls, self.cat["sources"]):
            self.assertEqual(call[1], self.gate.container(source))
            self.assertEqual(call[2][0], "python3")


class TransportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.evidence = g3.Evidence(self.temp.name, US)

    def test_exec_bounded_errors_raw_retained(self):
        for code in (1, 127, -15):
            run = Mock(return_value=subprocess.CompletedProcess([], code, "raw", "error"))
            io = g3.Transport("http://localhost:8088", self.evidence, run)
            with self.assertRaises(ValueError):
                io.exec("rangerdanger-corp-ws", ["python3", "-c", "pass"])
            self.assertEqual(run.call_args.kwargs["timeout"], 15)
            self.assertEqual(run.call_args.args[0][:3], ["docker", "exec", "rangerdanger-corp-ws"])
        self.assertTrue(list(Path(self.temp.name).glob("*exec.txt")))

    def test_exec_timeout_and_missing_executable(self):
        for error in (subprocess.TimeoutExpired("docker", 15), FileNotFoundError("docker")):
            io = g3.Transport("http://localhost:8088", self.evidence, Mock(side_effect=error))
            with self.assertRaises(type(error)):
                io.exec("rangerdanger-kali", ["python3"])

    def test_http_409_once_no_retry_and_raw_saved(self):
        io = g3.Transport("http://localhost:8088", self.evidence)
        error = urllib.error.HTTPError("url", 409, "conflict", {}, None)
        with patch.object(io.opener, "open", side_effect=error) as request:
            with self.assertRaises(urllib.error.HTTPError):
                io.http("/api/firewall/apply", {"config": "improved"})
        self.assertEqual(request.call_count, 1)
        self.assertEqual(request.call_args.kwargs["timeout"], 15)

    def test_cli_list_offline(self):
        for package, count in ((US, 156), (EU, 90)):
            result = subprocess.run(["python3", str(g3.ROOT / "scripts/simulator-api-isolation-smoke.py"),
                                     package, "--list"], capture_output=True, text=True,
                                    timeout=10, check=True)
            self.assertEqual(len(json.loads(result.stdout)["weak"]), count)

    def test_cli_live_pass_printed_only_after_gate_returns(self):
        path = g3.ROOT / "scripts/simulator-api-isolation-smoke.py"
        spec = importlib.util.spec_from_file_location("g3_cli", path)
        module = importlib.util.module_from_spec(spec)
        # The CLI's script directory is normally supplied by Python itself.
        with patch.dict("sys.modules", {"lib": __import__("scripts.lib", fromlist=["lib"]),
                                        "lib.simulator_api_isolation": g3}):
            spec.loader.exec_module(module)
        with patch("sys.argv", ["g3", US, "--evidence-dir", self.temp.name]):
            with patch.object(module, "Gate") as gate, patch("builtins.print") as output:
                gate.return_value.run.side_effect = RuntimeError("cleanup failed")
                self.assertEqual(module.main(), 1)
                self.assertFalse(any("PASS" in str(c) for c in output.call_args_list))


class LoopbackHTTPTests(unittest.TestCase):
    """Exercise the real Request, opener, response and HTTPError implementations."""
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.evidence = g3.Evidence(self.temp.name, US)

        class Handler(http.server.BaseHTTPRequestHandler):
            def setup(self):
                super().setup()
                self.connection.settimeout(2)

            def handle_request(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                self.server.requests.append((self.command, self.path, body))
                if self.path == "/chunked-truncated":
                    self.send_response(200)
                    self.send_header("Transfer-Encoding", "chunked")
                    self.end_headers()
                    self.wfile.write(b"5\r\nfirst\r\nA\r\nshort")
                    self.close_connection = True
                    return
                status = 409 if self.path == "/conflict" else 200
                payload = json.dumps({"method": self.command, "body": body.decode()}).encode()
                if self.path == "/redirect":
                    status, payload = 302, b""
                elif self.path == "/malformed":
                    payload = b"{"
                elif self.path == "/invalid-utf8":
                    payload = b"\xff"
                elif self.path == "/truncated":
                    payload = b'{"valid":true}'
                elif self.path == "/large":
                    payload = b"x" + "\u00e9".encode() * 524289
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                if status == 302:
                    self.send_header("Location", "/json")
                self.send_header("Content-Length", str(len(payload) +
                                                       (10 if self.path == "/truncated" else 0)))
                self.end_headers()
                self.wfile.write(payload)
                self.close_connection = True

            do_GET = do_POST = handle_request

            def log_message(self, *_args):
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.requests = []
        self.thread = threading.Thread(target=self.server.serve_forever,
                                       kwargs={"poll_interval": .01})
        self.thread.start()
        self.addCleanup(self.close_server)
        self.base_url = f"http://127.0.0.1:{self.server.server_port}"
        self.transport = g3.Transport(self.base_url, self.evidence)

    def close_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        self.assertFalse(self.thread.is_alive(), "loopback HTTP server did not stop")

    def local_exec(self, command, **kwargs):
        # Preserve real subprocess.run semantics, replacing only the Docker
        # launch boundary. These tests never execute Docker or a range command.
        self.assertEqual(command[:2], ["docker", "exec"])
        self.assertGreater(kwargs["timeout"], 0)
        return subprocess.run(command[3:], **kwargs)

    def local_gate(self):
        self.transport.run = self.local_exec
        return g3.Gate(US, g3.load_inventory(US), self.transport, self.evidence)

    def test_real_get_request_and_evidence_method(self):
        self.assertEqual(self.transport.http("/api/range")["method"], "GET")
        self.assertEqual(self.server.requests, [("GET", "/api/range", b"")])
        self.assertEqual(self.evidence.record["events"][-1]["method"], "GET")

    def test_real_post_request_and_evidence_method(self):
        result = self.transport.http("/api/firewall/apply", {"config": "weak"})
        self.assertEqual(result["method"], "POST")
        self.assertEqual(json.loads(result["body"]), {"config": "weak"})
        self.assertEqual(self.evidence.record["events"][-1]["method"], "POST")

    def test_real_http_error_body_retained_and_not_retried(self):
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.transport.http("/conflict", {"config": "improved"})
        self.assertEqual(failure.exception.code, 409)
        self.assertTrue(failure.exception.closed)
        self.assertEqual(len(self.server.requests), 1)
        event = self.evidence.record["events"][-1]
        self.assertEqual(event["status"], 409)
        self.assertEqual(json.loads((Path(self.temp.name) / event["raw"]).read_bytes())["method"],
                         "POST")

    def test_real_get_redirect_preserved_for_portal_routes(self):
        self.assertEqual(self.transport.http("/redirect")["method"], "GET")
        self.assertEqual([r[:2] for r in self.server.requests],
                         [("GET", "/redirect"), ("GET", "/json")])

    def test_real_post_redirect_cannot_silently_become_get(self):
        with self.assertRaises(urllib.error.HTTPError) as failure:
            self.transport.http("/redirect", {"config": "improved"})
        self.assertEqual(failure.exception.code, 302)
        self.assertTrue(failure.exception.closed)
        self.assertEqual([r[:2] for r in self.server.requests], [("POST", "/redirect")])

    def test_real_invalid_json_and_utf8_retain_raw_bytes(self):
        for path, payload in (("/malformed", b"{"), ("/invalid-utf8", b"\xff")):
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.transport.http(path)
            event = self.evidence.record["events"][-1]
            self.assertEqual((Path(self.temp.name) / event["raw"]).read_bytes(), payload)

    def test_real_short_content_length_body_cannot_prove_health(self):
        with self.assertRaisesRegex(ValueError, "truncated"):
            self.transport.http("/truncated")
        event = self.evidence.record["events"][-1]
        self.assertEqual((Path(self.temp.name) / event["raw"]).read_bytes(), b'{"valid":true}')

    def test_real_incomplete_chunked_body_retains_library_partial_input(self):
        with self.assertRaises(http.client.IncompleteRead):
            self.transport.http("/chunked-truncated")
        event = self.evidence.record["events"][-1]
        self.assertEqual(event["kind"], "http-error")
        self.assertEqual((Path(self.temp.name) / event["raw"]).read_bytes(), b"first")

    def test_real_body_limit_counts_bytes_not_decoded_characters(self):
        with self.assertRaisesRegex(ValueError, "HTTP positive/apply failed"):
            self.transport.http("/large", json_body=False)
        event = self.evidence.record["events"][-1]
        self.assertEqual(len((Path(self.temp.name) / event["raw"]).read_bytes()), 1048577)

    def test_real_peer_http_program_json_envelope_and_no_environment_proxy(self):
        gate = self.local_gate()

        def execute(command, **kwargs):
            self.assertEqual(command[:2], ["docker", "exec"])
            return subprocess.run(command[3:], env=dict(
                os.environ, http_proxy=self.base_url, HTTP_PROXY=self.base_url,
                no_proxy="", NO_PROXY=""), **kwargs)

        self.transport.run = execute
        gate.peer_control({"host": "127.0.0.1", "port": self.server.server_port,
                           "control": {"service": "rtac_sim", "tool": "python3",
                                       "path": "/json"}})
        # A proxy request would contain an absolute URL instead of /json.
        self.assertEqual([r[:2] for r in self.server.requests], [("GET", "/json")])
        event = next(e for e in self.evidence.record["events"] if e["kind"] == "exec")
        raw = json.loads((Path(self.temp.name) / event["raw"]).read_text())
        envelope = json.loads(raw["stdout"])
        self.assertEqual(envelope["status"], 200)
        self.assertIsInstance(envelope["body"], str)
        self.assertEqual(json.loads(envelope["body"])["method"], "GET")

    def test_real_peer_http_program_rejects_invalid_and_truncated_responses(self):
        gate = self.local_gate()
        for path in ("/conflict", "/malformed", "/invalid-utf8", "/truncated", "/large"):
            with self.subTest(path=path), self.assertRaises(ValueError):
                gate.peer_control({"host": "127.0.0.1", "port": self.server.server_port,
                                   "control": {"service": "rtac_sim", "tool": "python3",
                                               "path": path}})
        self.assertEqual(len(self.server.requests), 5)

    def test_real_preflight_json_from_subprocess(self):
        self.local_gate().preflight()
        events = [e for e in self.evidence.record["events"] if e["kind"] == "student-preflight"]
        self.assertEqual(len(events), 4)
        self.assertTrue(all(e["result"]["ready"] is True for e in events))
        self.assertTrue(all(isinstance(e["result"]["time"], float) for e in events))

    def test_real_socket_program_json_through_exec_and_probe(self):
        gate = self.local_gate()
        gate.probe("kali", [{"id": "loopback", "host": "127.0.0.1",
                             "port": self.server.server_port, "expected": "A"}], positive=True)
        event = self.evidence.record["events"][-1]
        self.assertEqual(event["kind"], "positive-tcp")
        self.assertTrue(event["rows"][0]["passed"])
        self.assertEqual(event["rows"][0]["local"][0], "127.0.0.1")

    def test_real_subprocess_nonzero_and_signal_are_not_listener_denials(self):
        self.local_gate()
        for program, code in (("import sys; print('stdout'); print('stderr',file=sys.stderr);"
                               " sys.exit(7)", 7),
                              ("import os,signal; os.kill(os.getpid(),signal.SIGTERM)", -15)):
            with self.subTest(code=code), self.assertRaises(ValueError):
                self.transport.exec("not-a-container", ["python3", "-c", program])
            event = self.evidence.record["events"][-1]
            self.assertEqual(event["returncode"], code)
            raw = json.loads((Path(self.temp.name) / event["raw"]).read_text())
            if code == 7:
                self.assertEqual(raw, {"stdout": "stdout\n", "stderr": "stderr\n"})

    def test_real_subprocess_timeout_preserves_partial_byte_output(self):
        self.local_gate()
        with self.assertRaises(subprocess.TimeoutExpired) as failure:
            self.transport.exec("not-a-container", ["python3", "-c",
                                "import sys,time; print('partial',flush=True);"
                                " print('error',file=sys.stderr,flush=True); time.sleep(10)"],
                                timeout=1)
        self.assertEqual(failure.exception.stdout, b"partial\n")
        self.assertEqual(failure.exception.stderr, b"error\n")
        event = self.evidence.record["events"][-1]
        raw = (Path(self.temp.name) / event["raw"]).read_text()
        self.assertIn("partial", raw)
        self.assertIn("error", raw)


if __name__ == "__main__":
    unittest.main()
