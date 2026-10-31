Reclosers are automatic circuit-interrupting devices installed at mid-feeder locations or at the head of lateral taps. Their defining feature is **auto-reclose**: after tripping on a fault, they automatically re-energize the circuit. This matters because 70-80% of distribution faults are transient (tree branches, animal contacts, lightning) and clear themselves once the line is briefly de-energized.

### Shot Sequence

A typical recloser uses a programmed shot sequence. For example:

1. **Fast trip** on initial fault, reclose after short delay
2. If fault persists, **delayed trip** and reclose again
3. After exhausting reclose attempts, **lockout** (stays open, requires manual reset)

This balances service continuity (clearing transient faults automatically) with safety (not repeatedly re-energizing a permanent fault).

### The Stealth Attack: Disabling Auto-Reclose

Disabling the auto-reclose function is a particularly insidious attack because **nothing visibly changes**. The recloser keeps operating normally until the next fault occurs. At that point, it trips and stays open permanently instead of automatically restoring service.

Every transient fault becomes a sustained outage requiring a crew dispatch. If an attacker disables auto-reclose on multiple devices across a feeder during a storm, the result is widespread prolonged outages that overwhelm the utility's restoration process.

### Lab Implementation

The recloser simulator (`recloser-sim`) runs at **10.40.40.21** on `field_net`. It models auto-reclose with shot counting and lockout behavior. The `reclose-enabled` flag can be toggled via:

- **Modbus**: coil write (FC05) on port 502
- **DNP3**: Direct Operate command to outstation address **2** on port 20000

One of the lab exercises demonstrates how an attacker on the enterprise network (10.10.10.50) can reach through the weak firewall policy to disable auto-reclose. The change has no immediate observable effect but degrades grid resilience for the next fault event.