"""Offline post-commit readiness and supersession contracts (no Docker)."""
import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location(
    "readiness", Path(__file__).resolve().parents[1] / "lib/containd_readiness.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def protection(state, apply="ours", installed="ours"):
    return {"state": state, "applyId": apply, "installedApplyId": installed,
            "components": [{"name": "nflog", "reason": "diagnostic"}]}


class ReadinessTests(unittest.TestCase):
    def wait(self, observations, receipt=None, **kwargs):
        now = [0]
        self.reads = []
        self.delays = []

        def read(path, remaining):
            self.reads.append((path, remaining))
            return observations.pop(0) if len(observations) > 1 else observations[0]

        def sleep(delay):
            self.delays.append(delay)
            now[0] += delay
        return MODULE.wait_ready(receipt or {"applyId": "ours"}, read,
                                 clock=lambda: now[0], sleep=sleep, **kwargs)

    def test_commit_protection_is_sufficient(self):
        self.wait([], {"applyId": "ours", "protection": protection("protecting")})
        self.assertEqual(self.reads, [])

    def test_pending_old_installation_then_protecting(self):
        self.wait([protection("pending", installed="old"),
                   protection("pending", installed="old"), protection("protecting")])
        self.assertEqual(self.delays, [0.5, 0.5])
        self.assertTrue(all(path == "/dataplane/protection" for path, _ in self.reads))

    def test_terminal_states_have_component_reasons(self):
        for state in ("degraded", "failed", "disabled"):
            with self.subTest(state=state), self.assertRaisesRegex(ValueError, "diagnostic"):
                self.wait([protection(state)])
            self.assertEqual(len(self.reads), 1)

    def test_protecting_wrong_installed_id_times_out(self):
        with self.assertRaisesRegex(ValueError, "timeout.*last observation"):
            self.wait([protection("protecting", installed="old")])
        self.assertEqual(sum(self.delays), 30)

    def test_never_infers_newer_order_from_opaque_ids(self):
        with self.assertRaisesRegex(ValueError, "timeout"):
            self.wait([protection("pending", apply="opaque-other", installed="old")])

    def test_current_id_superseded_after_ours_seen(self):
        with self.assertRaisesRegex(ValueError, "superseded"):
            self.wait([protection("pending", installed="old"),
                       protection("protecting", apply="other", installed="other")])

    def test_new_installation_supersedes_even_before_current_seen(self):
        with self.assertRaisesRegex(ValueError, "superseded"):
            self.wait([protection("pending", apply="", installed="old"),
                       protection("pending", apply="", installed="new")])

    def test_missing_apply_id_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "no applyId"):
            self.wait([], {"status": "ok"})

    def test_parent_deadline_caps_timeout(self):
        with self.assertRaisesRegex(ValueError, "timeout"):
            self.wait([protection("pending")], timeout=0.75)
        self.assertEqual(self.delays, [0.5, 0.25])

    def test_health_enforcing_is_polled(self):
        self.wait([{"enforcement": {"state": "pending"}},
                   {"enforcement": {"state": "enforcing"}}], health=True)
        self.assertEqual(self.reads[0][0], "/health")
        self.assertEqual(self.delays, [0.5])

    def test_unknown_state_never_passes(self):
        with self.assertRaisesRegex(ValueError, "timeout"):
            self.wait([protection("unknown")])


class BackendReceiptTests(unittest.TestCase):
    def test_protecting_receipt_is_returned_without_reads(self):
        receipt = {"applyId": "opaque-revision-not-a-sequence", "protection": "protecting"}
        self.assertIs(MODULE.require_backend_protection({"containd": receipt}), receipt)

    def test_missing_malformed_or_degraded_receipts_fail(self):
        for response in (None, [], {}, {"applyId": "id", "protection": "protecting"},
                         {"containd": None}, {"containd": {"protection": "protecting"}},
                         {"containd": {"applyId": "", "protection": "protecting"}}):
            with self.subTest(response=response), self.assertRaises(ValueError):
                MODULE.require_backend_protection(response)
        for state in ("pending", "degraded", "failed", "disabled", "unknown", "Protecting", None):
            with self.subTest(state=state), self.assertRaisesRegex(ValueError, "logging unavailable"):
                MODULE.require_backend_protection({"containd": {
                    "applyId": "id", "protection": state, "warning": "logging unavailable"}})


if __name__ == "__main__":
    unittest.main()
