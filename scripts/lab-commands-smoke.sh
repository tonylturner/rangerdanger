#!/usr/bin/env bash
# Semantic checks of reviewed YAML command examples on a disposable stack.
# Extraction/pins fail closed BEFORE live work. Never use on a student stack.
# Re-pin with: python3 scripts/lib/lab-commands-extract.py --repin
# Review null/new oracles in scripts/lab-commands-expectations.json, then
# validate offline with: python3 scripts/lib/lab-commands-extract.py --matrix
set -euo pipefail
API="${RANGERDANGER_API:-http://localhost:8088}"
PROBE_TIMEOUT="${LAB_CMD_TIMEOUT:-10}"
SETTLE_SECS="${SETTLE_SECS:-2}"
target="${1:-}"
if [ "$target" = eu-iec104-substation ]; then
  exec python3 scripts/lib/lab_commands_smoke.py "$target"
fi
helper="scripts/lib/lab-commands-extract.py"
logs="${LAB_CMD_LOG_DIR:-build/lab-commands}"
mkdir -p "$logs"
matrix=$(mktemp)
live_started=0
cleanup() {
  local result=$?
  trap - EXIT
  rm -f "$matrix"
  if [ "$live_started" -eq 1 ]; then
    # Also restore after a policy/API/tool error exits before the loop's end.
    apply_policy weak || result=1
  fi
  exit "$result"
}
trap cleanup EXIT
# A process substitution would hide extractor failure from the parent shell.
python3 "$helper" "$target" --matrix >"$matrix"
minimum=$(awk -F'|' '$5 == "RUN" { n++ } END { print n+0 }' "$matrix")
[ "$minimum" -gt 0 ] || { echo "no executable examples" >&2; exit 1; }
fail=0
executed=0
passed=0
skipped=0
current_policy=""
err() { printf 'FAIL: %s\n' "$*" >&2; fail=1; }
canary_tcp() {
  if docker exec rangerdanger-kali timeout 1 bash -c \
      'exec 3<>/dev/tcp/10.30.30.20/502' >/dev/null 2>&1; then
    echo allow
  else
    echo deny
  fi
}

wait_for_dataplane() {
  local expected="$1"
  local budget="${2:-15}"
  for _ in $(seq 1 $((budget * 2))); do
    [ "$(canary_tcp)" = "$expected" ] && return 0
    sleep 0.5
  done
  return 1
}

# ── Apply firewall policy via the API. No-op if already on it. ─────
# Fails the smoke run if the apply call errors or if /api/firewall/active
# doesn't reflect the requested policy. The old version swallowed apply
# failures with `2>/dev/null`, which let lab-commands-smoke report 65/65
# PASS while the underlying policy state was wrong — exactly the false-
# confidence mode the audit caught. Now also polls a kali->rtac:502
# canary to confirm the dataplane (not just the API) has reconciled
# before the next command runs.
apply_policy() {
  local name="$1"
  [ -z "$name" ] && return 0
  local resp
  resp=$(curl --max-time 10 -fsS -X POST -H 'Content-Type: application/json' \
    -d "{\"config\":\"$name\"}" "$API/api/firewall/apply" 2>&1) || {
    err "policy apply ($name) failed: $resp"
    exit 1
  }
  local active
  active=$(curl --max-time 10 -fsS "$API/api/firewall/active" 2>&1) || {
    err "could not read /api/firewall/active after applying $name: $active"
    exit 1
  }
  if ! echo "$active" | grep -q "\"active_config\":\"$name\""; then
    err "policy apply silently failed: requested $name, /api/firewall/active reports: $active"
    exit 1
  fi

  local expected_canary
  case "$name" in
    weak|baseline)     expected_canary=allow ;;
    improved|hardened) expected_canary=deny  ;;
    *) sleep "$SETTLE_SECS"; return 0 ;;
  esac
  if ! wait_for_dataplane "$expected_canary" 15; then
    err "dataplane never reconciled to $name (canary kali->rtac:502 still wrong after 15s)"
    exit 1
  fi
}


policy() {
  local cfg="$1"
  case "$cfg" in
    hardened) cfg=improved ;;
    weak|improved) ;;
    *) err "unsupported policy $cfg"; exit 1 ;;
  esac
  if [ "$current_policy" != "$cfg" ]; then
    apply_policy "$cfg"
    current_policy="$cfg"
  fi
}
curl -fsS --max-time 10 "$API/api/health" >/dev/null
# These semantic checks assert the US package, never another range.
# shellcheck source=lib/range.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/range.sh"
if ! range_is_ready us-dnp3-substation; then
  err "range is not us-dnp3-substation ready — GET $API/api/range: $(range_status)"
  exit 1
fi
live_started=1
# fd 3: docker exec children must never read the matrix from stdin.
while IFS='|' read -r idx scenario step cfg action intent <&3; do
  if [ "$action" = SKIP ]; then
    skipped=$((skipped + 1))
    printf 'SKIP %s step %s (see reviewed reason in expectations)\n' "$scenario" "$step"
    continue
  fi
  control=()
  if [ "$intent" = deny ]; then
    policy weak
    if ! python3 "$helper" "$target" --execute "$idx" --positive-control \
        --output "$logs/$idx" --timeout "$PROBE_TIMEOUT"; then
      err "positive control failed: $scenario step $step"
      continue
    fi
    control=(--control-passed)
  fi
  policy "$cfg"
  executed=$((executed + 1))
  if python3 "$helper" "$target" --execute "$idx" ${control[@]+"${control[@]}"} \
      --output "$logs/$idx" --timeout "$PROBE_TIMEOUT"; then
    passed=$((passed + 1))
  else
    err "$scenario step $step; stdout/stderr saved in $logs/$idx"
  fi
done 3<"$matrix"
# Restore the disposable range even after semantic failures.
policy weak
live_started=0
if [ "$executed" -lt "$minimum" ]; then
  err "executed $executed < pinned minimum $minimum"
fi
printf 'executed=%s passed=%s failed=%s skipped=%s unverified=%s minimum=%s\n' \
  "$executed" "$passed" "$((executed - passed))" "$skipped" "$((minimum - executed))" "$minimum"
exit "$fail"
