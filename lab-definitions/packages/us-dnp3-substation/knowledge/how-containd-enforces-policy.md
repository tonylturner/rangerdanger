This workshop separates enforcement evidence from protocol detection evidence.

### L4 Enforcement

containd compiles the active firewall policy into kernel nftables rules. Those rules evaluate zones, source and destination addresses, ports and connection state. In the hardened reference, field access is source-pinned to the RTAC.

**The L4 source pin enforces; DPI detects and logs.** Protocol decoding adds context to observations; it is not the workshop's proof that a write was prevented.

### Policy Changes

The Guided track uses **Apply Hardened** to load the reference policy. On the Advanced track, author and **Commit** a policy in containd, then verify the running policy. A saved candidate alone is not evidence that the running rules changed.

### Events and Evidence

Logged rule decisions and protocol observations feed the [Network Map](/console) event strip via `/api/substation/network-events`. Availability and logging configuration affect what appears. Use the prescribed positive and negative probes plus device state to establish the actual outcome, rather than inferring it from an event or from the absence of events.

### See Also

- [What is ICS DPI?](#ics-dpi)
- [Reading the Firewall Events Strip](#live-dpi-events-strip)
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened)
