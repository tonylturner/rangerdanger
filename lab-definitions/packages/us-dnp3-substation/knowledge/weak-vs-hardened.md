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
- Only the RTAC (10.30.30.20) can reach field devices on Modbus TCP
- Only the RTAC can reach field devices on DNP3 TCP
- Other OT hosts use the RTAC monitoring path rather than direct field access

Even if an attacker compromises the HMI or an enterprise host, they cannot directly manipulate field devices.

**The L4 source pin enforces; DPI detects and logs.**

### The Progressive Learning Model

The exercises follow a deliberate sequence:

1. Run attacks against the **weak baseline** and successfully compromise field devices from the enterprise zone
2. Observe the impact on the simulated power system via the HMI
3. Apply Your Plan or the **hardened policy**
4. Repeat the same tests. Selected closures deny; deferred paths remain reachable
5. Inspect the firewall logs to see the blocked connection attempts

This direct comparison builds intuition for why segmentation matters and exactly which rules provide the protection.

### Partial plans

Apply Your Plan compiles selected actions. Deferring an enforceable action retains its Weak exposure. An empty or planning-only plan is valid and leaves Weak exposures open; budget overrun is a warning only. Required operational flows remain. Plan proof checks that plan, while Hardened acceptance requires 8/8 authorized and 13/13 unauthorized denied, zero skipped, stable hash, capture and healthy HMI. Keep both results and the package revision, action IDs and policy hash.
