Deep packet inspection (DPI) is a general capability to interpret application-protocol content, rather than making a decision only from network and transport metadata such as addresses, ports and connection state. In industrial control systems, an inspection engine may use a protocol parser to evaluate selected application fields against an explicit policy. The available protocols, fields and enforcement behavior depend on the particular engine and its configuration.

### What a layer-4 rule can establish

A layer-4 allow rule establishes that a named source can open a connection to a destination service. For example, TCP/2404 carries IEC 104 telecontrol, while TCP/502 is used for Modbus/TCP in this range. A permitted connection does not, by itself, say whether its IEC 104 messages are observations or commands, or whether Modbus requests read or write values.

This distinction is about the scope of the rule, not whether the connection is legitimate. A source-and-port boundary can reduce who may reach a service while leaving application operations within an allowed session outside the firewall's decision. Read the actual source, destination and service boundary in [port and source rules](#eu-port-source-boundary) and [the policy comparison](#eu-policy-comparison).

### What protocol-aware inspection could mean

As general background, an ICS inspection design might parse selected IEC 104 ASDU fields or distinguish Modbus request types, then apply configured rules to the parsed content. Such an example describes a possible design pattern, not a promise that every product understands every message or offers the same policy fields. A decoder used to analyze a packet capture is also different from a firewall making an allow-or-deny decision on that packet.

For IEC 104, a capture decoder can show ASDU types, causes of transmission and information-object addresses as described in the [wire model](#iec104-wire-model). Seeing those fields in a decoded capture proves that the analysis tool interpreted the traffic; it does not prove that an enforcing device inspected or restricted those fields. Likewise, observing a Modbus function code is not evidence that a firewall applied a function-code rule.

### The EU package's enforcement boundary

The EU reference policy disables DPI and enforces at layer 4 only. DPI is generic background capability, **not enabled / not demonstrated in this package**. The policy does not filter IEC 104 commands by ASDU type, common address or IOA, and it does not filter Modbus requests by function code or register. There is no demonstrated deep inspection or command-aware firewall enforcement in this range.

Consequently, allowing TCP/2404 does not make a CC session read-only, and an allowed TCP/502 path is not restricted to reads. IEC 104 decoding and Modbus observations can support protocol analysis, but must not be presented as proof of an enforced application-layer restriction. Device authority, interlocks and network reachability are separate controls with separate evidence; see [control authority](#eu-control-authority).

### Build evidence for the claim actually tested

When evaluating a segmentation change, record the active policy, the test source, destination, service and whether the connection was new or already established. Pair a denied attempt from a disallowed source with evidence that the required named operational paths still work. This establishes the tested reachability boundary; it does not establish a command filter.

If a capture contains a decoded IEC 104 command, report it as observed traffic. If a new connection is denied by the reference policy, report the source-and-service denial. Do not infer DPI enforcement from either result. [Established sessions](#eu-established-sessions) can affect before-and-after tests, and [GI and quality evidence](#iec104-gi-cot-quality) helps distinguish an observation exchange from proof of physical state.
