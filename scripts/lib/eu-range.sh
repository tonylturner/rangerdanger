# shellcheck shell=bash
# EU range API/device operations and operator/truth/solver convergence helpers.

# jq definitions shared by every operator-state predicate (eu_wait_for_state,
# eu_assert_jq*): the four monitored points and their per-point quality.
# shellcheck disable=SC2016
EU_JQ_DEFS='
def breaker_points: [.points[] | select(.ioa == 1001 or .ioa == 1002 or .ioa == 2001 or .ioa == 2002)];
def four_points: ([breaker_points[].id] | sort)
  == ["feeder-breaker-position","feeder-current","feeder-protection-trip","feeder-voltage"];
def good: .quality.iv == false and .quality.nt == false and (.stale_reason == null or .stale_reason == "");
def all_good: four_points and all(breaker_points[]; good);
def all_stale($reason): four_points
  and all(breaker_points[]; .quality.iv == true and .quality.nt == true and .stale_reason == $reason);
def point($id): ([.points[] | select(.id == $id)][0] // {});
def epoch: capture("^(?<base>.*:[0-9]{2})(?:\\.(?<frac>[0-9]+))?Z$")
  | ((.base + "Z" | fromdateiso8601) + ("0." + (.frac // "0") | tonumber));
def near($a; $b; $tolerance):
  ($a | type) == "number" and ($b | type) == "number" and (($a - $b) | fabs) <= $tolerance;
'

eu_now_ms() {
  python3 -c 'import time; print(int(time.monotonic() * 1000))'
}

# Every API response is appended to the run's response log, which is kept as
# evidence when the gate fails (eu_save_evidence).
eu_log_response() {
  printf '%s %s %s HTTP %s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" \
    "${HTTP_STATUS:-none}" "$API_BODY" >>"$TMP_DIR/api-responses.log"
}

eu_api_get() {
  local path="$1"
  API_BODY=""
  HTTP_STATUS=$(curl -sS --max-time 2 -o "$TMP_DIR/api-get.json" \
    -w '%{http_code}' "$API$path" 2>"$TMP_DIR/curl.err") || {
    printf 'GET %s failed: %s\n' "$path" "$(cat "$TMP_DIR/curl.err")" >&2
    return 1
  }
  API_BODY=$(cat "$TMP_DIR/api-get.json")
  eu_log_response GET "$path"
  if [ "$HTTP_STATUS" != "200" ]; then
    printf 'GET %s returned HTTP %s, expected 200: %s\n' "$path" "$HTTP_STATUS" "$API_BODY" >&2
    return 1
  fi
  printf '%s' "$API_BODY" | jq -e 'type == "object" or type == "array"' >/dev/null 2>&1 || {
    printf 'GET %s returned empty or unparsable JSON: %s\n' "$path" "$API_BODY" >&2
    return 1
  }
  return 0
}

eu_api_post() {
  local path="$1" body="$2"
  API_BODY=""
  HTTP_STATUS=""
  HTTP_STATUS=$(curl -sS --max-time 30 -o "$TMP_DIR/api-response.json" \
    -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "$body" "$API$path" 2>"$TMP_DIR/curl.err") || {
      printf 'POST %s failed: %s\n' "$path" "$(cat "$TMP_DIR/curl.err")" >&2
      return 1
    }
  API_BODY=$(cat "$TMP_DIR/api-response.json")
  eu_log_response POST "$path"
  printf '%s' "$API_BODY" | jq -e 'type == "object" or type == "array"' >/dev/null 2>&1 || {
    printf 'POST %s returned empty or unparsable JSON (HTTP %s): %s\n' \
      "$path" "$HTTP_STATUS" "$API_BODY" >&2
    return 1
  }
  return 0
}

eu_assert_jq() {
  local body="$1" expression="$2" label="$3"
  if printf '%s' "$body" | jq -e "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy: $expression)"
  fi
}
eu_assert_jq_truth() {
  local body="$1" truth="$2" expression="$3" label="$4"
  if printf '%s' "$body" | jq -e --argjson truth "$truth" "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy the truth comparison)"
  fi
}
eu_assert_device_state_response() {
  local label="$1"
  if [ "$HTTP_STATUS" = "200" ] \
    && printf '%s' "$API_BODY" | jq -e '
      type == "object"
      and .schema == 2 and .kind == "breaker" and .device_id == "feeder-breaker"
      and (.generation | type == "number" and . >= 0)
      and (.revision | type == "number" and . >= 0)
      and (.position | type == "string" and (. == "open" or . == "closed"))
      and (.remote_enabled | type == "boolean")
      and (.lockout | type == "boolean")
      and (.protection_tripped | type == "boolean")
      and (.event_seq | type == "number" and . >= 0)
      and (.changed_at | type == "string" and length > 0)
      and (.measurements | type == "object")
      and (has("measurements") and
        (.measurements | has("current_a") and has("voltage_kv_ll") and has("valid")
          and has("solver_epoch") and has("solve_seq") and has("solved_at") and has("received_at")
          and has("source_kv_ll") and has("section_live") and has("source_live") and has("fault_current")))
      and (.measurements.current_a == null or (.measurements.current_a | type == "number"))
      and (.measurements.voltage_kv_ll == null or (.measurements.voltage_kv_ll | type == "number"))
      and (.measurements.source_kv_ll == null or (.measurements.source_kv_ll | type == "number"))
      and all(.measurements.section_live, .measurements.source_live, .measurements.fault_current;
        . == null or type == "boolean")
      and (.measurements.valid | type == "boolean")
      and (.measurements.solver_epoch == null or (.measurements.solver_epoch | type == "string"))
      and (.measurements.solve_seq == null or (.measurements.solve_seq | type == "number"))
      and (.measurements.solved_at == null or (.measurements.solved_at | type == "string"))
      and (.measurements.received_at == null or (.measurements.received_at | type == "string"))
  ' >/dev/null 2>&1; then
    pass "$label returned HTTP 200 with the device /api/state shape"
    return 0
  else
    fail "$label was not HTTP 200 with the device /api/state shape: HTTP ${HTTP_STATUS:-missing} $API_BODY"
    return 1
  fi
}
# Resolve authored step IDs from the served curriculum; execution stays index-based.
# Never retry an execution: a second POST is a second physical operation.
eu_execute_step() {
  local step="$1" label="$2" index scenario="iec104-telecontrol-boundary"
  if ! eu_api_get "/api/scenarios/$scenario"; then
    fail "$label cannot read the active scenario"
    return 1
  fi
  index=$(printf '%s' "$API_BODY" | jq -er --arg step "$step" '
    .steps | fromjson | to_entries | map(select(.value.id == $step))
    | if length == 1 and .[0].value.action != null then .[0].key
      else error("step must name exactly one executable action") end') || {
    fail "$label cannot resolve executable step $step"
    return 1
  }
  if ! eu_api_post "/api/scenarios/$scenario/steps/$index/execute" '{}'; then
    fail "$label returned no parseable execution result"
    return 1
  fi
  if [ "$HTTP_STATUS" != "200" ] || ! printf '%s' "$API_BODY" | jq -e '
    .success == true and .action_type != "manual"
    and (.results | type == "array" and length > 0 and all(.[]; .success == true))
  ' >/dev/null 2>&1; then
    fail "$label step execution failed: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
  pass "$label executed through scenario step $step"
}

# Preserve the physical readback assertions after a successful student action.
eu_device_step() {
  eu_execute_step "$1" "$2" || return 1
  if ! eu_get_field; then
    fail "$2 field readback unavailable"
    return 1
  fi
  API_BODY=$(printf '%s' "$API_BODY" | jq -c '.devices["breaker-1"]') || return 1
  eu_assert_device_state_response "$2"
}

eu_wait_for_state() {
  local path="$1" expression="$2" budget="$3" label="$4" start_ms="${5:-}" now_ms
  [ -n "$start_ms" ] || start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    if ! eu_api_get "$path"; then
      fail "$label returned no valid HTTP 200 JSON response"
      return 1
    fi
    WAIT_BODY="$API_BODY"
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]]; then
      fail "$label cannot read monotonic clock"
      return 1
    fi
    if [ "$((now_ms - start_ms))" -le "$((budget * 1000))" ] \
      && printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.1
  done
  fail "$label did not reach expected state within ${budget}s; last response: $WAIT_BODY"
  return 1
}

eu_assert_operator_shape() {
  local body="$1" label="$2"
  if printf '%s' "$body" | jq -e --slurpfile expected "$PACKAGE/protocols/points.json" '
    type == "object" and .schema == 2
    and (.cache_epoch | type == "string" and length > 0)
    and (.snapshot_seq | type == "number" and . >= 0)
    and (.rendered_at | type == "string" and endswith("Z"))
    and (.link | type == "object" and has("state") and (has("local") | not))
    and (.points | length) == 31
    and ([.points[] | {id,ioa,asdu}] | sort_by(.ioa))
      == ([$expected[0].points[] | {id,ioa,asdu}] | sort_by(.ioa))
    and all(.points[]; (.cot | type == "number") and has("source_time") and has("received_at")
      and (.quality | all(.iv,.nt,.sb,.bl; type == "boolean"))
      and (if .asdu == "M_ME_NC_1" or .asdu == "M_ST_TB_1"
        then (.quality.ov | type == "boolean") else .quality.ov == null end))
    and all(.points[] | select(.asdu == "M_ME_NC_1"); .source_time == null)
    and all(.points[] | select(.asdu == "M_ST_TB_1"); .value == null or
      (.value | (.value | type == "number") and (.transient | type == "boolean")))
  ' >/dev/null 2>&1; then
    return 0
  fi
  fail "$label operator state has an invalid link/point shape, a local endpoint is present, or a float source_time is not null"
  return 1
}

eu_wait_for_operator() {
  if ! eu_wait_for_state "/api/operator/state" "$1" "$2" "$3" "${4:-}"; then
    return 1
  fi
  eu_assert_operator_shape "$WAIT_BODY" "$3"
}
eu_wait_for_field() {
  eu_wait_for_state "/api/field/state" "$1" "$2" "$3"
}
eu_get_field() {
  eu_api_get "/api/field/state"
}
eu_get_operator() {
  if ! eu_api_get "/api/operator/state"; then
    return 1
  fi
  eu_assert_operator_shape "$API_BODY" "GET /api/operator/state"
}
eu_get_point_snapshot() {
  local body="$1"
  printf '%s' "$body" | jq -ce "$EU_JQ_DEFS"'
    if type == "object" and (.points | type == "array")
    and all(.points[]; has("id") and has("value") and has("received_at") and has("source_time"))
    and ([.points[] | select(.id == "feeder-current" or .id == "feeder-voltage")
          | .id] | sort == ["feeder-current", "feeder-voltage"])
    and all(.points[] | select(.id == "feeder-current" or .id == "feeder-voltage");
      .source_time == null)
    then [breaker_points[] | {id, value, received_at, source_time}] | sort_by(.id)
    else error("float point source_time must be null and both float points must exist")
    end
  '
}

# eu_baseline_mismatches OPERATOR FIELD: one line per disagreement between
# the operator view, device truth and solver; no output means a GOOD baseline.
# GI (COT 20) reports are untimed (contract §3.2), so a status point's time tag
# must fit its COT; whether a timed tag is synchronised is G2's check.
eu_baseline_mismatches() {
  printf '%s' "$1" | jq -r --argjson truth "$2" "$EU_JQ_DEFS"'
    def time_fits_cot:
      if .cot == 20 then .source_time == null and .source_time_invalid == null
      else (.source_time | type == "string" and length > 0)
        and (.source_time_invalid | type == "boolean")
      end;
    def pair($what; $operator; $device): "\($what): operator \($operator | tojson), truth \($device | tojson)";
    $truth.devices["breaker-1"] as $device | $truth.process as $solver
    | (if .link.state != "started" then "link state \(.link.state | tojson), expected started" else empty end),
      (if .link.gi.state != "complete" then "GI state \(.link.gi.state | tojson), expected complete" else empty end),
      (if four_points then empty else "operator point ids \([.points[].id] | sort | tojson)" end),
      (.points[] | select(good | not)
        | "\(.id) not GOOD: iv=\(.quality.iv) nt=\(.quality.nt) stale_reason=\(.stale_reason | tojson)"),
      (.points[] | select(.id == "feeder-breaker-position" or .id == "feeder-protection-trip")
        | select(time_fits_cot | not)
        | "\(.id) time tag does not fit COT \(.cot | tojson): source_time=\(.source_time | tojson) source_time_invalid=\(.source_time_invalid | tojson)"),
      (point("feeder-breaker-position").value as $value
        | if $value == $device.position then empty else pair("position"; $value; $device.position) end),
      (point("feeder-protection-trip").value as $value
        | if $value == $device.protection_tripped then empty
          else pair("protection trip"; $value; $device.protection_tripped) end),
      (point("feeder-current").value as $value
        | if near($value; $device.measurements.current_a; 0.5) then empty
          else pair("current A (tolerance 0.5)"; $value; $device.measurements.current_a) end),
      (point("feeder-voltage").value as $value
        | if near($value; $device.measurements.voltage_kv_ll; 0.05) then empty
          else pair("voltage kV (tolerance 0.05)"; $value; $device.measurements.voltage_kv_ll) end),
      (if $solver.devices["feeder-breaker"].delivered == true
        and $solver.devices["feeder-breaker"].solved_revision == $device.revision then empty
        else "solver has not delivered breaker revision \($device.revision)" end),
      (if $solver.truth_stale == false then empty else "solver truth_stale" end)
  '
}

# eu_wait_for_baseline LABEL BUDGET: poll operator and field state until
# eu_baseline_mismatches is empty; on timeout print every mismatch and both
# last snapshots (BASELINE_OPERATOR, BASELINE_TRUTH).
eu_wait_for_baseline() {
  local label="$1" budget="$2" start_ms now_ms mismatches
  start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    BASELINE_OPERATOR="" BASELINE_TRUTH=""
    if ! eu_api_get "/api/operator/state" 2>"$TMP_DIR/baseline.err"; then
      mismatches="operator state unavailable: $(cat "$TMP_DIR/baseline.err")"
    else
      BASELINE_OPERATOR="$API_BODY"
      if ! eu_api_get "/api/field/state" 2>"$TMP_DIR/baseline.err"; then
        mismatches="field state unavailable: $(cat "$TMP_DIR/baseline.err")"
      else
        BASELINE_TRUTH="$API_BODY"
        mismatches=$(eu_baseline_mismatches "$BASELINE_OPERATOR" "$BASELINE_TRUTH" 2>&1) \
          || mismatches="cannot compare operator and field state: $mismatches"
      fi
    fi
    if [ -z "$mismatches" ]; then
      eu_assert_operator_shape "$BASELINE_OPERATOR" "$label"
      return
    fi
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]] || [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.25
  done
  fail "$label did not converge within ${budget}s"
  printf '%s\n' "$mismatches" | sed 's/^/  mismatch: /' >&2
  printf '  operator snapshot: %s\n  field snapshot: %s\n' "${BASELINE_OPERATOR:-none}" "${BASELINE_TRUTH:-none}" >&2
  return 1
}
eu_wait_command() {
  eu_command feeder-breaker-command "$1" "$2"
}

# Explicit management operations used only by this destructive acceptance gate.
# Nothing on the CC side is populated from these responses.
eu_management() {
  local node="$1" method="$2" path="$3" body="${4:-}" ip output
  case "$node" in
    breaker-1) ip=10.99.99.22 ;; rmu-1) ip=10.99.99.24 ;;
    rmu-2) ip=10.99.99.25 ;; oltc-1) ip=10.99.99.26 ;;
    process) ip=10.99.99.23 ;; *) fail "unknown management node $node"; return 1 ;;
  esac
  output=$(docker exec "$FW" curl -sS --max-time 5 -w '\n%{http_code}' \
    -X "$method" -H 'Content-Type: application/json' -d "$body" "http://$ip:8080$path") || {
    fail "$node $method $path transport failed"; return 1;
  }
  HTTP_STATUS="${output##*$'\n'}" API_BODY="${output%$'\n'*}"
  eu_log_response "$method" "$node$path"
  printf '%s' "$API_BODY" | jq -e 'type == "object"' >/dev/null || {
    fail "$node $path non-JSON HTTP $HTTP_STATUS: $API_BODY"; return 1;
  }
}
eu_action() {
  local node="$1" action="$2" value="${3:-}" path=/api/sim/actions
  [ "$node" != process ] || path=/api/process/actions
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
  state_touched=1
  eu_management "$node" POST "$path" "{\"action\":\"$action\"${value:+,\"value\":$value}}" || return
  if [ "$HTTP_STATUS" != 200 ]; then
    fail "$node $action HTTP $HTTP_STATUS: $API_BODY"; return 1
  fi
  pass "$node $action acknowledged"
}
eu_reset_all() {
  local started
  started=$(eu_utc)
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
  state_touched=1
  eu_api_post /api/workshop/reset '{}' || return
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e \
    '.actions | length >= 7 and all(.[]; .success == true)' >/dev/null; then
    fail "EU reset failed: HTTP $HTTP_STATUS $API_BODY"; return 1
  fi
  eu_wait_for_operator ".link.state == \"started\" and all(.points[];
    good and (.received_at | epoch) >= (\"$started\" | epoch))" 30 "reset fresh CC recovery"
}

# The CC result and the device audit must independently prove this attempt.
# A cached final value, or a prior accepted write, cannot satisfy this predicate.
eu_command() {
  local point="$1" operation="$2" expected="$3" reason="${4:-}" node audit_op cursor generation response started ended feedback_id feedback_ioa target
  case "$point" in
    feeder-breaker-command) node=breaker-1; audit_op="$operation" ;;
    rmu-1-command) node=rmu-1; audit_op="$operation" ;;
    rmu-2-command) node=rmu-2; audit_op="$operation" ;;
    oltc-1-step) node=oltc-1; audit_op=manual_step ;;
    oltc-1-mode) node=oltc-1; audit_op=mode ;;
    *) fail "unknown command $point"; return 1 ;;
  esac
  # Intentional invalid-measurement rejection must not wait the fault away.
  if [ "$reason" != no_measurement ]; then eu_wait_valid_device "$node" || return; fi
  eu_get_operator || return
  feedback_id=$(jq -er --arg point "$point" '.commands[] | select(.id == $point) | .feedback' "$PACKAGE/protocols/points.json") || return
  feedback_ioa=$(printf '%s' "$API_BODY" | jq -er --arg id "$feedback_id" '.points[] | select(.id == $id) | .ioa') || return
  target=$(printf '%s' "$API_BODY" | jq -c --arg id "$feedback_id" --arg op "$operation" '
    if $op == "raise" or $op == "lower" then
      (.points[] | select(.id == $id) | .value.value) + (if $op == "raise" then 1 else -1 end)
      | {value:.,transient:false}
    elif $op == "auto" then true elif $op == "manual" then false
    elif $op == "open" then "open" else "closed" end') || return
  eu_management "$node" GET '/api/audit?after=0' || return
  cursor=$(printf '%s' "$API_BODY" | jq -er '.latest_seq // 0') || return
  generation=$(printf '%s' "$API_BODY" | jq -er '.generation') || return
  started=$(eu_utc)
  eu_api_post /api/operator/commands "{\"point\":\"$point\",\"operation\":\"$operation\"}" || return
  # shellcheck disable=SC2034 # caller checks the original command response
  response="$API_BODY" COMMAND_BODY="$API_BODY"
  ended=$(eu_utc)
  if [ "$HTTP_STATUS" != 200 ] || ! eu_command_result "$response" "$point" "$operation" "$expected" "$started" "$ended" "$feedback_ioa" "$target"; then
    fail "$point $operation expected $expected in attempt window: HTTP $HTTP_STATUS $response"; return 1
  fi
  eu_management "$node" GET "/api/audit?after=$cursor" || return
  if ! printf '%s' "$API_BODY" | jq -e --argjson gen "$generation" --argjson cursor "$cursor" \
    --arg op "$audit_op" --arg expected "$expected" --arg reason "$reason" --arg start "$started" --arg end "$ended" "$EU_JQ_DEFS"'
    .generation == $gen and .oldest_seq <= $cursor + 1
    and ([.entries[] | select(.seq > $cursor and .operation == $op
      and (.at | epoch) >= ($start | epoch) and (.at | epoch) <= ($end | epoch)
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.20:")))] | length == 1
      and all(.[]; .result == (if $expected == "completed" then "accepted" else "rejected" end)
        and ($reason == "" or .detail == $reason)))' >/dev/null; then
    fail "$point $operation lacks one fresh RTU Modbus audit ($reason): $API_BODY"; return 1
  fi
  pass "$point $operation $expected: fresh IEC104 confirmations/feedback and RTU Modbus audit${reason:+ ($reason)}"
}

# Use parsed UTC timestamps: lexical comparisons are wrong at fractional-second
# boundaries ("00Z" sorts after "00.5Z").
eu_utc() { python3 -c 'import datetime; print(datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"))'; }
eu_wait_valid_device() {
  local node="$1" start now
  start=$(eu_now_ms)
  while :; do
    eu_management "$node" GET /api/state || return
    if [ "$HTTP_STATUS" = 200 ] && printf '%s' "$API_BODY" | jq -e '.schema == 2 and .measurements.valid == true' >/dev/null; then return 0; fi
    now=$(eu_now_ms)
    if [ "$((now - start))" -ge 10000 ]; then fail "$node no valid pre-command solve: $API_BODY"; return 1; fi
    sleep 0.1
  done
}
