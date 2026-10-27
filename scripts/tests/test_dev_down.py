"""Teardown contract using fake tools only; never contact a Docker daemon."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
DOCKER = r'''
import json, os, pathlib, sys, time
root = pathlib.Path(os.environ["FAKE_ROOT"])
config = json.loads(os.environ["FAKE_CONFIG"])
args = sys.argv[1:]
with (root / "calls").open("a") as log:
    log.write(json.dumps(dict(args=args, cwd=os.getcwd())) + "\n")
if config.get("stall") == args[0]:
    time.sleep(30)
def fail():
    print("synthetic Docker error", file=sys.stderr)
    sys.exit(7)
def project():
    return next(a.rsplit("=", 1)[1] for a in args if a.startswith("label="))
def down(p):
    return (root / (p + ".down")).exists()
if args[:2] == ["ps", "-aq"] or args[:3] == ["network", "ls", "-q"]:
    p = project()
    if config.get("query_error") and (down(p) or config["query_error"] == "before"):
        fail()
    if not config.get("empty") and (not down(p) or config.get("left_resources")):
        print("container" if args[0] == "ps" else "network")
elif args[0] == "inspect":
    if config.get("mount_error"):
        fail()
    if not config.get("no_volumes"):
        print("anonymous\nanonymous\nsecond")
elif args[:2] == ["compose", "-p"]:
    p = args[2]
    (root / (p + ".down")).touch()
    if config.get("compose_error"):
        fail()
elif args == ["volume", "ls", "-q"]:
    if config.get("volume_error"):
        fail()
    counter = root / "polls"
    count = int(counter.read_text()) + 1 if counter.exists() else 1
    counter.write_text(str(count))
    print("unrelated")  # Do not treat unrelated volumes as a range leak.
    if config.get("leak") or count <= config.get("delayed_polls", 0):
        print("anonymous\nsecond")
else:
    raise SystemExit("unexpected fake Docker command: " + repr(args))
'''


class DevDownTests(unittest.TestCase):
    def run_down(self, config=None, args=()):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tools = root / "bin"
            tools.mkdir()
            docker = tools / "docker"
            docker.write_text(f"#!{sys.executable}\n" + DOCKER)
            docker.chmod(0o755)
            sleep = tools / "sleep"
            sleep.write_text(
                '#!/bin/sh\n'
                'printf "%s\\n" "$*" >> "$FAKE_ROOT/watchdogs"\n'
                'if [ "$*" = 1 ]; then printf "%s\\n" "$*" >> "$FAKE_ROOT/sleeps"; fi\n'
                'if [ "$*" = 1 ]; then exec /bin/sleep 0.02; fi\n'
                'if [ "${FAKE_WATCHDOG_FAST:-0}" = 1 ]; then exec /bin/sleep 0.3; fi\n'
                'exec /bin/sleep 1\n'
            )
            sleep.chmod(0o755)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                       FAKE_ROOT=tmp, FAKE_CONFIG=json.dumps(config or {}),
                       FAKE_WATCHDOG_FAST="1" if (config or {}).get("stall") else "0")
            run = subprocess.run(["bash", str(ROOT / "scripts/dev-down.sh"), *args],
                                 env=env, capture_output=True, text=True, timeout=10)
            calls = [json.loads(line) for line in (root / "calls").read_text().splitlines()]
            sleeps = (root / "sleeps").read_text().splitlines() if (root / "sleeps").exists() else []
            watchdogs = (root / "watchdogs").read_text().splitlines() \
                if (root / "watchdogs").exists() else []
            polls = int((root / "polls").read_text()) if (root / "polls").exists() else 0
            return run, calls, sleeps, watchdogs, polls

    def test_delayed_volume_removal_is_not_a_leak_and_teardown_order_is_preserved(self):
        run, calls, sleeps, watchdogs, polls = self.run_down(dict(delayed_polls=2))
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(sleeps, ["1", "1"])
        self.assertEqual(set(watchdogs), {"15", "1"})
        self.assertEqual(polls, 3)
        self.assertIn("2 anonymous volume(s) removed", run.stdout)
        compose = [call for call in calls if call["args"][0] == "compose"]
        self.assertEqual([call["args"] for call in compose], [
            ["compose", "-p", "rangerdanger", "down", "-v", "--remove-orphans"],
            ["compose", "-p", "rangerdanger-platform", "down", "--remove-orphans"]])
        for call in compose:
            self.assertNotEqual(call["cwd"], str(ROOT))
        self.assertFalse(any(call["args"][:2] == ["volume", "rm"] for call in calls))

    def test_removal_at_last_poll_succeeds(self):
        run, _, sleeps, watchdogs, polls = self.run_down(dict(delayed_polls=10))
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(len(sleeps), 10)
        self.assertEqual(set(watchdogs), {"15", "1"})
        self.assertEqual(polls, 11)

    def test_real_leak_fails_after_bounded_wait_without_stopping_platform(self):
        run, calls, sleeps, watchdogs, polls = self.run_down(dict(leak=True))
        self.assertNotEqual(run.returncode, 0)
        self.assertIn("2 volume(s) behind", run.stderr)
        self.assertEqual(len(sleeps), 10)
        self.assertEqual(set(watchdogs), {"15", "1"})
        self.assertEqual(polls, 11)
        self.assertFalse(any("rangerdanger-platform" in call["args"] for call in calls))

    def test_compose_failure_is_not_hidden_even_if_resources_were_removed(self):
        run, calls, sleeps, watchdogs, polls = self.run_down(dict(compose_error=True))
        self.assertNotEqual(run.returncode, 0)
        self.assertIn("Compose down failed", run.stderr)
        self.assertEqual(len([call for call in calls if call["args"][0] == "compose"]), 1)
        self.assertEqual(sleeps, [])
        self.assertEqual(polls, 0)

    def test_query_failures_cannot_prove_removal(self):
        for config in (dict(query_error="before"), dict(query_error="after"),
                       dict(mount_error=True), dict(volume_error=True)):
            with self.subTest(config=config):
                run, _, _, _, _ = self.run_down(config)
                self.assertNotEqual(run.returncode, 0)
                self.assertIn("cannot", run.stderr)

    def test_leftover_containers_or_networks_still_fail(self):
        run, _, _, _, _ = self.run_down(dict(left_resources=True))
        self.assertNotEqual(run.returncode, 0)
        self.assertIn("1 container(s), 1 network(s)", run.stderr)

    def test_range_only_never_queries_platform(self):
        run, calls, _, _, _ = self.run_down(args=("--range-only",))
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertNotIn("rangerdanger-platform", json.dumps(calls))

    def test_no_volumes_needs_no_poll(self):
        run, _, sleeps, _, polls = self.run_down(dict(no_volumes=True))
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual((sleeps, polls), ([], 0))

    def test_empty_projects_do_not_invoke_compose(self):
        run, calls, sleeps, _, polls = self.run_down(dict(empty=True))
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertFalse(any(call["args"][0] == "compose" for call in calls))
        self.assertEqual((sleeps, polls), ([], 0))

    def test_stalled_docker_queries_are_bounded_and_report_recovery_guidance(self):
        for operation, message in (
            ("ps", "cannot query containers/networks"),
            ("network", "cannot query containers/networks"),
            ("inspect", "cannot record mounted volumes"),
            ("volume", "cannot verify volume removal"),
        ):
            with self.subTest(operation=operation):
                run, _, _, watchdogs, _ = self.run_down(dict(stall=operation))
                self.assertNotEqual(run.returncode, 0)
                self.assertIn(message, run.stderr)
                self.assertIn(
                    "Start or restart Docker Desktop or dockerd, then re-run",
                    run.stderr,
                )
                self.assertIn("15", watchdogs)
