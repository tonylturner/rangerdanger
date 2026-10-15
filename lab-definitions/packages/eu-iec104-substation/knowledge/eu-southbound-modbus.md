A protocol gateway acquires device data in one representation and publishes it in another. In this range, Modbus TCP provides register access southbound while IEC 104 provides station telemetry northbound. A good northbound connection does not prove fresh register acquisition, and a successful register read does not prove that the underlying electrical measurement is valid.

### Poll blocks and encoding

All configured devices use TCP/502, unit 1 and zero-based addresses. The RTU's schema-2 map declares one contiguous poll block per device:

| Device | Endpoint | Read |
| --- | --- | --- |
| `feeder-breaker` | 10.40.40.20 | FC4 input 0–10 |
| `rmu-1` | 10.40.40.21 | FC4 input 0–12 |
| `rmu-2` | 10.40.40.22 | FC4 input 0–12 |
| `oltc-1` | 10.40.40.23 | FC4 input 0–14 |
| `plc-1` | 10.30.30.30 | FC3 holding 1024–1029 |

Each poll period is 500 ms, device timeout 1000 ms and device stale threshold 3000 ms. Each device has an independent worker, so a dropped connection to one should not hold all peers behind its timeout. Connect attempts are capped at 250 ms and reconnect backoff is bounded by the configured 2000 ms interval.

Floating-point and unsigned 32-bit values use two registers, high word first. Signed tap position uses i16. Do not apply a human-facing 30001/40001 offset to a tool already configured for zero-based addressing; an off-by-one read can decode plausible but unrelated data.

### Registers are typed by device

For breaker and RMUs, IR0 is position. Their IR1 flag meanings differ: the breaker includes lockout, current fault indication and protection trip, while an RMU includes earth, latched fault passage and current fault indication. IR2 is measurement-valid. Current and section voltage start at IR3 and IR5. RMUs add source voltage at IR11.

The OLTC instead uses IR0 for signed completed tap, IR1 for authority/mode/motion/limits/block/energisation, and IR3/IR5 for bus voltage/current. IR11 is target tap, IR12 integer volts, IR13 PLC request result and IR14 heartbeat. Reusing a breaker decoder for the OLTC silently swaps meanings.

### Writes and freshness

RTU commands use FC6. Switch HR0 accepts 1 open/2 close; OLTC HR0 accepts 1 lower/2 raise and HR2 accepts 1 manual/2 auto. The PLC separately uses FC16, quantity one, on OLTC HR1 for absolute requests. Device authority and interlocks remain authoritative even when the firewall permits Modbus.

Electrical measurements expire three seconds after delivery by the solver. Reading the register block successfully cannot refresh that validity. For the PLC, an unchanged HR1029 heartbeat for three seconds invalidates every mapped PLC point even if the Modbus server is still reachable. The [quality article](#iec104-gi-cot-quality) explains the resulting IEC 104 flags.

### Observe, then interpret

Use an authorised source and [mbpoll recipe](#eu-tool-mbpoll) to read the exact table, start, count and unit. Correlate the capture's function code and response with the register decoder and CC point. For command tests, retain exception or echo plus fresh feedback and device audit; a timeout is not a negative device decision.

### Limits

Modbus here provides no cryptographic user identity, and the improved firewall does not distinguish read from write function codes. RTU-to-PLC reads are same-subnet traffic, not firewall-enforced inter-zone operations. A permitted trusted source can still misuse writes; [authority checks](#eu-control-authority) and operating controls remain necessary.

Implementation: package `protocols/points.json`; `services/devices/modbus/registers.go`; `services/iec104/src/rtu.c` and `modbus.c`.
