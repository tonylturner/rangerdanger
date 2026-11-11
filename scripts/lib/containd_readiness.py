"""Strict post-commit evidence readiness, shared by shell and Python smokes."""
import argparse
import base64
import hashlib
import hmac
import json
import os
import subprocess
import sys
import time


def require_backend_protection(response):
    """Consume the route's own readiness receipt; never follow up with a GET."""
    receipt = response.get("containd") if isinstance(response, dict) else None
    if not isinstance(receipt, dict) or not isinstance(receipt.get("applyId"), str) \
            or not receipt["applyId"] or receipt.get("protection") != "protecting":
        raise ValueError(f"backend apply is not protecting: {response}")
    return receipt


def wait_ready(receipt, read, *, health=False, timeout=30, clock=time.monotonic, sleep=time.sleep):
    """Wait for the commit revision; never retry protection or recommit."""
    deadline = clock() + min(timeout, 30)
    if not isinstance(receipt, dict):
        raise ValueError(f"invalid commit response: {receipt}")
    revision = receipt.get("runningRevision")
    if not health and (not isinstance(receipt.get("status"), str) or not receipt["status"]
                       or not isinstance(revision, str) or not revision):
        raise ValueError(f"commit response has no status or runningRevision: {receipt}")
    last = receipt.get("protection") if not health else None
    first = last is None
    while True:
        if clock() > deadline:
            raise ValueError(f"readiness timeout for {revision}: last observation {last}")
        if last is not None:
            if not isinstance(last, dict):
                raise ValueError(f"invalid readiness response: {last}")
            if health:
                if last.get("enforcement", {}).get("state") == "enforcing":
                    return last
            else:
                desired = last.get("desiredRevision")
                if desired is not None and desired != revision:
                    raise ValueError(f"revision {revision} superseded: {last}")
                if last.get("state") == "disabled":
                    raise ValueError(f"revision {revision} disabled: {last}")
                for subsystem in last.get("subsystems", []):
                    fault = subsystem.get("fault") or {}
                    if subsystem.get("required") is True and fault.get("kind") in ("config", "render"):
                        raise ValueError(f"revision {revision} required subsystem fault: {last}")
                if last.get("ready") is True and last.get("protectingRevision") == revision:
                    return last
        remaining = deadline - clock()
        if remaining <= 0:
            raise ValueError(f"readiness timeout for {revision}: last observation {last}")
        if not first:
            sleep(min(0.5, remaining))
        first = False
        remaining = deadline - clock()
        if remaining <= 0:
            raise ValueError(f"readiness timeout for {revision}: last observation {last}")
        try:
            observation = read("/health" if health else "/dataplane/protection", remaining)
        except subprocess.TimeoutExpired as exc:
            if health:
                raise
            raise ValueError(f"readiness timeout for {revision}: last observation {last}") from exc
        except subprocess.CalledProcessError as exc:
            if health or exc.returncode != 28:  # curl's --max-time exhausted the remaining budget.
                raise
            raise ValueError(f"readiness timeout for {revision}: last observation {last}") from exc
        # A starting runtime (503) has no protection telemetry; retain the last.
        if observation is None and not health:
            continue
        if not isinstance(observation, dict):
            raise ValueError(f"invalid readiness response: {observation}")
        last = observation


def docker_reader(container, token=None):
    if token is None:
        def enc(value):
            return base64.urlsafe_b64encode(value).rstrip(b"=").decode()
        message = enc(b'{"alg":"HS256","typ":"JWT"}') + "." + enc(json.dumps(
            {"sub": "smoke", "role": "admin", "exp": int(time.time()) + 600}).encode())
        secret = os.environ.get("CONTAIND_JWT_SECRET", "rangerdanger-dev").encode()
        token = message + "." + enc(hmac.new(secret, message.encode(), hashlib.sha256).digest())

    def read(path, remaining):
        try:
            result = subprocess.run(
                ["docker", "exec", container, "curl", "-sS", "--max-time", str(remaining),
                 "-w", "\n%{http_code}", "-H", "Authorization: Bearer " + token,
                 "http://127.0.0.1:8080/api/v1" + path],
                capture_output=True, text=True, timeout=remaining, check=True)
        except subprocess.TimeoutExpired as exc:
            # Keep the existing remaining-budget deadline, but name the CLI
            # without printing its authorization header (including via main).
            raise ValueError(
                f"Docker CLI timed out after {remaining:g} seconds: "
                f"docker exec {container} curl GET {path}") from exc
        body, status = result.stdout.rsplit("\n", 1)
        if path == "/dataplane/protection" and status == "503":
            return None
        if status != "200" and not (path == "/dataplane/protection" and status == "502"):
            raise ValueError(f"GET {path} HTTP {status}: {body}")
        return json.loads(body)
    return read


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", default="rangerdanger-firewall")
    parser.add_argument("--token")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--health", action="store_true")
    modes.add_argument("--backend-receipt", action="store_true")
    args = parser.parse_args()
    try:
        receipt = {} if args.health else json.load(sys.stdin)
        if args.backend_receipt:
            require_backend_protection(receipt)
        else:
            wait_ready(receipt, docker_reader(args.container, args.token), health=args.health)
    except (ValueError, subprocess.SubprocessError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
