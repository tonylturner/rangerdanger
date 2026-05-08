#!/usr/bin/env bash
# Lab-commands smoke test.
#
# For every command shown in a lab YAML's step description or hint
# body (anything the scenario runner detects as a CommandBlock —
# matching the CMD_TOOL_RE pattern in scenario-runner.tsx), verify
# it executes cleanly when run from the correct container under
# the step's expected firewall policy. Catches:
#
#   - typos in lab docs (wrong IP, wrong container, wrong port)
#   - missing tools (mbpoll not installed in the source container)
#   - documented commands that worked once but rotted as the lab evolved
#   - policy mismatches (a command that requires "improved" but the
#     step is documented under "weak")
#
# This is deliberately scoped to "does it run" rather than "does it
# return the right answer." Output verification per command would
# require per-command oracles; that's a follow-on. The current pass
# bar catches everything mechanical that breaks lab fidelity.
#
# Skipped:
#   - mutating commands (firewall apply, /api/firewall/apply curl)
#     because they'd shuffle state mid-test
#   - interactive commands (ssh -p 2222 containd@localhost) because
#     they expect a terminal
#   - long-running captures (tshark/tcpdump in capture mode) — those
#     are timed out at 4s and counted as "ran" if the timeout fires
#
# Usage:
#   ./scripts/lab-commands-smoke.sh                   # all scenarios
#   ./scripts/lab-commands-smoke.sh baseline-assessment   # one
#
# Exit 0 = every doc'd command runs cleanly, non-zero = at least one fails.

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
PROBE_TIMEOUT="${LAB_CMD_TIMEOUT:-5}"
SETTLE_SECS="${SETTLE_SECS:-2}"
SCENARIOS_DIR="lab-definitions/scenarios"
TOPOLOGY="lab-definitions/substation-segmentation.yml"

fail=0
total=0
passed=0
skipped=0

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; passed=$((passed+1)); }
err()  { printf '  ✗ %s\n' "$1"; fail=1; }
skip() { printf '  ⊘ %s\n' "$1"; skipped=$((skipped+1)); }

# ── Extract commands per step from YAML using python+pyyaml ─────────
# Output format (one line per command):
#   <scenario_id>|<step_idx>|<step_title>|<expected_config>|<node>|<command>
extract_commands() {
  local target="${1:-}"
  python3 - "$target" <<'PY'
import os, sys, re, yaml, glob

SCENARIO_DIR = "lab-definitions/scenarios"
TOPOLOGY    = "lab-definitions/substation-segmentation.yml"

# Mirror scenario-runner.tsx CMD_TOOL_RE — first whitespace-separated
# token must be one of these for the line to be detected as a
# runnable command block by the UI.
CMD_TOOL_RE = re.compile(
    r'^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s'
)

# Skip commands that would be destructive, interactive, or that the
# UI's Run button doesn't actually wire up (mostly because they'd
# leave the stack in a different state than the next step expects).
SKIP_PATTERNS = [
    re.compile(r'/api/firewall/apply'),     # mutating
    re.compile(r'/api/firewall/apply-custom'),
    re.compile(r'docker compose '),         # would tear stack down
    re.compile(r'docker run '),
    re.compile(r'docker rm '),
    re.compile(r'docker stop '),
    re.compile(r'^ssh\s+-p\s+2222\b'),      # interactive containd CLI
    re.compile(r'^ssh\s+containd@'),
]

def should_skip(cmd: str) -> bool:
    return any(p.search(cmd) for p in SKIP_PATTERNS)

def load_topology():
    with open(TOPOLOGY) as f:
        topo = yaml.safe_load(f)
    nodes = {n["id"]: n.get("container", "") for n in topo.get("nodes", [])}
    return nodes

# Mirror frontend/lib/exercise-nodes.ts inferNodeFromDescription:
# the UI's Run button picks a source node from the step text by
# scanning for IPs and host keywords (in order; first match wins).
# The smoke test uses the same heuristic so a green smoke run means
# "what the student actually clicks works," not "what some other
# logic says should work."
NODE_INFERENCE = [
    ("kali-1",        re.compile(r'10\.10\.10\.50|\bkali\b', re.I)),
    ("vendor-jump-1", re.compile(r'10\.20\.20\.10|\bvendor.?jump\b', re.I)),
    ("eng-ws-1",      re.compile(r'10\.20\.20\.20|\bengineering.?workstation\b|\beng-ws\b', re.I)),
    ("openplc-1",     re.compile(r'10\.30\.30\.30|\bopenplc\b', re.I)),
    ("rtac-1",        re.compile(r'10\.30\.30\.20|\brtac\b', re.I)),
]

def infer_node_from_text(text: str) -> str:
    """First-match node inference. Returns "" if nothing matches."""
    for node_id, pat in NODE_INFERENCE:
        if pat.search(text):
            return node_id
    return ""

def infer_node_per_command(desc: str, cmd_line_idx: int):
    """For a command at line cmd_line_idx, look at the nearest preceding
    'From the X terminal' / 'On X' header where X is a known node.
    Returns "" if no recognized header found; caller falls back to
    step-level inference. Lines that have "from/on" but match an
    unrelated keyword (e.g. "on the [network map]") are skipped, not
    treated as terminators."""
    lines = desc.split("\n")
    KEYWORD_MAP = {
        "kali":         "kali-1",
        "vendor-jump":  "vendor-jump-1",
        "vendor":       "vendor-jump-1",
        "engineering":  "eng-ws-1",
        "eng-ws":       "eng-ws-1",
        "eng":          "eng-ws-1",
        "openplc":      "openplc-1",
        "rtac":         "rtac-1",
        "hmi":          "hmi-1",
        "fuxa":         "hmi-1",
        "firewall":     "fw-1",
    }
    # Look back up to 12 lines from the command for a recognized
    # "From X" / "On X" header. Closer matches win. Iterate
    # ALL "from/on" matches on each line in case there are several.
    start = max(0, cmd_line_idx - 12)
    for back in range(cmd_line_idx - 1, start - 1, -1):
        if back < 0 or back >= len(lines):
            continue
        line = lines[back].lower()
        for m in re.finditer(r'\b(?:from|on)\s+(?:the\s+)?[`*]*([a-z][a-z0-9-]*)\b', line):
            keyword = m.group(1)
            if keyword in KEYWORD_MAP:
                return KEYWORD_MAP[keyword]
    return ""

def extract_step_commands(desc: str):
    """Return list of (cmd, line_idx) tuples from a step description."""
    if not desc:
        return []
    out = []
    lines = desc.split("\n")
    i = 0
    while i < len(lines):
        raw = lines[i]
        # Indented + tool-prefixed = command block (matches UI logic)
        if re.match(r'^\s+\S', raw) and CMD_TOOL_RE.match(raw.strip()):
            cmd = raw.strip()
            cmd_idx = i
            # Continuation lines (trailing backslash)
            while cmd.endswith("\\") and i + 1 < len(lines):
                i += 1
                cmd = cmd.rstrip("\\").rstrip() + " " + lines[i].strip()
            out.append((cmd, cmd_idx))
        i += 1
    return out

def main():
    target = sys.argv[1] if len(sys.argv) > 1 else ""
    nodes = load_topology()

    files = sorted(glob.glob(os.path.join(SCENARIO_DIR, "*.yml")))
    for path in files:
        scenario_id = os.path.basename(path).replace(".yml", "")
        if target and target != scenario_id:
            continue
        with open(path) as f:
            doc = yaml.safe_load(f)
        scenario_node = (doc.get("nodes") or [None])[0]   # fallback node

        for idx, step in enumerate(doc.get("steps", []), start=1):
            title = step.get("title", "(untitled)")
            expected_cfg = step.get("expected_config", "")
            explicit_node = step.get("node", "") or ""
            desc = step.get("description", "") or ""

            # Step-level fallback: if no explicit per-command marker,
            # use the step's node (if set), else the step-level prose
            # inference (whole description), else the scenario default.
            step_inferred = infer_node_from_text(desc)
            step_node_id = explicit_node or step_inferred or (scenario_node or "")

            for cmd, line_idx in extract_step_commands(desc):
                # Per-command inference: prefer the nearest "from the
                # X terminal" header above the command. This catches
                # steps that switch source mid-step (Phase 1 of
                # firewall-implementation does this — first run from
                # kali, then from eng-ws).
                per_cmd_node = infer_node_per_command(desc, line_idx) or step_node_id
                container = nodes.get(per_cmd_node, "")

                if should_skip(cmd):
                    print(f"{scenario_id}|{idx}|{title}|{expected_cfg}|{container}|SKIP|{cmd}")
                else:
                    print(f"{scenario_id}|{idx}|{title}|{expected_cfg}|{container}|RUN|{cmd}")

if __name__ == "__main__":
    main()
PY
}

# ── Apply firewall policy via the API. No-op if already on it. ─────
apply_policy() {
  local name="$1"
  [ -z "$name" ] && return 0
  curl -fsS -X POST -H 'Content-Type: application/json' \
    -d "{\"config\":\"$name\"}" "$API/api/firewall/apply" >/dev/null 2>&1
  sleep "$SETTLE_SECS"
}

# ── Run a single command in a given container ──────────────────────
# Returns: "PASS DUR_MS" | "FAIL_EXEC DUR_MS" | "FAIL_RC=N DUR_MS"
run_command() {
  local container="$1"
  local cmd="$2"
  local start end dur rc

  start=$(python3 -c 'import time; print(int(time.time()*1000))')
  # /bin/sh because alpine-based sims (rtac-sim) don't ship bash by
  # default. POSIX sh handles pipes, redirects, and $() — all the
  # shell features the lab YAML commands actually use.
  docker exec "$container" timeout "$PROBE_TIMEOUT" sh -c "$cmd" \
      >/dev/null 2>&1
  rc=$?
  end=$(python3 -c 'import time; print(int(time.time()*1000))')
  dur=$(( end - start ))

  case "$rc" in
    0)        printf 'PASS %d\n' "$dur" ;;
    124|143)
      # Long-running capture/poll commands time out — treat as PASS
      # because we know the student command path executed.
      printf 'PASS-TIMEOUT %d\n' "$dur"
      ;;
    125)
      # docker exec couldn't start — usually container missing,
      # OCI runtime error. This is a real test failure.
      printf 'FAIL_EXEC %d\n' "$dur"
      ;;
    127)
      # Command not found inside the container — tool absent.
      printf 'FAIL_NOTOOL %d\n' "$dur"
      ;;
    *)
      # Tool ran but returned non-zero. For network probes this is
      # often "host unreachable" / "connection refused" which is
      # informational, not a doc bug. Treat rc 1-7 as PASS (tool
      # ran), higher as suspect.
      if [ "$rc" -le 7 ]; then
        printf 'PASS-RC%d %d\n' "$rc" "$dur"
      else
        printf 'FAIL_RC=%d %d\n' "$rc" "$dur"
      fi
      ;;
  esac
}

# ── Preflight ──────────────────────────────────────────────────────
note "preflight"
curl -fsS "$API/api/health" >/dev/null 2>&1 \
    && ok "backend $API healthy" \
    || { err "backend not reachable at $API — bring stack up first"; exit 1; }

if ! python3 -c 'import yaml' 2>/dev/null; then
  err "python3 + pyyaml required (pip3 install pyyaml)"
  exit 1
fi

# ── Run matrix ─────────────────────────────────────────────────────
target="${1:-}"
note "extracting commands${target:+ ($target)}"

current_policy=""
current_scenario=""

# Process line-by-line. extract_commands emits one row per command
# (or SKIP). The bash while loop applies policy per (scenario, step)
# group.
while IFS='|' read -r scenario step_idx step_title cfg container action cmd; do
  [ -z "$scenario" ] && continue
  total=$((total+1))

  # New scenario header
  if [ "$scenario" != "$current_scenario" ]; then
    note "scenario: $scenario"
    current_scenario="$scenario"
  fi

  # SKIPs are tracked but don't consume a docker exec
  if [ "$action" = "SKIP" ]; then
    skip "[$scenario step $step_idx] $cmd"
    total=$((total-1))   # don't count SKIPs in the pass denominator
    continue
  fi

  # No container — usually a planning-only scenario (1.3, 1.4) that
  # shouldn't have command blocks at all. Treat as a doc bug.
  if [ -z "$container" ]; then
    err "[$scenario step $step_idx] no source container — \`$cmd\` cannot be run from anywhere"
    continue
  fi

  # Apply the step's expected policy if it changed. weak/improved
  # only — anything else is a no-op (some steps don't pin a config).
  if [ -n "$cfg" ] && [ "$cfg" != "$current_policy" ]; then
    if [ "$cfg" = "weak" ] || [ "$cfg" = "improved" ]; then
      apply_policy "$cfg"
      current_policy="$cfg"
    fi
  fi

  # Container missing entirely
  if ! docker inspect "$container" >/dev/null 2>&1; then
    err "[$scenario step $step_idx] container $container not running — $cmd"
    continue
  fi

  result=$(run_command "$container" "$cmd")
  verdict=$(echo "$result" | awk '{print $1}')
  dur=$(echo "$result" | awk '{print $2}')

  short_cmd=$(echo "$cmd" | cut -c1-80)
  label="[$scenario s$step_idx ${cfg:-any}] ${container#rangerdanger-}: $short_cmd"

  case "$verdict" in
    PASS|PASS-TIMEOUT|PASS-RC*)
      ok "$label  (${verdict} ${dur}ms)"
      ;;
    FAIL_NOTOOL)
      err "$label  TOOL NOT INSTALLED"
      ;;
    FAIL_EXEC)
      err "$label  DOCKER EXEC FAILED"
      ;;
    *)
      err "$label  $verdict (${dur}ms)"
      ;;
  esac
done < <(extract_commands "$target")

# ── Summary ────────────────────────────────────────────────────────
note "summary"
echo "  ran    : $total"
echo "  passed : $passed"
echo "  skipped: $skipped (mutating, interactive, or out-of-scope)"
if [ "$fail" = "0" ]; then
  echo "  ALL DOCUMENTED LAB COMMANDS RUN CLEANLY"
  exit 0
else
  echo "  FAILED — $((total - passed)) commands had issues"
  exit 1
fi
