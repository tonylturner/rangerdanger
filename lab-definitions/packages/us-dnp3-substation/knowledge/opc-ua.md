**OPC UA** (OPC Unified Architecture) is a modern, vendor-neutral, platform-independent protocol for industrial communication, increasingly the go-to choice for **IT/OT integration**: getting OT data into MES, ERP, historians, analytics platforms, and cloud services. Where DNP3 and IEC 61850 are substation-and-utility specific, OPC UA is broader - it shows up in manufacturing, oil and gas, building automation, and water/wastewater.

### What OPC UA Replaces

**Classic OPC (sometimes called OPC DA, HDA, A&E)** was the previous generation: Windows-only, built on DCOM, painful to firewall, painful to debug, painful to secure. Anyone who has tried to traverse a firewall with DCOM has a story. OPC UA replaces all of that with a single specification that works across Windows, Linux, and embedded devices, runs over TCP (port 4840) or HTTPS, and includes security as a first-class concern rather than a bolt-on.

### The Information Model

OPC UA's distinctive feature is its **address space** - a graph of nodes with types, attributes, and references that describes the data semantically, not just by tag name. A node can be marked as a "Temperature" with units, scaling, and engineering range; another node can be a "PumpController" with predefined methods like `Start()` and `Stop()`. Clients query the address space, discover what is there, and bind to it without prior schema knowledge.

This is a sharp contrast with DNP3 and Modbus, where the meaning of "holding register 17" lives in a vendor's datasheet rather than in the protocol itself. OPC UA is self-describing.

### Where You Encounter OPC UA on an OT Network

- **Historians.** PI System, Wonderware Historian, Cogent DataHub, and most modern historians use OPC UA to collect data from controllers.
- **Modern PLCs.** Siemens S7-1500, Beckhoff TwinCAT, Rockwell ControlLogix, Schneider M580 - all expose OPC UA servers natively.
- **MES / ERP integration.** Data from the plant floor flowing up to manufacturing-execution and enterprise-resource-planning systems usually rides on OPC UA.
- **IT/OT brokers.** Cloud-bound OT data (AWS IoT SiteWise, Azure IoT Hub, GE Predix) often passes through an OPC UA aggregator.
- **Wind / solar SCADA.** Renewable-generation control rooms increasingly use OPC UA for aggregation across fleets.

### Security Built In

OPC UA has security baked into the spec rather than retrofitted:

- **TLS / certificate-based authentication.** Each client and server presents an X.509 certificate; the connection is mutually authenticated. No clear-text protocol mode is encouraged.
- **User-level access control** on individual nodes. The protocol distinguishes anonymous, username/password, and certificate-bound identities.
- **Message-level signing and encryption.** Per-message integrity protection on top of TLS.

In practice OPC UA deployments still vary widely - many devices ship with self-signed certs, expired certs, or anonymous-allowed configs, so the "security built in" is only as good as the deployment hygiene. But the protocol itself is a substantial improvement over the legacy ICS protocols.

### Why the Lab Doesn't Simulate It

A few reasons:

- The lab focuses on distribution-substation segmentation, where the predominant SCADA protocols are DNP3 and (increasingly) IEC 61850. OPC UA is more common at the IT/OT *boundary* and inside *industrial-automation* environments than inside utility distribution substations.
- Simulating a faithful OPC UA server with a meaningful address space is substantially more work than simulating a Modbus or DNP3 outstation - the protocol stack is heavier, the address-space modeling matters, and the security model needs to be authentic to teach the protocol's strengths.
- The cross-protocol firewall lessons the lab teaches (L4 + DPI defense in depth, source-pinning, segmentation by zone) all carry over to OPC UA cleanly once you map "Modbus function code" to "OPC UA service code" and "DNP3 Direct Operate" to "OPC UA Write/CallMethod."

For IT/OT integration architectures, the OPC UA server is usually positioned in a DMZ between the OT network and the IT/cloud side. Treat that DMZ with the same defense-in-depth posture the lab teaches for the field-zone conduit, layered with OPC UA's own certificate-and-user-level controls. The segmentation logic generalizes; the protocol surface is different.