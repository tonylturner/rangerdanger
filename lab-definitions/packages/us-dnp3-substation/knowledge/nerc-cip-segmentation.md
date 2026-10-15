NERC CIP (North American Electric Reliability Corporation Critical Infrastructure Protection) is the mandatory cybersecurity standard for the bulk electric system in North America. Distribution substations are not always subject to NERC CIP (it depends on voltage level and system impact), but the CIP framework provides segmentation concepts that apply broadly to any utility OT environment.

### CIP-005: Electronic Security Perimeters

CIP-005 is the standard most directly relevant to segmentation. It requires:

- **CIP-005 R1**: All applicable BES Cyber Systems must reside within a defined Electronic Security Perimeter (ESP), with all external routable communication through identified Electronic Access Points (EAPs)
- **CIP-005 R2**: Each EAP must permit only known and necessary communication. Everything else is denied by default.

### Key Terminology

- **ESP** (Electronic Security Perimeter): the logical boundary around BES Cyber Systems
- **EAP** (Electronic Access Point): any device controlling traffic at the ESP boundary (firewall interface, router ACL)
- **ERC** (External Routable Connectivity): any routable IP communication crossing the ESP boundary
- **IRA** (Interactive Remote Access): user-initiated sessions crossing the ESP, requiring encryption, MFA, and an intermediate system

### Mapping to the Lab

In the RangerDanger lab, the containd NGFW functions as the EAP for all zone boundaries. The `ot_ops_net` and `field_net` together sit inside the ESP - with the field devices (relays, reclosers, regulators) as its most critical core - while `enterprise_net` and `vendor_net` sit outside it. The `vendor_net` simulates the vendor remote access path, which in a CIP-compliant environment requires IRA controls. (See [ESP, EAP, and ERC Explained](#esp-eap-erc) for the same boundary broken down per acronym.)

The exercises demonstrate why CIP-005 R2 (deny by default) is essential. The weak baseline violates this principle by allowing broad access. The hardened policy enforces it.

### Beyond Mandatory Compliance

Even for utilities not subject to NERC CIP, the framework provides an excellent segmentation template. Defining security perimeters, identifying all access points, and maintaining explicit allow-lists for cross-perimeter communication are universal best practices. The lab exercises build hands-on experience with these concepts so you understand both why the controls matter and what happens when they are absent.