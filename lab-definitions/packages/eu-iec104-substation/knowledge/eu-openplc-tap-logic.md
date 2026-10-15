A PLC controller needs more than a comparison between voltage and setpoint. It must establish input freshness, respect operating authority, suppress repeated requests and recover safely from restart. A successful Modbus write proves transport acceptance; it does not necessarily prove that the actuator accepted a new movement.

### The seeded controller

`plc-1` runs OpenPLC in the station zone at `10.30.30.30`. The engineering UI is TCP/8080; its Modbus slave is TCP/502. The UI is also reachable by the platform proxy on management address `10.99.99.30:8080`. The PLC has neither a field-network nor a process-network interface: its cross-zone master traffic goes through the firewall.

The image bakes in `tap-control.st` and the OLTC slave-device configuration. Startup checks the seed, compiles it, verifies persisted program and device settings, records the seed hash and checks that the runtime heartbeat advances. A web login page alone is not proof that the correct program is executing.

The ST task runs every 50 ms. Its Modbus master reads OLTC input registers 0–14 into `%IW100` onward and writes one register, HR1, from `%QW100`, using FC16. Master polling is configured at 100 ms. The target is `10.40.40.23:502`, unit 1.

### Validity, permission and arming

Input validity requires an observed change of the OLTC heartbeat, no two-second heartbeat timeout, IR2 equal to 1 and energisation. Permission additionally requires remote, auto, idle and not blocked. Retained input-register values are not proof that the field device is still communicating.

Voltage below 19800 V requests raise; above 20200 V requests lower. The demand must persist in the same direction for ten seconds. The PLC encodes an adjacent absolute target as `100 + completed tap ± 1`, within −8…+8. Zero is written when there is no demand or control is inhibited. A pending request is cleared on completion or inhibition; a new step needs a new delay.

The OLTC requires zero before accepting a nonzero request after reset or entry into remote+auto. Generation-change and unarmed-result handling in the PLC help re-establish that handshake. A well-formed HR1 request can be acknowledged but ignored by the device; inspect IR13 or `plc_request_result` rather than treating every echo as movement.

### Observe the status block

The RTU reads PLC holding registers 1024–1029 using FC3:

- 1024: running, input-valid and control-permitted flag bits.
- 1025: setpoint, 20000 V; CC IOA 2401 scales it to kV.
- 1026: deadband, 10 tenths-percent; CC IOA 2402 shows percent.
- 1027: delay remaining in seconds, rounded up; CC IOA 2403.
- 1028: last demand, 0 none / 1 lower / 2 raise.
- 1029: wrapping heartbeat, advanced every 500 ms.

CC running and input-valid points are IOAs 1401 and 1402. If the PLC heartbeat stops changing for three seconds, the RTU marks every PLC point invalid with stale reason `heartbeat`, even if a Modbus listener still answers. Compare status, [tap telemetry](#eu-iec104-step-telemetry) and [voltage evidence](#eu-voltage-regulation), not merely container health.

### Limits

This is a single seeded control loop, not a safety PLC, full substation automation system or independent protective relay. The web engineering surface can change control behaviour and must be treated as maintenance authority. RTU-to-PLC polling is same-subnet station traffic, so the range does not claim that the inter-zone firewall enforces that exchange.

Implementation: package `seeds/openplc/README.md`, `tap-control.st`, `install-seed.py` and `oltc.json`; `protocols/points.json`.
