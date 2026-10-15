The BDEW/Oesterreichs Energie/VSE whitepaper describes security requirements for control and telecommunication systems in the energy sector. It is useful for discussing procurement, zoning and maintenance access, but this range is not a conformity demonstration.

The source used here is version 3.0, dated 30 September 2024: the [authoritative German publisher PDF](https://www.bdew.de/media/documents/BDEW-OE-VSE-Whitepaper-3.0.pdf) and [English courtesy translation](https://www.strom.ch/system/files/media/documents/20240930-bdew-oe-vse-whitepaper-requirements-for-secure-control-and-telecommuniation-systems.pdf). German prevails. The older 2018 BDEW landing page still points to version 2.0, so it is not a reliable latest-version pointer. Version 3.0 was the latest located in the October 2026 research; recheck before the course.

### Zones need restrictive, observable crossings

Sections 4.4.1–4.4.2 address functional/site zoning, restrictive firewalls or gateways and monitorable perimeters. The range illustrates this by separating the CC, station, field and historian DMZ and naming their conduits.

The relevant lesson is not “we have seven firewall zone names.” It is that each crossing has a stated function and a testable restriction. CC-to-RTU supports supervisory operation, PLC-to-OLTC supports regulation, and enterprise-to-historian supports archive consumption without a direct field conduit.

The historian receives outgoing CC snapshots instead of pulling inward from the control system. That maps to the whitepaper's preference for connections initiated from higher-security zones. It is a design analogy, not proof that every aspect of this implementation satisfies the document.

### Remote maintenance is controlled access

Section 4.4.4 discusses operator-controlled access servers in a DMZ, two-factor authentication, no direct terminal-device dial-in, centralized logs and failed-attempt reporting.

Compare that with the range's engineering workstation and OpenPLC web conduit. The lab lets you reason about which maintenance destination is necessary and why general vendor-to-field access is excessive. It does not demonstrate production two-factor authentication, individual accountability or a complete managed remote-access system.

The portal's embedded desktops, management proxy and baked-in credentials are workshop conveniences. Do not present their availability as satisfaction of a remote-maintenance requirement. See [vendor and engineering access](#eu-vendor-engineering-access) for the separate station and management paths.

### Protocol security is more than a port rule

IEC 101/104 without additions lacks native authentication, integrity and encryption of the kind discussed in the whitepaper. It recommends available IEC 62351 extensions while considering performance and key management.

The published [IEC 62351-3:2023](https://webstore.iec.ch/en/publication/68410), [IEC 62351-5:2023](https://webstore.iec.ch/en/publication/65511) and [IEC TS 60870-5-7:2025](https://webstore.iec.ch/en/publication/87773) describe transport/application security and their mapping to 101/104. Their publication does not prove this range's devices implement them. Interoperability, certificates and key management were not verified.

Even authenticated transport does not make a compromised authorized endpoint harmless. The [source/port boundary](#eu-port-source-boundary) and command authorization remain distinct concerns.

For a procurement discussion, ask which extensions each endpoint supports, how interoperability will be tested, who owns keys and certificates, and how maintenance access is revoked and logged. Request measured behavior under failure and recovery, not only a feature checkbox. None of those answers can be inferred from decoding an unencrypted IEC 104 capture in this workshop.

### Know the demonstration's hard limit

Section 4.3.5(b) prohibits containers of different trust zones sharing hosts. This single-host Docker range deliberately does exactly the kind of co-location that prevents a production-conformity claim. Its simulated electrical model, shared host and lab management plane must stay visible as limitations.

Use the whitepaper to ask better design and procurement questions, then retain the answers and gaps in the [acceptance package](#eu-acceptance-handover). Do not label the range “BDEW compliant” or treat a green lab result as an assessment of a real utility.
