Default-deny means that a flow not matched by an allow rule is denied. It makes least-privilege segmentation enforceable, but the way a firewall reaches that final decision differs by implementation. Do not infer the behavior from a product label or from seeing several deny rules: identify the effective fallback for the policy path being reviewed.

### Two policy patterns

With **implicit default-deny**, the policy engine supplies a terminal deny when no configured rule matches. The administrator writes the required allows; unmatched traffic is denied by the engine's default behavior.

With **explicit default-deny**, the administrator configures the relevant chain or policy's default action as deny, or adds a final deny rule. If that explicit fallback is missing, the result depends on that implementation's actual default—it may not be deny. A final deny rule can also make the intended boundary visible during review, but whether it logs, and which traffic reaches it, depends on the platform and configuration.

These are useful design patterns, not a reliable vendor taxonomy. Products can expose defaults at different levels, and generated rules or interface-specific policies may affect the effective result. Consult the documentation for the exact version and configuration, inspect the effective policy, and test an unmatched flow from the relevant source. Do not assume that behavior on one firewall, Linux host, bridge or virtual switch carries over to another.

### What the EU reference policies demonstrate

Both EU package policies set `defaultAction` to `DENY`. The weak policy is still permissive because it adds broad cross-zone allows; “weak” does not mean its fallback is allow. The improved policy narrows the permitted sources and destinations and includes explicit denies for selected unauthorized IEC 104 and Modbus attempts. Other unmatched traffic is covered by the default deny. See the [weak and improved policy comparison](#eu-policy-comparison) for the actual exchanges.

The reference policy makes network decisions using sources, destinations and services. It does not authorize particular IEC 104 commands or Modbus operations: allowing TCP/2404 does not make a session read-only, and allowing TCP/502 does not filter Modbus function codes. Improved also observes selected CC breaker and OLTC commands using explicit monitor-only IEC 104 predicates. Those observations never admit or deny traffic and do not enforce ASDU/IOA restrictions; Modbus DPI remains planning-only. Keep network reachability separate from [device and control authority](#eu-control-authority).

### Verify the intended boundary

For a proposed policy change:

1. Name the required initiator, destination, service and operational purpose. Identify who owns the flow and what fails if it is denied.
2. Confirm the effective fallback for the relevant interface, direction and chain. Distinguish a configured default from an explicit rule, and check whether policy ordering or another path changes the result.
3. Test both sides from the actual source: the required exchange must still work, and an unauthorized new connection must be denied. For this range, preserve legitimate Control Centre observation and required field polling and voltage regulation while checking the intended exclusions.
4. Record the policy identity, source/destination tuple, test time and service-level evidence. A missing log entry alone does not prove a flow was denied or allowed.

An established connection can outlive a policy change, so distinguish new-connection results from existing-session behavior; see [conntrack and temporal proof](#eu-established-sessions). Pair restrictive results with evidence that required operation remains available, as described in the [policy comparison](#eu-policy-comparison).
