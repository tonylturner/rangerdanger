Voltage regulation is a feedback problem with finite authority. A controller compares a measured voltage with a target, waits through short disturbances and requests an actuator step only if the deviation persists. Deadband avoids unnecessary operations; delay avoids chasing every fluctuation. Mechanical limits mean that correct control can still leave an unacceptable voltage when the upstream condition is too severe.

### The implemented operating band

The seeded PLC regulates the primary LV bus toward 20400 V, with a ±204 V band (±1%): 20.196–20.604 kV. Outside-band demand must persist in the same direction for 30 s while control is permitted. Low voltage requests raise; high voltage requests lower. After a step settles, a new delay is required. The tap changer's two-second movement and 1.25% ratio steps on the 110 kV winding (asymmetric MV range +11.1% / −9.1%) are separate from that delay.

Automatic control requires valid input, remote authority, auto mode, no block and no motion. Inhibition or no demand writes zero rather than retaining a nonzero request. The device also enforces adjacent-target and arming checks, so the PLC is not the only layer guarding movement. Read [tap logic](#eu-openplc-tap-logic) for the register handshake.

### Expected cases, not tuned targets

The package's recorded static [reference cases](#eu-process-reference-cases) show:

| Condition | Completed tap | Bus voltage, approximately |
| --- | --- | --- |
| Nominal load, source 1.00 p.u., initial | 0 | 19.9135 kV |
| Nominal load, settled | +2 | 20.4292 kV |
| Load multiplier 3.0, before correction | 0 | 19.6848 kV |
| Same load after two raises | +2 | 20.2026 kV |
| Source 0.90 p.u., exhausted raise range | +8 | 19.9125 kV |
| Source 1.10 p.u., settled correction | −6 | 20.3820 kV |

These cases start at tap zero with the daily profile disabled; load ×3 starting from a settled +2 may already be in band. Re-enable the Europe/Berlin daily profile after deterministic checks; reset leaves its enabled state unchanged. These are solved circuit values, not promises about the exact time a live PLC will reach them. In particular, the high-source case settles in band before reaching −8. Teaching that both source extremes must hit their respective limits would contradict the circuit.

### Observe regulation as a sequence

Start from a known baseline and save CC state, physical process state and a historian window. Use the authored load/source step, not an ad-hoc solver call from a student host. Confirm the disturbance actually changed `load_multiplier` or `source_pu` in process evidence. Then track `oltc-1-bus-kv`, `oltc-1-tap`, `oltc-1-auto`, PLC input-valid and configured delay. IOA 2403 is the configured 30 s, not a countdown; prove elapsed delay with request/motion timestamps.

A defensible result shows sustained error, expiry of the delay, motion, completed tap and a new voltage solve. If regulation stops, inspect authority, block, motion, input validity and limits before blaming network reachability. If only the CC feed is lost, physical regulation may continue while its operator observations become stale. The [historian](#eu-historian) will preserve stale CC observations rather than independently discovering the true voltage.

### Limits and failure interpretation

This loop is not coordinated volt/VAR control, line-drop compensation, distributed-generation control or a transformer loading study. The controlled quantity is the primary LV bus, not every customer voltage. A tap at +8 with undervoltage is an honest limit case. Rapid repeated movement, by contrast, requires evidence about delay, input provenance and requests; a final low voltage alone does not prove “runaway taps.”

The static reference artifact cannot prove PLC scan timing, conduit enforcement or actual command completion. Retain fresh protocol feedback for those claims. Do not fill missing CC data from the solver, interpolate invalid measurements as valid, or interpret a null value as zero volts.

Implementation: package `seeds/openplc/tap-control.st`, `process/reference-cases.json`; `services/devices/fielddevice/oltc.go`.
