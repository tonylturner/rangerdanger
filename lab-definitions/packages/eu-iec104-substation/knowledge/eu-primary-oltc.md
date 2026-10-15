An on-load tap changer changes a transformer's effective turns ratio while supplying load. It offers discrete voltage correction, not an unlimited source of voltage. Supervisory control must distinguish a requested step, motion in progress and the completed position. A displayed target is not proof that the actuator reached it.

### T1 in this range

The primary transformer is 110/20 kV. Its simulated tap changer, `oltc-1`, is at `10.40.40.23:502`, unit 1. Completed tap ranges from −8 to +8; each positive step raises the solved LV bus voltage by changing the ratio in 1.25% increments. Autonomous OpenDSS RegControl is disabled: the [OpenPLC loop](#eu-openplc-tap-logic), not a hidden solver controller, requests automatic steps.

At reset, the tap is 0, remote authority is enabled, auto is enabled, and the PLC request path is unarmed. Each accepted step takes two seconds. During motion the completed tap remains at its starting value while `target_tap` identifies the destination. There is no motion queue. Completion advances the device revision and requires a fresh solve before measurements become valid again.

### Manual, automatic and blocked are distinct

Manual supervisory stepping uses HR0: 1 lower or 2 raise. It requires remote+manual, idle, not blocked, valid energisation and a target inside the mechanical limits. Refused operating requests return exception 04 and are audited.

HR2 selects manual with 1 and auto with 2, provided the device is remote-enabled. A mode change is allowed during motion; the existing step finishes. Thus switching to manual is not an emergency stop. Blocking prevents acceptance of new steps, rather than cancelling an already accepted motion.

The PLC writes a separate HR1 absolute request: 0 means no request and 100+target encodes taps −8 through +8 as 92 through 108. A syntactically valid write is acknowledged even if the device ignores the requested movement. `plc_request_result` explains the decision. After reset or entry into remote+auto, the device must observe zero on HR1 before accepting a nonzero request. This arming rule prevents a retained request from becoming an unintended new step.

### Observe a complete step

The CC receives `oltc-1-tap` at IOA 1301 as a step-position object, including its transient flag. Remote and auto are IOAs 1302 and 1303; lower/upper limit and blocked are 1304–1306. LV bus voltage is 2301 and current 2302. The command points are 3301 (`oltc-1-step`) and 3302 (`oltc-1-mode`).

For a manual exercise, use the operator panel or [operator API](#eu-rtu-control-centre-apis) to select manual, verify feedback, and request one step. Capture the old completed tap with transient true, followed by the new completed tap with transient false. Compare the voltage change only after good post-motion measurement delivery. The [step telemetry article](#eu-iec104-step-telemetry) explains why the intermediate state is meaningful evidence rather than a display glitch.

### Limits

The model omits diverter-switch contact sequencing, resistor heating, wear, maintenance counters and transformer thermal limits. A healthy control response does not establish those properties. The simulated two-second duration is not a specification for a real tap changer. Reset cancels motion and bypasses operating interlocks, which is appropriate for a reproducible lab baseline but not for normal plant control. Voltage outside the band at a tap limit can be an honest exhausted-control case, not necessarily a controller defect.

Implementation: `services/devices/fielddevice/oltc.go`; `services/opendss-sim/eu_circuit.py`; package `protocols/points.json`.
