A step-position indication represents discrete actuator progress. It contains a signed position and a transient flag, so the receiver can distinguish a completed setting from motion between settings. Representing a tap changer as an ordinary analogue value loses that distinction; treating the requested destination as the completed position falsely reports success before the mechanism has finished.

### The OLTC point

`oltc-1-tap`, IOA 1301, is a step-position point. Its source is signed input register IR0, with transient taken from IR1 bit 2, the moving flag. The RTU sends M_ST_TB_1 spontaneously, including station observation time, and M_ST_NA_1 during GI. Both carry the position/transient semantics. Command-return COT 11 also uses timed M_ST_TB_1. The Points tab’s `asdu` is the last received type, so a GI can replace a timed type/tag with M_ST_NA_1 and no source time. Quiet step status does not age out.

The native CC schema-3 value is an object such as `{"value":-3,"transient":true}`, or null if never received. A transient value of −3 means the completed tap is still −3 while movement is in progress; it does not mean the target is −3 or that the step has failed. Physical device state separately exposes `target_tap`.

The actuator takes two seconds per step, retains the old completed tap during motion and accepts no queued movement. The mechanical range is −8…+8. IEC 104's signed seven-bit VTI representation has a wider −64…63 range; the map loader/RTU checks representation limits separately from the actuator's operating limits. An out-of-representation source becomes invalid, not a wrapped plausible tap.

### Regulating-step and mode commands

`oltc-1-step`, IOA 3301, uses C_RC_NA_1 with select-before-operate. RCS 1 lowers and 2 raises. The target is Modbus HR0. This is a relative one-step request, unlike the PLC's absolute HR1 encoding.

The RTU obtains a fresh good completed start value after select. For completion it requires fresh feedback equal to that start ±1, transient false and good quality, within the five-second feedback budget. A write echo or transient indication alone is insufficient. The CC independently expects matching COT 11 feedback after execution confirmation plus positive termination.

`oltc-1-mode`, IOA 3302, is C_SC_NA_1: 1 auto and 0 manual, mapped to HR2 2/1. Feedback is auto IOA 1303. Mode can change during motion, but the accepted step finishes. For manual steps, select manual first and verify its feedback; auto enabled is not permission for an operator HR0 step.

### Observe a complete motion

Record the starting tap, auto state and voltage from `GET /api/operator/state`. In a controlled manual exercise, save the command result and journal cursor, request one step and inspect monitor output for transient onset followed by settled position. Compare the physical device revision and post-motion process solve separately. See [station time](#eu-station-clock) when comparing source and receipt timestamps.

The [historian](#eu-historian) stores the step object unchanged. Each timed step report gets its own snapshot, but a failed or dropped push is never replayed, so absence of a transient sample in a trend is not proof that there was no motion. Use protocol capture and device audit when the precise sequence matters.

### Limits

Step telemetry is not a mechanical travel recorder, contact-position trace or SOE archive. A transient flag does not establish the health of every component inside a real OLTC. An ambiguous result must not trigger an automatic retry: a repeated relative raise can create an extra tap change if the first already completed. Reconcile fresh state and operating intent before another [SBO attempt](#iec104-sbo-commands).

Implementation: package `protocols/points.json`; `services/iec104/internal/rtu/`, `services/iec104/internal/controlcentre/`; `services/devices/fielddevice/oltc.go`.
