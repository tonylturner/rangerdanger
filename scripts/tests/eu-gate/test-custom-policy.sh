#!/usr/bin/env bash
# Offline custom-policy derivation: isolate one destination, not its peers.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

IMPROVED="$PACKAGE/firewall/improved.json"
INPUT="$TMP_DIR/input.json"
OUTPUT="$TMP_DIR/custom.json"
# shellcheck disable=SC2034 # bounds read by eu_policy_has_no_ephemeral_allow
EPHEMERAL_LOW=32768 EPHEMERAL_HIGH=60999

# Cover pinned CIDRs, bare IPs and both spellings in the same rule. The full
# document comparison also pins first-match order, management and DPI rules.
for spelling in cidr bare both; do
  jq --arg spelling "$spelling" --arg src "$RTU_IP" --arg dst "$BREAKER_IP" '
    .firewall.rules[1] |= (
      .sources = [if $spelling == "bare" then $src else ($src + "/32") end]
      | .destinations = (
          (if $spelling == "cidr" then [($dst + "/32")]
           elif $spelling == "bare" then [$dst]
           else [$dst, ($dst + "/32")] end) + .destinations[1:])
    )
  ' "$IMPROVED" >"$INPUT" || exit 1
  t_run eu_make_custom_policy "$INPUT" "$RTU_IP" "$BREAKER_IP" 502 "$OUTPUT"
  t_expect_eq "$T_STATUS" 0 "$spelling: G11 custom policy derives successfully"
  t_run jq -e --slurpfile original "$INPUT" '
    . == ($original[0] | .firewall.rules[1].destinations = [
      "10.40.40.21/32", "10.40.40.22/32", "10.40.40.23/32", "10.40.40.24/32"
    ])
  ' "$OUTPUT"
  t_expect_eq "$T_STATUS" 0 "$spelling: only breaker is removed; four peers and every other field stay ordered"
  t_run eu_policy_has_no_ephemeral_allow "$OUTPUT"
  t_expect_eq "$T_STATUS" 0 "$spelling: G11 custom policy retains bounded grants and valid monitors"
done

# G7 and the bad-change lesson share this single-destination CC allow.
for spelling in cidr bare both; do
  jq --arg spelling "$spelling" --arg src "$CC_IP" --arg dst "$RTU_IP" '
    .firewall.rules[0] |= (
      .sources = [if $spelling == "bare" then $src else ($src + "/32") end]
      | .destinations = (
          if $spelling == "cidr" then [($dst + "/32")]
          elif $spelling == "bare" then [$dst]
          else [$dst, ($dst + "/32")] end)
    )
  ' "$IMPROVED" >"$INPUT" || exit 1
  t_run eu_make_custom_policy "$INPUT" "$CC_IP" "$RTU_IP" 2404 "$OUTPUT"
  t_expect_eq "$T_STATUS" 0 "$spelling: single-destination CC policy derives successfully"
  t_run jq -e --slurpfile original "$INPUT" '
    . == ($original[0] | del(.firewall.rules[0]))
  ' "$OUTPUT"
  t_expect_eq "$T_STATUS" 0 "$spelling: empty CC allow is dropped; all other rules and settings are unchanged"
done

# Zero and ambiguous matches must fail, not yield a policy for commit.
for matches in 0 2; do
  jq --argjson matches "$matches" '
    if $matches == 0 then del(.firewall.rules[1])
    else .firewall.rules += [.firewall.rules[1] | .id = "duplicate-rtu-allow"] end
  ' "$IMPROVED" >"$INPUT" || exit 1
  t_run eu_make_custom_policy "$INPUT" "$RTU_IP" "$BREAKER_IP" 502 "$OUTPUT"
  t_expect_eq "$T_STATUS" 1 "$matches matching pinned allows fail closed"
  t_expect_fails 1 "$matches matches record one gate failure"
  t_expect_out "expected exactly one pinned allow, found $matches" "$matches matches identify the cardinality error"
done

# Preserve the guard against removing the last rule in a policy.
jq '.firewall.rules = [.firewall.rules[0]]' "$IMPROVED" >"$INPUT" || exit 1
t_run eu_make_custom_policy "$INPUT" "$CC_IP" "$RTU_IP" 2404 "$OUTPUT"
t_expect_eq "$T_STATUS" 1 "removing the last rule fails closed"
t_expect_fails 1 "empty derived policy records one gate failure"
t_expect_out "derived custom policy is empty or invalid" "empty policy failure identifies the guard"

t_finish
