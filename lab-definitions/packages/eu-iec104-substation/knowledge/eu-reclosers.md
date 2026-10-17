**EU scope:** This is general protection-engineering background only. The EU range does not model reclosers or simulate an automatic-reclosing shot sequence.

A recloser is a protective switching device designed to interrupt fault current and automatically attempt to restore a feeder after a temporary fault. Automatic reclosing can limit the duration of an outage when a fault clears after the circuit is de-energised. The device's protection and control settings determine whether, when and how another close is attempted.

### Auto-reclose cycle

A typical cycle opens the circuit on a detected fault, waits for a configured dead time, and makes a close attempt. If the fault remains, the device trips again and may make further attempts up to its configured limit. Exhausting those attempts leads to **lockout**: the device remains open until an authorised restoration action. The number and timing of attempts vary by equipment and protection design; there is no single sequence to assume.

Automatic reclosing is a continuity measure, not proof that a line is safe to approach or that a fault has cleared permanently. A transient fault may no longer be present when the circuit is re-energised. A persistent fault can instead cause repeated trips and ultimately lockout. Operators must interpret indications and follow the applicable switching and safety procedures.

### Protection, authority and security

Protection trips and supervisory operating requests have different roles. Local protection must respond to its configured conditions; telemetry and remote control support observation and authorised operation but do not replace protection engineering. Changes to reclose enablement, sequence or lockout behavior can alter how a feeder responds to its next fault. Such settings should be controlled, reviewed and verified against the intended design. A command acknowledgement alone does not establish that a device moved or that service was restored.

### Distinguish the EU range equipment

The range's CB1 is a feeder breaker with a local protection-trip and lockout model, but it has no simulated auto-reclose shots. Clearing a modeled fault does not clear its lockout; reset and a later close are distinct actions ([breaker protection](#eu-feeder-breaker-protection)). Its ring main units are load-break switchgear: they can latch an indication that fault current passed, but they do not interrupt fault current or act as reclosers ([RMU switchgear](#eu-rmu-switchgear)). The primary OLTC changes transformer ratio for voltage regulation; it is not feeder fault-protection or reclose equipment ([primary OLTC](#eu-primary-oltc)).

These devices are not interchangeable. Do not infer a recloser, an auto-reclose sequence, or a recloser control endpoint from the EU range's breaker, RMUs, telecontrol points or voltage-control model.
