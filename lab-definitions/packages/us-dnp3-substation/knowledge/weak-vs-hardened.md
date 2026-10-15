The lab provides two firewall configurations representing opposite ends of the segmentation spectrum. Both use the exact same network topology. The only difference is the containd firewall rules.

### Weak Baseline (substation-weak.json)

The weak policy is intentionally permissive, reflecting the reality of many substations where firewalls exist but are configured with broad allow rules. Under this policy:

- The enterprise zone can communicate freely with OT operations and field devices
- The Kali attacker at 10.10.10.50 can directly reach the relay at 10.40.40.20 on Modbus (port 502) and DNP3 (port 20000)
- The vendor zone has similarly broad access
- There is **no protocol-level filtering**, so Modbus writes and DNP3 Direct Operate commands pass through unchallenged

This configuration enables all attack exercises to succeed.

### Hardened Policy (substation-improved.json)

The hardened policy represents the target state after a segmentation improvement project:

- Enterprise zone **cannot reach** the field device zone at all
- Vendor zone has limited access to OT operations for engineering purposes, but **no direct field device access**
- Only the RTAC (10.30.30.20) can send Modbus write commands (FC05, FC06) to field devices
- Only the RTAC can send DNP3 Direct Operate commands to field devices
- Other OT hosts (HMI, PLC) can read from field devices but cannot write

Even if an attacker compromises the HMI or an enterprise host, they cannot directly manipulate field devices.

### The Progressive Learning Model

The exercises follow a deliberate sequence:

1. Run attacks against the **weak baseline** and successfully compromise field devices from the enterprise zone
2. Observe the impact on the simulated power system via the HMI
3. Switch to the **hardened policy**
4. Attempt the same attacks again and watch them fail
5. Inspect the firewall logs to see the blocked connection attempts

This direct comparison builds intuition for why segmentation matters and exactly which rules provide the protection.