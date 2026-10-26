A zone groups assets with similar security requirements; a conduit describes the controlled communications between zones. Neither is simply another name for an IP subnet. Start with the work the substation must do, then decide which assets may communicate to support it.

This range models a CIGRE-derived 20 kV teaching feeder, not a complete utility architecture. Its boundaries let you test a design argument: an enterprise host should read an archive without gaining switching authority, and a maintenance workstation should reach a PLC without becoming a second telecontrol master.

### The implemented zones

| Function | Subnet / firewall zone | Important assets |
|---|---|---|
| Enterprise | 10.10.10.0/24 / wan | Kali, 10.10.10.50 |
| Vendor and engineering | 10.20.20.0/24 / dmz | vendor .10; engineering .20 |
| Station | 10.30.30.0/24 / lan1 | RTU .20, OpenPLC .30, clock .50 |
| Field | 10.40.40.0/24 / lan2 | CB1 .20, RMUs .21/.22, OLTC .23, F2 breaker .24 |
| Control Centre | 10.60.60.0/24 / lan4 | CC .20 |
| Operations DMZ | 10.70.70.0/24 / lan5 | historian .40 |

The firewall's network interfaces carry these zone names on every host, so `tcpdump -i lan4` in the firewall shell captures Control Centre traffic. The vendor zone named `dmz` and the historian's Operations DMZ named `lan5` are different networks. Confusing the two can turn an archive rule into a maintenance-access rule.

Management, 10.99.99.0/24 (`lan3`), carries the portal's upstream API access. The separate 10.50.50.0/24 process network carries simulated physics. Neither is a student control conduit. Document these as range infrastructure rather than drawing them as invisible exceptions.

The Network console explains these omissions under collapsed **Infrastructure planes**, including isolated browser-desktop delivery. “Through containd” describes cross-zone teaching conduits, not every management, process, delivery or same-zone path. Portal OpenPLC access is management access, not the engineering conduit below; see the [node map](#eu-range-node-map).

### Write a communications matrix

For each conduit, name its initiator, destination, service, operational purpose, owner and failure consequence. The improved reference policy permits:

- CC 10.60.60.20 → RTU 10.30.30.20, TCP/2404: observation and supervisory control.
- RTU 10.30.30.20 → field .20 through .24, TCP/502: polling all five field devices; supervisory commands are mapped for CB1, RMUs and OLTC, not F2.
- PLC 10.30.30.30 → OLTC 10.40.40.23, TCP/502: voltage feedback and automatic tap requests.
- CC → historian 10.70.70.40, TCP/8081: outgoing observation snapshots.
- Enterprise → historian, TCP/8080: archive reads.
- Engineering 10.20.20.20 → PLC 10.30.30.30, TCP/8080: engineering maintenance.

No new historian-initiated OT flow is allowed. Return packets for an established CC push are not an inward historian polling service. The historian never queries the RTU, devices or solver.

Selected-flow evidence separates **Topology** (declared by the package), **Observed liveness** and **Authorization**. It also explains operational purpose and connection initiation; arrows are not a one-way information-flow claim. Read the [telecontrol path](#eu-telecontrol-architecture) and [authorization evidence limits](#eu-port-source-boundary) alongside the matrix.

### Find the boundaries that do not exist

The RTU reads the PLC's status block and polls the station clock within the station subnet. Neither path crosses containd. A firewall rule mentioning `lan1` cannot prove same-subnet isolation. The [station-clock article](#eu-station-clock) explains why the time path is simulated HTTP rather than NTP/PTP. Likewise, field devices share a subnet; the range does not demonstrate independently firewalled zones around each RMU.

These are residual design constraints, not reasons to pretend the topology offers finer enforcement. A production assessment might justify separate controller or actuator zones, host controls or other architecture, but that would be a new design.

### Validate the argument

Pair a denied new Kali-to-RTU connection with continued legitimate CC observation. Pair denied enterprise field access with successful historian reads. Verify that PLC regulation still works after tightening field access; blocking all Modbus would look restrictive while defeating an operational dependency.

The boundary remains source/port based, not command aware. A permitted CC can still switch equipment. Explain that residual risk alongside the drawing, using [port and source rules](#eu-port-source-boundary), [control authority](#eu-control-authority) and the [risk-design workflow](#eu-iec62443-risk-design).
