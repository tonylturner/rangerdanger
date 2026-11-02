#!/usr/bin/env bash
# The real walk, decoder, reducer and retention run against bounded API stubs.
# shellcheck disable=SC2329 # callbacks invoked by sourced helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
command -v tshark >/dev/null 2>&1 || exit 1
python3 "$T_DIR/test_g22_evidence.py" || exit 1
python3 "$T_DIR/make_g22_evidence.py" "$TMP_DIR/seed" || exit 1
printf '0\n' >"$TMP_DIR/clock-index"

eu_utc() {
  local index
  index=$(cat "$TMP_DIR/clock-index")
  index=$((index + 1))
  printf '%s\n' "$index" >"$TMP_DIR/clock-index"
  sed -n "${index}p" "$TMP_DIR/seed/clock-lines"
}
docker() { printf 'unexpected Docker call\n' >&2; return 99; }
EVENT_LOG=""
FIELD_COUNT=0
CC_COUNT=0
CONTROL_COUNT=0
eu_lab_step() {
  EVENT_LOG="$EVENT_LOG step:$2"
  if [ "$2" = apply-the-hardened-policy ]; then
    API_BODY=$(cat "$TMP_DIR/seed/G22-improved-apply.json")
  fi
}
eu_wait_for_field() {
  local name
  FIELD_COUNT=$((FIELD_COUNT + 1))
  case "$FIELD_COUNT" in
    1) name=baseline-field ;;
    2) name=consequence-field ;;
    3) name=restoration-field ;;
    *) return 1 ;;
  esac
  WAIT_BODY=$(cat "$TMP_DIR/seed/G22-$name.json")
  EVENT_LOG="$EVENT_LOG $name"
  printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $1" >/dev/null
}
eu_wait_for_operator() {
  local name
  CC_COUNT=$((CC_COUNT + 1))
  case "$CC_COUNT" in
    1) name=baseline-cc ;;
    2) name=consequence-cc ;;
    3) name=restoration-cc ;;
    *) return 1 ;;
  esac
  WAIT_BODY=$(cat "$TMP_DIR/seed/G22-$name.json")
  EVENT_LOG="$EVENT_LOG $name"
  printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $1" >/dev/null
}
eu_lab_reconnect_cc() {
  local name=improved-control-before
  CONTROL_COUNT=$((CONTROL_COUNT + 1))
  [ "$CONTROL_COUNT" -eq 1 ] || name=improved-control-after
  WAIT_BODY=$(cat "$TMP_DIR/seed/G22-$name.json")
  EVENT_LOG="$EVENT_LOG $name"
}
eu_get_field() { API_BODY=$(cat "$TMP_DIR/seed/G22-improved-held-field.json"); }
eu_api_post() {
  local name=weak-command
  [ "$CONTROL_COUNT" -eq 0 ] || name=improved-command
  # shellcheck disable=SC2034 # consumed by the sourced exec helpers
  HTTP_STATUS=200
  # shellcheck disable=SC2034 # consumed by the sourced exec helpers
  API_BODY=$(cat "$TMP_DIR/seed/G22-$name.json")
  EVENT_LOG="$EVENT_LOG $name:$2"
}
eu_window_start() { EVENT_LOG="$EVENT_LOG capture"; }
eu_window_finish() {
  EVENT_LOG="$EVENT_LOG decode"
  cp "$TMP_DIR/seed/G22-weak.pcap" "$TMP_DIR/G22-weak.pcap" || return
  cp "$TMP_DIR/seed/G22-weak-tcpdump.log" "$TMP_DIR/G22-weak-tcpdump.log" || return
  eu_check_capture_integrity "$TMP_DIR/G22-weak.pcap" "$TMP_DIR/G22-weak-tcpdump.log" G22-weak
}
# Copy outside TMP_DIR's subtree to avoid recursive self-copy.
RETAIN_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/g22-retained.XXXXXX") || exit 1
EU_GATE_EVIDENCE_DIR="$RETAIN_ROOT/evidence"
t_cleanup_hook() { rm -rf "$RETAIN_ROOT"; }
t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 0 "complete G22 walk validates and retains success"
t_run jq -e '.status == "PASS" and (.events | length == 19)' "$EU_GATE_EVIDENCE_DIR/G22-chain.json"
t_expect_eq "$T_STATUS" 0 "retained correlated record is complete"
t_run test -s "$EU_GATE_EVIDENCE_DIR/G22-weak.pcap"
t_expect_eq "$T_STATUS" 0 "raw PCAP retained on success"
case "$EVENT_LOG" in
  *"capture weak-command:"*" consequence-field consequence-cc decode step:restore-the-substation restoration-field restoration-cc step:apply-the-hardened-policy improved-control-before step:re-test-the-attack-under-the-hardened-policy improved-command:"*" improved-control-after step:validate")
    t_ok "capture/command/consequence/decode/recovery/Improved/identical-retest order" ;;
  *) t_not_ok "unexpected event order: $EVENT_LOG" ;;
esac
t_run jq -e -s '.[0].command == .[1].command
  and .[0].command == "iec104cmd 10.30.30.20 dc 3001 open -sbo --json"' \
  <(printf '%s\n' "$EVENT_LOG" | python3 -c '
import json,re,sys
for match in re.findall(r"(?:weak|improved)-command:(\{.*?\})",sys.stdin.read()):
    print(match)
')
t_expect_eq "$T_STATUS" 0 "both API exec requests carry the identical literal"

# Retention failures and early-stop records are observable failures, never PASS.
printf 'file\n' >"$RETAIN_ROOT/not-a-directory"
EU_GATE_EVIDENCE_DIR="$RETAIN_ROOT/not-a-directory/child"
t_run eu_save_evidence
t_expect_eq "$T_STATUS" 1 "evidence-copy failure propagates"
t_expect_out "could not keep gate evidence" "copy failure has diagnostics"
FIELD_COUNT=0 CC_COUNT=0 CONTROL_COUNT=0
printf '0\n' >"$TMP_DIR/clock-index"
t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "otherwise successful walk fails if evidence cannot be retained"
t_expect_out "could not keep gate evidence" "walk reports its retention failure"
EU_GATE_EVIDENCE_DIR="$RETAIN_ROOT/failed"
eu_window_start() { fail "injected capture permission failure"; return 1; }
FIELD_COUNT=0 CC_COUNT=0 CONTROL_COUNT=0
printf '0\n' >"$TMP_DIR/clock-index"
t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "missing capture permission fails the walk"
t_run jq -e '.status == "FAIL" and (.errors | length > 0)
  and (.raw["baseline-field"] != null)' "$EU_GATE_EVIDENCE_DIR/G22-chain.json"
t_expect_eq "$T_STATUS" 0 "failed walk retains partial diagnostics and FAIL record"
t_finish
