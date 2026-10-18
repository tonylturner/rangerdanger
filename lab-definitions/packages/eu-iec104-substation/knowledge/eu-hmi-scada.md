An HMI (human-machine interface) is the operator-facing display and control surface. SCADA (supervisory control and data acquisition) describes the wider system that gathers remote observations and supports supervisory operation; it can include communications, data handling, alarms and history as well as an HMI. A DCS (distributed control system) describes an architecture in which control is distributed among controllers close to a process. These terms overlap in practice: a screen alone does not tell you which system acquired its data or issued its commands.

### The EU range's Control Centre

In this package, the Control Centre (CC) is the IEC 104 client at `10.60.60.20`; it receives monitored points from the RTU at `10.30.30.20:2404`. The RTU separately acquires field values over southbound Modbus TCP. The portal's `/substation` page selects the EU Control Centre view for this package. It presents CC observations and supervisory controls; it is not another controller between the CC and the equipment. See the [telecontrol path](#eu-telecontrol-architecture) and [node map](#eu-range-node-map) for the actors and network boundaries.

The view orients an operator with:

- **Control Centre** and **Physical System** are separate views.
- A compact observation-status line above the feeder schematic: IEC 104 link state, current-connection GI completion time (UTC), and **Points current N/M**. **Interrogate station** requests an operator GI (on-connect and 15-minute periodic GI remain automatic). Detailed **Link diagnostics** stay collapsed below the records.
- A feeder one-line built from CC-monitored points. It shows breaker and RMU positions, section voltages, and OLTC telemetry; the CB1 VT is on the line side, while S1 voltage is from RMU1's source side. F2 is selectable with four indication-only points: position, protection trip, current and MW. It is an alternative feeder from the same T1 / bus20, not an independent source. The 110 kV source remains unobserved. No F2 supervisory controls are provided; missing or stale F2 observations leave its path unknown.
- A selected-device faceplate with identity and select-before-operate controls for CB1, RMUs and OLTC before secondary telemetry rows. Abnormal-condition detail follows the schematic and faceplate, rather than preceding the one-line.
- **Alarms**, **Events**, **Points** and **Trends** tabs. Alarms are active conditions derived from CC points, not latched/acknowledged alarm state; Events uses the bounded CC journal, not a durable SOE archive. Points shows stable kind, last received ASDU, value, quality, stale reason, source/receipt times and COT. Receipt age is provenance, not status expiry; floats use the reporting watchdog. Trends plots archived CC observations.

### Reading observation status

The current inventory in `protocols/points.json` contains **42 monitored points** across six polled devices, including F2's four points. The status line counts the inventory returned in the operator snapshot, not a fixed display total. N counts points with a value and no non-topical flag or acquisition-stale reason, but is zero unless the API is available, the link is started and GI has completed on this connection. M is the snapshot's point count. A GI completed before the current connection cannot establish it; a later periodic or operator GI does not revoke that connection's completed GI.

The shared annunciator remains visible for non-OK states above both **Control Centre** and **Physical System**, independently of the selected records tab:

| View state | Meaning |
| --- | --- |
| `unavailable` | Operator API unavailable; even a cached answer cannot establish current observations. |
| `not-established` | No snapshot yet, or a non-started link with no received point evidence or completed GI. |
| `lost` | Link no longer started after observation evidence existed: **SCADA VIEW LOST — retained values are not current**. |
| `recovering` | Link started but no completed GI for this connection: **VIEW RECOVERING — GI/GOOD observations pending**. |
| `degraded` | Current-connection GI complete, but inventory empty or one or more observations not current; the banner reports the affected count and devices. |
| `ok` | Link started, current-connection GI complete and all returned inventory observations current; no warning text. |

“Current” is not synonymous with electrically valid or GOOD. A null value, NT, or a stale reason of `link_lost`, `device_unreachable`, `heartbeat`, `not_updated` or `never_received` makes an observation not current. IV alone can mean an electrically undefined measurement without loss of view. The abnormal summary separately reports non-GOOD floating-point measurements as observation uncertainty; **No alarms in GOOD observations** is not an all-clear for uncertain data. See [GI and quality](#iec104-gi-cot-quality) for the underlying flags and freshness rules.

When the RTU link is lost, the CC retains its last values and marks their quality stale; the display does not turn an old position into a current observation. This view warning is not a per-point process alarm or an acknowledgement engine. Interpret command progress together with fresh feedback, not just a button result; see the [SBO evidence rules](#iec104-sbo-commands).

### Operator observation is not physical truth

The same portal page also has a student-facing [**Physical System** view](/substation?view=physical-system). It reads field-device state and process-solver results through management endpoints, outside the IEC 104 observation path. It presents an operational overview, physical-versus-CC agreement, the same feeder one-line with simulation observations, expandable equipment cards, and advanced diagnostics with device audit evidence. SCADA data quality and physical agreement are separate indicators; usable IEC observations can still disagree with the simulation. Device selection carries across the two views. These are useful for comparing what the simulated equipment is doing with what the CC last received, but they do not populate or repair the CC cache. A successful management read is not proof that the Control Centre's IEC 104 path worked. Keep `/api/operator/state` and `/api/field/state` evidence distinct, as described in the [operator and lab API guide](#eu-rtu-control-centre-apis).

The historian trend is another distinct view of archived CC observations. The CC publishes its own state to the historian; historian data does not provide an independent field measurement or fill a gap in CC telemetry. Preserve the quality and timing context when comparing it with the [historian record](#eu-historian).

### Limits

This is a training view of the package's modeled feeder and IEC 104 point map, not a complete production HMI, SCADA suite or DCS. The CC one-line only displays IEC-mapped observations, and the **Physical System** view is deliberately separate from operator knowledge. A stale or missing point is not proof of a device position; a fresh CC point is not a complete view of uninstrumented equipment. Nor does the display replace local protection: the simulated breaker protection path is separate from the CC-to-RTU link ([protection behavior](#eu-feeder-breaker-protection)).

Implementation: `frontend/components/control-centre-view.tsx`, `control-centre-one-line.tsx`, `feeder-one-line.tsx`, `physical-system.tsx`; `services/iec104/src/cc_state.c`; package `protocols/points.json`.
