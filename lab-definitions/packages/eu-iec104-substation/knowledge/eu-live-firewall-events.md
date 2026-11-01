The segmentation drawer's shared “Live Firewall Events” panel is useful as a narrow view of logged firewall-rule evidence in the EU range. Improved also emits explicit-monitor IEC 104 command observations, not command denials. The L4 source pin enforces; DPI detects and logs. Modbus DPI remains planning-only.

### Where the events come from

The panel polls `/api/substation/network-events` every 2.5 seconds. The backend requests events since the latest of five minutes ago, the current range generation's start and the last policy change. It follows filtered pages of up to 5,000 events, keeps events whose source or destination belongs to a teaching-zone subnet and returns at most 50 rows. This is a refreshed, time-bounded view, not a continuous packet capture or a complete record of every packet.

containd retains DENY rule events in a count-bounded bank with no age expiry.
They survive policy commits, but not process restarts; later DENYs can evict
older ones. That retention does not widen the strip's window: rows before its
`since` boundary are excluded. A retained row inside the window can still
predate your latest probe, so match its timestamp and tuple to your test.

The EU reference policies use rule logging selectively. When a matching rule has logging enabled, containd can report a firewall rule hit with its action and rule identity. For example, the improved policy logs explicit unauthorized IEC 104 and Modbus denies. Other allowed or denied traffic may have no corresponding row if its matching rule is not logged. See [the policy comparison](#eu-policy-comparison) for the exact boundaries and logging examples.

### Reading the display

For a firewall-rule event, the row shows a localized time, an `ALLOW` or `DENY` action chip, source and destination addresses, the destination port when present, the rule ID, and the protocol when present. It does not show a source port or the full packet. A hypothetical row for a logged improved-policy deny might read:

```text
12:34:56  DENY  10.10.10.50 → 10.30.30.20:2404  deny-unauthorized-iec104  [tcp]
```

When events are present, the badge summarizes counts for `rule`, `dpi`, and `ids` event kinds plus `deny`; the list itself has no category or verdict filter. It displays up to five rows in compact mode or ten in the full drawer. Keep logged L4 verdicts distinct from `ics.decision` monitor rows labeled **would allow** or **would deny (monitor)**. These rule-local hypothetical decisions do not count as denied commands or in the deny counter. Match `mode: monitor`, `ruleId`, `decision: would_allow` / `would_drop` and the UTC window; configured `action: DENY` alone does not establish a packet denial. Use the embedded containd **Firewall/ICS tables** for applied shadow counters; the RangerDanger event strip is not a counter API. See [counter lifetimes, missing evidence and forwarding dependency](#eu-ics-dpi).

For an L4 rule-hit row, `DENY` reports the firewall action, unlike a monitor would-deny observation. A commit re-evaluates established connections on their next packet against the current first-match policy; traffic no longer admitted stops. Sessions may retain the conntrack entry until timeout, not proof of delivery. This event row does not establish application failure-detection timing, command completion or what the equipment did. An allowed TCP/2404 connection is not proof of read-only IEC 104 access, and TCP/502 is not function-code filtering. Keep reachability separate from command authority, as described in [the L4 boundary article](#eu-port-source-boundary).

### Empty and unavailable states

The API returns `since`, `historyTruncated` and `engineError`, but the drawer
does not display that metadata. Incomplete history or an engine read error
can leave partial or empty results without an on-screen warning; preserve
the API response when assessing completeness.

If containd cannot be reached, the panel says `containd unreachable`. If the endpoint returns no recent events, the EU view shows a generic prompt to run a probe from a terminal. Neither state proves that no traffic occurred: the endpoint is bounded to recent events, only retained events can appear, and the panel may not yet have refreshed after a probe. Check the selected policy and exact source, destination, service and test time; use a packet capture or other protocol evidence when the question requires more than a logged rule verdict.

For an auditable comparison, record the active policy and probe tuple, run a controlled positive or negative test, and preserve the row with the corresponding policy and service result. A single deny row does not establish that legitimate operation continued; a missing row does not establish that a deny failed. Distinguish retained conntrack entries from delivery using [established-session evidence](#eu-established-sessions) and preserve the surrounding state and time bounds using the [evidence lifecycle](#eu-evidence-lifecycle).

Implementation: `frontend/components/segmentation-view.tsx`, `frontend/lib/live-queries.ts`, `backend/internal/server/substation.go`; EU policies `firewall/weak.json` and `firewall/improved.json`.
