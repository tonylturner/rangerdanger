#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called by sourced helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

curl() {
  printf '%s\n' "$*" >>"$TMP_DIR/curl-trace"
  printf '{}' >"$TMP_DIR/api-response.json"
  printf '200'
}

EU_READY_PACKAGE=""
t_run eu_api_post /api/scenarios/shared/validate '{}'
t_expect_eq "$T_STATUS" 1 "scenario POST refuses an unverified package"
t_expect_eq "$(test -f "$TMP_DIR/curl-trace" && cat "$TMP_DIR/curl-trace")" "" "no HTTP work before readiness verification"

t_run eu_api_post /api/operator/gi '{}'
t_expect_eq "$T_STATUS" 0 "non-scenario POST still works without a verified package"
t_run grep -q 'X-RangerDanger-Package' "$TMP_DIR/curl-trace"
t_expect_eq "$T_STATUS" 1 "unrelated POST does not guess a package"
t_run grep -q 'Content-Type: application/json' "$TMP_DIR/curl-trace"
t_expect_eq "$T_STATUS" 0 "every POST keeps its JSON content type"
: >"$TMP_DIR/curl-trace"

# shellcheck disable=SC2034 # consumed by the sourced eu_api_post helper
EU_READY_PACKAGE="verified-consumer"
for path in /api/scenarios/shared/validate /api/scenarios/shared/steps/0/execute /api/workshop/test-suite; do
  t_run eu_api_post "$path" '{}'
  t_expect_eq "$T_STATUS" 0 "verified package POST succeeds"
  t_expect_eq "$(grep -c 'X-RangerDanger-Package: verified-consumer' "$TMP_DIR/curl-trace")" \
    "$(wc -l <"$TMP_DIR/curl-trace" | tr -d ' ')" "every scenario POST carries the verified consumer"
done

t_finish
