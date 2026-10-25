Vendor remote access is the persistent thorn in OT security: utility staff cannot keep up with every vendor-specific protective relay, SCADA system, and PLC controller, so vendors need *some* path into the OT network to support their gear. How you let them in defines a large chunk of your attack surface.

### The Common Patterns

**1. Direct VPN.** Each vendor gets a site-to-site or remote-access VPN into the OT network, typically landing in a vendor zone. Easy to set up, hard to scope. The vendor's whole engineering team can usually reach more than they need to. Lab 2.3-bonus simulates this pattern.

**2. Vendor jump host.** A dedicated server in a DMZ that the vendor logs into (typically via RDP, SSH, or Citrix), runs the vendor-specific tools on, and uses to reach the OT devices. Better than direct VPN because the surface from the vendor's network to your DMZ is narrow (one host, a few protocols), and you can monitor the jump host's outbound to OT. This is what the lab's `vendor-jump` node models.

**3. Privileged Access Management (PAM) broker.** A specialized appliance - BeyondTrust Privileged Remote Access, CyberArk PSM, Cyolo, Claroty Secure Remote Access, Dispel, Xage, or similar - that the vendor authenticates against. The broker enforces just-in-time access (the path opens for a scheduled window and closes after), MFA, session recording, and click-stream auditing. This is the modern utility standard for high-criticality access.

**4. Zero-Trust Network Access (ZTNA).** An access broker (Zscaler ZPA, Cloudflare Access, Tailscale, Twingate) that builds a per-application tunnel from the vendor's identity to the specific service they need - without exposing the network. The vendor's laptop talks to the broker, the broker talks to the target device, and the network between them stays invisible. Newer, IT-centric, increasingly adopted on the OT/IT boundary.

### Where Vendor Access Usually Fails

The audit findings on real utility engagements tend to cluster in a few areas:

- **Scope creep.** A vendor needs access to "their" devices, so they get access to the OT-Ops VLAN - and from there can reach things that are not theirs.
- **Shared credentials.** "`vendor` / `vendor`" on the jump host, used by ten people across two organizations. No way to attribute an action to a specific human.
- **No session recording.** The vendor connects, does something to a relay, disconnects. The audit trail is "vendor logged in at 14:03, logged out at 14:42." That is not enough to investigate an incident.
- **Persistent access.** The vendor's path is always open instead of opened on demand for a maintenance window.
- **East-west blindness.** The firewall watches enterprise-to-DMZ but not DMZ-to-OT. A compromised vendor jump host can pivot freely into OT.

### How the Lab Demonstrates This

Lab 2.3-bonus models the persistent-access plus east-west-blindness failure modes together. Under Weak, the vendor's RDP path is open from the enterprise zone (the attacker exploits this with stolen credentials), and from the vendor jump host the Modbus path to the field zone is also open. The attacker laundered through the vendor session never appears to be the attacker - the field device sees the command coming from the vendor's trusted IP. The hardened policy closes both links of the kill chain: enterprise → vendor RDP is denied at the perimeter, and vendor → field Modbus is denied at the DMZ-to-Field conduit. Either one alone breaks the attack; together is defense in depth.

The PAM-broker and ZTNA patterns are *not* simulated in the lab - they would require more infrastructure than fits in a docker compose - but the same defensive lesson generalizes: scope each access path narrowly, log who did what, and assume any path you do not actively constrain will be exploited.

With Apply Your Plan, `restrict-enterprise-to-vendor` owns the RDP/3389 and VNC/5900 closure. `block-vendor-to-field` owns the field hop. Selected means denied; deferred means reachable. Test each link independently. SSH/22 stays available for management and does not test the RDP/VNC closure. Selecting `vendor-zta-broker` is planning-only; it deploys no broker. Restore normal operations after every test.
