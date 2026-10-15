Protection and supervisory control have different jobs. Protection interrupts a dangerous electrical condition without waiting for an operator or a remote communications link. SCADA provides observations and authorised operating requests. Removing SCADA connectivity should not turn a locally protected feeder into an unprotected one; nor should it persuade the operator that old observations are current.

### The range breaker

CB1 is node `breaker-1`, with device id `feeder-breaker`. Its field Modbus endpoint is `10.40.40.20:502`, unit 1. At reset it is closed and remote-enabled, with lockout and protection-trip latches clear. It supplies S1 and, through normally closed RMU1, S2 in the [normal feeder arrangement](#eu-mv-feeder).

The process owns `fault_section`, rather than a fault flag injected into the breaker. On receipt of a valid measurement with `fault_current: true`, a closed breaker opens, sets `lockout` and `protection_tripped`, and records `local_protection_trip` in its audit. The state revision advances. The measurements calculated for the pre-trip revision are no longer valid until the solver delivers a matching post-trip solve.

This trip does not traverse the CC-to-RTU IEC 104 conduit. The process and device continue their exchange on the separate process network during a telecontrol outage. In this simulator, however, protection depends on that measurement exchange: it is not a hardware relay independently sampling a physical CT.

### Clearing a fault is not restoring authority

A fault disappearing does not prove that re-energisation is appropriate. Process action `clear_fault` removes the electrical fault but leaves breaker lockout and protection-trip latches set. Device action `reset_lockout` clears those latches while leaving the breaker open. A later remote close is a distinct operation and still needs valid measurements and permitted conditions.

Remote Modbus HR0 uses 1 for open and 2 for close. A close is refused with lockout, invalid measurements, or both sides live; remote operation also requires remote authority. The refusal is audited. Full device `reset` is different: it restores the initial closed state and bypasses operating interlocks. Use authored lab steps and [Reset Lab](#eu-reset-acceptance), not reset as an operational substitute for a switching order.

### Observe an outage without inventing evidence

Before an authored fault step, save CC state and the breaker audit cursor. Afterwards compare:

1. Physical `position`, `lockout`, `protection_tripped` and the audit's operation, peer and result.
2. Process fault location, solved section voltages and delivery revisions.
3. CC position IOA 1001, protection trip 1002, current 2001 and voltage 2002, including quality and receipt times.

The portal provides `GET /api/operator/state`, `/api/field/state` and `/api/field/audit?after=N`. The field audit proxy is specifically the breaker's audit, not an aggregate of all devices. A CC that is disconnected may retain its previous closed indication while the physical section already shows open. That disagreement is expected evidence of lost observation, not evidence that local protection failed.

### Limits and interpretation

The range does not implement inverse-time curves, protection grading, auto-reclose shots or a complete relay event recorder. A fault-passage indicator on an [RMU](#eu-rmu-switchgear) is not another breaker trip. A completed command also need not prove movement: an already-satisfied open can be accepted as a no-op. Conversely, a lost confirmation does not prove that the breaker stayed still. Preserve command results and fresh feedback together, following [SBO evidence rules](#iec104-sbo-commands).

Implementation: `services/devices/fielddevice/measurements.go`, `switch.go`, `actions.go`; package `protocols/points.json`.
