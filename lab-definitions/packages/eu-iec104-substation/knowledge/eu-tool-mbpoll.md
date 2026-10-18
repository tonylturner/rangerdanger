`mbpoll` reads or writes Modbus data; it is not inherently a read-only tool. The recipes here deliberately request register reads and never append write values. Kali, engineering and vendor Dockerfiles all explicitly install it. Tool availability does not imply that any of those hosts has an allowed Modbus conduit.

### Read a device's input block

All four field devices use TCP/502 and unit 1. Addresses in the package point map are zero-based. From Kali, these reads should succeed under weak as the **teaching exposure**, then fail on new connections under improved:

```sh
mbpoll -m tcp -a 1 -p 502 -t 3 -0 -r 0 -c 17 -1 10.40.40.20
mbpoll -m tcp -a 1 -p 502 -t 3 -0 -r 0 -c 13 -1 10.40.40.21
mbpoll -m tcp -a 1 -p 502 -t 3 -0 -r 0 -c 13 -1 10.40.40.22
mbpoll -m tcp -a 1 -p 502 -t 3 -0 -r 0 -c 21 -1 10.40.40.23
```

`-t 3` selects input registers (FC4); it does not mean function code 3. `-0` makes the starting reference zero-based, `-r 0` selects IR0, `-c` selects the register count and `-1` makes one poll rather than a continuous loop. The addresses identify breaker, RMU1, RMU2 and OLTC respectively.

Weak policy permits station-to-field Modbus and direct enterprise- and vendor-to-field TCP/502 through `weak-foreign-field-modbus`. This includes Kali, vendor-jump and eng-ws. Improved policy removes that foreign access and narrows station access to the RTU and the PLC's OLTC conduit. Close baseline clients and match fresh denied attempts to logged `deny-unauthorized-modbus` events under improved, recording source, destination and attempt times. Do not add a diagnostic allow, move a container or impersonate a controller to obtain output.

Do not translate these into 30001/40001 display notation and then feed that notation back as an offset. An off-by-one read can produce plausible but entirely wrong evidence.

### Interpret the register block

For breaker and RMUs, IR0 is double-point position: 1 open, 2 closed, with 0 intermediate and 3 indeterminate. IR1 contains flags; their meaning depends on device kind. For an RMU, bits 0–3 mean remote, earth closed, latched fault passage and present FPI pickup at 300 A. IR2 indicates measurement validity.

For the OLTC, IR0 is the completed signed tap, IR11 the target tap and IR1 includes auto and moving. IR12 is bus voltage in volts, with 0 when invalid; consult IR2 before interpreting that zero. IR13 classifies the PLC request, and IR14 is the heartbeat. IR1 bit 5 is effective block and bit 7 is the 866 A overcurrent block. P/Q/PF start at OLTC IR15/17/19 and breaker IR11/13/15.

Current and kV fields are two-register IEEE float32 values, high word first. Event sequence and generation are two-register unsigned integers. Raw word output is useful evidence but is not itself a decoded engineering value; keep the original words and the exact map used to interpret them. A negative tap is a signed 16-bit value, not a huge positive position.

### Read the PLC status block separately

```sh
mbpoll -m tcp -a 1 -p 502 -t 4 -0 -r 1024 -c 6 -1 10.30.30.30
```

This is an FC3 holding-register read request, expected to be denied from Kali under both reference policies. The field teaching exposure does not permit TCP/502 into the station zone. The block contains runtime flags, setpoint volts, deadband in tenths of a percent, configured delay (30 s, not the internal countdown), last demand and heartbeat.

The RTU reads this block from the station subnet. That legitimate same-subnet exchange is not proof of a routed firewall rule.

### Know what a successful read proves

Successful direct field reads from student hosts are expected under weak, not improved. They demonstrate the teaching exposure, not authorized control. Compare the words with legitimate RTU observations and field/process exports to interpret the map. A timeout alone needs matching policy/network evidence; a successful new foreign field connection under improved is a boundary finding to investigate, not the intended success.

Where a legitimate read is observed, its response does not prove the CC received fresh IEC 104 telemetry, that the PLC is executing correctly, or that a prior command caused the current state. Compare changing heartbeats rather than treating “running” as sufficient.

Keep writes inside the prescribed lab command workflow. A permitted TCP/502 connection is not an FC-level authorization boundary. See [southbound Modbus](#eu-southbound-modbus), [control authority](#eu-control-authority) and [port/source rules](#eu-port-source-boundary) for those distinctions.
