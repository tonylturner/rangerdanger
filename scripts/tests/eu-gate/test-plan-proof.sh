#!/usr/bin/env bash
# shellcheck disable=SC2329 # stubs are invoked by the sourced gate
# Full/partial plan proof uses compile goldens, not an embedded selection/matrix.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Change the golden root without modifying repository fixtures. The gate must
# follow the files here for both its request and every expected measurement value.
ORIGINAL_ROOT="$ROOT"
ROOT="$TMP_DIR/root"
mkdir -p "$ROOT/frontend/lib/testdata/plans" "$ROOT/scripts/tests/fixtures"
cp "$ORIGINAL_ROOT"/frontend/lib/testdata/plans/eu-*.json "$ROOT/frontend/lib/testdata/plans/"
cp "$ORIGINAL_ROOT/scripts/tests/fixtures/plan-gate.jq" "$ROOT/scripts/tests/fixtures/"
EU_GATE_EVIDENCE_DIR="$TMP_DIR/retained"
CALLS=""
CASE_INDEX=0
CASE_NOW=""
MUTATION="."
BREAK_AT=partial
APPLY_STATUS=200
MATRIX_STATUS=200
RESTORE_FAIL=0
LEAK_PLAN=0
sleep() { :; }
eu_mark_protection_ready() { :; }
docker() { return 99; }
curl() { return 99; }

# Build independent HTTP fixtures directly from the golden, without invoking
# the oracle under test. A faithful partial matrix matches its plan while its measured rows
# still expose the remaining Hardened prevention gaps.
make_matrix() {
  jq -c '
    {policyHash, policySource:"plan", policyStable:true,
      policyChangedAt:"2026-11-09T10:00:00Z", startedAt:"2026-11-09T10:01:00Z", finishedAt:"2026-11-09T10:01:20Z",
      rows:[.expectations[] | {id, observed:.planVerdict, error:""}]}' "$1"
}
eu_api_post() {
  local file request
  case "$1" in
    /api/firewall/apply-plan)
      CASE_INDEX=$((CASE_INDEX + 1))
      case "$CASE_INDEX" in
        1) CASE_NOW=full ;;
        2) CASE_NOW=partial ;;
        *) CASE_NOW=$(jq -er --argjson index "$((CASE_INDEX - 3))" \
          '[.actions[] | select(.kind == "enforce" or .kind == "detect") | .id][$index]' \
          "$ROOT/frontend/lib/testdata/plans/eu-full.json") || return 99 ;;
      esac
      CALLS="$CALLS plan:$CASE_NOW:$3"
      file="$ROOT/frontend/lib/testdata/plans/eu-$CASE_NOW.json"
      request=$(jq -c '{packageId,revision,actionIds:[.actions[] | select(.chosen) | .id]}' "$file")
      if ! jq -ne --argjson sent "$2" --argjson want "$request" '$sent == $want' >/dev/null; then
        return 99
      fi
      API_BODY=$(jq -c '{applied:true,warnings:[],plan:.,containd:{applyId:"plan-id",protection:"protecting"}}' "$file")
      HTTP_STATUS="$APPLY_STATUS"
      ;;
    /api/workshop/probe-matrix)
      [ "$2" = "" ] || return 99
      CALLS="$CALLS matrix:$CASE_NOW:$3"
      API_BODY=$(make_matrix "$ROOT/frontend/lib/testdata/plans/eu-$CASE_NOW.json")
      HTTP_STATUS="$MATRIX_STATUS"
      ;;
    /api/firewall/apply)
      CALLS="$CALLS named:$2"
      [ "$RESTORE_FAIL" -eq 0 ] || return 1
      API_BODY='{"warnings":[],"containd":{"applyId":"named-id","protection":"protecting"}}'
      # shellcheck disable=SC2034 # read by the real eu_apply_named
      HTTP_STATUS=200
      ;;
    *) return 99 ;;
  esac
  if [ "$CASE_NOW" = "$BREAK_AT" ] && [ "$1" != /api/firewall/apply ]; then
    API_BODY=$(printf '%s' "$API_BODY" | jq -c "$MUTATION") || return 1
  fi
}
eu_api_get() {
  [ "$1" = /api/firewall/active ] || return 99
  CALLS="$CALLS active"
  API_BODY='{"active_config":"weak","policy_source":"weak"}'
  if [ "$LEAK_PLAN" -eq 1 ]; then
    API_BODY='{"active_config":"weak","policy_source":"weak","plan":{}}'
  fi
}
run_plan_gate() {
  CALLS="" CASE_INDEX=0 CASE_NOW=""
  t_run eu_gate_G28
}

run_plan_gate
t_expect_eq "$T_STATUS" 0 "golden full and partial plans pass their independent judgements"
t_expect_fails 0 "partial hardened rejection is expected, not a failed plan proof"
expected_calls=' plan:full:60 matrix:full:90 plan:partial:60 matrix:partial:90'
for action in $(jq -r '.actions[] | select(.kind == "enforce" or .kind == "detect") | .id' \
  "$ROOT/frontend/lib/testdata/plans/eu-full.json"); do
  expected_calls="$expected_calls plan:$action:60 matrix:$action:90"
done
expected_calls="$expected_calls named:{\"config\":\"weak\"} active"
t_expect_eq "$CALLS" "$expected_calls" \
  "G28 runs full, partial and every catalog enforce/detect singleton before named weak"
t_run jq -e --slurpfile golden "$ROOT/frontend/lib/testdata/plans/eu-partial.json" \
  '. == ($golden[0] | {packageId,revision,actionIds:[.actions[] | select(.chosen) | .id]})' \
  "$TMP_DIR/G28-partial-request.json"
t_expect_eq "$T_STATUS" 0 "request evidence contains the golden-derived partial selection"
for selection in full partial $(jq -r '.actions[] | select(.kind == "enforce" or .kind == "detect") | .id' \
  "$ROOT/frontend/lib/testdata/plans/eu-full.json"); do
  for artifact in request apply matrix; do
    t_run test -s "$EU_GATE_EVIDENCE_DIR/G28/G28-$selection-$artifact.json"
    t_expect_eq "$T_STATUS" 0 "retained G28 $selection $artifact JSON is available"
  done
done

# RESET is complete recorded evidence with attribution, but never a passing
# deny (or allow), never skipped, and never an open prevention gap.
golden="$ROOT/frontend/lib/testdata/plans/eu-full.json"
for verdict in allow deny; do
  make_matrix "$golden" | jq --slurpfile golden "$golden" --arg verdict "$verdict" '
    ($golden[0].expectations | map(.hardenedVerdict) | index($verdict)) as $i
    | .rows[$i] |= (.observed="reset" | .ruleId="manual-reset"
        | .error="blocked by RESET (manual-reset): the timeout test cannot prove it; use DENY")
  ' >"$TMP_DIR/reset-matrix.json"
  t_run jq -e -L "$ROOT/scripts/tests/fixtures" --slurpfile golden "$golden" '
    include "plan-gate";
    plan_matrix_recorded($golden[0]) and (plan_matrix($golden[0]) | not)
    and (matrix_reference($golden[0]) |
      .acceptancePass == false and .summary.result == "FAIL" and .summary.skipped == 0
      and .preventionGaps == []
      and (.resetFailures | length) == 1 and .resetFailures[0].ruleId == "manual-reset"
      and ([.acceptanceRows[] | select(.observed == "reset")] |
        length == 1 and .[0].pass == false and .[0].ruleId == "manual-reset"))
  ' "$TMP_DIR/reset-matrix.json"
  t_expect_eq "$T_STATUS" 0 "RESET on required $verdict is recorded as a separate attributed failure"
done
# Even ordinary binary rows may now carry running first-match IDs.
make_matrix "$golden" | jq '.rows[].ruleId = "running-rule"' >"$TMP_DIR/attributed-matrix.json"
t_run jq -e -L "$ROOT/scripts/tests/fixtures" --slurpfile golden "$golden" \
  'include "plan-gate"; plan_matrix($golden[0])' "$TMP_DIR/attributed-matrix.json"
t_expect_eq "$T_STATUS" 0 "ordinary running rule IDs do not change binary golden proof"

BREAK_AT=full
MUTATION='if has("rows") then .rows[0] |= (.observed="reset" | .ruleId="manual-reset" | .error="blocked by RESET (manual-reset): the timeout test cannot prove it; use DENY") else . end'
run_plan_gate
t_expect_eq "$T_STATUS" 1 "G28 never passes a recorded RESET"
t_expect_out '"resetFailures"' "G28 reports RESET separately from prevention gaps"
t_expect_out '"ruleId": "manual-reset"' "G28 preserves the RESET first-match rule ID"
t_expect_out '"skipped": 0' "G28 does not label RESET untested"
MUTATION="."
BREAK_AT=partial

# Changing labels, revision, selected IDs, counts and gap IDs proves the gate
# reads the goldens, rather than merely mentioning their paths.
for selection in full partial; do
  file="$ROOT/frontend/lib/testdata/plans/eu-$selection.json"
  jq '
    .revision += 1 | .label += "-fixture" | .actions[].id += "-fixture"
    | .expectations[].id += "-fixture"
    | ([.expectations[] | select(.planVerdict != .hardenedVerdict)][0]
        // .expectations[0]) as $extra
    | .expectations += [$extra | .id += "-extra"]
  ' "$file" >"$TMP_DIR/changed.json" && cp "$TMP_DIR/changed.json" "$file"
done
for fixture in "$ROOT"/frontend/lib/testdata/plans/eu-*.json; do
  case "${fixture##*/}" in
    eu-empty.json|eu-full.json|eu-partial.json) continue ;;
  esac
  renamed="${fixture%.json}-fixture.json"
  jq '.actions[].id += "-fixture"' "$fixture" >"$TMP_DIR/changed.json" \
    && mv "$TMP_DIR/changed.json" "$renamed" \
    && rm "$fixture"
done
run_plan_gate
t_expect_eq "$T_STATUS" 0 "gate follows changed golden labels, requests, tuple counts and prevention gaps"
t_expect_fails 0 "changed compile authority needs no duplicated constants"

for BREAK_AT in full partial; do
  for MUTATION in \
    'if has("applied") then .applied=false else . end' \
    'if has("plan") then .warnings=["fixture warning"] else . end' \
    'if has("plan") then del(.warnings) else . end' \
    'if has("plan") then .plan.label="wrong" else . end' \
    'if has("plan") then .plan.policyHash += "-different" else . end' \
    'if has("rows") then .policySource="manual-custom" else . end' \
    'if has("rows") then .policyHash="" else . end' \
    'if has("rows") then .policyStable=false else . end' \
    'if has("rows") then .rows[0].observed="untested" else . end' \
    'if has("rows") then .rows[0].observed="reset" else . end' \
    'if has("rows") then .rows[0] |= (.observed="reset" | .ruleId="") else . end' \
    'if has("rows") then .rows[0] |= (.observed="reset" | .ruleId=42) else . end' \
    'if has("rows") then .rows=[] else . end' \
    'if has("rows") then .rows[0].error="exec failed" else . end' \
    'if has("rows") then .rows[0] |= (.observed="reset" | .ruleId="manual-reset" | .error="") else . end' \
    'if has("rows") then .rows[0] |= (.observed="reset" | .ruleId="manual-reset" | .error="blocked by RESET (different-rule): the timeout test cannot prove it; use DENY") else . end' \
    'if has("rows") then .rows |= reverse else . end' \
    'if has("rows") then .rows[0].observed="invalid" else . end' \
    'if has("rows") then .rows += [.rows[0]] else . end' \
    'if has("rows") then .rows[0].id="unknown" else . end' \
    'if has("rows") then .rows[0].id=.rows[1].id else . end' \
    'if has("rows") then .rows[0].observed |= (if . == "allow" then "deny" else "allow" end) else . end' \
    'if has("rows") then .policyHash = ((if .policyHash[0:1] == "0" then "1" else "0" end) + .policyHash[1:]) else . end' \
    'if has("rows") then .startedAt="" else . end' \
    'if has("rows") then .finishedAt="" else . end' \
    'if has("rows") then .policyChangedAt="" else . end'; do
    run_plan_gate
    t_expect_eq "$T_STATUS" 1 "$BREAK_AT fails closed: $MUTATION"
    t_expect_fails 1 "invalid apply/matrix records a gate failure"
    t_expect_eq "${CALLS##* named:}" '{"config":"weak"} active' "failed proof still clears provenance"
  done
done
MUTATION='if has("rows") then .rows |= .[1:] else . end'
run_plan_gate
t_expect_eq "$T_STATUS" 1 "missing partial prevention gap fails exact equality"
MUTATION='if has("plan") then .containd.protection="degraded" else . end'
run_plan_gate
t_expect_eq "$T_STATUS" 1 "degraded plan receipt fails despite a faithful plan"
t_expect_eq "${CALLS##* named:}" '{"config":"weak"} active' "degraded plan still restores named weak"
MUTATION='if has("plan") then del(.containd) else . end'
run_plan_gate
t_expect_eq "$T_STATUS" 1 "missing plan receipt cannot borrow a later apply identity"
MUTATION="."
for APPLY_STATUS in 422 500; do
  run_plan_gate
  t_expect_eq "$T_STATUS" 1 "non-200 apply is not a successful plan"
  t_expect_eq "${CALLS##* named:}" '{"config":"weak"} active' "apply failure still restores named weak"
done
APPLY_STATUS=200 MATRIX_STATUS=500
run_plan_gate
t_expect_eq "$T_STATUS" 1 "non-200 matrix fails"
MATRIX_STATUS=200 RESTORE_FAIL=1
run_plan_gate
t_expect_eq "$T_STATUS" 1 "failed cleanup cannot pass the gate"
RESTORE_FAIL=0 LEAK_PLAN=1
run_plan_gate
t_expect_eq "$T_STATUS" 1 "real eu_apply_named rejects lingering plan provenance"
t_expect_fails 1 "provenance clearing is asserted, not merely requested"
LEAK_PLAN=0
rm "$ROOT/frontend/lib/testdata/plans/eu-full.json"
unset API_BODY HTTP_STATUS
run_plan_gate
t_expect_eq "$T_STATUS" 1 "missing golden fails even without any earlier API response"
t_expect_eq "$CALLS" ' named:{"config":"weak"} active' "missing golden still restores weak through real named apply"
t_finish
