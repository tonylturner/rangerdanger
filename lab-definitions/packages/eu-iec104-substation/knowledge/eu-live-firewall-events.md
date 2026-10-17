The segmentation drawer's shared “Live DPI Events” panel is useful as a narrow view of logged firewall-rule evidence in the EU range. Its title and generic category counters do not mean this package inspects IEC 104 or Modbus commands: the EU reference policy disables DPI and enforces its network boundary at L4. DPI being off is an intentional design choice, not a gap in the evidence.

### Where the events come from

The panel polls `/api/substation/network-events` every 2.5 seconds. The backend asks containd for up to 50 recent events, then keeps events whose source or destination belongs to a teaching-zone subnet. This is a refreshed, bounded event view, not a continuous packet capture or a complete record of every packet.

The EU reference policies use rule logging selectively. When a matching rule has logging enabled, containd can report a firewall rule hit with its action and rule identity. For example, the improved policy logs explicit unauthorized IEC 104 and Modbus denies. Other allowed or denied traffic may have no corresponding row if its matching rule is not logged. See [the policy comparison](#eu-policy-comparison) for the exact boundaries and logging examples.

### Reading the display

For a firewall-rule event, the row shows a localized time, an `ALLOW` or `DENY` action chip, source and destination addresses, the destination port when present, the rule ID, and the protocol when present. It does not show a source port or the full packet. A hypothetical row for a logged improved-policy deny might read:

```text
12:34:56  DENY  10.10.10.50 → 10.30.30.20:2404  deny-unauthorized-iec104  [tcp]
```

When events are present, the badge summarizes counts for `rule`, `dpi`, and `ids` event kinds plus `deny`; the list itself has no category or verdict filter. It displays up to five rows in compact mode or ten in the full drawer. In the EU reference setup, expect logged firewall-rule evidence rather than DPI-decoded IEC 104 or Modbus decisions. The shared UI can represent other event kinds, but their labels and counters are generic UI capability, not evidence that EU command inspection is enabled.

The row's `DENY` reports a firewall action for that logged rule hit. It does not show whether an already established connection stopped carrying traffic, whether the application command completed, or what the equipment did. An allowed TCP/2404 connection is not proof of read-only IEC 104 access, and TCP/502 is not function-code filtering. Keep reachability separate from command authority, as described in [the L4 boundary article](#eu-port-source-boundary).

### Empty and unavailable states

If containd cannot be reached, the panel says `containd unreachable`. If the endpoint returns no recent events, the EU view shows a generic prompt to run a probe from a terminal. Neither state proves that no traffic occurred: the endpoint is bounded to recent events, only logged matches can appear, and the panel may not yet have refreshed after a probe. Check the selected policy and exact source, destination, service and test time; use a packet capture or other protocol evidence when the question requires more than a logged rule verdict.

For an auditable comparison, record the active policy and probe tuple, run a controlled positive or negative test, and preserve the row with the corresponding policy and service result. A single deny row does not establish that legitimate operation continued; a missing row does not establish that a deny failed. Account for [established sessions](#eu-established-sessions) and preserve the surrounding state and time bounds using the [evidence lifecycle](#eu-evidence-lifecycle).

Implementation: `frontend/components/segmentation-view.tsx`, `frontend/lib/live-queries.ts`, `backend/internal/server/substation.go`; EU policies `firewall/weak.json` and `firewall/improved.json`.
