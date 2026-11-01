#!/usr/bin/env bash
# Offline coverage of full-table ordered policy traversal and fail-closed parsing.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

EPHEMERAL_LOW=32768
# shellcheck disable=SC2034 # read by eu_nft_has_no_ephemeral_allow
EPHEMERAL_HIGH=60999
# Representative grants using the policy-chain layout documented for 0.1.35;
# the spec provides the chain header/state rule, not a full live policy dump.
POLICY_CHAIN='table inet containd_policy {
  chain forward {
    type filter hook forward priority filter; policy drop;
    ct state { established, related } accept
    ip saddr 10.10.10.50 ip daddr 10.30.30.20 tcp dport 2404 counter packets 0 bytes 0 accept
    ip saddr 10.30.30.20 ip daddr 10.50.50.20 tcp dport 502 accept
  }
}'
UNSAFE_CHAIN="${POLICY_CHAIN/    ct state/    tcp dport $EPHEMERAL_LOW accept
    ct state}"

t_run eu_nft_has_no_ephemeral_allow "$POLICY_CHAIN"
t_expect_eq "$T_STATUS" 0 "policy chain with stateful replies and bounded service grants passes"
t_run eu_nft_has_no_ephemeral_allow "$UNSAFE_CHAIN"
t_expect_eq "$T_STATUS" 1 "an ephemeral destination-port grant fails"
t_expect_out "nft accept includes ephemeral destination port(s)" "failure identifies the ephemeral grant"

NFT_CHAIN="$POLICY_CHAIN"
NFT_STATUS=0
docker() {
  printf '%s\n' "$*" >>"$TMP_DIR/nft-calls"
  if [ "$*" != "exec $FW nft list table inet containd_policy" ]; then
    printf 'unexpected Docker command: %s\n' "$*" >&2
    return 99
  fi
  if [ "$NFT_STATUS" -ne 0 ]; then
    printf 'policy chain unavailable\n' >&2
    return "$NFT_STATUS"
  fi
  printf '%s\n' "$NFT_CHAIN"
}

t_run eu_check_nft_chain "split-table"
t_expect_eq "$T_STATUS" 0 "reader requests the policy table, never the pre-filter table"
t_expect_fails 0 "valid policy chain produces no gate failures"
t_expect_out "PASS split-table effective nft forward chain has no ephemeral" "reader reports safe policy grants"

NFT_CHAIN="$UNSAFE_CHAIN"
t_run eu_check_nft_chain "ephemeral"
t_expect_eq "$T_STATUS" 1 "reader propagates the ephemeral grant rejection"
t_expect_fails 1 "unsafe policy chain records a gate failure"

NFT_STATUS=1
: >"$TMP_DIR/nft-calls"
t_run eu_check_nft_chain "missing"
t_expect_eq "$T_STATUS" 1 "missing policy chain fails closed"
t_expect_fails 1 "unavailable policy chain records a gate failure"
t_expect_out "policy chain unavailable" "reader preserves the nft error"
t_expect_eq "$(cat "$TMP_DIR/nft-calls")" \
  "exec $FW nft list table inet containd_policy" \
  "missing policy table triggers exactly one read without an old-table fallback"

# Exact candidate table captured under Improved. Unreachable input/prerouting
# grants must not be mistaken for forwarding grants.
ORDERED=$(cat "$(dirname "${BASH_SOURCE[0]}")/fixtures/policy-ordered.nft")
t_run eu_nft_has_no_ephemeral_allow "$ORDERED"
t_expect_eq "$T_STATUS" 0 "candidate ordered table keeps bounded original-direction services"

# A goto inherits its calling service bound, including conntrack tuple bounds.
for selector in 'tcp dport 2404' 'ct reply proto-src 2404'; do
  # Avoid bash 3.2's expensive global substitution on a full multiline table.
  BAD_ORDERED=$(printf '%s\n' "$ORDERED" | python3 -c \
    'import sys; old=sys.argv[1]; print(sys.stdin.read().replace(old, old.replace("2404", "40000")))' "$selector")
  t_run eu_nft_has_no_ephemeral_allow "$BAD_ORDERED"
  t_expect_eq "$T_STATUS" 1 "ephemeral service in $selector fails through admission subchains"
done

# Small tables isolate graph and grammar failures. A safe terminal branch must
# not hide a second unsafe route, and a jump return must resume its caller.
for rule in \
  'tcp dport 32768 accept' \
  'tcp dport { 502, 40000 } accept' \
  'tcp dport 30000-40000 accept' \
  'tcp dport != 502 accept' \
  'tcp dport @services accept' \
  'ct mark & 0x00000007 == 0x00000006 accept' \
  'accept' \
  'goto missing' \
  'goto forward' \
  'meta l4proto tcp vmap { 6 : accept }' \
  'tcp dport 502 masquerade'; do
  TABLE="table inet containd_policy {
  chain service {
    tcp dport 502 accept
    return
  }
  chain forward {
    type filter hook forward priority filter; policy drop;
    jump service
    $rule
  }
}"
  t_run eu_nft_has_no_ephemeral_allow "$TABLE"
  t_expect_eq "$T_STATUS" 1 "fail closed for reachable rule: $rule"
done

TABLE='table inet containd_policy {
  chain admit {
    accept
  }
  chain forward {
    type filter hook forward priority filter; policy drop;
    tcp dport { 502, 8080-8081 } jump admit
    ct state { established, related } goto admit
  }
}'
t_run eu_nft_has_no_ephemeral_allow "$TABLE"
t_expect_eq "$T_STATUS" 0 "bounded jumps and existing stateful reply exemption pass"
TABLE="${TABLE/policy drop/policy accept}"
t_run eu_nft_has_no_ephemeral_allow "$TABLE"
t_expect_eq "$T_STATUS" 1 "implicit forward acceptance fails closed"

t_finish
