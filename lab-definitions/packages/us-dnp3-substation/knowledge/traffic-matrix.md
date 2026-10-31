A traffic matrix documents all expected communication flows between zones and devices in an OT network. For each flow, you record the source, destination, protocol, port, direction, frequency, and purpose. Creating an accurate traffic matrix is the essential first step in designing firewall rules, because you cannot write effective allow rules without knowing what legitimate traffic looks like.

### Baseline vs. Exercise Traffic

The lab distinguishes between two categories:

**Baseline traffic** is the normal operational communication that runs continuously:
- RTAC polling field devices via HTTP
- HMI fetching aggregated state from the RTAC
- Physics engine exchanging data with the RTAC

This represents the legitimate communication that the firewall must always permit.

**Exercise traffic** is the additional communication generated during attack steps:
- Modbus writes from the Kali box to field devices
- DNP3 commands from unauthorized sources
- Port scans from the enterprise zone into field device zone

### Using the Matrix for Anomaly Detection

When reviewing containd events or packet captures, compare observed traffic against the matrix. For example, if the matrix shows that only the RTAC (10.30.30.20) should send Modbus traffic to the relay (10.40.40.20), then any Modbus traffic from 10.10.10.50 (Kali) to 10.40.40.20 is immediately suspicious.

### From Matrix to Firewall Rules

The traffic matrix is the specification for the hardened firewall policy. Each row representing a legitimate, necessary flow becomes an explicit allow rule. Everything not in the matrix is denied by default. This approach, called a positive security model or whitelisting, is the recommended practice per IEC 62443 and NERC CIP-005.

You can use the traffic matrix as a reference when reviewing the hardened firewall rules to verify that each rule corresponds to a documented legitimate flow.

### In a Real Engagement

In a real substation, building the traffic matrix is often the most time-consuming step. It requires interviewing operations engineers, reviewing relay settings, understanding SCADA polling configurations, and capturing baseline traffic for analysis. The lab provides the matrix as a given, but in practice, developing it is a significant portion of the segmentation assessment work.