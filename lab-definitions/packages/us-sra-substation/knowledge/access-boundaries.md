# Remote access boundaries

The vendor desktop is `10.20.20.10` in the Vendor zone. The RTAC is
`10.30.30.20` in OT Operations. Weak Baseline permits direct vendor SSH
(TCP 22) and HTTPS (TCP 443), along with broader OT access.

Brokered Vendor Access removes direct Vendor-to-OT and Vendor-to-Field
allows. It admits only the vendor desktop to Keycloak (`10.25.25.10:8080`)
and Guacamole (`10.25.25.20:8080`) in the Secure Remote Access DMZ.
Use the vendor desktop browser, not the host browser or portal proxy,
to reach those addresses.

The session path is browser → Guacamole → guacd → Ziti router →
OT overlay host → RTAC SSH. The OT host (`10.30.30.60`) dials out to
the controller (`10.25.25.40:1280`) and router (`10.25.25.50:3022`).
No vendor-to-OT inbound allowance is required.

The US feeder simulation, RTAC source pins, intra-OT communication and GPS
time synchronization are retained. The L4 source pin enforces; DPI detects
and logs. Management on the reserved management network remains reachable
under either policy.

Lesson 1.1 observes only the weak direct path. An open TCP port proves
reachability, not successful authentication, MFA or a recorded session.
