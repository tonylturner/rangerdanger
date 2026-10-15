These three acronyms from NERC CIP-005 define the building blocks of OT network perimeter security. Understanding them concretely helps you map abstract compliance requirements to real network architecture.

### ESP (Electronic Security Perimeter)

The ESP is the logical boundary enclosing network segments that contain BES Cyber Systems. Think of it as drawing a line on a network diagram around all devices that control or monitor the physical power system. Every IP address inside that boundary must be protected according to CIP requirements.

In this lab, the `field_net` (10.40.40.0/24) and `ot_ops_net` (10.30.30.0/24) together form the ESP, containing the relay, recloser, regulator, RTAC, HMI, and PLC.

### EAP (Electronic Access Point)

An EAP is any device or interface that controls traffic crossing the ESP boundary. In practice, these are firewall interfaces, router ACLs, or managed switches with access control. Each EAP must enforce an explicit policy permitting only documented, necessary communication.

In this lab, the containd NGFW interfaces on `ot_ops_net` (eth2/lan1) and `field_net` (eth3/lan2) are the EAPs. The firewall rules on these interfaces determine what enters or leaves the ESP.

### ERC (External Routable Connectivity)

ERC is any IP-routable communication crossing the ESP boundary. This includes automated data flows (SCADA polling, historian transfers) and interactive sessions (operator access, engineering configuration, vendor support). CIP requires all ERC to be documented with EAPs permitting only the documented flows.

In this lab, `enterprise_net` (10.10.10.0/24) and `vendor_net` (10.20.20.0/24) are outside the ESP. Any traffic from these networks into `ot_ops_net` or `field_net` is ERC.

### IRA (Interactive Remote Access)

IRA is a specific type of ERC where a human initiates a session crossing the ESP. This is the highest-risk form because it involves real-time interactive control from outside the perimeter. CIP-005 requires IRA to use an **Intermediate System** (jump host) that terminates the external session and initiates a new internal one.

The `vendor_jumpbox` at 10.20.20.10 serves exactly this function. Vendor personnel connect to the jump box, then access devices inside the ESP from there.

### The Lab Demonstration

Under the weak config, the ESP boundary is effectively unenforced. The Kali box at 10.10.10.50 can send Modbus commands directly to the relay at 10.40.40.20. Under the hardened config, the EAP policies on containd block this, forcing all field device communication through the RTAC. That is the difference between an ESP that exists on paper and one that actually works.