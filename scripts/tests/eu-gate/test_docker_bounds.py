"""Offline coverage guard and real-shell watchdog tests (fake Docker only)."""
import ast
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
HELPER = ROOT / "scripts/lib/docker-bounded.sh"


def shell_words(text):
    """Lex shell words, descending into substitutions but not quoted prose."""
    # EU heredocs are Python/JSON/plan text, not host Docker commands.
    text = re.sub(r"""<<-?\s*(['"])(\w+)\1[^\n]*\n.*?^\2$""",
                  "\n", text, flags=re.M | re.S)
    words = []

    def scan(i=0, closing=""):
        word = ""
        quote = None
        while i < len(text):
            char = text[i]
            if closing == "`" and char == "`":
                if word:
                    words.append(word)
                return i + 1
            if quote != "'" and text.startswith("$(", i):
                i = scan(i + 2, ")")
                continue
            if quote != "'" and char == "`":
                i = scan(i + 1, "`")
                continue
            if char == "\\" and quote != "'":
                if i + 1 < len(text) and text[i + 1] != "\n":
                    word += text[i + 1]
                i += 2
                continue
            if quote:
                if char == quote:
                    quote = None
                else:
                    word += char
            elif char in "'\"":
                quote = char
            elif char == "#" and not word:
                end = text.find("\n", i)
                i = len(text) if end < 0 else end
                continue
            elif char.isspace() or char in ";|&()<>{}":
                if word:
                    words.append(word)
                    word = ""
                if closing == ")" and char == ")":
                    return i + 1
            else:
                word += char
            i += 1
        if word:
            words.append(word)
        return i

    scan()
    return words


class DockerBounds(unittest.TestCase):
    def test_all_eu_shell_calls_use_watchdog(self):
        files = [ROOT / "scripts/eu-iec104-smoke.sh",
                 *sorted((ROOT / "scripts/lib").glob("eu-*.sh")),
                 ROOT / "scripts/lib/range.sh"]
        # No unbounded-by-design calls. The only raw CLI boundary is the
        # shared docker-bounded.sh watchdog itself, tested below.
        for path in files:
            text = path.read_text()
            # This is a tool inventory, not an invocation.
            text = text.replace("for tool in curl jq python3 docker tshark;", "")
            with self.subTest(path=str(path.relative_to(ROOT))):
                self.assertNotIn("docker", shell_words(text))
                # Do not let a newly sourced helper escape the reviewed scope.
                for source in re.findall(r'^\s*(?:\.|source)\s+"\$SCRIPT_DIR/lib/([^"]+)"', text, re.M):
                    self.assertIn(ROOT / "scripts/lib" / source, [*files, HELPER])

    def test_guard_recognizes_command_positions(self):
        for text in ('docker inspect x', 'command docker ps', 'env X=y docker ps',
                     'if docker exec x true; then :; fi', 'value=$(docker ps)',
                     'value="$(docker ps)"', '"docker" ps', 'value=`docker ps`',
                     'printf x | docker exec -i x cat', 'docker \\\nexec x true'):
            with self.subTest(text=text):
                self.assertIn("docker", shell_words(text))
        self.assertNotIn("docker", shell_words(
            '# docker ps\nfail "docker exec x failed"\ncat <<\'PY\'\n'
            'print("docker ps")\nPY\n'))

    def test_python_docker_calls_keep_explicit_deadlines(self):
        files = [*sorted((ROOT / "scripts/lib").glob("eu-*.py")),
                 ROOT / "scripts/lib/containd_readiness.py"]
        for path in files:
            for node in ast.walk(ast.parse(path.read_text())):
                if not isinstance(node, ast.Call) or not node.args:
                    continue
                command = node.args[0]
                if (isinstance(command, (ast.List, ast.Tuple)) and command.elts
                        and isinstance(command.elts[0], ast.Constant)
                        and command.elts[0].value == "docker"):
                    with self.subTest(path=path.name, line=node.lineno):
                        self.assertTrue(any(
                            k.arg == "timeout" and not (
                                isinstance(k.value, ast.Constant) and k.value.value is None)
                            for k in node.keywords))

    def run_shell(self, script):
        with tempfile.TemporaryDirectory() as tmp:
            env = dict(os.environ, TEST_TMP=tmp)
            env.pop("DOCKER_BOUNDED_TIMEOUT_FILE", None)
            env.pop("DOCKER_BOUNDED_TIMEOUT_CONTINUE", None)
            return subprocess.run(
                ["bash", "-uo", "pipefail", "-c", f'. "{HELPER}"\n' + script],
                text=True, capture_output=True, env=env, timeout=15)

    def test_timeout_names_call_and_limit_despite_redirects(self):
        result = self.run_shell(r'''
exec 3>&2
docker() { exec python3 -c 'import time; time.sleep(30)'; }
out=$(docker_bounded 1 exec rtac-sim apk info -v 2>/dev/null)
exit "$?"
''')
        self.assertEqual(result.returncode, 124, result)
        self.assertIn("timed out after 1 seconds: docker exec rtac-sim apk", result.stderr)

    def test_timeout_survives_pipeline(self):
        result = self.run_shell(r'''
docker() { printf 'match\n'; exec python3 -c 'import time; time.sleep(30)'; }
docker_bounded 1 exec example cat | grep match
''')
        self.assertEqual(result.returncode, 124, result)
        self.assertIn("docker exec example cat", result.stderr)

    def test_timeout_cannot_be_retried_as_success(self):
        result = self.run_shell(r'''
DOCKER_BOUNDED_TIMEOUT_FILE="$TEST_TMP/timeouts"
docker() { exec python3 -c 'import time; time.sleep(30)'; }
out=$(docker_bounded 1 inspect example 2>/dev/null) || :
docker() { printf 'must not run\n'; }
docker_bounded 1 inspect example
rc=$?
cat "$DOCKER_BOUNDED_TIMEOUT_FILE" >&2
exit "$rc"
''')
        self.assertEqual(result.returncode, 124, result)
        self.assertEqual(result.stdout, "")
        self.assertIn("docker inspect example", result.stderr)

    def test_cleanup_can_continue_without_erasing_timeout(self):
        result = self.run_shell(r'''
DOCKER_BOUNDED_TIMEOUT_FILE="$TEST_TMP/timeouts"
printf 'earlier timeout\n' >"$DOCKER_BOUNDED_TIMEOUT_FILE"
DOCKER_BOUNDED_TIMEOUT_CONTINUE=1
docker() { printf 'restored\n'; }
docker_bounded 1 unpause example || exit "$?"
test -s "$DOCKER_BOUNDED_TIMEOUT_FILE"
''')
        self.assertEqual(result.returncode, 0, result)
        self.assertEqual(result.stdout, "restored\n")

    def test_stdin_and_original_exit_status_survive_watchdog(self):
        result = self.run_shell(r'''
docker() { cat; return 7; }
docker_bounded 1 exec -i example cat <<< 'policy body'
''')
        self.assertEqual(result.returncode, 7, result)
        self.assertEqual(result.stdout, "policy body\n")

    def test_gate_rejects_swallowed_timeout_and_skips_later_gates(self):
        result = self.run_shell(f'''
. "{ROOT}/scripts/eu-iec104-smoke.sh"
eu_gate_preflight() {{
  TMP_DIR="$TEST_TMP"
  DOCKER_BOUNDED_TIMEOUT_FILE="$TEST_TMP/timeouts"
}}
eu_gate_G0() {{
  docker() {{ exec python3 -c 'import time; time.sleep(30)'; }}
  out=$(docker_bounded 1 inspect example 2>/dev/null) || :
  return 0
}}
eu_gate_resources_finish() {{ :; }}
eu_gate_G1() {{ printf 'must not run\\n'; }}
eu_gate_main
''')
        self.assertEqual(result.returncode, 1, result)
        self.assertIn("GATE G0 FAIL", result.stdout)
        self.assertIn("G0 Docker timeout:", result.stderr)
        self.assertNotIn("must not run", result.stdout)

    def test_cleanup_rejects_ignored_teardown_timeout(self):
        result = self.run_shell(f'''
. "{ROOT}/scripts/eu-iec104-smoke.sh"
TMP_DIR="$TEST_TMP"
DOCKER_BOUNDED_TIMEOUT_FILE="$TEST_TMP/timeouts"
eu_stop_capture() {{ :; }}
eu_stop_monitor() {{ :; }}
eu_save_evidence() {{ :; }}
eu_stop_plc_scan() {{
  docker() {{ exec python3 -c 'import time; time.sleep(30)'; }}
  out=$(docker_bounded 1 stop example 2>/dev/null) || :
}}
eu_cleanup 0
''')
        self.assertEqual(result.returncode, 1, result)
        self.assertIn("Docker timeout recorded during EU gate/cleanup", result.stderr)
        self.assertIn("docker stop example", result.stderr)
        self.assertIn("EU IEC104 gate FAIL", result.stderr)

    def test_management_inspect_uses_shared_watchdog(self):
        text = (ROOT / "scripts/lib/eu-management-probes.py").read_text()
        self.assertIn("'5', 'inspect'", text)
        self.assertIn('docker_bounded "$@"', text)

    def test_readiness_keeps_budget_and_names_timeout_without_token(self):
        spec = importlib.util.spec_from_file_location(
            "readiness", ROOT / "scripts/lib/containd_readiness.py")
        readiness = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(readiness)
        for path, health in (("/health", True), ("/dataplane/protection", False)):
            with self.subTest(path=path):
                receipt = {} if health else {"status": "committed", "runningRevision": "r1"}
                with patch.object(readiness.subprocess, "run", side_effect=
                                  subprocess.TimeoutExpired(["docker", "SECRET"], 12.5)) as run:
                    with self.assertRaisesRegex(
                            ValueError, f"after 12.5 seconds: docker exec example curl GET {path}") as error:
                        readiness.wait_ready(receipt, readiness.docker_reader("example", "SECRET"),
                                             health=health, timeout=12.5, clock=lambda: 0)
                    self.assertEqual(run.call_args.kwargs["timeout"], 12.5)
                    self.assertEqual(run.call_count, 1)
                    self.assertNotIn("SECRET", str(error.exception))


if __name__ == "__main__":
    unittest.main()
