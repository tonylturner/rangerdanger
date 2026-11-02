"""US feeder consequence gate. The L4 source pin enforces; DPI detects and logs."""
import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

from containd_readiness import require_backend_protection


ROOT = Path(__file__).resolve().parents[2]
DESTINATION = "10.40.40.20:20000"
OPERATE = ("docker", "exec", "rangerdanger-kali", "dnp3cmd",
           DESTINATION, "-a", "1", "crob", "0", "trip")
CONTROL = ("docker", "exec", "rangerdanger-rtac-sim", "dnp3poll",
           "-a", "1", "-t", "3s", DESTINATION)
# A UDP connect selects the unbound socket's source without sending a packet.
SOURCE = ("docker", "exec", "rangerdanger-kali", "python3", "-c", """
import datetime, json, shutil, socket
if not shutil.which("dnp3cmd"):
    raise RuntimeError("missing dnp3cmd")
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
    sock.connect(("10.40.40.20", 20000))
    print(json.dumps({"source": sock.getsockname()[0],
                      "utc": datetime.datetime.now(datetime.timezone.utc).isoformat()}))
""")
RESTORE = (
    ("relay", "clear_fault", None), ("relay", "unlock", None),
    ("relay", "close", None), ("recloser", "clear_fault", None),
    ("recloser", "reset_lockout", None), ("recloser", "enable_reclose", None),
    ("recloser", "close", None), ("regulator", "set_tap", 0),
    ("regulator", "set_auto", None),
)


class GateError(RuntimeError):
    """A link that cannot be proven, including transport and cleanup failures."""


@dataclass
class HTTPReply:
    status: int
    body: str


def timestamp(value):
    if not isinstance(value, str):
        raise GateError("invalid range timestamp: not a string")
    try:
        stamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise GateError(f"invalid range timestamp: {value}") from exc
    if stamp.tzinfo is None or stamp.year < 2000:
        raise GateError(f"invalid range timestamp: {value}")
    return stamp


def object_json(raw):
    try:
        value = json.loads(raw)
    except (ValueError, TypeError) as exc:
        raise GateError("malformed JSON input") from exc
    if not isinstance(value, dict):
        raise GateError("JSON input is not an object")
    return value


def check_state(state, boundary, energized):
    """Strict device/solve agreement; zero-valued booleans are not booleans."""
    try:
        electrical, physics = state["electrical"], state["physics"]
        solved = timestamp(electrical["solved_at"])
        if solved <= boundary or timestamp(physics["solved_at"]) != solved:
            raise GateError("unchanged solve or physics/solve timestamp mismatch")
        poll, good = timestamp(state["last_poll"]), timestamp(physics["last_good_at"])
        if not (-10 <= (poll - solved).total_seconds() <= 10
                and 0 <= (poll - good).total_seconds() <= 10):
            raise GateError("solve/physics not fresh against range last_poll")
        if physics["stale"] is not False or electrical["converged"] is not True:
            raise GateError("stale or nonconverged physics")
        if type(physics["consecutive_failures"]) is not int or physics["consecutive_failures"] != 0:
            raise GateError("physics failures")
        for device in ("relay", "recloser", "regulator", "capbank"):
            if state["device_comms"][device] is not True:
                raise GateError(f"device communications failed: {device}")
        checks = (
            (state["devices"]["relay"]["breaker_closed"], energized),
            (state["devices"]["recloser"]["closed"], True),
            (state["devices"]["recloser"]["reclose_enabled"], True),
            (electrical["breaker_closed"], energized),
            (electrical["recloser_closed"], True),
            (electrical["general_load_energized"], energized),
            (electrical["critical_load_energized"], energized),
        )
        if any(actual is not expected for actual, expected in checks):
            raise GateError("device/electrical outage or recovery mismatch")
        for name in ("downstream_voltage_v", "critical_load_voltage_v", "feeder_current_a"):
            value = electrical[name]
            if type(value) not in (int, float) or not math.isfinite(value):
                raise GateError(f"invalid electrical measurement: {name}")
            if (energized and value <= 0) or (not energized and value != 0):
                raise GateError(f"electrical consequence mismatch: {name}={value}")
    except (KeyError, TypeError) as exc:
        raise GateError(f"missing/malformed state field: {exc}") from exc


def operate_result(result, elapsed, denied):
    """dnp3cmd exits zero even when the transport or protocol failed."""
    if result.returncode != 0 or result.stderr.strip():
        raise GateError(f"operate tool-error rc={result.returncode}: {result.stderr}")
    lines = re.findall(r"^\s*Result:\s*(.*?)\s*$", result.stdout, re.M)
    if len(lines) != 1:
        raise GateError("missing/ambiguous operate Result")
    value = lines[0]
    if not denied:
        if value != "SUCCESS":
            raise GateError(f"operate rejected: {value}")
        return
    # Match a dial failure to this literal listener, never a read/write timeout.
    if re.fullmatch(r"FAILED: dial tcp " + re.escape(DESTINATION) + r": i/o timeout", value):
        if elapsed < 2.5:
            raise GateError("dial timeout too short for the client's 3s connect deadline")
        return
    for needle, classification in (
            ("connection refused", "refusal"), ("no route", "no-route"),
            ("network is unreachable", "no-route"), ("read tcp", "application-read"),
            ("write tcp", "application-write"), ("lookup ", "dns-error"),
            ("SUCCESS", "unexpected-success")):
        if needle in value:
            raise GateError(f"{classification} is not denial: {value}")
    raise GateError(f"not an explicit dial timeout: {value}")


class ChainGate:
    def __init__(self, evidence, api="http://localhost:8088", *, http=None,
                 runner=subprocess.run, clock=time.monotonic, sleep=time.sleep,
                 utc=lambda: datetime.now(timezone.utc).isoformat()):
        self.directory = Path(evidence)
        self.api = api.rstrip("/")
        self.runner, self.clock, self.sleep, self.utc = runner, clock, sleep, utc
        self.http = http or self.curl_http
        self.deadline = clock() + 210
        self.record = {"gate": "us-chain", "destination": DESTINATION,
                       "events": [], "errors": [], "result": "RUNNING"}
        self.mutable = False
        self.stage = "preflight"

    def save(self):
        try:
            self.directory.mkdir(parents=True, exist_ok=True)
            path = self.directory / "us-chain.json"
            pending = path.with_suffix(".json.tmp")
            pending.write_text(json.dumps(self.record, indent=2, allow_nan=False) + "\n")
            pending.replace(path)
        except OSError as exc:
            self.evidence_failure(exc)

    def evidence_failure(self, exc):
        message = f"evidence write failed: {exc}"
        if message not in self.record["errors"]:
            self.record["errors"].append(message)
        self.record["result"] = "FAIL"
        # Loss of evidence aborts the chain, but must not prevent safety cleanup.
        if self.stage != "cleanup":
            raise exc

    def event(self, kind, **data):
        sequence = len(self.record["events"]) + 1
        entry = {"sequence": sequence, "stage": self.stage, "kind": kind,
                 "utc": self.utc(), "monotonic": self.clock(), **data}
        self.record["events"].append(entry)
        self.save()
        # Preserve original text as well as the chronological, parsed record.
        fields = (("stdout", "stderr") if kind == "process-end" else
                  ("raw",) if kind == "http-end" else ())
        for field in fields:
            if field in data:
                name = f"{sequence:04d}-{kind}.{field}"
                try:
                    (self.directory / name).write_text(data[field])
                    entry[field + "_file"] = name
                except OSError as exc:
                    self.evidence_failure(exc)
        if fields:
            self.save()
        if kind == "complete":
            entry["result"] = self.record["result"]

    def remaining(self, maximum):
        remaining = self.deadline - self.clock()
        if remaining <= 0:
            raise GateError("gate deadline exceeded")
        return min(maximum, remaining)

    def process(self, argv, maximum=10):
        argv = list(argv)
        limit = self.remaining(maximum)
        self.event("process-start", argv=argv, timeout=limit)
        limit = self.remaining(maximum)
        start = self.clock()
        try:
            result = self.runner(argv, capture_output=True, text=True, timeout=limit)
        except subprocess.TimeoutExpired as exc:
            def text(value):
                return value.decode(errors="replace") if isinstance(value, bytes) else value or ""
            self.event("process-end", argv=argv, elapsed=self.clock() - start,
                       stdout=text(exc.stdout), stderr=text(exc.stderr), error="subprocess-deadline")
            raise GateError("subprocess-deadline is not a dial denial") from exc
        except OSError as exc:
            self.event("process-end", argv=argv, error=str(exc))
            raise GateError(f"tool-error: {exc}") from exc
        elapsed = self.clock() - start
        self.event("process-end", argv=argv, elapsed=elapsed,
                   returncode=result.returncode, stdout=result.stdout, stderr=result.stderr)
        return result, elapsed

    def checked_process(self, argv, maximum=10):
        result, elapsed = self.process(argv, maximum)
        if result.returncode != 0:
            raise GateError(f"tool-error rc={result.returncode}: {result.stderr}")
        return result, elapsed

    def curl_http(self, method, path, body, timeout):
        # curl's absolute transfer deadline also bounds slow/trickling replies.
        argv = ["curl", "--disable", "--silent", "--show-error", "--max-time", str(timeout),
                "--retry", "0", "--request", method, "--write-out", "\n%{http_code}"]
        if body is not None:
            argv += ["--header", "Content-Type: application/json",
                     "--data-binary", json.dumps(body)]
        argv += [self.api + path]
        result, _ = self.checked_process(argv, timeout)
        try:
            raw, status = result.stdout.rsplit("\n", 1)
            return HTTPReply(int(status), raw)
        except ValueError as exc:
            raise GateError("malformed HTTP transport response") from exc

    def request(self, method, path, body=None, maximum=8):
        self.event("http-start", method=method, path=path, body=body)
        reply = self.http(method, path, body, self.remaining(maximum))
        self.event("http-end", method=method, path=path, status=reply.status, raw=reply.body)
        if reply.status != 200:
            raise GateError(f"HTTP {reply.status}: {method} {path}; no retry")
        return object_json(reply.body)

    def apply(self, policy):
        reply = self.request("POST", "/api/firewall/apply", {"config": policy})
        require_backend_protection(reply)
        if reply.get("status") != "applied" or reply.get("active_config") != policy:
            raise GateError(f"policy apply mismatch: {policy}")
        self.event("policy", policy=policy, receipt=reply)

    def source(self):
        result, _ = self.checked_process(SOURCE)
        source = object_json(result.stdout)
        timestamp(source.get("utc"))
        if source.get("source") != "10.10.10.50":
            raise GateError(f"unexpected Kali source: {source}")
        self.event("source", observation=source, destination=DESTINATION)

    def control(self):
        result, _ = self.checked_process(CONTROL)
        if result.stderr.strip() or not re.search(
                r"^DNP3 Outstation " + re.escape(DESTINATION) + r" \(addr 1\).*IIN: 0x[0-9A-Fa-f]{4}$",
                result.stdout, re.M):
            raise GateError("listener positive control: missing decoded relay header")
        outputs = re.search(r"^Binary Outputs \(Group 10\):\n(.*?)(?:\n\n|\Z)",
                            result.stdout, re.M | re.S)
        if not outputs or not re.search(r"^\s*\[\s*0\] = ON\s*$", outputs[1], re.M):
            raise GateError("listener positive control: missing closed breaker Binary Output 0")
        self.event("listener-control", destination=DESTINATION)

    def state(self, maximum=8):
        return self.request("GET", "/api/substation/state", maximum=maximum)

    def boundary(self):
        state = self.state()
        try:
            return timestamp(state["electrical"]["solved_at"])
        except (KeyError, TypeError) as exc:
            raise GateError("missing solve boundary") from exc

    def wait_state(self, name, boundary, energized):
        deadline = self.clock() + self.remaining(25)
        reason = "no observation"
        while self.clock() < deadline:
            state = self.state(min(8, deadline - self.clock()))
            try:
                check_state(state, boundary, energized)
            except GateError as exc:
                reason = str(exc)
                self.event("state-pending", name=name, reason=reason)
            else:
                self.event("state-proof", name=name, boundary=boundary.isoformat(), state=state)
                return
            remaining = min(deadline, self.deadline) - self.clock()
            if remaining > 0:
                self.sleep(min(1, remaining))
        raise GateError(f"{name} poll deadline: {reason}")

    def restore(self, best_effort=False):
        errors = []
        for device, command, value in RESTORE:
            body = {"command": command}
            if value is not None:
                body["value"] = value
            try:
                response = self.request("POST", f"/api/substation/command/{device}", body)
                if response.get("result") != "executed":
                    raise GateError(f"supervisory command rejected: {device}/{command}: {response}")
            except Exception as exc:
                if not best_effort:
                    raise
                errors.append(str(exc))
        # Sample after the last command; require a subsequent solve, not a cached one.
        try:
            self.wait_state("recovery", self.boundary(), True)
        except Exception as exc:
            if not best_effort:
                raise
            errors.append(str(exc))
        if errors:
            raise GateError("; ".join(errors))

    def preflight(self):
        status = self.request("GET", "/api/range")
        if status.get("package") != "us-dnp3-substation" or status.get("phase") != "ready":
            raise GateError("US range is not ready")
        require_backend_protection(status)
        for container in ("rangerdanger-kali", "rangerdanger-rtac-sim"):
            result, _ = self.checked_process(("docker", "inspect", "--format",
                                             "{{json .State}}", container))
            state = object_json(result.stdout)
            if state.get("Running") is not True or state.get("Paused") is not False:
                raise GateError(f"container not running: {container}")
        self.checked_process(("docker", "exec", "rangerdanger-rtac-sim",
                              "sh", "-c", "command -v dnp3poll"))
        self.source()
        # No mutation is allowed until US readiness, receipt, and source are proven.
        self.mutable = True

    def chain(self):
        self.preflight()
        self.stage = "weak-baseline"
        self.apply("weak")
        self.restore()
        self.stage = "weak-operate"
        self.source()
        result, elapsed = self.process(OPERATE)
        operate_result(result, elapsed, denied=False)
        self.wait_state("outage", self.boundary(), False)
        self.stage = "recovery"
        self.restore()
        self.stage = "improved"
        self.apply("improved")
        self.control()
        self.source()
        self.stage = "improved-operate"
        result, elapsed = self.process(OPERATE)
        operate_result(result, elapsed, denied=True)
        self.wait_state("denied-energized", self.boundary(), True)
        self.control()

    def run(self):
        self.save()
        try:
            self.chain()
        except Exception as exc:
            self.record["errors"].append(f"{self.stage}: {type(exc).__name__}: {exc}")
        finally:
            if self.mutable:
                self.stage = "cleanup"
                cleanup_deadline = self.clock() + 110
                # Always attempt both, even when the first cleanup operation fails.
                for name, action in (
                        ("feeder", lambda: self.restore(best_effort=True)),
                        ("weak", lambda: self.apply("weak"))):
                    try:
                        # Reserve a final bounded apply even if feeder recovery hangs.
                        self.deadline = cleanup_deadline - (10 if name == "feeder" else 0)
                        self.event("cleanup-start", action=name)
                        action()
                    except Exception as exc:
                        self.record["errors"].append(f"cleanup {name}: {type(exc).__name__}: {exc}")
            self.record["result"] = "FAIL" if self.record["errors"] else "PASS"
            self.event("complete", result=self.record["result"])
        return 1 if self.record["errors"] else 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api", default=os.environ.get("API", "http://localhost:8088"))
    parser.add_argument("--evidence-dir", type=Path,
                        default=ROOT / "build/smoke-receipts/us-chain")
    args = parser.parse_args(argv)
    gate = ChainGate(args.evidence_dir, args.api)

    def interrupted(signum, _frame):
        raise GateError(f"interrupted by signal {signum}")

    previous = {sig: signal.signal(sig, interrupted) for sig in (signal.SIGTERM, signal.SIGINT)}
    try:
        result = gate.run()
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    print(f"us-chain-smoke {'PASS' if result == 0 else 'FAIL'}: {args.evidence_dir / 'us-chain.json'}")
    for error in gate.record["errors"]:
        print(error, file=sys.stderr)
    return result


if __name__ == "__main__":
    sys.exit(main())
