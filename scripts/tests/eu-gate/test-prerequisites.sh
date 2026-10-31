#!/usr/bin/env bash
# G11/G12 own their healthy baseline even after a failed gate. G8/G9 still
# require G7's verified disconnect to attribute their cache/recovery evidence.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DOCKER_LOG="$TMP_DIR/docker-calls.log"
OPENDSS_STATE=false
: >"$DOCKER_LOG"
docker() {
  printf '%s\n' "$*" >>"$DOCKER_LOG"
  case "$1" in
    pause) OPENDSS_STATE=true ;;
    unpause) OPENDSS_STATE=false ;;
    inspect) printf '%s\n' "$OPENDSS_STATE" ;;
    *) return 99 ;;
  esac
}
eu_wait_for_operator() {
  printf 'waited for operator: %s\n' "$3"
}
eu_command() { pass "intentional no_measurement rejection (stub)"; }
eu_apply_named() {
  pass "$1 applied (stub)"
}
eu_reset_all() { pass "reset (stub)"; }
BASELINE_FAIL=false
eu_wait_for_baseline() {
  if [ "$BASELINE_FAIL" = true ]; then
    fail "$1 unavailable (stub)"
    return 1
  fi
  pass "$1 (stub)"
}
# The held G7/G11 readback comparison fails as it did in run 1.
eu_commit_manual_policy() {
  fail "containd effective firewall rules differ from custom $2 before session kill"
  return 1
}
gate() {
  gate_start "$1"
  "eu_gate_$1"
  gate_end "$1"
}

t_run gate G11
t_expect_out "GATE G11 FAIL" "the held custom-policy readback failure fails G11"
t_expect_eq "$g11_loss_verified" 0 "G11 records no verified Modbus loss"
t_run gate G12
t_expect_fails 0 "G12 establishes its own baseline after G11 fails"
t_expect_out "GATE G12 PASS" "G12 passes with its own baseline and both quality transitions"
t_expect_out "improved applied (stub)" "G12 restores the conduit even if G11 left it blocked"
t_expect_out "reset (stub)" "G12 resets devices rather than trusting earlier gates"
t_expect_out "G12 GOOD baseline before measurement loss" "G12 waits for operator/truth/solver agreement"
t_expect_eq "$OPENDSS_STATE" false "G12 unpaused the OpenDSS container it paused"

: >"$DOCKER_LOG"
BASELINE_FAIL=true
t_run gate G12
t_expect_fails 1 "G12 fails when its own baseline cannot converge"
t_expect_out "GATE G12 FAIL" "a failed baseline cannot silently pass"
t_expect_eq "$(cat "$DOCKER_LOG")" "" "G12 does not pause OpenDSS without its baseline"
t_run gate G11
t_expect_fails 1 "G11 fails when its own baseline cannot converge"
t_expect_no_out "custom" "G11 does not restrict Modbus without its baseline"

# shellcheck disable=SC2034 # read by eu_gate_G8 and eu_gate_G9
g7_loss_verified=0
t_run gate G8
t_expect_fails 1 "G8 fails without the verified G7 disconnect"
t_run gate G9
t_expect_fails 1 "G9 fails without the verified G7 disconnect"
t_expect_out "without the verified G7 CC disconnect" "the G9 failure names the unmet G7 prerequisite"
t_expect_no_out "waited for operator" "G9 does not wait for a recovery that cannot be attributed"

# Exercise the real named apply readback after the gate-level stubs above.
# shellcheck source-path=SCRIPTDIR source=../../lib/eu-policy.sh
. "$T_REPO/scripts/lib/eu-policy.sh"
eu_api_post() {
  HTTP_STATUS=200
  API_BODY='{"status":"applied"}'
}
eu_api_get() {
  API_BODY="$ACTIVE_READBACK"
}
ACTIVE_READBACK='{"active_config":"improved","policy_source":"manual-custom"}'
t_run eu_apply_named improved
t_expect_fails 1 "named restore rejects retained manual commit attribution"
ACTIVE_READBACK='{"active_config":"improved","policy_source":"hardened-reference","plan":{}}'
t_run eu_apply_named improved
t_expect_fails 1 "named restore rejects retained plan provenance"
ACTIVE_READBACK='{"active_config":"improved","policy_source":"hardened-reference"}'
t_run eu_apply_named improved
t_expect_fails 0 "named restore confirms hardened reference attribution with no plan"
ACTIVE_READBACK='{"active_config":"weak","policy_source":"weak"}'
t_run eu_apply_named weak
t_expect_fails 0 "weak restore confirms weak attribution with no plan"

# The real manual helper must never call a backend mutation or flush sessions.
MANUAL_INPUT="$TMP_DIR/manual-input.json"
STAGED_INPUT="$TMP_DIR/staged-input.json"
EFFECTIVE_INPUT="$TMP_DIR/effective-input.json"
RUNNING_INPUT="$TMP_DIR/running-input.json"
manual_fixture_reset() {
  printf '%s\n' '{"dataplane":{"enforcement":false,"dpiEnabled":false},
    "interfaces":[{"name":"mgmt","device":"policy-mgmt","zone":"management"},
      {"name":"field","device":"policy-field"},{"name":"unbound","device":"policy-unbound"},
      {"name":"empty","device":"policy-empty"},{"name":"logical","zone":"logical"}],
    "firewall":{"rules":[{"id":"retained-rule","action":"ALLOW"}]}}' >"$MANUAL_INPUT"
  printf '%s\n' '{"interfaces":[{"name":"field","device":"eth2"},
      {"name":"mgmt","device":"eth0"},{"name":"empty","device":""},
      {"name":"appliance-only","device":"eth9"}],
    "firewall":{"rules":[]}}' >"$RUNNING_INPUT"
  cp "$MANUAL_INPUT" "$EFFECTIVE_INPUT"
  : >"$DOCKER_LOG"
  rm -f "$STAGED_INPUT"
  FW_TOKEN=fixture-admin
  RUNNING_STATUS=200
  RUNNING_RC=0
  CANDIDATE_STATUS=201
  COMMIT_STATUS=204
  CANDIDATE_RC=0
  COMMIT_RC=0
  COMMIT_HEADERS=$'HTTP/1.1 204 No Content\r\nX-Containd-Warnings: \t\r\n\r\n'
  ACTIVE_CALLS=0
  ACTIVE_AFTER=11
  SLEEPS=0
  GET_RC=0
  INITIAL_READBACK='{"active_config":"custom","policy_source":"plan","plan":{}}'
  FINAL_READBACK='{"active_config":"custom","policy_source":"manual-custom"}'
}
docker() {
  printf '%s\n' "$*" >>"$TMP_DIR/manual-docker-args"
  case "$*" in
    "exec -i $FW curl "*"/api/v1/config/candidate")
      printf 'candidate\n' >>"$DOCKER_LOG"
      cat >"$STAGED_INPUT"
      printf '{"status":"staged"}\n%s' "$CANDIDATE_STATUS"
      return "$CANDIDATE_RC" ;;
    "exec $FW curl "*"/api/v1/config/commit")
      printf 'commit\n' >>"$DOCKER_LOG"
      printf '%s' "$COMMIT_HEADERS" >&2
      printf '{"status":"committed"}\n%s' "$COMMIT_STATUS"
      return "$COMMIT_RC" ;;
    "exec $FW curl "*"/api/v1/config")
      if grep -qx commit "$DOCKER_LOG"; then
        printf 'readback\n' >>"$DOCKER_LOG"
        cat "$EFFECTIVE_INPUT"
        printf '\n200'
      else
        printf 'bindings\n' >>"$DOCKER_LOG"
        cat "$RUNNING_INPUT"
        printf '\n%s' "$RUNNING_STATUS"
        return "$RUNNING_RC"
      fi ;;
    *) return 99 ;;
  esac
}
eu_api_post() {
  fail "manual commit unexpectedly called backend mutation $1"
  return 1
}
eu_api_get() {
  [ "$1" = /api/firewall/active ] || return 1
  ACTIVE_CALLS=$((ACTIVE_CALLS + 1))
  if [ "$ACTIVE_CALLS" -gt "$ACTIVE_AFTER" ]; then
    API_BODY="$FINAL_READBACK"
  else
    API_BODY="$INITIAL_READBACK"
  fi
  return "$GET_RC"
}
sleep() {
  [ "$1" = 1 ] || return 1
  SLEEPS=$((SLEEPS + 1))
}
manual_fixture_reset
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 0 "manual commit converges after grace plus tick"
t_expect_fails 0 "manual commit has no warning, backend mutation or session flush"
t_expect_eq "$(cat "$DOCKER_LOG")" "$(printf 'bindings\ncandidate\ncommit\nreadback')" \
  "manual commit reads bindings before staging, committing and reading effective rules"
t_expect_eq "$ACTIVE_CALLS" 12 "custom with plan source cannot satisfy manual observation"
t_expect_eq "$SLEEPS" 11 "observer convergence may exceed the ten-second grace plus tick"
t_run jq -e --slurpfile original "$MANUAL_INPUT" \
  '.dataplane.enforcement == true and .dataplane.dpiEnabled == false
   and .firewall == $original[0].firewall' "$STAGED_INPUT"
t_expect_eq "$T_STATUS" 0 "staging enables enforcement without changing rules or other dataplane fields"
t_run jq -e '.interfaces == [
  {"name":"mgmt","device":"eth0","zone":"management"},
  {"name":"field","device":"eth2"},{"name":"unbound"},{"name":"empty"},
  {"name":"logical","zone":"logical"}]' "$STAGED_INPUT"
t_expect_eq "$T_STATUS" 0 \
  "staging matches running devices by name, strips document devices and adds no interfaces"
t_run grep -F -- '-H Authorization: Bearer fixture-admin -H Content-Type: application/json' \
  "$TMP_DIR/manual-docker-args"
t_expect_eq "$T_STATUS" 0 "containd requests use the admin JWT and JSON content type"

for running_failure in transport http malformed null interfaces false-interfaces device; do
  manual_fixture_reset
  case "$running_failure" in
    transport) RUNNING_RC=7 ;;
    http) RUNNING_STATUS=500 ;;
    malformed) printf '{' >"$RUNNING_INPUT" ;;
    null) printf 'null' >"$RUNNING_INPUT" ;;
    interfaces) printf '{"interfaces":{},"firewall":{"rules":[]}}' >"$RUNNING_INPUT" ;;
    false-interfaces) printf '{"interfaces":false,"firewall":{"rules":[]}}' >"$RUNNING_INPUT" ;;
    device) printf '{"interfaces":[{"name":"mgmt","device":42}],"firewall":{"rules":[]}}' >"$RUNNING_INPUT" ;;
  esac
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 1 "running binding read/decode failure stops manual commit: $running_failure"
  t_expect_fails 1 "running binding failure reports a gate failure: $running_failure"
  t_expect_eq "$(cat "$DOCKER_LOG")" bindings \
    "running binding failure stops before staging or commit: $running_failure"
  t_expect_eq "$ACTIVE_CALLS" 0 "running binding failure cannot observe success: $running_failure"
  t_run test ! -e "$STAGED_INPUT"
  t_expect_eq "$T_STATUS" 0 "running binding failure sends no candidate body: $running_failure"
done
manual_fixture_reset
RUNNING_STATUS=503
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_out "cannot read appliance bindings before staging manual policy fixture" \
  "running-config read failure explains why manual staging stopped"

for interfaces_patch in 'del(.interfaces)' '.interfaces = null' \
  '.interfaces += [{"name":"mgmt","device":""}]'; do
  manual_fixture_reset
  jq "$interfaces_patch" "$RUNNING_INPUT" >"$TMP_DIR/running-mutated.json"
  cp "$TMP_DIR/running-mutated.json" "$RUNNING_INPUT"
  ACTIVE_AFTER=0
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 0 "running bindings follow backend decoding: $interfaces_patch"
  t_run jq -e '.interfaces[0] | has("device") | not' "$STAGED_INPUT"
  t_expect_eq "$T_STATUS" 0 "missing or last-empty binding removes document device: $interfaces_patch"
done

for mutation in 'del(.dataplane)' '.dataplane = null' '.dataplane = "invalid"' \
  '.dataplane.enforcement = true'; do
  manual_fixture_reset
  jq "$mutation" "$MANUAL_INPUT" >"$TMP_DIR/mutated-policy.json"
  ACTIVE_AFTER=0
  t_run eu_commit_manual_policy "$TMP_DIR/mutated-policy.json" fixture
  t_expect_eq "$T_STATUS" 0 "manual commit handles enforcement injection: $mutation"
  t_run jq -e '.dataplane.enforcement == true' "$STAGED_INPUT"
  t_expect_eq "$T_STATUS" 0 "staged enforcement is enabled: $mutation"
done

for stage_status in 200 204; do
  manual_fixture_reset
  CANDIDATE_STATUS="$stage_status"
  COMMIT_STATUS=200
  ACTIVE_AFTER=0
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 0 "backend-supported candidate $stage_status and commit 200 succeed"
done

for warning_headers in \
  $'X-Containd-Warnings: nft apply failed\r\nX-Containd-Warnings: route apply failed\r\n' \
  $'x-containd-warnings: \t\r\nx-CoNtAiNd-WaRnInGs:routing failed\r\n'; do
  manual_fixture_reset
  COMMIT_HEADERS="$warning_headers"
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 1 "commit rejects repeated or mixed-case nonempty warning headers"
  t_expect_out "reported policy import warnings" "commit warning failure is explicit"
  t_expect_eq "$ACTIVE_CALLS" 0 "warnings prevent claiming observed success"
done

manual_fixture_reset
CANDIDATE_STATUS=422
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "rejected candidate fails"
t_expect_eq "$(cat "$DOCKER_LOG")" "$(printf 'bindings\ncandidate')" \
  "rejected candidate is never committed"
manual_fixture_reset
COMMIT_STATUS=500
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "rejected commit fails"
t_expect_eq "$ACTIVE_CALLS" 0 "rejected commit cannot poll toward success"
for transport in candidate commit; do
  manual_fixture_reset
  if [ "$transport" = candidate ]; then CANDIDATE_RC=7; else COMMIT_RC=7; fi
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 1 "$transport transport failure propagates"
done
manual_fixture_reset
GET_RC=1
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "active-policy read failure propagates"
manual_fixture_reset
ACTIVE_AFTER=99
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "observer non-convergence fails"
t_expect_eq "$SLEEPS" 15 "observer wait has a fifteen-second sleep budget"
t_expect_eq "$ACTIVE_CALLS" 16 "observer polling is bounded including its initial read"
manual_fixture_reset
ACTIVE_AFTER=0
FINAL_READBACK='{"active_config":"improved","policy_source":"manual-custom"}'
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "manual source alone cannot satisfy custom active configuration"
manual_fixture_reset
ACTIVE_AFTER=0
FINAL_READBACK='{"active_config":"custom","policy_source":"manual-custom","plan":{}}'
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "manual observation must clear plan provenance"

manual_fixture_reset
ACTIVE_AFTER=0
jq '.firewall.rules[].ics = {}' "$MANUAL_INPUT" >"$EFFECTIVE_INPUT"
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 0 "empty ICS readback equals absent policy ICS"
t_expect_fails 0 "empty ICS readback does not report a gate failure"
manual_fixture_reset
ACTIVE_AFTER=0
jq '.firewall.rules[].ics = {}' "$EFFECTIVE_INPUT" >"$MANUAL_INPUT"
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 0 "empty policy ICS equals absent readback ICS"

for ics_value in '{"enabled":true}' null '[]' false; do
  manual_fixture_reset
  ACTIVE_AFTER=0
  jq --argjson ics "$ics_value" '.firewall.rules[].ics = $ics' \
    "$MANUAL_INPUT" >"$EFFECTIVE_INPUT"
  t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
  t_expect_eq "$T_STATUS" 1 "readback ICS other than an empty object still differs: $ics_value"
  t_expect_out "before session kill" "ICS mismatch fails before tuple kills: $ics_value"
done

manual_fixture_reset
ACTIVE_AFTER=0
jq '.firewall.rules[0] |= (.ics = {} | .id = "unexpected-rule")' \
  "$MANUAL_INPUT" >"$EFFECTIVE_INPUT"
t_run eu_commit_manual_policy "$MANUAL_INPUT" fixture
t_expect_eq "$T_STATUS" 1 "changed rule field still fails despite empty ICS normalization"
t_expect_out "before session kill" "effective readback failure retains its ordering diagnostic"

t_finish
