Remote support is an operational need, not a reason to give a maintenance workstation the same authority as the Control Centre. The useful design question is “what specific task requires this path, for how long, under whose approval?” A rule named “vendor access” answers none of those questions.

### Two hosts, not one identity

The EU range places the vendor jump host at `10.20.20.10` and the engineering workstation at `10.20.20.20`. Both are in the vendor/engineering zone, but improved policy grants only engineering `.20` a maintenance conduit to OpenPLC `10.30.30.30:8080`.

That is the PLC's engineering web interface, not its Modbus status service and not the RTU's IEC 104 listener. Allowing engineering maintenance does not justify vendor-to-field TCP/502 or vendor-to-RTU TCP/2404. Preserve that distinction when writing the [communications matrix](#eu-zone-conduit-design).

The engineering image includes `mbpoll`, `nmap`, `tcpdump`, `tshark` and `iec104cmd`. The vendor image includes `mbpoll`, `nmap` and `tcpdump`, but does not explicitly install `tshark` or `iec104cmd`. A tool being available does not authorize its use, and absence of a tool is not a network security boundary.

### Treat the engineering interface as a control surface

A PLC web page may look less sensitive than a field protocol port. In practice, changing a control program or its slave mapping can alter how the tap changer behaves. Maintenance access therefore belongs in the risk assessment even when direct field access is denied.

The range's seeded PLC reads OLTC inputs and writes tap requests over its approved Modbus conduit. A permitted maintenance path reaches a host that already has that downstream trust. Review both the immediate destination and the authority reachable through it.

This is a residual trusted-host risk, not evidence that the improved policy claims to sandbox every action performed by an engineer.

### Separate station access from the portal

The platform's `/apps/openplc/` proxy reaches the PLC through management at `10.99.99.30:8080`. Engineering's direct path uses station `10.30.30.30:8080`. Opening the embedded app in the portal does not prove the engineering conduit works; it exercises a different path.

The in-browser engineering and vendor desktops instead use separate platform-owned internal delivery bridges: `desk_eng_net` (`10.98.3.0/28`) and `desk_vendor_net` (`10.98.2.0/28`). Only the proxy (`.2`) and the designated desktop (`.3`) share each bridge. Neither desktop attaches to `mgmt_net`; their default routes still point to containd on the vendor zone. Delivery is not a containd zone.

For full vendor isolation, run Lab 2's unbound HTTP probes from **both** desktops. Under the accepted policy, every declared `10.99.99.x` management host must be unreachable, including platform, containd and simulator/solver APIs. Under weak, containd is deliberately reachable from any zone host: its admin UI (8080/8443) and SSH (2222) are a finding. Other management hosts must remain unreachable under both policies. The local delivery proxy at `10.98.2.2:8080` or `10.98.3.2:8080` must return **403**, including portal, API, containd, apps and redirects. Missing tools, broken exec and a dead proxy are failures, not proof of isolation. Repeat under weak and the accepted policy; record the results alongside the teaching-plane deny matrix and the permitted engineering PLC GET. The accepted policy switches off all four interface access flags on every teaching zone while retaining lan3 management for RangerDanger. The firewall engine on TCP/8081 is loopback-only under both policies and must never be reachable from either desktop or any zone host. Do not mutate simulator state to test access.

Separately verify that portal desktop rendering and WebSocket input remain connected. Successful rendering proves delivery, not routed workstation-to-station access. The validator tests named hosts and ports, not every possible protocol or an engineer's downstream authority after authorized PLC access.

### Plan the work and its closure

A maintenance record should identify the operator who authorizes the work, the engineer, the approved target, the expected program/configuration version, the window and the rollback condition. Take before/after evidence of PLC runtime health and OLTC mode, tap and input validity. Keep a known-good seed/configuration reference for recovery.

In production, operator-controlled remote access, strong authentication, session accountability and reviewed changes would need their own design and validation. The range's baked-in credentials and desktop access are workshop conveniences, not a demonstration of those controls. The [BDEW context](#eu-bdew-security-context) explains relevant expectations without claiming conformity.

After the task, verify that no temporary broad rule remains, legitimate regulation still functions, and the maintenance event is recorded. If access must remain continuously enabled, document that as an explicit operational decision with an owner and review date. “We might need it later” is not an acceptance criterion.
