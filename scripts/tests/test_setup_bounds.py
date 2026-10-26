"""Installer wait budgets, with fake clients only (no Docker or range)."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = (ROOT / "setup.sh").read_text()


def run_bash(script, **env):
    return subprocess.run(
        ["bash", "-c", "set -euo pipefail\n" + script],
        env=dict(os.environ, **env), capture_output=True, text=True, timeout=10,
    )


class SetupBoundsTests(unittest.TestCase):
    def test_docker_watchdog_preserves_output_status_and_kills_stalled_client(self):
        function = SOURCE[SOURCE.index("docker_bounded() {"):]
        function = function[:function.index("\n}\n") + 3]
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
        function = SOURCE[SOURCE.index("docker_bounded() {"):]
        function = function[:function.index("\n}\n") + 3]
        # run_bash allows 10 s; a watchdog sleep holding the pipe would
        # keep $(...) open for the whole 30 s limit.
        result = run_bash(
            function + "\ndocker() { printf 2.36.0; }\n"
            'value=$(docker_bounded 30 compose version --short)\n'
            'printf %s "$value"'
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "2.36.0")

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
        bounded = SOURCE[SOURCE.index("docker_bounded() {"):]
        bounded = bounded[:bounded.index("\n}\n") + 3]
        start = SOURCE.index("# Compose v2")
        end = SOURCE.index("# Architecture", start)
        self.assertIn('. "$ROOT_DIR/scripts/lib/docker-floor.sh"', SOURCE[:start])
        script = bounded + """
say() { echo "$*"; }; die() { echo "$*" >&2; exit 1; }
docker() {
    case "$*" in
        "compose version") return 0 ;;
        "compose version --short") echo "$COMPOSE" ;;
        "version --format {{.Server.APIVersion}}") echo "$API" ;;
        *) exit 99 ;;
    esac
}
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
