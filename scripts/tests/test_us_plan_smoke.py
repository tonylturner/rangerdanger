"""US compiled-plan and DPI orchestration, with no Docker or live range."""
import json
import re
import shlex
import subprocess
import tempfile
import unittest
from pathlib import Path

from . import test_smoke_workflow
from .test_events_smoke import BOUNDARY, dpi_event

ROOT = test_smoke_workflow.ROOT


def function(source, name):
    end = r"^PY\n\}" if name == "install_modbus_probe" else r"^\}"
    return re.search(r"(?ms)^" + name + r"\(\) \{.*?" + end, source)[0]


class USPlanSmokeTests(unittest.TestCase):
    def test_wrong_or_unready_package_refuses_without_applying_or_restoring(self):
        for package, phase, protection in (
                ("eu-iec104-substation", "ready", "protecting"),
                ("us-dnp3-substation", "starting", "protecting"),
                ("us-dnp3-substation", "ready", "degraded")):
            with self.subTest(package=package, phase=phase, protection=protection):
                status = json.dumps({"package": package, "phase": phase,
                                     "containd": {"applyId": "fixture", "protection": protection}},
                                    separators=(",", ":"))
                with tempfile.TemporaryDirectory() as directory:
                    source = test_smoke_workflow.SmokeWorkflowTests().plan_script(directory)
                    result = subprocess.run(
                        ["bash", "-c", 'curl() { case "${!#}" in */api/range) printf "%s" '
                         + shlex.quote(status) + ' ;; *) echo forbidden; return 99 ;; esac; }\n'
                         + source], cwd=ROOT, capture_output=True, text=True, timeout=5)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("forbidden", result.stdout + result.stderr)
                    self.assertFalse((Path(directory) / "receipts").exists())

    def test_singleton_goldens_enable_dpi_and_allow_rtac_field_conduits(self):
        for proto in ("modbus", "dnp3"):
            config = json.loads((ROOT / f"frontend/lib/testdata/plans/us-{proto}-dpi.json")
                                .read_text())["config"]
            with self.subTest(proto=proto):
                self.assertIs(config["dataplane"]["dpiEnabled"], True)
                self.assertIs(config["dataplane"]["dpiIcsProtocols"][proto], True)
                self.assertEqual(config["dataplane"]["nfqueueGroup"], 101)
                for port in ("502", "20000"):
                    # First matching L4 conduit: the target address is unrestricted.
                    rule = next(r for r in config["firewall"]["rules"]
                                if "lan1" in r.get("sourceZones", [])
                                and "lan2" in r.get("destZones", [])
                                and (not r.get("sources") or "10.30.30.20/32" in r["sources"])
                                and any(p["name"] == "tcp" and p["port"] == port
                                        for p in r.get("protocols", [])))
                    self.assertEqual(rule["action"], "ALLOW")
                    self.assertFalse(rule.get("destinations"))
                    if port == {"modbus": "502", "dnp3": "20000"}[proto]:
                        self.assertEqual(rule["ics"]["protocol"], proto)

    def run_dpi_case(self, proto, mutate=None, after=False, events=None,
                     apply_mutate=None, boundary=BOUNDARY):
        source = (ROOT / "scripts/events-smoke.sh").read_text()
        helpers = "\n".join(function(source, name) for name in (
            "count_dpi_requests", "wait_for_dpi_request", "install_modbus_probe",
            "observe_dpi_request", "require_plan_active", "plan_dpi_case"))
        golden = json.loads((ROOT / f"frontend/lib/testdata/plans/us-{proto}-dpi.json").read_text())
        active = {"active_config": "custom", "policy_source": "plan",
                  "plan": {"actionIds": [proto + "-dpi"], "policyHash": golden["policyHash"]}}
        changed = json.loads(json.dumps(active))
        if mutate:
            mutate(changed)
        apply = {"applied": True, "warnings": [], "plan": golden,
                 "containd": {"applyId": "fixture", "protection": "protecting"}}
        if apply_mutate:
            apply_mutate(apply)
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            for name, value in (("apply", apply), ("before", active if after else changed),
                                ("after", changed if after else active),
                                ("events", [dpi_event(proto=proto, function_code=8 if proto == "modbus" else 5)]
                                 if events is None else events)):
                (fixture / f"{name}.json").write_text(json.dumps(value))
            helpers = helpers.replace("build/smoke-receipts/events-plans", directory + "/receipts")
            stubs = r'''
err() { echo "$1" >&2; }
ok() { echo "$1"; }
curl() {
  case "${!#}" in
    */apply-plan) echo apply >>"$fixture/trace"; cat "$fixture/apply.json" ;;
    */active)
      if [ -f "$fixture/seen" ]; then
        echo active-after >>"$fixture/trace"; cat "$fixture/after.json"
      else
        touch "$fixture/seen"
        echo active-before >>"$fixture/trace"; cat "$fixture/before.json"
      fi ;;
    *) return 99 ;;
  esac
}
docker() {
  case "$*" in
    *'date -u +%s%N') echo clock >>"$fixture/trace"; echo "$FIXTURE_BOUNDARY" ;;
    *'cat > /tmp/_smoke_modbus.py') cat >/dev/null ;;
    *'which dnp3cmd') return 0 ;;
    *'python3 /tmp/_smoke_modbus.py'*|*'crob 0 trip') echo traffic >>"$fixture/trace" ;;
    *) return 99 ;;
  esac
}
engine_events() { echo events >>"$fixture/trace"; cat "$fixture/events.json"; }
'''
            result = subprocess.run(
                ["bash", "-c", f"set -uo pipefail\nfixture={shlex.quote(directory)}\n"
                 f"SCRIPT_DIR={shlex.quote(str(ROOT / 'scripts'))}\nAPI=http://fixture\n"
                 f"EVENT_POLL_BUDGET=0\nrelay_touched=0\nFIXTURE_BOUNDARY={shlex.quote(boundary)}\n"
                 + stubs + helpers + f"\nplan_dpi_case {proto}"],
                cwd=ROOT, capture_output=True, text=True, timeout=10)
            self.assertTrue((fixture / "trace").exists(), result.stdout + result.stderr)
            return result, (fixture / "trace").read_text().splitlines()

    def test_each_plan_brackets_fresh_protocol_evidence_with_provenance(self):
        for proto in ("modbus", "dnp3"):
            with self.subTest(proto=proto):
                result, trace = self.run_dpi_case(proto)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(trace, ["apply", "active-before", "clock", "traffic", "events", "active-after"])

    def test_plan_provenance_mismatch_fails_before_and_after_traffic(self):
        mutations = (
            lambda a: a.update(active_config="weak"),
            lambda a: a.update(policy_source="manual"),
            lambda a: a["plan"].update(actionIds=[]),
            lambda a: a["plan"].update(policyHash=a["plan"]["policyHash"] + "-different"),
        )
        for proto in ("modbus", "dnp3"):
            for after in (False, True):
                for mutate in mutations:
                    with self.subTest(proto=proto, after=after, mutation=mutate):
                        result, trace = self.run_dpi_case(proto, mutate, after)
                        self.assertNotEqual(result.returncode, 0)
                        self.assertEqual("traffic" in trace, after)
                        self.assertIn("plan provenance is not active", result.stderr)

    def test_missing_stale_wrong_protocol_or_malformed_events_fail(self):
        for events in ([], [dpi_event(timestamp="2026-10-25T12:00:00.123456789Z")],
                       [dpi_event(proto="dnp3", function_code=5)],
                       [dpi_event(timestamp="invalid")]):
            with self.subTest(events=events):
                result, trace = self.run_dpi_case("modbus", events=events)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(trace[-1], "active-after")
        result, trace = self.run_dpi_case("modbus", boundary="not-a-clock")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("traffic", trace)

    def test_unprotected_plan_apply_fails_before_traffic(self):
        for mutate in (lambda a: a.update(warnings=["fixture warning"]),
                       lambda a: a["containd"].update(protection="degraded")):
            result, trace = self.run_dpi_case("modbus", apply_mutate=mutate)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(trace, ["apply"])

    def test_event_cleanup_restores_weak_even_after_failure_and_propagates_restore_failure(self):
        cleanup = function((ROOT / "scripts/events-smoke.sh").read_text(), "cleanup_events")
        for original, restore in ((0, 0), (1, 0), (0, 1)):
            result = subprocess.run(
                ["bash", "-c", f"relay_touched=0\n{cleanup}\n"
                 f"apply_policy() {{ echo restore-$1; return {restore}; }}\n"
                 f"trap 'cleanup_events || exit 1' EXIT\nexit {original}"],
                capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, int(bool(original or restore)))
            self.assertEqual(result.stdout.strip(), "restore-weak")

    def test_cleanup_closes_probed_relay_and_requires_independent_state(self):
        cleanup = function((ROOT / "scripts/events-smoke.sh").read_text(), "cleanup_events")
        for closed in (True, False):
            state = json.dumps({"devices": {"relay": {"breaker_closed": closed}}})
            result = subprocess.run(
                ["bash", "-c", f"relay_touched=1\n{cleanup}\n"
                 "apply_policy() { echo restore-$1; }\n"
                 "docker() { case \"$*\" in *'crob 0 close') closed_probe=1 ;; *) return 99 ;; esac; }\n"
                 "sleep() { :; }\nerr() { echo \"$1\" >&2; }\n"
                 f"curl() {{ printf '%s' {shlex.quote(state)}; }}\n"
                 "cleanup_events\nrc=$?\necho closed-probe=${closed_probe:-0}\nexit \"$rc\""],
                capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, int(not closed), result.stderr)
            self.assertEqual(result.stdout.splitlines(), ["restore-weak", "closed-probe=1"])


if __name__ == "__main__":
    unittest.main()
