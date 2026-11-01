# shellcheck shell=bash
# Strict firewall conntrack tuple selection, deletion and closing-history drain.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

# Closing rows are history, both before a gate owns a connection and when
# selecting its non-closing diagnostic tuple. Unknown states remain eligible
# (fail closed). A conntrack entry is not proof of traffic delivery.
# shellcheck disable=SC2016
EU_SESSION_JQ_DEFS='
  def closing: .state as $state
    | ["TIME_WAIT", "CLOSE", "CLOSE_WAIT", "FIN_WAIT", "LAST_ACK"] | any(. == $state);
'

# eu_read_sessions DPORT SRC [allow-empty]: the filtered conntrack list. An
# empty list fails unless the caller is proving that no session exists.
eu_read_sessions() {
  local dport="$1" src="$2" minimum=1
  [ "${3:-}" != "allow-empty" ] || minimum=0
  if ! eu_api_get "/api/firewall/sessions?dport=$dport&src=$src"; then
    fail "firewall session list for src $src dport $dport is unavailable or unparsable"
    return 1
  fi
  SESSION_BODY="$API_BODY"
  if ! printf '%s' "$SESSION_BODY" | jq -e --arg src "$src" --argjson dport "$dport" \
    --argjson minimum "$minimum" '
    type == "object"
    and (.sessions | type == "array")
    and (.count | type == "number" and . >= 0 and floor == .)
    and .count == (.sessions | length)
    and .count >= $minimum
    and all(.sessions[];
            (.proto | type) == "string"
            and (.src | type) == "string"
            and (.dst | type) == "string"
            and (.sport | type == "number" and floor == . and . >= 1 and . <= 65535)
            and (.dport | type == "number" and floor == . and . == $dport)
            and has("state") and has("timeout") and .src == $src)
  ' >/dev/null 2>&1; then
    fail "firewall session envelope is empty or violates the filtered integer-port contract (src $src dport $dport): $SESSION_BODY"
    return 1
  fi
  return 0
}
eu_session_tuple() {
  local body="$1" src="$2" dst="$3" dport="$4" matches count
  SESSION_TUPLE=""
  matches=$(printf '%s' "$body" | jq -c --arg src "$src" --arg dst "$dst" \
    --argjson dport "$dport" "$EU_SESSION_JQ_DEFS"'
      [.sessions[] | select((.proto | ascii_downcase) == "tcp"
        and .src == $src and .dst == $dst and .dport == $dport
        and (closing | not))]
    ') || {
      printf 'session response cannot be parsed while selecting %s -> %s tcp/%s\n' \
        "$src" "$dst" "$dport" >&2
      return 1
    }
  count=$(printf '%s' "$matches" | jq 'length') || count=""
  if [ "$count" != "1" ]; then
    printf 'expected exactly one non-closing session tuple for %s -> %s tcp/%s, found %s\n' \
      "$src" "$dst" "$dport" "${count:-unparsable}" >&2
    return 1
  fi
  # shellcheck disable=SC2034
  SESSION_TUPLE="$matches"
  return 0
}
eu_kill_tuple() {
  local tuple="$1" src dst sport dport proto response
  proto=$(printf '%s' "$tuple" | jq -r '.[0].proto // empty') || return 1
  src=$(printf '%s' "$tuple" | jq -r '.[0].src // empty') || return 1
  dst=$(printf '%s' "$tuple" | jq -r '.[0].dst // empty') || return 1
  sport=$(printf '%s' "$tuple" | jq -r '.[0].sport // empty') || return 1
  dport=$(printf '%s' "$tuple" | jq -r '.[0].dport // empty') || return 1
  if [ -z "$proto" ] || [ -z "$src" ] || [ -z "$dst" ] \
    || ! [[ "$sport" =~ ^[0-9]+$ ]] || ! [[ "$dport" =~ ^[0-9]+$ ]]; then
    fail "session kill tuple is incomplete or has non-integer ports: $tuple"
    return 1
  fi
  if ! eu_api_post "/api/firewall/sessions/kill" \
    "{\"proto\":\"$proto\",\"src\":\"$src\",\"dst\":\"$dst\",\"sport\":$sport,\"dport\":$dport}"; then
    fail "session kill returned no parseable response for $src:$sport -> $dst:$dport"
    return 1
  fi
  response="$API_BODY"
  if [ "$HTTP_STATUS" != "200" ] \
    || ! printf '%s' "$response" | jq -e '.status == "deleted"' >/dev/null 2>&1; then
    fail "conntrack kill was not a confirmed 200/deleted for $src:$sport -> $dst:$dport: HTTP $HTTP_STATUS $response"
    return 1
  fi
  pass "exact conntrack tuple deleted: $src:$sport -> $dst:$dport"
  return 0
}

# eu_wait_session_history_drained SRC DST DPORT LABEL: before a monitor whose
# tuple must be the only match, wait for earlier gate clients' closing rows
# (the G4 one-shot GI leaves TIME_WAIT) to expire. A live row has no gate owner
# at this point and fails. Nothing is flushed or killed.
eu_wait_session_history_drained() {
  local src="$1" dst="$2" dport="$3" label="$4" rows live budget="" start_ms now_ms
  start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    eu_read_sessions "$dport" "$src" allow-empty || return 1
    rows=$(printf '%s' "$SESSION_BODY" | jq -c --arg src "$src" --arg dst "$dst" \
      --argjson dport "$dport" "$EU_SESSION_JQ_DEFS"'
      [.sessions[] | select((.proto | ascii_downcase) == "tcp"
        and .src == $src and .dst == $dst and .dport == $dport)]
      | map(. + {closing: closing})
    ') || {
      fail "$label cannot select $src -> $dst tcp/$dport rows: $SESSION_BODY"
      return 1
    }
    live=$(printf '%s' "$rows" | jq -c 'map(select(.closing | not))')
    if [ "$live" != "[]" ]; then
      fail "$label found unowned non-closing $src -> $dst tcp/$dport conntrack rows that no gate step owns: $live"
      return 1
    fi
    if [ "$rows" = "[]" ]; then
      pass "$label no earlier $src -> $dst tcp/$dport conntrack rows remain"
      return 0
    fi
    if [ -z "$budget" ]; then
      # Closing rows expire after their own timeout; Linux's longest default
      # closing state (TIME_WAIT, FIN_WAIT) is 120 s.
      budget=$(printf '%s' "$rows" | jq -r '
        if all(.[]; .timeout | type == "number") then map(.timeout) | max + 5 else "invalid" end')
      if ! [[ "$budget" =~ ^[0-9]+$ ]] || [ "$budget" -gt 125 ]; then
        fail "$label closing rows have missing or over-long timeouts: $rows"
        return 1
      fi
      printf '%s waiting up to %ss for closing conntrack rows to expire: %s\n' "$label" "$budget" "$rows"
    fi
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]] || [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      fail "$label closing conntrack rows did not expire within ${budget}s: $rows"
      return 1
    fi
    sleep 1
  done
}
