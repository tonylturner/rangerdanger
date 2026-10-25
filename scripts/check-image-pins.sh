#!/usr/bin/env bash
# Check tracked Dockerfiles and the two Python conformance peers without Docker.
# Usage: scripts/check-image-pins.sh [repository-root]
set -euo pipefail
ROOT="${1:-$(git rev-parse --show-toplevel)}"
exec python3 - "$ROOT" <<'PY'
from pathlib import Path
import os
import re
import shlex
import subprocess
import sys

ROOT = Path(sys.argv[1]).resolve()
PIN = re.compile(r"[^\s@$]+:[A-Za-z0-9_][A-Za-z0-9_.-]*@sha256:[0-9a-f]{64}")
VARIABLE = re.compile(r"\$(?:\{([A-Za-z_]\w*)\}|([A-Za-z_]\w*))")
errors = []


def fail(path, line, message):
    errors.append(f"{path}:{line}: {message}")


def pinned(path, line, image):
    if not PIN.fullmatch(image):
        fail(path, line, f"base must be name:tag@sha256:<64 lowercase hex>: {image}")


def instructions(text):
    """Join Docker continuations and skip comments and heredoc bodies."""
    escape = "\\"
    pending = ""
    start = 0
    heredocs = []
    for number, raw in enumerate(text.splitlines(), 1):
        if heredocs:
            marker, strip_tabs = heredocs[0]
            if (raw.lstrip("\t") if strip_tabs else raw) == marker:
                heredocs.pop(0)
            continue
        if raw.lstrip().startswith("#"):
            directive = re.fullmatch(r"\s*#\s*escape\s*=\s*([`\\])\s*", raw, re.I)
            if directive:
                escape = directive[1]
            continue
        if not raw.strip():
            continue
        if not pending:
            start = number
        if raw.rstrip().endswith(escape):
            pending += raw.rstrip()[:-1]
            continue
        instruction = pending + raw
        pending = ""
        # Only RUN/COPY accept heredocs. Quoted delimiters are literal too.
        if re.match(r"\s*(RUN|COPY)\s", instruction, re.I):
            for match in re.finditer(r"<<(-?)[ \t]*(['\"]?)([\w.-]+)\2", instruction):
                heredocs.append((match[3], bool(match[1])))
        yield start, instruction
    if pending or heredocs:
        raise ValueError("unterminated continuation or heredoc")


def dockerfile(path):
    arguments = {}
    stages = set()
    in_stage = False

    def expand(value, seen=()):
        def replace(match):
            name = match[1] or match[2]
            if name not in arguments or name in seen:
                raise ValueError(f"undefined or cyclic base-image ARG: {name}")
            return expand(arguments[name], (*seen, name))
        result = VARIABLE.sub(replace, value)
        if "$" in result:
            raise ValueError(f"unsupported base-image ARG expression: {value}")
        return result

    try:
        for line, instruction in instructions((ROOT / path).read_text()):
            match = re.match(r"\s*(ARG|FROM)(?:\s+(.*))?$", instruction, re.I)
            if not match:
                continue
            kind, body = match[1].upper(), match[2] or ""
            try:
                if kind == "ARG":
                    name, separator, value = body.partition("=")
                    name = name.strip()
                    if not re.fullmatch(r"[A-Za-z_]\w*", name):
                        raise ValueError("expected one ARG declaration")
                    value = " ".join(shlex.split(value, comments=False))
                    if not in_stage:
                        arguments[name] = value if separator else ""
                    # Check image defaults even if a later edit stops using them.
                    if separator and (name.endswith("_IMAGE") or "@sha256:" in value):
                        pinned(path, line, expand(value))
                    continue
                in_stage = True
                words = shlex.split(body, comments=False)
                if words and words[0].startswith("--platform="):
                    words.pop(0)
                if not words or (len(words) != 1 and
                                 not (len(words) == 3 and words[1].upper() == "AS")):
                    raise ValueError("expected FROM [--platform=...] image [AS stage]")
                image = expand(words[0])
                if image.lower() not in stages:
                    pinned(path, line, image)
                if len(words) == 3:
                    stages.add(words[2].lower())
            except ValueError as error:
                fail(path, line, str(error))
    except ValueError as error:
        fail(path, 1, str(error))


try:
    result = subprocess.run(
        ["git", "-C", str(ROOT), "ls-files", "-z"],
        check=True, capture_output=True, timeout=30,
    )
    tracked = [os.fsdecode(path) for path in result.stdout.split(b"\0") if path]
    dockerfiles = [
        path for path in tracked
        if not path.endswith(".dockerignore") and
           (Path(path).name == "Dockerfile" or
            Path(path).name.startswith("Dockerfile.") or
            Path(path).name.endswith(".Dockerfile"))
    ]
    if not dockerfiles:
        raise ValueError("no tracked Dockerfiles found")
    for path in dockerfiles:
        dockerfile(path)
    peers = {
        "services/iec104/check/run.sh": r"^PY=(\S+)$",
        "services/iec104/check/reference/build.py": r'"peer":\s*"([^"]+)"',
    }
    for path, pattern in peers.items():
        if path not in tracked:
            continue
        text = (ROOT / path).read_text()
        matches = list(re.finditer(pattern, text, re.M))
        if len(matches) != 1:
            fail(path, 1, "expected exactly one Python peer image reference")
        for match in matches:
            pinned(path, text.count("\n", 0, match.start()) + 1, match[1])
except (OSError, ValueError, subprocess.SubprocessError) as error:
    errors.append(str(error))

if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print(f"Image pins OK ({len(dockerfiles)} tracked Dockerfiles)")
PY
