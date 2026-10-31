The Purdue Model is a functional reference for discussing how industrial operations relate to the physical process, control, site operations and enterprise systems. It is a way to reason about roles and paths, not a required network diagram or a security policy. Implementations and extensions vary; in particular, L3.5 is commonly used for an industrial demilitarized zone (IDMZ) between operations and enterprise functions.

### The levels

| Level | Typical function | EU range interpretation |
|---|---|---|
| **L0** | Physical process | The modeled feeder and connected load are solved by the process engine; separate field-device simulators expose corresponding equipment state. This is a simulation, not physical substation equipment. |
| **L1** | Basic control and field functions | Breaker, RMU and OLTC endpoints represent field-side equipment. Their network placement does not by itself define every control function's Purdue level. |
| **L2** | Area supervisory functions | Operator supervision belongs to this general functional tier; the range's Control Centre (CC) provides supervisory operation, but its exact level depends on the system boundary being modeled. |
| **L3** | Site operations | Site-wide operational and engineering functions may be discussed at this tier. The EU station subnet is not a Purdue level: it contains the RTU, PLC and clock, whose different roles should not be collapsed into one asset category. |
| **L3.5** | IDMZ or controlled exchange tier | An IDMZ mediates selected exchanges between operational and business environments. It is an architectural role, not a synonym for every network named “DMZ.” |
| **L4** | Site business systems | Business-side services sit above site operations. The range's Enterprise network is a teaching boundary; its Kali host is a probe, not a model of a production business system. |
| **L5** | Enterprise-wide systems | Central enterprise services are generally outside a substation's operational scope. This package does not model an L5 environment. |

These labels are useful questions, not an automatic assignment for every device. The EU RTU is a station gateway: it terminates the CC's IEC 104 connection and separately polls field devices over Modbus TCP. It has a station interface only and is not a packet-forwarding router. The [telecontrol architecture](#eu-telecontrol-architecture) describes those two protocol hops and their separate evidence.

### An IDMZ is a design role, not a magic subnet

The purpose of an IDMZ is to avoid an uncontrolled direct path between enterprise services and lower-level control. A design may place brokers, jump services or replicated data there, but the appropriate services and permitted directions depend on the system's requirements. A label alone does not prove that a boundary is mediated or secure.

Keep the EU package's two similarly named areas distinct. Vendor/Engineering is `10.20.20.0/24` (`dmz`); the Historian is in the separate Operations DMZ, `10.70.70.0/24` (`lan5`). The CC pushes observations to the Historian, while Enterprise clients read its archive. This is a bounded data path, not evidence that the two DMZs are one Purdue layer or that the Historian is a general-purpose proxy. See the [range node map](#eu-range-node-map) and [zone and conduit design](#eu-zone-conduit-design) for actual roles and permitted flows.

### Purdue levels are not IEC 62443 security levels

Purdue levels describe an asset's architectural function. IEC 62443 zones and conduits describe groupings and communications boundaries, while Security Levels express security requirements or capability targets. These ideas can inform the same design, but there is no automatic mapping from a Purdue level to a zone, firewall rule or numeric Security Level. Use the [risk-design workflow](#eu-iec62443-risk-design) to document the system scope, risks and target requirements rather than inferring them from a layer diagram.

For this range, name the actual interface and path alongside any Purdue label: for example, CC `10.60.60.20` to RTU `10.30.30.20:2404`, RTU to field devices on TCP/502, or Enterprise to Historian `10.70.70.40:8080`. This keeps functional placement separate from reachability. The reference firewall enforces source- and port-based boundaries at L4, with DPI disabled; a Purdue diagram does not imply IEC 104 command filtering, ASDU/IOA predicates or Modbus function filtering. See [policy comparison](#eu-policy-comparison) for the policy's demonstrated limits.

### Limits

Treat the mapping above as a teaching aid, not a universal utility topology, compliance result or claim that each Purdue level is a separate subnet. The range has distinct management and process networks as well as its student-facing zones; those implementation paths must not be silently drawn as ordinary operational conduits. The [node map](#eu-range-node-map) explains those distinctions. A production design should assign roles and boundaries from its own asset inventory, operating model and risk assessment.
