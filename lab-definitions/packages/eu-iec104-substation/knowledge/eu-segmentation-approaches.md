“Segment the network” describes an objective, not a single control. Physical separation, network boundaries, application inspection and endpoint controls act at different places and answer different questions. A sound design chooses controls for the required communication and verifies both that unwanted paths are blocked and that necessary operation still works.

### Separate the mechanisms

**Separation** limits which systems have a path to one another. **Enforcement** decides whether a particular exchange may cross a boundary. **Inspection** describes what information the enforcement point can evaluate. These are related but not interchangeable: a VLAN can separate broadcast domains without deciding whether a Modbus request is a read or a write, while a firewall rule can permit a service without authorizing every operation that service carries.

Treat the approaches below as a general design taxonomy, not a list of capabilities demonstrated by this range. In the EU reference package, the firewall policy is enforced at L4 and DPI is disabled. No IEC 104 ASDU, common-address or IOA predicate, or Modbus function-code/register filter is demonstrated. A decoded packet is evidence of observation, not command-aware enforcement.

### Physical separation and one-way links

A physical air gap has no live network path between the separated systems. That can reduce remote reachability, but it does not remove risks from maintenance, removable media or other controlled transfers; those processes need their own protections and evidence. “Air gapped” should describe a verified architecture, not merely a label on a diagram.

A data diode is designed to permit information to flow in one direction only. It can suit a deliberately one-way export, such as sending selected telemetry outward, but it cannot by itself support an interactive exchange that needs commands or responses in both directions. Neither physical air gaps nor data diodes are capabilities demonstrated by the EU range.

### Layer 2: switches and VLANs

A switch forwards Ethernet frames within a Layer-2 network. VLANs can keep traffic in separate broadcast domains on shared switching infrastructure; communication between those domains requires a routing or bridging path. VLANs help organize boundaries, but do not inspect application commands. Trunk, assignment or gateway mistakes can also undermine the intended separation.

A VLAN is therefore not a substitute for an explicit policy at the point where traffic crosses zones. The EU topology uses named operational zones and subnets; do not infer from that map that the range configures physical VLANs.

### Layer 3: routing and access lists

Routers move packets between IP networks. An access-control list can restrict a path by such attributes as source, destination, protocol and port. This is useful for coarse reachability policy, but an address-and-port match does not identify the operation inside an application session.

State handling varies by product and configuration: a basic packet-filtering ACL may evaluate packets independently, while some platforms provide stateful features. Verify the actual behavior rather than assuming all router ACLs have the same return-traffic semantics.

### Layer 4: stateful firewall policy

A stateful firewall can apply source, destination and service rules while tracking connection state, so replies to an admitted session can be distinguished from a new initiation. It is a useful control for limiting which zones and hosts can reach a service. It still does not make a service permission equivalent to command authorization: a permitted TCP port may carry more than one application operation.

The EU reference policies use this kind of source- and service-based boundary. For example, the improved design permits the named Control Centre (CC) to reach the station RTU on IEC 104 TCP/2404 and the RTU to reach specified field devices on Modbus TCP/502. A permitted flow is not an ASDU/IOA allowlist or a Modbus read-only rule. See [port- and source-based rules](#eu-port-source-boundary) and [weak versus improved policy](#eu-policy-comparison) for the exact scope and remaining risk.

Stateful rules also make timing relevant: a policy change that blocks new sessions does not necessarily terminate an already established one. Distinguish new-connection tests from existing-session behavior; see [conntrack and temporal proof](#eu-established-sessions).

### Layer 7: deep packet inspection

Deep packet inspection (DPI) is a generic capability in which a supported application parser examines protocol content in addition to network headers. In principle, a product with a suitable parser and policy controls could make decisions about selected application fields or operations. Actual coverage depends on the product, protocol support and configuration; a label such as “next-generation firewall” is not evidence that a particular command predicate is enforced.

For this EU package, DPI is explicitly disabled in the reference policy and command-aware IEC 104 or Modbus filtering is not demonstrated. Do not claim that the firewall allows or blocks IEC 104 commands by ASDU type, common address or IOA, or Modbus operations by function code or register. Captures and decoders can explain what was observed, but do not change the L4 enforcement boundary. The [risk-design workflow](#eu-iec62443-risk-design) likewise treats the absence of command-aware enforcement as a design limitation, not a hidden feature.

### Host controls and controls within a zone

Network boundaries do not automatically control communication between two systems that remain in the same zone. Host firewalls or workload-based microsegmentation can add controls closer to the endpoints, where supported and operationally maintainable. Their coverage and failure modes differ from a routed firewall policy; do not assume an endpoint agent can be installed on every field device.

In the EU topology, for example, RTU-to-PLC traffic on the station subnet is not an inter-zone conduit enforced by the firewall. This is a scope boundary, not proof of a separate host-level control. The [zone and conduit design](#eu-zone-conduit-design) describes the modeled networks and their residual boundaries.

### Choose controls from required communication

Start with the function and its necessary communication, then identify initiator, destination, service, owner and failure consequence. For the EU range, the CC-to-RTU IEC 104 path, RTU-to-field Modbus paths, PLC-to-OLTC regulation path and historian exchange have distinct purposes; a broad “allow OT” rule would hide those distinctions. The [zone and conduit design](#eu-zone-conduit-design) provides the package's communications matrix.

| Approach | Primary contribution | Important limit |
| --- | --- | --- |
| Physical separation | Removes a live network path | Does not govern maintenance or removable-media transfers |
| VLAN / Layer 2 | Separates broadcast domains | Does not authorize application operations |
| Router ACL / Layer 3 | Restricts routed addresses and services | Does not identify commands within an allowed service |
| Stateful firewall / Layer 4 | Controls new flows and tracks sessions | A permitted service can still carry multiple operations |
| DPI / Layer 7 | May inspect supported application content | Capability and enforcement depend on parser and policy; disabled and not demonstrated here |
| Host controls | Can limit selected endpoint or within-zone paths | Coverage depends on endpoint support and configuration |
| Data diode | Enforces a designed one-way transfer | Not suited by itself to interactive, bidirectional control |

Test the negative case from the actual disallowed source and the positive case for each required operational path. Record the active policy, source/destination/service and whether the test was a new or existing session. A denied probe alone does not show that the design preserves observation, local operation or regulation. These are design options and evidence boundaries, not an IEC 62443 certification or a claim that any single mechanism is sufficient.

### See Also

- [Zones and conduits for the EU substation](#eu-zone-conduit-design) — modeled networks and required exchanges
- [Port- and source-based rules](#eu-port-source-boundary) — what the reference policy does and does not enforce
- [IEC 62443-3-2 risk assessment in practice](#eu-iec62443-risk-design) — risk, countermeasures and residual limitations
