The EU range uses containd as a stateful, L4 boundary between network zones. Its reference policy decides whether traffic may pass based on the source and destination zones and addresses, transport protocol and destination port. It does not authorize individual IEC 104 or Modbus operations. Improved enables explicit monitor-only IEC 104 observations alongside unchanged L4 admission rules. The L4 source pin enforces; DPI detects and logs.

### From policy to packet verdict

containd validates the policy configuration and compiles it into a data-plane snapshot. The firewall compiler turns zone bindings, addresses, transport and port rules into nftables rules. For ordinary L4 rules in the forwarding chain, an `ALLOW` rule accepts a matching packet and a `DENY` rule drops it. Separate explicit-monitor ICS rules never admit or deny traffic, regardless of their configured action. The first matching enforcing terminal rule in listed order decides. Put required exceptions before overlapping broader denials; monitor rules never decide. Rule IDs do not set priority. `RATE_LIMIT` meters and `TAG` labels traffic without deciding admission. If no enforcing terminal rule matches, `firewall.defaultAction: DENY` drops the traffic.

The improved policy makes the boundary concrete. It allows the named Control Centre host `10.60.60.20` to open TCP/2404 sessions to the RTU at `10.30.30.20`. It also allows the RTU to reach the named breaker, RMUs and OLTC on TCP/502, while a separate rule permits the PLC to reach the OLTC on that port. The address and service limits reduce which hosts can connect; they do not authorize what a connected application asks the device to do. Separate IEC 104 monitor predicates observe selected CC requests without changing admission.

### A permitted service is not command authorization

IEC 104 telemetry, general interrogation and switching share TCP/2404. Allowing that port is not a read-only grant. Likewise, Modbus TCP/502 can carry reads and writes; an allow rule does not limit function codes or register ranges. The weak baseline's broader field access permits both reads and writes, while the improved policy narrows the initiating sources and destinations. Neither policy turns those L4 rules into command-level authorization. See [the source/port boundary](#eu-port-source-boundary) for the distinction between reachability and application authority.

The improved policy JSON sets `dpiEnabled` to `true`, keeps global `dpiMode: enforce` and NFQUEUE **101**, and appends three **explicit** `ics.mode: monitor` IEC 104 rules. Rule-local monitor overrides global enforce; omitting it is not equivalent. Never replace or overlay the operational CC L4 allow with a monitor rule: monitor ALLOW cannot admit it. The rules observe CA 1 breaker IOA 3001/type 46 and OLTC IOA 3301/type 47, not Modbus function codes. See [the full predicates and shadow evidence](#eu-ics-dpi).

Monitor traffic traverses NFQUEUE 101, adding engine latency, with no queue bypass. If the engine is down, the CC conduit drops. This is a forwarding dependency, not an availability-neutral change. Verify healthy engine/decoder, reconnect CC for a fresh SYN after apply, then prove GI/PLC/historian continuity independently.

RangerDanger's package lint permits only explicit monitor ICS rules in EU imports. Direct enforcing edits in the embedded containd UI are detected and reclassified, not prevented by that import gate; do not treat it as a universal editing guardrail.

### Stateful sessions and policy changes

A commit re-evaluates established connections on their next packet against the current first-match policy. Traffic the policy no longer admits stops. Sessions may still show the conntrack entry until it times out, so an entry is not proof that traffic passes. Identify the relevant five-tuple and policy-change time. Test fresh-handshake admission separately from delivery on existing connections. [Established-session evidence](#eu-established-sessions) describes the range's bounded procedure and optional exact-tuple diagnostics; do not delete unrelated sessions. The L4 source pin enforces; DPI detects and logs.

### Firewall events are a separate evidence path

For a rule with logging enabled, containd adds an nftables log statement before its L4 verdict. Its NFLOG consumer turns matching kernel log records into `firewall.rule.hit` events containing the rule ID, action and available packet tuple fields (addresses, ports and transport). The backend can retrieve containd events for the substation view. These are records of logged rule matches, not payload-decoding results.

The improved EU policy explicitly logs selected denied cross-zone attempts, including unauthorized IEC 104 and Modbus connections. Logging is not automatic for every packet: an unmatched packet dropped by the default policy does not itself prove that a corresponding rule-hit event will be present. Conversely, a quiet event list is not proof that no traffic was blocked. Preserve the active policy, source and destination, service, test interval and event evidence together; keep missing events distinct from a demonstrated allow or deny. See [the evidence lifecycle](#eu-evidence-lifecycle) for retention and provenance limits.

References: EU `firewall/weak.json` and `firewall/improved.json`; containd [v0.1.37 policy model](https://github.com/tonylturner/containd/blob/v0.1.37/docs/mkdocs/policy-model.md#established-connections-and-policy-changes) for rule order and established-connection re-evaluation; range event retrieval in `backend/internal/containd/events.go`.
