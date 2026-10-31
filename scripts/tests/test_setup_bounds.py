"""Installer wait budgets, with fake clients only (no Docker or range)."""
import json
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "setup.sh").read_text()
BOUNDED = (ROOT / "scripts/lib/docker-bounded.sh").read_text()
FAKE_DOCKER = r'''
import json, os, sys, time
root = os.environ["FAKE_ROOT"]
args = sys.argv[1:]
with open(os.path.join(root, "calls"), "a") as log:
    log.write(json.dumps(args) + "\n")
config = json.loads(os.environ["FAKE_CONFIG"])
def matches(prefix):
    return args[:len(prefix)] == prefix
if any(matches(prefix) for prefix in config.get("stall", [])):
    time.sleep(30)
for prefix, status in config.get("fail", []):
    if matches(prefix):
        print(config.get("error", "synthetic Docker error"), file=sys.stderr)
        raise SystemExit(status)
if matches(["info", "--format", "{{.MemTotal}}"]):
    print(config.get("memory", "8589934592"))
elif matches(["compose", "version", "--short"]):
    print("2.36.0")
elif matches(["compose", "version"]):
    print("Docker Compose version v2.36.0")
elif matches(["version", "--format", "{{.Server.APIVersion}}"]):
    print("1.49")
elif matches(["ps", "--filter"]):
    print("127.0.0.1:8088->8080/tcp")
elif matches(["network", "ls"]):
    if config.get("networks_present"):
        print("network-id")
elif matches(["network", "inspect"]):
    print(config.get("owner", "rangerdanger-platform"))
elif matches(["volume", "ls"]):
    pass
elif matches(["logs"]):
    print(config.get("logs_stderr", ""), file=sys.stderr)
elif matches(["inspect"]):
    print("anonymous")
'''


def run_bash(script, **env):
    return subprocess.run(
        ["bash", "-c", "set -euo pipefail\n" + script],
        env=dict(os.environ, **env), capture_output=True, text=True, timeout=10,
    )


class SetupBoundsTests(unittest.TestCase):
    def fake_docker_env(self, root, config=None):
        tools = root / "bin"
        tools.mkdir()
        docker = tools / "docker"
        docker.write_text(f"#!{os.sys.executable}\n" + FAKE_DOCKER)
        docker.chmod(0o755)
        sleep = tools / "sleep"
        sleep.write_text(
            '#!/bin/sh\nprintf "%s\\n" "$*" >> "$FAKE_ROOT/sleeps"\n'
            'if [ "$*" = 1 ]; then exec /bin/sleep 0.02; fi\n'
            'if [ "${FAKE_WATCHDOG_FAST:-0}" = 1 ]; then exec /bin/sleep 0.3; fi\n'
            'exec /bin/sleep 1\n'
        )
        sleep.chmod(0o755)
        return dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                    FAKE_ROOT=str(root), FAKE_CONFIG=json.dumps(config or {}),
                    FAKE_WATCHDOG_FAST="1" if (config or {}).get("stall") else "0")

    def fake_run(self, root, script, config=None):
        env = self.fake_docker_env(root, config)
        env["ROOT"] = str(ROOT)
        result = subprocess.run(
            ["bash", "-c", "set -euo pipefail\n" + script],
            env=env,
            capture_output=True, text=True, timeout=10,
        )
        calls_path = root / "calls"
        calls = [json.loads(line) for line in calls_path.read_text().splitlines()] \
            if calls_path.exists() else []
        sleeps_path = root / "sleeps"
        sleeps = sleeps_path.read_text().splitlines() if sleeps_path.exists() else []
        return result, calls, sleeps

    def test_docker_watchdog_preserves_output_status_and_kills_stalled_client(self):
        function = BOUNDED
        for command, expected, output in (
            ("printf running", 0, "running"),
            ("return 19", 19, ""),
            ("exec sleep 20", 124, ""),
        ):
            with self.subTest(command=command):
                result = run_bash(
                    function + "\ndocker() { " + command + "; }\n"
                    "docker_bounded 1 inspect"
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                self.assertEqual(result.stdout, output)

    def test_docker_watchdog_releases_command_substitution(self):
        function = BOUNDED
        # run_bash allows 10 s; a watchdog sleep holding the pipe would
        # keep $(...) open for the whole 30 s limit.
        result = run_bash(
            function + "\ndocker() { printf 2.36.0; }\n"
            'value=$(docker_bounded 30 compose version --short)\n'
            'printf %s "$value"'
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "2.36.0")

    def test_setup_preflight_bounds_and_runs_each_version_probe(self):
        start = SOURCE.index("# Docker engine reachable")
        end = SOURCE.index("# Architecture", start)
        preflight = (
            'exec 3>&2\n'
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'docker_floor_check() { return 0; }\n'
            'say() { :; }; die() { echo "$*" >&2; exit 1; }\n'
            + SOURCE[start:end]
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(root, preflight, {"networks_present": True})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, [
                ["info"], ["compose", "version"], ["compose", "version", "--short"],
                ["version", "--format", "{{.Server.APIVersion}}"],
            ])
            self.assertEqual(sleeps, ["30", "20", "20", "20"])

    def test_setup_preflight_stall_prints_recovery_guidance(self):
        start = SOURCE.index("# Docker engine reachable")
        end = SOURCE.index("# Architecture", start)
        preflight = (
            'exec 3>&2\n'
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'docker_floor_check() { return 0; }\n'
            'say() { :; }; die() { echo "$*" >&2; exit 1; }\n'
            + SOURCE[start:end]
        )
        for prefix, limits in ((["info"], ["30"]),
                               (["compose", "version"], ["30", "20"])):
            with self.subTest(prefix=prefix), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                result, calls, sleeps = self.fake_run(root, preflight, {"stall": [prefix]})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Start or restart Docker Desktop or dockerd, then re-run",
                              result.stderr)
                self.assertEqual(sleeps, limits)

    def test_timeout_guidance_survives_caller_stderr_redirect(self):
        script = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'exec 3>&2\n'
            'if docker_bounded 30 info 2>/dev/null; then exit 99; else rc=$?; fi\n'
            '[ "$rc" = 124 ]\n'
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(root, script, {"stall": [["info"]]})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, [["info"]])
            self.assertEqual(sleeps, ["30"])
            self.assertIn("Start or restart Docker Desktop or dockerd, then re-run",
                          result.stderr)

    def test_openplc_logs_stderr_still_triggers_emulation_hint(self):
        start = SOURCE.index("    if docker_bounded 5 logs --tail 200 rangerdanger-openplc")
        end = SOURCE.index("\n    fi", start) + len("\n    fi")
        script = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'exec 3>&2\n'
            'warn() { printf "%s\\n" "$*" >> "$WARNED"; }\n'
            'BINFMT_REF=fixture\n'
            + SOURCE[start:end]
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            env = self.fake_docker_env(root, {"logs_stderr": "exec format error"})
            env["ROOT"] = str(ROOT)
            env["WARNED"] = str(root / "warned")
            result = subprocess.run(
                ["bash", "-c", "set -euo pipefail\n" + script],
                env=env, capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("exec format error", (root / "warned").read_text())

    def test_missing_openplc_container_hides_docker_inspect_error(self):
        start = SOURCE.index('    if [ "$(docker_bounded 5 inspect -f')
        end = SOURCE.index("\n    fi", start) + len("\n    fi")
        script = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'exec 3>&2\n'
            + SOURCE[start:end]
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, _ = self.fake_run(
                root, script,
                {"fail": [(["inspect"], 1)],
                 "error": "Error: No such object: rangerdanger-openplc"},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(calls), 1)
            self.assertNotIn("No such object", result.stderr)

    def test_setup_memory_and_held_port_call_sites_use_budgets(self):
        memory_start = SOURCE.index("MEM_GB=0")
        memory_end = SOURCE.index("# ─── required host ports", memory_start)
        ports_start = SOURCE.index("lab_held_ports() {")
        ports_end = SOURCE.index("\n}", ports_start) + 2
        script = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'say() { :; }; warn() { :; }\n'
            'RANGE_PROJECT=rangerdanger PLATFORM_PROJECT=rangerdanger-platform\n'
            + SOURCE[memory_start:memory_end]
            + SOURCE[ports_start:ports_end]
            + '\nlab_held_ports\n'
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(root, script)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, [
                ["info", "--format", "{{.MemTotal}}"],
                ["info", "--format", "{{.MemTotal}}"],
                ["ps", "--filter", "label=com.docker.compose.project=rangerdanger",
                 "--format", "{{.Ports}}"],
                ["ps", "--filter", "label=com.docker.compose.project=rangerdanger-platform",
                 "--format", "{{.Ports}}"],
            ])
            self.assertEqual(sleeps, ["30", "30", "15", "15"])

    def test_setup_network_queries_distinguish_absent_and_daemon_errors(self):
        start = SOURCE.index("for DELIVERY_NET in rangerdanger_desk_corp")
        end = SOURCE.index("# Stopped pre-split containers", start)
        network_checks = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'die() { echo "$*" >&2; exit 1; }\n'
            'PLATFORM_PROJECT=rangerdanger-platform RANGE_PROJECT=rangerdanger\n'
            'MGMT_NET=rangerdanger_mgmt_net\n'
            + SOURCE[start:end]
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(root, network_checks)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(calls), 4)
            self.assertTrue(all(call[:2] == ["network", "ls"] for call in calls))
            self.assertEqual(sleeps, ["15"] * 4)

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(
                root, network_checks, {"networks_present": True})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(sum(call[:2] == ["network", "inspect"] for call in calls), 4)
            self.assertEqual(sleeps, ["15"] * 8)

        for config, diagnostic, calls_expected in (
            ({"fail": [(["network", "ls"], 7)]}, "Could not query network", 1),
            ({"networks_present": True,
              "fail": [(["network", "inspect"], 9)]}, "Could not inspect network", 2),
        ):
            with self.subTest(config=config), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                result, calls, _ = self.fake_run(root, network_checks, config)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(diagnostic, result.stderr)
                self.assertEqual(len(calls), calls_expected)

    def test_setup_migration_ps_call_site_uses_15_second_budget(self):
        start = SOURCE.index("OLD_PLATFORM=$(docker_bounded")
        end = SOURCE.index("\n# ─── image acquisition", start)
        script = (
            '. "$ROOT/scripts/lib/docker-bounded.sh"\n'
            'RANGE_PROJECT=rangerdanger\n'
            + SOURCE[start:end]
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result, calls, sleeps = self.fake_run(root, script)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(calls, [[
                "ps", "-aq", "--filter", "label=com.docker.compose.project=rangerdanger",
                "--filter", "label=com.docker.compose.service=backend",
            ]])
            self.assertEqual(sleeps, ["15"])

    def test_health_fails_closed_and_uses_elapsed_time_budget(self):
        start = SOURCE.index('HEALTH_OK=0')
        end = SOURCE.index('# ─── start the range', start)
        for healthy in (True, False):
            with self.subTest(healthy=healthy):
                result = run_bash("""
say() { :; }; die() { exit 1; }
curl() {
    [[ "$*" = *"--connect-timeout 2 --max-time 2"* ]] || exit 99
    [ "$HEALTHY" = 1 ]
}
sleep() { SECONDS=$((SECONDS + 60)); }
HEALTH_URL=http://fixture
""" + SOURCE[start:end], HEALTHY=str(int(healthy)))
                self.assertEqual(result.returncode, 0 if healthy else 1, result.stderr)

    def test_all_readiness_http_calls_have_per_call_bounds(self):
        readiness = SOURCE[SOURCE.index('# ─── health smoke check'):]
        for line in readiness.splitlines():
            if "curl -" in line and not line.lstrip().startswith(("Check:", "curl ")):
                self.assertIn("--connect-timeout", line)
                self.assertIn("--max-time", line)
        self.assertIn('UP_FLAGS=(up -d --wait --wait-timeout 120)', SOURCE)
        self.assertNotIn('$(docker inspect', readiness)
        self.assertNotIn('if docker logs', readiness)

    def test_range_transition_waits_through_unreachable_and_rejects_failures(self):
        start = SOURCE.index("RANGE_BODY='{}'")
        end = SOURCE.index("# Workshop-critical surfaces", start)
        helpers = """
say() { :; }; die() { echo "$*" >&2; exit 1; }
sleep() { SECONDS=$((SECONDS + 300)); }
API=http://fixture PACKAGE=eu
. "$ROOT/scripts/lib/range.sh"
curl() {
    [[ "$*" = *"--connect-timeout 3 --max-time "* ]] || exit 99
    if [[ "$*" = *"-X POST"* ]]; then
        echo POST >> "$CALLS"
        if [ "$SCENARIO" = conflict ]; then
            printf '{}\\n409'
        else
            printf '{"target":"eu","phase":"preflight"}\\n202'
        fi
    elif [ "$SCENARIO" = failed ]; then
        echo '{"phase":"failed","package":"eu","error":"fixture failure"}'
    elif [ "$SCENARIO" = wrong ]; then
        echo '{"phase":"ready","package":"us"}'
    elif [ "$SCENARIO" = conflict ]; then
        return 28
    else
        echo '{"phase":"ready","package":"eu"}'
    fi
}
"""
        for scenario, expected in (("ready", 0), ("failed", 1),
                                   ("wrong", 1), ("conflict", 1)):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as tmp:
                calls = Path(tmp) / "calls"
                result = run_bash(
                    helpers + SOURCE[start:end],
                    ROOT=str(ROOT), SCENARIO=scenario, CALLS=str(calls),
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                if scenario == "conflict":
                    self.assertEqual(calls.read_text().splitlines(), ["POST"] * 3)

    def test_docker_floor_compares_compose_and_engine_api(self):
        cases = (
            ("2.36.0", "1.49", 0), ("v2.36.0", "1.49", 0),
            ("2.36.0-desktop.1", "1.49", 0), ("5.5.1", "1.56", 0),
            ("2.35.1", "1.49", 1), ("2.36.0", "1.48", 1), ("2.40.0", "1.9", 1),
            ("", "1.49", 1), ("2.36.0", "", 1), ("abc", "1.49", 1),
        )
        for compose, api, expected in cases:
            with self.subTest(compose=compose, api=api):
                result = run_bash(
                    '. "$ROOT/scripts/lib/docker-floor.sh"\n'
                    'docker_floor_check "$COMPOSE" "$API"',
                    ROOT=str(ROOT), COMPOSE=compose, API=api,
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                if expected:
                    self.assertIn("Docker Desktop >= 4.42", result.stdout)
                    self.assertIn("Engine >= 28.1 (API >= 1.49)", result.stdout)
                    self.assertIn("Compose >= 2.36.0", result.stdout)
                else:
                    self.assertEqual(result.stdout, "")

    def test_preflight_refuses_docker_below_the_interface_name_floor(self):
        start = SOURCE.index("# Compose v2")
        end = SOURCE.index("# Architecture", start)
        self.assertIn('. "$ROOT_DIR/scripts/lib/docker-floor.sh"', SOURCE[:start])
        script = """
say() { echo "$*"; }; die() { echo "$*" >&2; exit 1; }
docker() {
    case "$*" in
        "compose version") return 0 ;;
        "compose version --short") echo "$COMPOSE" ;;
        "version --format {{.Server.APIVersion}}") echo "$API" ;;
        *) exit 99 ;;
    esac
}
. "$ROOT/scripts/lib/docker-bounded.sh"
. "$ROOT/scripts/lib/docker-floor.sh"
""" + SOURCE[start:end]
        for compose, api, expected in (("2.36.0", "1.49", 0), ("2.35.0", "1.51", 1),
                                       ("2.38.2", "1.48", 1)):
            with self.subTest(compose=compose, api=api):
                result = run_bash(script, ROOT=str(ROOT), COMPOSE=compose, API=api)
                self.assertEqual(result.returncode, expected, result.stderr)
                if expected:
                    self.assertIn(f"found Compose {compose} on Engine API {api}", result.stderr)
                    self.assertIn("Docker Desktop >= 4.42", result.stderr)
                else:
                    self.assertIn(f"Compose {compose} on Engine API {api}", result.stdout)
