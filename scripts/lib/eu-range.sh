# shellcheck shell=bash
# EU range API/device operations and operator/truth/solver convergence helpers.

# jq definitions shared by every operator-state predicate (eu_wait_for_state,
# eu_assert_jq*): baseline CB1 subset and full-station/F2 quality.
# Embed the package inventory count once so direct jq callers need no extra args.
EU_JQ_DEFS=$(jq -er '.points | arrays | select(length > 0)
  | "def station_point_count: \(length);"' "$PACKAGE/protocols/points.json") || {
  printf 'Cannot load station point count from %s/protocols/points.json\n' "$PACKAGE" >&2
  return 1
}
# shellcheck disable=SC2016
EU_JQ_DEFS+='
def breaker_points: [.points[] | select(.ioa == 1001 or .ioa == 1002 or .ioa == 2001 or .ioa == 2002)];
def four_points: ([breaker_points[].id] | sort)
  == ["feeder-breaker-position","feeder-current","feeder-protection-trip","feeder-voltage"];
def good: .value != null and .quality.iv == false and .quality.nt == false
  and .quality.sb == false and .quality.bl == false and (.quality.ov == null or .quality.ov == false)
  and .stale_reason == null;
def all_good: four_points and all(breaker_points[]; good);
def all_stale($reason): four_points
  and all(breaker_points[]; .quality.iv == true and .quality.nt == true and .stale_reason == $reason);
def station_good: (.points | length == station_point_count) and all(.points[]; good);
def f2_good: ([.points[] | select(.device == "f2-breaker")] | length == 4 and all(.[]; good));
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
  local path="$1" budget="${2:-2}"
  API_BODY=""
  HTTP_STATUS=$(curl -sS --max-time "$budget" -o "$TMP_DIR/api-get.json" \
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
  local path="$1" body="$2" budget="${3:-30}"
  API_BODY=""
  HTTP_STATUS=""
  HTTP_STATUS=$(curl -sS --max-time "$budget" -o "$TMP_DIR/api-response.json" \
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
      and .schema == 3 and .kind == "breaker" and .device_id == "feeder-breaker"
      and (.generation | type == "number" and . >= 0)
      and (.revision | type == "number" and . >= 0)
      and (.position | type == "string" and (. == "open" or . == "closed"))
      and (.remote_enabled | type == "boolean")
      and (.lockout | type == "boolean")
      and (.protection_tripped | type == "boolean")
      and (.event_seq | type == "number" and . >= 0)
      and (.changed_at | type == "string" and length > 0)
      and (.measurements | keys == ["current_a","p_mw","pf","q_mvar","received_at","section_live",
        "solve_seq","solved_at","solver_epoch","source_kv_ll","source_live","valid","voltage_kv_ll"])
      and (.measurements.current_a == null or (.measurements.current_a | type == "number"))
      and (.measurements.voltage_kv_ll == null or (.measurements.voltage_kv_ll | type == "number"))
      and (.measurements.source_kv_ll == null or (.measurements.source_kv_ll | type == "number"))
      and all(.measurements.section_live, .measurements.source_live;
        . == null or type == "boolean")
      and all(.measurements.p_mw, .measurements.q_mvar, .measurements.pf; . == null or type == "number")
      and (.protection | keys == ["delay_ms","function","instantaneous_a","last_trip_current_a","picked_up","picked_up_since","pickup_a"]
        and .function == "overcurrent" and (.pickup_a | type == "number")
        and (.delay_ms | type == "number") and (.picked_up | type == "boolean")
        and all(.instantaneous_a, .last_trip_current_a; . == null or type == "number")
        and (.picked_up_since == null or (.picked_up_since | type == "string")))
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
  local step="$1" label="$2" index scenario="${3:-}"
  if [ -z "$scenario" ]; then
    fail "$label requires an explicit served scenario"
    return 1
  fi
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
  if [ -z "${3:-}" ]; then
    fail "$2 requires an explicit served scenario"
    return 1
  fi
  eu_execute_step "$1" "$2" "$3" || return 1
  eu_breaker_readback "$2"
}

eu_breaker_readback() {
  local label="$1"
  if ! eu_get_field; then
    fail "$label field readback unavailable"
    return 1
  fi
  API_BODY=$(printf '%s' "$API_BODY" | jq -c '.devices["breaker-1"]') || return 1
  eu_assert_device_state_response "$label"
}

# Physical gates use the range action endpoint without a curriculum step,
# while preserving the same breaker truth readback as eu_device_step.
eu_action_step() {
  local node="$1" action="$2" label="$3" value="${4:-}"
  eu_action "$node" "$action" "$value" || return 1
  eu_breaker_readback "$label"
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
    type == "object" and .schema == 3
    and (.cache_epoch | type == "string" and length > 0)
    and (.snapshot_seq | type == "number" and . >= 0)
    and (.rendered_at | type == "string" and endswith("Z"))
    and (.link | type == "object" and has("state") and (has("local") | not))
    and (.link.gi | keys == ["last_complete_at","next_due_at","started_at","state","trigger"]
      and (.state == "none" or .state == "running" or .state == "complete")
      and (.trigger == null or .trigger == "connect" or .trigger == "periodic" or .trigger == "operator")
      and all(.started_at,.last_complete_at,.next_due_at; . == null or (type == "string" and endswith("Z"))))
    and (.points | length) == ($expected[0].points | length)
    and ([.points[] | {id,ioa,kind,device,limits}] | sort_by(.ioa))
      == ([$expected[0].points[] | {id,ioa,kind,device:.source.device,limits:(.limits // null)}] | sort_by(.ioa))
    and all(.points[]; (.cot | type == "number") and has("source_time") and has("received_at")
      and has("reporting") and has("device") and has("limits")
      and (.quality | keys == ["bl","iv","nt","ov","sb"] and all(.iv,.nt,.sb,.bl; type == "boolean"))
      and (if .kind == "float" or .kind == "step-position"
        then (.quality.ov | type == "boolean") else .quality.ov == null end))
    and all(.points[];
      if .kind == "float" then .asdu == "M_ME_NC_1" and .source_time == null
        and (.reporting | keys == ["cyclic_ms","deadband"]
          and (.cyclic_ms | type == "number" and . >= 1000)
          and (.deadband | type == "number" and . >= 0))
      else .reporting == null and
        (if .kind == "single-point" then .asdu == "M_SP_NA_1" or .asdu == "M_SP_TB_1"
         elif .kind == "double-point" then .asdu == "M_DP_NA_1" or .asdu == "M_DP_TB_1"
         else .asdu == "M_ST_NA_1" or .asdu == "M_ST_TB_1" end)
      end)
    and all(.points[] | select(.kind == "step-position"); .value == null or
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
  local body="$1" scope="${2:-breaker}"
  printf '%s' "$body" | jq -ce --arg scope "$scope" "$EU_JQ_DEFS"'
    if type == "object" and (.points | type == "array")
    and all(.points[]; has("id") and has("value") and has("received_at") and has("source_time"))
    and ([.points[] | select(.id == "feeder-current" or .id == "feeder-voltage")
          | .id] | sort == ["feeder-current", "feeder-voltage"])
    and all(.points[] | select(.id == "feeder-current" or .id == "feeder-voltage");
      .source_time == null)
    then [(if $scope == "station" then .points else breaker_points end)[]
      | {id, value, received_at, source_time}] | sort_by(.id)
    else error("float point source_time must be null and both float points must exist")
    end
  '
}

# This explicit gate request is not the CC periodic scheduler. A GI can report
# data received before ACT-TERM; completion, not receipt-age advancement, is the barrier.
eu_request_gi_since() {
  local boundary="$1" label="$2" budget="${3:-30}"
  eu_api_post /api/operator/gi '' "$budget" || return
  if [ "$HTTP_STATUS" != 202 ]; then
    fail "$label operator GI not accepted: HTTP $HTTP_STATUS $API_BODY"; return 1
  fi
  EU_GI_REQUESTED=$(printf '%s' "$API_BODY" | jq -er --arg boundary "$boundary" "$EU_JQ_DEFS"'
    select(.trigger == "operator" and (.requested_at | epoch) >= ($boundary | epoch)) | .requested_at') || {
    fail "$label operator GI request predates settling"; return 1;
  }
}

eu_interrogate_since() {
  local boundary="$1" budget="$2" label="$3" requested epoch connection
  eu_wait_for_operator '.link.state == "started" and .link.gi.state != "running"' "$budget" "$label idle GI" || return
  epoch=$(printf '%s' "$WAIT_BODY" | jq -er '.cache_epoch') || return
  connection=$(printf '%s' "$WAIT_BODY" | jq -er '.link.connection_seq') || return
  eu_request_gi_since "$boundary" "$label" || return
  requested="$EU_GI_REQUESTED"
  eu_wait_for_operator ".cache_epoch == \"$epoch\" and .link.connection_seq == $connection
    and .link.state == \"started\" and .link.gi.state == \"complete\" and .link.gi.trigger == \"operator\"
    and (.link.gi.started_at | epoch) >= (\"$requested\" | epoch)
    and (.link.gi.last_complete_at | epoch) >= (.link.gi.started_at | epoch)" "$budget" "$label operator GI"
}

# Wait for solver delivery, then allow a full acquisition cycle before requesting
# a snapshot. GI reads RTU cache; it does not synchronously acquire device truth.
eu_settle_for_gi() {
  # shellcheck disable=SC2016 # $field and $device are jq variables.
  eu_wait_for_field '.process.truth_stale == false and (.devices | length > 0)
    and all(.devices[]; .moving != true and .measurements.valid == true)
    and (. as $field | all(.devices[]; . as $device |
      $field.process.devices[$device.device_id].delivered == true
      and $field.process.devices[$device.device_id].solved_revision == $device.revision))' "$1" "$2 settled field" || return
  sleep 2
}

# eu_baseline_mismatches OPERATOR FIELD: one line per disagreement between
# the operator view, device truth and solver; no output means a GOOD baseline.
# GI (COT 20) reports are untimed (contract §3.2), so a status point's time tag
# must fit its COT; whether a timed tag is synchronised is G2's check.
eu_baseline_mismatches() {
  printf '%s' "$1" | jq -r --argjson truth "$2" "$EU_JQ_DEFS"'
    def time_fits_cot:
      if .cot == 20 then (.asdu == "M_SP_NA_1" or .asdu == "M_DP_NA_1" or .asdu == "M_ST_NA_1")
        and .source_time == null and .source_time_invalid == null
      else (.cot == 3 or .cot == 11)
        and (.asdu == "M_SP_TB_1" or .asdu == "M_DP_TB_1" or .asdu == "M_ST_TB_1")
        and (.source_time | type == "string" and length > 0)
        and (.source_time_invalid | type == "boolean")
      end;
    def pair($what; $operator; $device): "\($what): operator \($operator | tojson), truth \($device | tojson)";
    $truth.devices["breaker-1"] as $device | $truth.process as $solver
    | (if .link.state != "started" then "link state \(.link.state | tojson), expected started" else empty end),
      (if .link.gi.last_complete_at == null then "GI integrity missing" else empty end),
      (if four_points then empty else "operator point ids \([.points[].id] | sort | tojson)" end),
      (.points[] | select(good | not)
        | "\(.id) not GOOD: iv=\(.quality.iv) nt=\(.quality.nt) stale_reason=\(.stale_reason | tojson)"),
      (.points[] | select(.id == "feeder-breaker-position" or .id == "feeder-protection-trip")
        | select(time_fits_cot | not)
        | "\(.id) time tag does not fit COT \(.cot | tojson): source_time=\(.source_time | tojson) source_time_invalid=\(.source_time_invalid | tojson)"),
      (point("f2-position").value as $value
        | if $value == $truth.devices["f2-breaker"].position then empty
          else pair("F2 position"; $value; $truth.devices["f2-breaker"].position) end),
      (point("f2-protection-trip").value as $value
        | if $value == $truth.devices["f2-breaker"].protection_tripped then empty
          else pair("F2 trip"; $value; $truth.devices["f2-breaker"].protection_tripped) end),
      (["current", "p-mw"][] as $suffix
        | (if $suffix == "current" then "current_a" else "p_mw" end) as $key
        | point("f2-" + $suffix).value as $value
        | if near($value; $truth.devices["f2-breaker"].measurements[$key];
            (if $suffix == "current" then 2 else 0.05 end)) then empty
          else pair("F2 " + $suffix; $value; $truth.devices["f2-breaker"].measurements[$key]) end),
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
  local label="$1" budget="$2" start_ms now_ms mismatches boundary
  eu_settle_for_gi "$budget" "$label" || return
  boundary=$(eu_utc)
  eu_interrogate_since "$boundary" "$budget" "$label" || return
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
    breaker-1) ip=10.99.99.22 ;; f2-breaker) ip=10.99.99.28 ;; rmu-1) ip=10.99.99.24 ;;
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
# Profile ownership is restored both on normal completion and aborted G16 runs.
eu_set_profile() {
  local enabled="$1"
  # shellcheck disable=SC2034 # consumed by eu-cleanup.sh
  profile_disabled=1 state_touched=1
  eu_management process POST /api/process/actions "{\"action\":\"set_profile\",\"enabled\":$enabled}" || return
  if [ "$HTTP_STATUS" != 200 ]; then
    fail "process set_profile HTTP $HTTP_STATUS: $API_BODY"; return 1
  fi
  # shellcheck disable=SC2034 # ownership is consumed by eu-cleanup.sh
  if [ "$enabled" = true ]; then profile_disabled=0; fi
}
eu_reset_all() {
  local settled
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
  state_touched=1
  eu_api_post /api/workshop/reset '{}' || return
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e \
    '.actions | length >= 7 and all(.[]; .success == true)' >/dev/null; then
    fail "EU reset failed: HTTP $HTTP_STATUS $API_BODY"; return 1
  fi
  eu_settle_for_gi 30 "reset" || return
  settled=$(eu_utc)
  eu_interrogate_since "$settled" 40 "reset CC recovery" || return
  eu_wait_for_operator 'all(.points[]; good)' 10 "reset GOOD observations"
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
    if [ "$HTTP_STATUS" = 200 ] && printf '%s' "$API_BODY" | jq -e '.schema == 3 and .measurements.valid == true' >/dev/null; then return 0; fi
    now=$(eu_now_ms)
    if [ "$((now - start))" -ge 10000 ]; then fail "$node no valid pre-command solve: $API_BODY"; return 1; fi
    sleep 0.1
  done
}
