The RangerDanger lab models a distribution substation with four network zones separated by the containd Next-Generation Firewall (NGFW), plus an unfirewalled physics simulation network.

### Network Zones

| Zone | Subnet | Key Hosts |
|------|--------|-----------|
| Enterprise | 10.10.10.0/24 | Corporate workstation (10.10.10.10), Kali attacker (10.10.10.50) |
| Vendor | 10.20.20.0/24 | Jump box (10.20.20.10), Engineering workstation (10.20.20.20) |
| OT Operations | 10.30.30.0/24 | HMI/FUXA (10.30.30.10), RTAC (10.30.30.20), OpenPLC (10.30.30.30) |
| Field Devices | 10.40.40.0/24 | Relay (10.40.40.20), Recloser (10.40.40.21), Regulator (10.40.40.22) |
| Physics | 10.50.50.0/24 | OpenDSS engine (10.50.50.20), not firewalled |

All inter-zone traffic flows through containd, which acts as the default gateway for every zone.

### Three Protocol Interfaces

The field device simulators are custom Go services that implement three protocol interfaces simultaneously on the same shared state:

1. **HTTP REST** on port 8080: `GET /api/state`, `POST /api/command`, `GET /api/audit`, `GET /api/health`
2. **Modbus TCP** on port 502: function codes FC01/FC03/FC04 for reads, FC05/FC06 for writes
3. **DNP3 TCP** on port 20000: Read (FC01), Direct Operate (FC05), Select-Before-Operate (FC03/FC04)

All three interfaces operate on the same device state. A Modbus write to the relay breaker coil is immediately reflected in HTTP and DNP3 responses. Use whichever protocol suits the exercise.

### The Simulation Loop

The RTAC polls all field devices and the physics engine at regular intervals, aggregating their state into a unified view. The HMI displays this on a one-line diagram showing feeder topology, breaker positions, voltage levels, and device health. The physics engine calculates electrical quantities (voltage, energization) based on current device states, creating a **closed-loop simulation** where opening a breaker actually de-energizes the downstream feeder section.

### Background Traffic

The RTAC polling cycle generates baseline autonomous traffic on the OT and field networks. This is realistic background traffic that you can observe and analyze. When you execute attack exercises, your malicious traffic is superimposed on this baseline. You can use the containd event feed and the HMI to observe both the network-level and process-level effects of your actions.