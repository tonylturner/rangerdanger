A ring main unit provides compact medium-voltage switching at a distribution connection. Ring operation offers an alternative supply route, but networks commonly run with a normally-open point so that normal operation remains radial. Opening and closing load-break ways, indicating fault passage and applying earths are different functions. Treating them as interchangeable is a dangerous operating assumption.

### What the two simulated RMUs represent

The range models one motor-operated load-break way in each RMU, not every compartment of a commercial switchboard. `rmu-1` is normally closed at `10.40.40.21:502`; `rmu-2` is the normally-open tie at `10.40.40.22:502`. Both use Modbus unit 1 and begin remote-enabled, earth open and fault-passage clear.

RMU1 lies between S1 and S2. RMU2 lies between S3 and S2. Both devices' **section** side is S2; their source sides are S1 and S3 respectively. Their voltage names remain fixed when supply direction changes. This matters when a switching order restores S2 from the second feeder rather than from CB1.

On receipt of `fault_current: true`, an RMU latches `fault_passage`. This says the way carried fault current during a solve. It does not trip the way, locate the fault uniquely or assert that fault current still flows. Latching advances the truth revision and temporarily invalidates measurements until a matching solve arrives.

### Authority and interlocks

A remote HR0 write requests open with 1 or close with 2. The shared device model checks authority and electrical interlocks atomically:

- Local mode refuses remote operations.
- Close refuses an applied earth, invalid measurements (`no_measurement`), or two live sides (`live_live`).
- Open refuses invalid measurements or measured fault current. A load-break way cannot interrupt fault current and is not permitted to open blind.

A refusal becomes a Modbus exception 04 with a device audit entry. Restoring communications does not waive these conditions.

There is no remote telecontrol earth command. Authored simulation actions can set local/remote, close/open the earth, reset fault passage or reset the device. `earth_close` requires the way open, valid measurements and a dead section. This is a simplified interlock demonstration, not a complete field safety procedure.

### Read the evidence

The input register block is zero-based IR0 through IR12. IR0 holds position. IR1 bits 0, 1 and 2 are remote, earth and latched fault passage; bit 3 is current fault-current indication. IR2 reports measurement validity. Current starts at IR3, section voltage at IR5 and source voltage at IR11, each floating-point value occupying two high-word-first registers.

Through IEC 104, RMU1 position is IOA 1101 and RMU2 position 1201. Their associated remote/earth/fault-passage points occupy 1102–1104 and 1202–1204. Compare these with both side voltages before concluding that a close is feasible. The [Modbus article](#eu-southbound-modbus) explains polling and quality; [switching orders](#eu-rmu-switching-orders) applies the indications to restoration.

### Limits

There is no model of a complete multi-way RMU, mechanical key exchange, visible isolation, test-for-dead equipment or work permits. The simulator's valid dead-section indication is not a real-world proving-dead procedure. Reset bypasses interlocks, so it must never be presented as a normal switching operation. A fault-passage reset erases an indication, not the underlying process fault. Save evidence before using it.

Implementation: `services/devices/fielddevice/switch.go`, `measurements.go`, `actions.go`; `services/devices/modbus/registers.go`.
