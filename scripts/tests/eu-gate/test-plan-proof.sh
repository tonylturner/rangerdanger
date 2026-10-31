#!/usr/bin/env bash
# shellcheck disable=SC2329 # stubs are invoked by the sourced gate
# Full/partial plan proof uses compile goldens, not an embedded selection/matrix.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Change the golden root without modifying repository fixtures. The gate must
# follow the files here for both its request and every expected report value.
ORIGINAL_ROOT="$ROOT"
ROOT="$TMP_DIR/root"
mkdir -p "$ROOT/frontend/lib/testdata/plans" "$ROOT/scripts/tests/fixtures"
cp "$ORIGINAL_ROOT"/frontend/lib/testdata/plans/eu-{full,partial}.json "$ROOT/frontend/lib/testdata/plans/"
cp "$ORIGINAL_ROOT/scripts/tests/fixtures/plan-gate.jq" "$ROOT/scripts/tests/fixtures/"
CALLS=""
CASE_INDEX=0
CASE_NOW=""
MUTATION="."
BREAK_AT=partial
APPLY_STATUS=200
REPORT_STATUS=200
RESTORE_FAIL=0
LEAK_PLAN=0
sleep() { :; }
docker() { return 99; }
curl() { return 99; }

# Build independent HTTP fixtures directly from the golden, without invoking
# the oracle under test. A faithful partial report has successful proof rows,
# failed acceptance rows and only the remaining allow/deny prevention gaps.
make_report() {
  jq -c '
    .expectations as $rows
    | all($rows[]; .planVerdict == .hardenedVerdict) as $accepted
    | {
      planProof: {applicable:true, pass:true, hashStable:true, skipped:0,
        rows:[$rows[] | {id, expected:.planVerdict, observed:.planVerdict, pass:true}]},
      acceptance: {pass:$accepted, skipped:0,
        rows:[$rows[] | {id, expected:.hardenedVerdict, observed:.planVerdict,
          pass:(.planVerdict == .hardenedVerdict)}],
        preventionGaps:[$rows[] | select(.planVerdict == "allow"
          and .hardenedVerdict == "deny") | .id]},
      summary: {
        authorized_total:([$rows[] | select(.category == "authorized")] | length),
        authorized_pass:([$rows[] | select(.category == "authorized"
          and .planVerdict == .hardenedVerdict)] | length),
        unauthorized_total:([$rows[] | select(.category == "unauthorized")] | length),
        unauthorized_pass:([$rows[] | select(.category == "unauthorized"
          and .planVerdict == .hardenedVerdict)] | length),
        skipped:0, result:(if $accepted then "PASS" else "FAIL" end)}
    }' "$1"
}
eu_api_post() {
  local file request
  case "$1" in
    /api/firewall/apply-plan)
      CASE_INDEX=$((CASE_INDEX + 1))
      case "$CASE_INDEX" in
        1) CASE_NOW=full ;;
        2) CASE_NOW=partial ;;
        *) return 99 ;;
      esac
      CALLS="$CALLS plan:$CASE_NOW:$3"
      file="$ROOT/frontend/lib/testdata/plans/eu-$CASE_NOW.json"
      request=$(jq -c '{packageId,revision,actionIds:[.actions[] | select(.chosen) | .id]}' "$file")
      if ! jq -ne --argjson sent "$2" --argjson want "$request" '$sent == $want' >/dev/null; then
        return 99
      fi
      API_BODY=$(jq -c '{applied:true,warnings:[],plan:.}' "$file")
      HTTP_STATUS="$APPLY_STATUS"
      ;;
    /api/firewall/validation-report)
      CALLS="$CALLS report:$CASE_NOW:$3"
      API_BODY=$(make_report "$ROOT/frontend/lib/testdata/plans/eu-$CASE_NOW.json")
      HTTP_STATUS="$REPORT_STATUS"
      ;;
    /api/firewall/apply)
      CALLS="$CALLS named:$2"
      [ "$RESTORE_FAIL" -eq 0 ] || return 1
      API_BODY='{"warnings":[]}'
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
t_expect_eq "$CALLS" \
  ' plan:full:60 report:full:90 plan:partial:60 report:partial:90 named:{"config":"weak"} active' \
  "G28 runs full then partial with bounded calls, then named weak and provenance readback"
t_run jq -e --slurpfile golden "$ROOT/frontend/lib/testdata/plans/eu-partial.json" \
  '. == ($golden[0] | {packageId,revision,actionIds:[.actions[] | select(.chosen) | .id]})' \
  "$TMP_DIR/G28-partial-request.json"
t_expect_eq "$T_STATUS" 0 "request evidence contains the golden-derived partial selection"

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
run_plan_gate
t_expect_eq "$T_STATUS" 0 "gate follows changed golden labels, requests, tuple counts and prevention gaps"
t_expect_fails 0 "changed compile authority needs no duplicated constants"

for BREAK_AT in full partial; do
  for MUTATION in \
    'if has("applied") then .applied=false else . end' \
    'if has("plan") then .warnings=["fixture warning"] else . end' \
    'if has("plan") then del(.warnings) else . end' \
    'if has("plan") then .plan.label="wrong" else . end' \
    'if has("planProof") then .planProof.applicable=false else . end' \
    'if has("planProof") then .planProof.pass=false else . end' \
    'if has("planProof") then .planProof.hashStable=false else . end' \
    'if has("planProof") then .planProof.skipped=1 else . end' \
    'if has("planProof") then .planProof.rows=[] else . end' \
    'if has("acceptance") then .acceptance.pass |= not else . end' \
    'if has("acceptance") then .acceptance.skipped=1 else . end' \
    'if has("acceptance") then .acceptance.rows=[] else . end' \
    'if has("acceptance") then .acceptance.preventionGaps += ["unexpected"] else . end' \
    'if has("summary") then .summary.unauthorized_total=0 else . end'; do
    run_plan_gate
    t_expect_eq "$T_STATUS" 1 "$BREAK_AT fails closed: $MUTATION"
    t_expect_fails 1 "invalid apply/report records a gate failure"
    t_expect_eq "${CALLS##* named:}" '{"config":"weak"} active' "failed proof still clears provenance"
  done
done
MUTATION='if has("acceptance") then .acceptance.preventionGaps |= .[1:] else . end'
run_plan_gate
t_expect_eq "$T_STATUS" 1 "missing partial prevention gap fails exact equality"
MUTATION="."
for APPLY_STATUS in 422 500; do
  run_plan_gate
  t_expect_eq "$T_STATUS" 1 "non-200 apply is not a successful plan"
  t_expect_eq "${CALLS##* named:}" '{"config":"weak"} active' "apply failure still restores named weak"
done
APPLY_STATUS=200 REPORT_STATUS=500
run_plan_gate
t_expect_eq "$T_STATUS" 1 "non-200 report fails"
REPORT_STATUS=200 RESTORE_FAIL=1
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
