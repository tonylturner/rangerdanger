DNP3 (Distributed Network Protocol version 3) is the dominant SCADA protocol in North American electric utilities. Developed in the 1990s based on IEC 60870-5, it was designed specifically for utility communication: reliable delivery over unreliable links, time-stamped event reporting, and a data model that maps well to power system objects. It runs over both serial and TCP/IP, with TCP typically using **port 20000**.

### Master-Outstation Architecture

DNP3 uses a master-outstation model. The master (RTAC or SCADA system) initiates requests to outstations (field devices). Key operations:

- **Read** (FC01): retrieve data objects from the outstation
- **Direct Operate** (FC05): command an action (open breaker, change setpoint) in a single message, with the outstation returning a status response
- **Direct Operate No Ack** (FC06): the same single-message control write, but the outstation sends **no response**. Attackers favor it because it is quieter (no reply packet to notice) and because a naive function-code filter that only lists FC05 will miss it - which is exactly why a correct DPI rule must cover both FC05 and FC06
- **Select-Before-Operate** (FC03/FC04): two-step confirmation for safety-critical operations. The master selects the control point, the outstation confirms readiness, then the master issues the operate. SBO is recommended for critical controls but not universally deployed. The lab's `dnp3cmd` supports all three control modes (`-sbo` for Select-Before-Operate, `-no-ack` for FC06, and Direct Operate by default).

### Security (or Lack Thereof)

Standard DNP3 has no built-in authentication or encryption. DNP3 Secure Authentication (SA, IEEE 1815-2012) adds challenge-response authentication, but adoption remains limited. Most fielded implementations accept commands from any source that can establish a TCP connection and send properly formatted messages. This makes DNP3 outstations vulnerable to command injection from any host with network access to port 20000.

### Protocol Layers

DNP3 is more complex than Modbus, with three layers:

1. **Data link layer**: CRC error checking and frame synchronization
2. **Transport layer**: message reassembly for large data
3. **Application layer**: SCADA data objects organized by group and variation

This complexity makes DPI harder than Modbus, but the containd NGFW implements application-layer inspection to filter by function code.

### Lab Implementation

Each field device runs a DNP3 TCP outstation on port 20000 via the `dnp3go` library:

| Device | IP | Outstation Address |
|--------|----|--------------------|
| Relay | 10.40.40.20 | **1** |
| Recloser | 10.40.40.21 | **2** |
| Regulator | 10.40.40.22 | **3** |
| RTAC | 10.30.30.20 | **10** (read-only) |

You can use DNP3 tools on the Kali box to send Direct Operate commands, tripping breakers and changing tap positions. The hardened firewall policy restricts DNP3 command traffic to the RTAC only.

### See Also

- [What is ICS DPI?](#ics-dpi) - the DNP3 Direct Operate restriction (FC05 and FC06 No Ack) is what containd's DPI rule for DNP3 enforces
- [`dnp3poll` & `dnp3cmd` - DNP3 Tools](#tool-dnp3) - the CLI tools used in the labs
- [IEC 61850 and GOOSE](#iec-61850-goose) - the modern standard sometimes deployed alongside or instead of DNP3
- [The OT Kill Chain](#ot-kill-chain) - the Industroyer historical example mirrors the Lab 2.3 DNP3 attack