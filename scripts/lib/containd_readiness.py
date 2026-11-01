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


def wait_ready(receipt, read, *, health=False, timeout=30, clock=time.monotonic, sleep=time.sleep):
    """Never refresh an apply identity or recommit to hide failed readiness."""
    deadline = clock() + min(timeout, 30)
    if not isinstance(receipt, dict):
        raise ValueError(f"invalid commit response: {receipt}")
    apply_id = receipt.get("applyId")
    if not health and (not isinstance(apply_id, str) or not apply_id):
        raise ValueError(f"commit response has no applyId: {receipt}")
    last = receipt.get("protection") if not health else None
    seen = False
    previous_installed = None
    while True:
        if clock() > deadline:
            raise ValueError(f"readiness timeout for {apply_id}: last observation {last}")
        if last is not None:
            if not isinstance(last, dict):
                raise ValueError(f"invalid readiness response: {last}")
            if health:
                if last.get("enforcement", {}).get("state") == "enforcing":
                    return last
            else:
                current, installed = last.get("applyId"), last.get("installedApplyId")
                if (current and current != apply_id and seen) or (
                    installed and installed != apply_id and
                    (previous_installed is not None and installed != previous_installed)
                ):
                    raise ValueError(f"apply {apply_id} superseded: {last}")
                seen = seen or current == apply_id or installed == apply_id
                if installed:
                    previous_installed = installed
                if last.get("state") in ("degraded", "failed", "disabled"):
                    raise ValueError(f"apply {apply_id} not protecting: {last}")
                if current == installed == apply_id and last.get("state") == "protecting":
                    return last
        remaining = deadline - clock()
        if remaining <= 0:
            raise ValueError(f"readiness timeout for {apply_id}: last observation {last}")
        if last is not None:
            sleep(min(0.5, remaining))
        remaining = deadline - clock()
        if remaining <= 0:
            raise ValueError(f"readiness timeout for {apply_id}: last observation {last}")
        last = read("/health" if health else "/dataplane/protection", remaining)
        if not isinstance(last, dict):
            raise ValueError(f"invalid readiness response: {last}")


def docker_reader(container, token=None):
    if token is None:
        def enc(value):
            return base64.urlsafe_b64encode(value).rstrip(b"=").decode()
        message = enc(b'{"alg":"HS256","typ":"JWT"}') + "." + enc(json.dumps(
            {"sub": "smoke", "role": "admin", "exp": int(time.time()) + 600}).encode())
        secret = os.environ.get("CONTAIND_JWT_SECRET", "rangerdanger-dev").encode()
        token = message + "." + enc(hmac.new(secret, message.encode(), hashlib.sha256).digest())

    def read(path, remaining):
        result = subprocess.run(
            ["docker", "exec", container, "curl", "-sS", "--max-time", str(remaining),
             "-w", "\n%{http_code}", "-H", "Authorization: Bearer " + token,
             "http://127.0.0.1:8080/api/v1" + path],
            capture_output=True, text=True, timeout=remaining, check=True)
        body, status = result.stdout.rsplit("\n", 1)
        if status != "200":
            raise ValueError(f"GET {path} HTTP {status}: {body}")
        return json.loads(body)
    return read


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", default="rangerdanger-firewall")
    parser.add_argument("--token")
    parser.add_argument("--health", action="store_true")
    args = parser.parse_args()
    try:
        receipt = {} if args.health else json.load(sys.stdin)
        wait_ready(receipt, docker_reader(args.container, args.token), health=args.health)
    except (ValueError, subprocess.SubprocessError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
