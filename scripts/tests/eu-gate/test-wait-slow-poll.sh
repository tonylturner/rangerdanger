#!/usr/bin/env bash
# shellcheck disable=SC2329 # curl is invoked by eu_api_get in a command substitution
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# The fake curl persists its call count because eu_api_get captures curl in a
# command substitution, then returns the selected transport or HTTP outcome.
CURL_COUNT_FILE="$TMP_DIR/curl-count"
printf '0\n' >"$CURL_COUNT_FILE"
CURL_MODE=""
curl() {
  local output_file="" attempt
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -o)
        output_file="$2"
        shift 2
        ;;
      *)
        shift
        ;;
    esac
  done
  attempt=$(cat "$CURL_COUNT_FILE")
  attempt=$((attempt + 1))
  printf '%s\n' "$attempt" >"$CURL_COUNT_FILE"

  case "$CURL_MODE" in
    timeout-then-success)
      if [ "$attempt" -eq 1 ]; then
        printf 'curl: (28) fake timeout\n' >&2
        return 28
      fi
      printf '{}\n' >"$output_file"
      printf '200'
      ;;
    timeouts)
      printf 'curl: (28) fake timeout\n' >&2
      return 28
      ;;
    non-200)
      printf '{"error":"unavailable"}\n' >"$output_file"
      printf '503'
      ;;
    invalid-json)
      printf 'not-json\n' >"$output_file"
      printf '200'
      ;;
    connection-refused)
      printf 'curl: (7) fake connection refused\n' >&2
      return 7
      ;;
    *)
      printf 'unexpected fake curl mode: %s\n' "$CURL_MODE" >&2
      return 99
      ;;
  esac
}

eu_now_ms() {
  python3 -c 'import time; print(int(time.monotonic() * 1000))'
}

CURL_MODE=timeout-then-success
t_run eu_wait_for_state /api/operator/state true 2 "slow poll recovers"
t_expect_eq "$T_STATUS" 0 "a timeout followed by a valid state passes"
t_expect_eq "$(cat "$CURL_COUNT_FILE")" 2 "the timed-out GET is retried once"
t_expect_out "GET /api/operator/state timed out; continuing wait" "the tolerated timeout is logged"
t_expect_fails 0 "a recovered wait records no gate failure"

printf '0\n' >"$CURL_COUNT_FILE"
CURL_MODE=timeouts
t_run eu_api_get /api/operator/state
t_expect_eq "$T_STATUS" 1 "a one-shot GET still fails on curl timeout"
t_expect_eq "$(cat "$CURL_COUNT_FILE")" 1 "a one-shot GET does not retry"
t_expect_eq "$EU_API_CURL_EXIT_CODE" 28 "the one-shot GET exposes curl's transport code to wait callers"
t_expect_out "GET /api/operator/state failed" "a one-shot GET keeps its transport error"

printf '0\n' >"$CURL_COUNT_FILE"
t_run eu_wait_for_state /api/operator/state true 1 "slow poll expires"
t_expect_eq "$T_STATUS" 1 "repeated transport timeouts fail at the wait budget"
t_expect_out "did not reach expected state within 1s" "timeout failure keeps the normal budget message"
t_expect_out "last transport error: curl: (28) fake timeout" "timeout failure includes its last transport error"
t_expect_out "GET /api/operator/state timed out; continuing wait" "each timeout before expiry is logged as tolerated"
attempts=$(cat "$CURL_COUNT_FILE")
retry_logs=$(printf '%s\n' "$T_OUT" | grep -c 'timed out; continuing wait')
t_expect_eq "$retry_logs" "$((attempts - 1))" "every retried timeout has a log line"

printf '0\n' >"$CURL_COUNT_FILE"
CURL_MODE=non-200
t_run eu_wait_for_state /api/operator/state true 5 "HTTP error remains immediate"
t_expect_eq "$T_STATUS" 1 "a non-200 response fails"
t_expect_eq "$(cat "$CURL_COUNT_FILE")" 1 "a non-200 response is not retried"
t_expect_out "returned HTTP 503" "the HTTP status failure remains visible"
t_expect_no_out "did not reach expected state" "a non-200 response does not become a wait timeout"

printf '0\n' >"$CURL_COUNT_FILE"
CURL_MODE=invalid-json
t_run eu_wait_for_state /api/operator/state true 5 "invalid JSON remains immediate"
t_expect_eq "$T_STATUS" 1 "an invalid JSON response fails"
t_expect_eq "$(cat "$CURL_COUNT_FILE")" 1 "invalid JSON is not retried"
t_expect_out "returned empty or unparsable JSON" "the JSON validation failure remains visible"
t_expect_no_out "did not reach expected state" "invalid JSON does not become a wait timeout"

printf '0\n' >"$CURL_COUNT_FILE"
CURL_MODE=connection-refused
t_run eu_wait_for_state /api/operator/state true 5 "connection refused remains immediate"
t_expect_eq "$T_STATUS" 1 "connection refusal fails"
t_expect_eq "$(cat "$CURL_COUNT_FILE")" 1 "connection refusal is not retried"
t_expect_out "returned no valid HTTP 200 JSON response" "connection refusal uses the immediate API failure"
t_expect_no_out "did not reach expected state" "connection refusal does not become a wait timeout"

t_finish
