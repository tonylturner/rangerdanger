A switching order is a controlled sequence with prerequisites, actions, observations and stop conditions. Its purpose is not merely to reach a final diagram. It prevents an apparently helpful operation from energising a fault, paralleling live supplies or removing another customer's supply. In the range, write the expected electrical consequence before sending each command, then compare it with fresh observations.

### Normal configuration and the open point

CB1 and RMU1 normally supply S1 and S2; RMU2 is open between S2 and S3. F2 independently supplies S3 from the primary bus. This normally-open tie permits alternative feeding without operating the ring continuously closed. See the [feeder model](#eu-mv-feeder) for the fixed source/section measurement names.

A close with both sides live is refused by the breaker and RMUs. A refused live-live close demonstrates a device interlock, not a firewall denial. If a race nevertheless produces a closed ring, OpenDSS solves the resulting mesh; it does not invent an error state to enforce the teaching diagram.

### Distinguish S1 and S2 faults

An S1 fault causes CB1 to carry fault current and trip. RMU1 does not lie on that source-to-fault path, so its fault-passage latch stays clear. After the trip, S2 can be isolated from S1 by opening RMU1, then restored from S3 by closing RMU2 when the measurements and interlocks permit it.

An S2 fault initially causes both CB1 and RMU1 to carry fault current. CB1 trips and RMU1 latches fault passage. Opening RMU1 separates the first feeder, but does **not** remove a fault on S2. Closing RMU2 into that remaining fault brings F2 onto the fault path, trips its latched head protection and can lose S3 as well. This is why “breaker open” is not sufficient restoration evidence.

Use authored lab fault steps rather than inventing direct management commands. The process actions distinguish fault removal (`clear_fault`) from reclosing the F2 head (`reset_f2`). Reclosing F2 with its fault path still connected causes another trip on the next solve.

### A defensible observation sequence

1. Record the starting policy, CC epoch, device positions, quality and section voltages.
2. After the fault, confirm breaker trip and inspect fault-passage indications. Wait for valid post-trip measurements rather than opening an RMU immediately on an invalid snapshot.
3. Before opening a way, confirm remote authority and valid measurements with no fault current. Before closing, check earth open and the source/section live states.
4. Send one [select-before-operate command](#iec104-sbo-commands), retain its result and obtain fresh matching feedback.
5. Recheck voltages and unintended consequences, including S3 voltage at IOA 2203. Record a stop condition if expected feedback or measurement validity is missing.

The portal's physical process section can explain a failed hypothesis, but it must not silently replace the CC evidence an operator lacked. A restored-looking physical feeder during lost telecontrol is still an observation outage.

### Limits

This is a training switching order, not authority to operate real equipment. There is no crew dispatch, safety document system, full earth-switch/key interlocking or protection-selectivity study. F2 is process state without a controllable IEC 104 point. The [RMU model](#eu-rmu-switchgear) is one load-break way per device. Full [Reset Lab](#eu-reset-acceptance) bypasses operating interlocks and restores a teaching baseline; it is not a shortcut in an operational restoration sequence.

Implementation: `services/opendss-sim/eu_circuit.py`, `eu_controls.py`; `services/devices/fielddevice/switch.go`.
