An HMI (human-machine interface) is the operator-facing display and control surface. SCADA (supervisory control and data acquisition) describes the wider system that gathers remote observations and supports supervisory operation; it can include communications, data handling, alarms and history as well as an HMI. A DCS (distributed control system) describes an architecture in which control is distributed among controllers close to a process. These terms overlap in practice: a screen alone does not tell you which system acquired its data or issued its commands.

### The EU range's Control Centre

In this package, the Control Centre (CC) is the IEC 104 client at `10.60.60.20`; it receives monitored points from the RTU at `10.30.30.20:2404`. The RTU separately acquires field values over southbound Modbus TCP. The portal's `/substation` page selects the EU Control Centre view for this package. It presents CC observations and supervisory controls; it is not another controller between the CC and the equipment. See the [telecontrol path](#eu-telecontrol-architecture) and [node map](#eu-range-node-map) for the actors and network boundaries.

The view orients an operator with:

- **Control Centre** and **Field truth** are separate views.
- IEC 104 link and general-interrogation status, with **Interrogate station** for an operator GI (on-connect and 15-minute periodic GI remain automatic).
- A feeder one-line built from CC-monitored points. It shows breaker and RMU positions, section voltages, and OLTC telemetry; the CB1 VT is on the line side, while S1 voltage is from RMU1's source side. Parts with no CC point, including the F2 head and 110 kV source, are explicitly marked unobserved.
- A selected-device faceplate with select-before-operate controls for the breaker, RMUs and OLTC.
- **Alarms**, **Events**, **Points** and **Trends** tabs. Alarms are active conditions derived from CC points, not latched/acknowledged alarm state; Events uses the bounded CC journal, not a durable SOE archive. Points shows stable kind, last received ASDU, value, quality, stale reason, source/receipt times and COT. Receipt age is provenance, not status expiry; floats use the reporting watchdog. Trends plots archived CC observations.

When the RTU link is lost, the CC retains its last values and marks their quality stale; the display does not turn an old position into a current observation. Interpret command progress together with fresh feedback, not just a button result; see the [SBO evidence rules](#iec104-sbo-commands).

### Operator observation is not physical truth

The same portal page also has a separate **Field truth** view. It reads field-device state and process-solver results through management endpoints, outside the IEC 104 observation path. That view shows device and solver state, a field-versus-CC comparison, and device audit evidence. These are useful for comparing what the simulated equipment is doing with what the CC last received, but they do not populate or repair the CC cache. A successful management read is not proof that the Control Centre's IEC 104 path worked. Keep `/api/operator/state` and `/api/field/state` evidence distinct, as described in the [operator and lab API guide](#eu-rtu-control-centre-apis).

The historian trend is another distinct view of archived CC observations. The CC publishes its own state to the historian; historian data does not provide an independent field measurement or fill a gap in CC telemetry. Preserve the quality and timing context when comparing it with the [historian record](#eu-historian).

### Limits

This is a training view of the package's modeled feeder and IEC 104 point map, not a complete production HMI, SCADA suite or DCS. The one-line only displays mapped observations, and the **Field truth** view is deliberately separate from operator knowledge. A stale or missing point is not proof of a device position; a fresh CC point is not a complete view of uninstrumented equipment. Nor does the display replace local protection: the simulated breaker protection path is separate from the CC-to-RTU link ([protection behavior](#eu-feeder-breaker-protection)).

Implementation: `frontend/components/control-centre-view.tsx`, `control-centre-one-line.tsx`, `physical-feeder.tsx`; `services/iec104/src/cc_state.c`; package `protocols/points.json`.
