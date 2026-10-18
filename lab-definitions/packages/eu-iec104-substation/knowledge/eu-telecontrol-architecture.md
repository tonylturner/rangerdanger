Telecontrol separates an operator's request from the equipment that acts on it. A station gateway translates between protocols and keeps an observation cache, while field devices retain local authority and interlocks. Diagnosing a failure therefore requires asking which hop failed and which observer produced each value, rather than treating the whole system as one connection.

### The northbound and southbound paths

The Control Centre (CC) is an IEC 104 client at `10.60.60.20`. It opens the northbound connection to RTU `10.30.30.20:2404`. The RTU is the controlled station/server. It independently polls the southbound devices using Modbus TCP, unit 1, on TCP/502:

- Breaker `10.40.40.20`.
- RMU1 `10.40.40.21` and RMU2 `10.40.40.22`.
- OLTC `10.40.40.23`.
- PLC status at `10.30.30.30`.

The first four cross the Station-to-Field boundary. RTU-to-PLC is same-subnet station traffic, not an inter-zone firewall conduit. Automatic PLC-to-OLTC control is another separate cross-zone path. A healthy CC-to-RTU link cannot prove that any of these Modbus exchanges succeeded.

The [point map](#iec104-wire-model) translates register values into IEC 104 points and operator commands back into device writes. A command needs both protocol progress and fresh matching feedback. Local protection can trip the breaker without waiting for either northbound availability or a CC command.

### Observation, physics and lab control

The CC's operational state comes only from IEC 104. The portal's `/substation` page also shows physical device state and process results, obtained by the backend over management endpoints. The **Control Centre** and **Physical System** views are separate and field data is never used to repair the CC cache. The Control Centre’s **Alarms**, **Events**, **Points** and **Trends** tabs show CC-derived evidence; **Interrogate station** requests GI without reconnecting.

On `process_net`, the solver reads device truth and returns solved measurements tagged with their truth revision. This closed loop supplies the electrical simulation and local protection inputs. It is not the RTU's telemetry acquisition path. On management, field-device `sim` endpoints accept authored lab actions and reset. The solver's management API owns faults, load and source changes. These are simulation controls, not hidden operator command routes.

The CC additionally pushes its own observations to the [historian](#eu-historian) in the Operations DMZ. The historian does not reach back to the station to fill gaps. A recording of stale observations remains stale.

### Diagnose each boundary separately

Save portal `GET /api/operator/state` and `/api/field/state` before and after a controlled change. Check the CC link/GI state, point quality and receipt times; compare physical positions, revisions and process delivery separately. Use the CC journal for link and command phases, and a capture for actual IEC 104 or Modbus exchanges inside the attempt window.

If the CC link is lost while physical protection trips, that is an observation outage with continuing local behaviour. If only one Modbus device becomes unreachable, healthy peers should continue to update. If historian ingestion stops while CC observation stays good, investigate the publication conduit rather than the IEC 104 station link.

### Limits

This architecture deliberately uses Modbus southbound; it does not emulate IEC 61850, IEC 101 or IEC 103. Management and process networks support the training implementation. Management isolation includes behavioural discipline, not a claim that every service is physically unable to contact every other management endpoint. Neither SBO nor an established session proves user identity or per-command authorisation. Use [policy comparison](#eu-policy-comparison) to state exactly which source/port boundary the firewall enforces.

Implementation: package `compose.source.yml`, `protocols/points.json`, `process/profile.json`; backend `operator.go` and `field.go`.
