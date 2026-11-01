#!/usr/bin/env bash
# Offline contract: G20 and the student Run use one hard-bounded capture.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

STUDENT_COMMAND=$(python3 - "$PACKAGE/scenarios/eu-baseline-assessment.yml" <<'PY'
import sys
import yaml
with open(sys.argv[1]) as source:
    scenario = yaml.safe_load(source)
step = next(s for s in scenario['steps'] if s['id'] == 'capture-traffic-at-the-firewall')
print(next(line.strip() for line in step['description'].splitlines() if line.strip().startswith('tcpdump ')))
PY
) || { echo "cannot read the student capture command" >&2; exit 1; }
[ -n "$STUDENT_COMMAND" ] || { echo "empty student capture command" >&2; exit 1; }
eu_lab_wait_traffic_quiescent() { :; }
# shellcheck disable=SC2034 # response global read by eu_lab_wait_capture
eu_api_post() { HTTP_STATUS=200; }
CAPTURE_COMMAND=""
eu_lab_exec() {
  if [ "$1" = fw-1 ] && [[ "$2" == tcpdump* ]]; then
    CAPTURE_COMMAND="$2"
    CAPTURE_BUDGET="$4"
  fi
}
t_run eu_lab_wait_capture baseline
t_expect_eq "$T_STATUS" 0 "G20 executes the capture walk"
t_expect_eq "$CAPTURE_COMMAND" "$STUDENT_COMMAND" "G20 runs the exact student command"
t_expect_eq "$CAPTURE_BUDGET" 25 "capture stays inside G20 exec budget"

# Exercise the authored shell without Docker or waiting 12 seconds.
# shellcheck disable=SC2329 # invoked by bash eval below
tcpdump() {
  t_expect_eq "$*" "-i any -nn not host 127.0.0.1 -G 12 -W 1 -w /data/captures/baseline.pcap" \
    "capture requests one 12s file"
  return "$TOOL_EXIT"
}
for TOOL_EXIT in 0 124 127 137; do
  t_run eval "$STUDENT_COMMAND"
  if [ "$TOOL_EXIT" -eq 0 ]; then
    t_expect_eq "$T_STATUS" 0 "capture accepts completion/deadline status $TOOL_EXIT"
  else
    t_expect_eq "$T_STATUS" "$TOOL_EXIT" "capture preserves tool failure $TOOL_EXIT"
  fi
done

t_finish
