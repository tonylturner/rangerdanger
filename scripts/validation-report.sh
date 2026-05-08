#!/usr/bin/env bash
# Substation segmentation validation report generator.
#
# Runs the same authorized + unauthorized traffic test suite Lab 2.4
# walks the student through, captures PCAP at the firewall during
# the run, and emits a change-board-ready markdown report. The
# script is the lab's *deliverable form* — the artifact a real
# operator would attach to a change ticket after applying
# segmentation.
#
# Differs from scripts/firewall-smoke.sh in audience and shape:
#   - firewall-smoke is a CI/dev gate: terminal pass/fail output,
#     tests both weak and improved, fails the build on mismatches.
#   - validation-report is a deliverable: structured markdown with
#     timestamps, policy snapshot, PCAP evidence reference. Always
#     tests against the hardened policy (it's a "did the change
#     work?" report, not a "regression check").
#
# Usage:
#   ./scripts/validation-report.sh                   # report to stdout
#   ./scripts/validation-report.sh --out report.md   # write to file
#
# Output is markdown. To produce a PDF:
#   ./scripts/validation-report.sh | pandoc -o report.pdf

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
PROBE_TIMEOUT="${PROBE_TIMEOUT:-3}"
PCAP_DURATION_SECS="${PCAP_DURATION_SECS:-15}"
SETTLE_SECS="${SETTLE_SECS:-2}"

OUT_FILE=""
case "${1:-}" in
  --out|-o)
    OUT_FILE="${2:-}"
    [ -z "$OUT_FILE" ] && { echo "--out requires a path" >&2; exit 2; }
    shift 2 ;;
  -h|--help)
    sed -n '2,/^set/p' "$0" | sed 's/^# \?//;/^set/d' >&2
    exit 0 ;;
esac

# Run the report generation in a subshell whose stdout we capture,
# so the actual terminal stderr keeps showing live progress.
TS=$(python3 -c 'import time, datetime; print(datetime.datetime.utcnow().strftime("%Y%m%dT%H%M%SZ"))')
PCAP_PATH="/data/captures/validation-${TS}.pcap"
PCAP_HOST_PATH="data/firewall/captures/validation-${TS}.pcap"

ms_now() { python3 -c 'import time; print(int(time.time()*1000))'; }
err() { echo "validation-report: $*" >&2; }

# ── Probe primitive (same shape as firewall-smoke.sh) ────────────────
probe_tcp() {
  local src="$1" dst="$2" port="$3"
  local rc start end dur
  start=$(ms_now)
  if docker exec "$src" sh -c 'command -v bash >/dev/null 2>&1' 2>/dev/null; then
    docker exec "$src" timeout "$PROBE_TIMEOUT" bash -c "exec 3<>/dev/tcp/$dst/$port" >/dev/null 2>&1
    rc=$?
  else
    docker exec "$src" sh -c "timeout $PROBE_TIMEOUT nc -w $PROBE_TIMEOUT $dst $port < /dev/null > /dev/null 2>&1"
    rc=$?
  fi
  end=$(ms_now)
  dur=$(( end - start ))
  case "$rc" in
    0)        printf 'allow %d\n' "$dur" ;;
    124|143)  printf 'deny %d\n'  "$dur" ;;
    1)
      if [ "$dur" -lt 500 ]; then
        printf 'allow %d\n' "$dur"
      else
        printf 'deny %d\n' "$dur"
      fi
      ;;
    *)        printf 'error:rc=%d %d\n' "$rc" "$dur" ;;
  esac
}

# ── Test matrix — narrower than firewall-smoke, scoped to what 2.4
#    asks the student to verify. Each row: src|dst|port|expected|note|category
# Test matrix is intentionally narrower than firewall-smoke. Probes
# only target ports where the destination has a listener — without a
# listener, an "allow" decision lands on a fast TCP RST that Docker
# Desktop's bridge sometimes drops (mac-only quirk, big enough to
# make the report unreliable). RTAC now hosts sshd on :22 + nginx
# on :443 (services/Dockerfile rtac-sim stage) so the vendor →
# RTAC management rows in the improved policy are probeable.
read -r -d '' MATRIX <<EOF || true
rangerdanger-rtac-sim|10.40.40.20|502|allow|RTAC Modbus poll to relay|authorized
rangerdanger-rtac-sim|10.40.40.21|20000|allow|RTAC DNP3 poll to recloser|authorized
rangerdanger-rtac-sim|10.40.40.22|20000|allow|RTAC DNP3 poll to regulator|authorized
rangerdanger-rtac-sim|10.40.40.30|8080|allow|RTAC HTTP API to OpenPLC|authorized
rangerdanger-fuxa-hmi|10.30.30.20|8080|allow|HMI to RTAC HTTP intra-zone|authorized
rangerdanger-historian-sim|10.30.30.20|8080|allow|Historian to RTAC intra-zone|authorized
rangerdanger-vendor-jump|10.30.30.20|22|allow|Vendor SSH mgmt to RTAC|authorized
rangerdanger-vendor-jump|10.30.30.20|443|allow|Vendor HTTPS mgmt to RTAC|authorized
rangerdanger-kali|10.40.40.20|502|deny|Enterprise Modbus to field relay|unauthorized
rangerdanger-kali|10.40.40.20|20000|deny|Enterprise DNP3 to field relay|unauthorized
rangerdanger-kali|10.40.40.30|8080|deny|Enterprise HTTP to OpenPLC|unauthorized
rangerdanger-kali|10.30.30.20|8080|deny|Enterprise HTTP to RTAC|unauthorized
rangerdanger-kali|10.30.30.20|502|deny|Enterprise Modbus to RTAC|unauthorized
rangerdanger-eng-ws|10.40.40.21|502|deny|Vendor Modbus to field recloser|unauthorized
rangerdanger-eng-ws|10.40.40.21|20000|deny|Vendor DNP3 to field recloser|unauthorized
rangerdanger-eng-ws|10.40.40.30|8080|deny|Vendor HTTP to OpenPLC (only 443/22 allowed)|unauthorized
rangerdanger-vendor-jump|10.30.30.20|502|deny|Vendor Modbus to RTAC (improved blocks non-mgmt)|unauthorized
rangerdanger-historian-sim|10.40.40.22|502|deny|Non-RTAC OT (historian) to field regulator (Modbus)|unauthorized
rangerdanger-historian-sim|10.40.40.22|20000|deny|Non-RTAC OT (historian) to field regulator (DNP3)|unauthorized
EOF
# Note: historian-sim is the canonical "non-RTAC OT" probe source
# because it's lan1-only — it has no direct field interface, so
# every probe to 10.40.40.x must traverse the firewall. OpenPLC
# was used in earlier drafts but is multi-homed (lan1 + field) and
# bypasses the firewall for traffic from its field-side interface,
# making the probe result indeterminate. The OpenPLC multi-homed
# bypass is itself a finding — see Lab 2.4 step 5 "Other
# observations" — but it's not a clean test of firewall enforcement.

# ── Apply hardened policy ────────────────────────────────────────────
err "applying hardened policy via $API/api/firewall/apply"
apply_resp=$(curl -fsS -X POST -H 'Content-Type: application/json' \
                  -d '{"config":"improved"}' "$API/api/firewall/apply" 2>&1) \
    || { err "apply failed: $apply_resp"; exit 1; }
sleep "$SETTLE_SECS"

# Compute policy fingerprint — short hash of the active policy file.
POLICY_HASH=$(shasum -a 256 lab-definitions/firewall/substation-improved.json 2>/dev/null \
              | awk '{print substr($1,1,12)}')
[ -z "$POLICY_HASH" ] && POLICY_HASH="unknown"

# ── Start PCAP capture in background on firewall lan2 interface ──────
err "starting PCAP capture (${PCAP_DURATION_SECS}s, $PCAP_PATH)"
docker exec rangerdanger-firewall mkdir -p /data/captures 2>/dev/null
docker exec -d rangerdanger-firewall sh -c \
    "timeout $PCAP_DURATION_SECS tcpdump -i any -w $PCAP_PATH \
      'host 10.40.40.20 or host 10.40.40.21 or host 10.40.40.22 or host 10.40.40.23 or host 10.40.40.30' 2>/dev/null"
sleep 1   # let tcpdump open the file

# ── Run probes ───────────────────────────────────────────────────────
declare -a results
declare -i auth_pass=0 auth_total=0 unauth_pass=0 unauth_total=0
err "running probe matrix (~$(echo "$MATRIX" | wc -l | tr -d ' ') rows)"

while IFS='|' read -r src dst port expect note category; do
  [ -z "$src" ] && continue
  if ! docker inspect "$src" >/dev/null 2>&1; then
    results+=("$category|$src|$dst|$port|$expect|skipped|0|$note (container not running)")
    continue
  fi
  out=$(probe_tcp "$src" "$dst" "$port")
  actual=$(echo "$out" | awk '{print $1}')
  dur=$(echo "$out" | awk '{print $2}')
  if [ "$actual" = "$expect" ]; then
    verdict="PASS"
  else
    verdict="FAIL"
  fi
  results+=("$category|$src|$dst|$port|$expect|$actual|$dur|$note")
  case "$category" in
    authorized)
      auth_total=$((auth_total + 1))
      [ "$verdict" = "PASS" ] && auth_pass=$((auth_pass + 1))
      ;;
    unauthorized)
      unauth_total=$((unauth_total + 1))
      [ "$verdict" = "PASS" ] && unauth_pass=$((unauth_pass + 1))
      ;;
  esac
done <<< "$MATRIX"

# Wait for PCAP capture window to finish.
err "waiting for PCAP window to close"
remain=$(( PCAP_DURATION_SECS - 2 ))
[ "$remain" -gt 0 ] && sleep "$remain"

# ── PCAP source-IP analysis (which sources actually touched field) ───
# tcpdump -i any prepends "In  " / "Out " direction markers so the
# IP fields are at $4/$6, not $3/$5. We strip them in awk to get just
# the source IP (no port), then aggregate.
err "analysing PCAP for source-IP summary"
PCAP_SUMMARY=$(docker exec rangerdanger-firewall sh -c "
    tcpdump -r $PCAP_PATH -nn 2>/dev/null \
      | awk '
        /^[0-9]/ {
          # tcpdump -i any line shape:
          #   <ts> <iface> <In/Out> IP <src.port> > <dst.port>: ...
          # Only count packets the firewall actually FORWARDED toward
          # the field zone (direction \"Out\" in tcpdump -i any). \"In\"
          # packets include dropped probe attempts captured at the
          # ingress hook before the deny verdict, which would falsely
          # show non-RTAC sources \"reaching\" field.
          if (\$3 != \"Out\") next
          ip = \$5
          n = split(ip, a, \".\")
          if (n < 4) next
          # also skip field-to-field replies (sources in 10.40.40/24);
          # we want only sources from other zones.
          if (a[1] == \"10\" && a[2] == \"40\" && a[3] == \"40\") next
          print a[1] \".\" a[2] \".\" a[3] \".\" a[4]
        }' \
      | sort | uniq -c | sort -rn | head -10
    " 2>/dev/null)
[ -z "$PCAP_SUMMARY" ] && PCAP_SUMMARY="(no packets captured during window)"

# ── Render the markdown report ───────────────────────────────────────
render_report() {
  local now_iso
  now_iso=$(python3 -c 'import datetime; print(datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"))')

  cat <<MD
# Substation Segmentation — Validation Report

| Field | Value |
|---|---|
| **Generated** | $now_iso |
| **Policy** | hardened (\`substation-improved.json\` sha256:$POLICY_HASH) |
| **PCAP evidence** | \`$PCAP_HOST_PATH\` |
| **Probe timeout** | ${PROBE_TIMEOUT}s |

## Summary

| Category | Confirmed | Total |
|---|---|---|
| **Authorized flows working** | $auth_pass | $auth_total |
| **Unauthorized flows blocked** | $unauth_pass | $unauth_total |

MD
  if [ "$auth_pass" = "$auth_total" ] && [ "$unauth_pass" = "$unauth_total" ]; then
    cat <<MD
**Result: PASS** — every authorized flow works, every unauthorized flow is blocked. The hardened policy is enforcing what the design specified.

MD
  else
    cat <<MD
**Result: REVIEW REQUIRED** — at least one row didn't match expectations. See the per-test detail below.

MD
  fi

  cat <<MD
## Authorized flow tests

These flows the substation depends on; each must succeed under the hardened policy.

| # | Source | Destination | Port | Expected | Actual | Duration | Test |
|---|---|---|---|---|---|---|---|
MD
  local n=0
  for row in "${results[@]}"; do
    IFS='|' read -r category src dst port expect actual dur note <<< "$row"
    if [ "$category" = "authorized" ]; then
      n=$((n + 1))
      icon="✓"
      [ "$actual" != "$expect" ] && icon="✗"
      printf "| %d | %s | \`%s\` | %s | %s | %s | %sms | %s |\n" \
        "$n" "$icon ${src#rangerdanger-}" "$dst" "$port" "$expect" "$actual" "$dur" "$note"
    fi
  done

  cat <<MD

## Unauthorized flow tests

These flows the policy must deny. Each blocked attempt is positive evidence the segmentation is enforcing.

| # | Source | Destination | Port | Expected | Actual | Duration | Test |
|---|---|---|---|---|---|---|---|
MD
  n=0
  for row in "${results[@]}"; do
    IFS='|' read -r category src dst port expect actual dur note <<< "$row"
    if [ "$category" = "unauthorized" ]; then
      n=$((n + 1))
      icon="✓"
      [ "$actual" != "$expect" ] && icon="✗"
      printf "| %d | %s | \`%s\` | %s | %s | %s | %sms | %s |\n" \
        "$n" "$icon ${src#rangerdanger-}" "$dst" "$port" "$expect" "$actual" "$dur" "$note"
    fi
  done

  cat <<MD

## PCAP source analysis

During the ${PCAP_DURATION_SECS}-second capture window, traffic to the field zone (\`10.40.40.0/24\`) came from:

\`\`\`
$PCAP_SUMMARY
\`\`\`

For a hardened policy, only the RTAC (\`10.30.30.20\`) and the GPS time server (\`10.30.30.50\`, NTP) should appear. Any other source IP indicates a missing rule or a leaky deny.

## Methodology notes

- **Allow** verdict means the source container's TCP SYN reached the destination — either connection succeeded or destination sent RST. Both prove the firewall didn't drop the packet.
- **Deny** verdict means the SYN was dropped (timeout at the probe budget). The firewall log on the source rule is the corresponding positive evidence.
- Probes ran from inside the listed containers via \`docker exec bash -c "exec 3<>/dev/tcp/<dst>/<port>"\` (or \`nc -w\` on busybox-only containers). Same probe primitive used by the lab smoke test (\`scripts/firewall-smoke.sh\`).
- PCAP was captured on the firewall's interface set during the probe run. The file referenced above is host-readable at \`$PCAP_HOST_PATH\` (RangerDanger mounts \`./data/firewall\` to the firewall container's \`/data\`).

## Reviewer checklist

Before approving the change to production, confirm:

- [ ] Every authorized-flow row reads PASS.
- [ ] Every unauthorized-flow row reads PASS.
- [ ] The PCAP source analysis shows only expected sources reaching the field zone.
- [ ] The policy fingerprint above matches the change ticket's planned policy.
- [ ] Per-rule logs in containd show deny events for the unauthorized-flow attempts (cross-reference \`show audit\` on the firewall).
MD
}

if [ -n "$OUT_FILE" ]; then
  render_report > "$OUT_FILE"
  err "report written to $OUT_FILE"
  err "PCAP at $PCAP_HOST_PATH"
else
  render_report
fi

# Exit non-zero if any row failed — useful for unattended runs.
if [ "$auth_pass" != "$auth_total" ] || [ "$unauth_pass" != "$unauth_total" ]; then
  exit 1
fi
exit 0
