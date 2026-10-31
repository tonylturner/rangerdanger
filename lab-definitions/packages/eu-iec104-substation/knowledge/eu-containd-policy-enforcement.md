The EU range uses containd as a stateful, L4 boundary between network zones. Its reference policy decides whether traffic may pass based on the source and destination zones and addresses, transport protocol and destination port. It does not authorize individual IEC 104 or Modbus operations. The EU policy deliberately has DPI disabled; L4 firewall events remain valid enforcement evidence.

### From policy to packet verdict

containd validates the policy configuration and compiles it into a data-plane snapshot. The firewall compiler turns zone bindings, addresses, transport and port rules into nftables rules. In the forwarding chain, an `ALLOW` rule accepts a matching packet and a `DENY` rule drops it. The EU policies use a default `DENY`, so traffic that matches no allow rule is dropped. The compiler emits `ALLOW` entries before `DENY` entries, so the improved policy's named allows precede its broader denials.

The improved policy makes the boundary concrete. It allows the named Control Centre host `10.60.60.20` to open TCP/2404 sessions to the RTU at `10.30.30.20`. It also allows the RTU to reach the named breaker, RMUs and OLTC on TCP/502, while a separate rule permits the PLC to reach the OLTC on that port. The address and service limits reduce which hosts can connect; they do not inspect what a connected application asks the device to do.

### A permitted service is not command authorization

IEC 104 telemetry, general interrogation and switching share TCP/2404. Allowing that port is not a read-only grant. Likewise, Modbus TCP/502 can carry reads and writes; an allow rule does not limit function codes or register ranges. The weak baseline's broader field access permits both reads and writes, while the improved policy narrows the initiating sources and destinations. Neither policy turns those L4 rules into command-level authorization. See [the source/port boundary](#eu-port-source-boundary) for the distinction between reachability and application authority.

The improved policy JSON sets `dpiEnabled` to `false`. That is an intentional scope choice, not a gap in the firewall evidence: containd still applies the L4 rules and can report logged rule hits. A `dpiMode` value or a generic capability in containd must not be read as evidence that this EU range is inspecting IEC 104 payloads. The package does not configure IEC 104 command predicates, ASDU filters, common-address or IOA allowlists, or Modbus function-code filters. Packet decoding or a successful policy test cannot establish that such filtering occurred.

### Stateful sessions and policy changes

The compiled forwarding path accepts packets in established or related conntrack state before evaluating ordinary L4 rules for new traffic. Therefore, applying a new deny does not necessarily terminate a connection admitted under the previous policy. Test new connections separately from existing ones and identify the relevant five-tuple and policy-change time. [Established-session evidence](#eu-established-sessions) describes the range's bounded procedure; do not delete unrelated sessions or treat a surviving session as proof that the new rule failed.

### Firewall events are a separate evidence path

For a rule with logging enabled, containd adds an nftables log statement before its L4 verdict. Its NFLOG consumer turns matching kernel log records into `firewall.rule.hit` events containing the rule ID, action and available packet tuple fields (addresses, ports and transport). The backend can retrieve containd events for the substation view. These are records of logged rule matches, not payload-decoding results.

The improved EU policy explicitly logs selected denied cross-zone attempts, including unauthorized IEC 104 and Modbus connections. Logging is not automatic for every packet: an unmatched packet dropped by the default policy does not itself prove that a corresponding rule-hit event will be present. Conversely, a quiet event list is not proof that no traffic was blocked. Preserve the active policy, source and destination, service, test interval and event evidence together; keep missing events distinct from a demonstrated allow or deny. See [the evidence lifecycle](#eu-evidence-lifecycle) for retention and provenance limits.

Implementation: EU policy files `firewall/weak.json` and `firewall/improved.json`; containd `pkg/cp/compile/compile.go`, `pkg/dp/enforce/enforce.go` and `pkg/dp/capture/nflog_linux.go`; range event retrieval in `backend/internal/containd/events.go`.
