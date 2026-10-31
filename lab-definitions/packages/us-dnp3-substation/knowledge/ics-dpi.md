**Deep Packet Inspection (DPI)** reads application payloads, including Modbus function codes and DNP3 operations. It provides protocol context beyond the IP addresses and ports visible to L4 filtering.

### Enforcement in This Workshop

**The L4 source pin enforces; DPI detects and logs.** Under the hardened reference policy, field access is restricted to the RTAC's authorized source address. Do not attribute a failed connection or an unchanged breaker to a DPI event alone.

### Collecting Evidence

1. Confirm the active policy in the [Network Map](/console).
2. Run the lab's prescribed positive and negative checks.
3. Record connectivity results and device state alongside the matching firewall events.

Rule events describe L4 decisions. Protocol events add decoded operation details. A detection record is not proof that a write was prevented. Logging depends on the active rules and event source; an empty strip is not proof of a quiet network.

### See Also

- [How containd Enforces Policy](#how-containd-enforces-policy)
- [Reading the Firewall Events Strip](#live-dpi-events-strip)
- [Modbus TCP](#modbus-tcp) and [DNP3](#dnp3)
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened)
