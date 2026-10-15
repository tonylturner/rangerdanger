A control action matters because it changes an electrical process, not because a packet counter increased. The EU range links switching, fault location, tap control and observation quality so that a security decision can be explained in operating terms.

The model is a reduced CIGRE-derived teaching feeder, not a full utility study or the complete benchmark. Its results illustrate consequences under its stated assumptions; they are not equipment ratings, protection settings or customer-impact forecasts for a real station.

### Loss of supply depends on topology

The 110/20 kV transformer supplies a 20 kV bus. CB1 feeds S1, then normally closed RMU1 connects S2. Normally open RMU2 is the tie from S2 to S3 on feeder 2. The normally-open point matters: indiscriminate closing can join live sources rather than simply “restore power.”

For an S1 fault, CB1 trips while RMU1 sees no fault passage. The lab's isolation/back-feed exercise can restore S2 from feeder 2 after opening RMU1 and using the tie. S1 remains isolated.

For an S2 fault, CB1 trips and RMU1 latches fault passage. Merely opening RMU1 and closing RMU2 still connects feeder 2 to the fault. The modeled F2 head trips, potentially losing S3 as well. The same apparent restoration action can therefore enlarge the outage when fault location is misunderstood.

The F2 head is a process element, not a remotely controlled field device. Its state is not directly telemetered to the CC; S3 energization is observed through RMU2's source-voltage point. Do not fill the operator diagram from process truth to make it look complete.

### Interlocks do not replace an operating order

Live-live closes and closes without valid measurements are refused. RMU opening is also refused during fault current or invalid measurements. These checks prevent specific unsafe transitions but do not prove the operator chose the correct isolation boundary.

Fault passage is latched history, not proof that fault current still flows. A position change bumps device revision, temporarily invalidating measurements until the solver delivers a consistent new result. Wait for valid evidence rather than treating a transient refusal as a reason to bypass the interlock.

### Voltage consequences need time

Positive OLTC steps raise the modeled LV bus. The range uses taps −8 through +8, 1.25% per step and two seconds of motion. Automatic regulation uses a 20 kV setpoint, ±1% band and ten seconds continuously out of band before an adjacent request.

At the reference operating point, load multiplier 3.0 moves the bus outside the band and requires only a few steps to recover. Source 0.90 p.u. reaches the +8 limit while voltage remains low: actuator saturation is not successful regulation. Source 1.10 p.u. settles negative, measured around −7, rather than necessarily reaching −8.

Use the [reference cases](#eu-process-reference-cases) and measured attempt evidence, not those descriptions as invented telemetry. A completed tap and target tap differ during motion.

### Visibility is a separate consequence

Cutting IEC 104 can leave the CC showing an old closed breaker while local protection has tripped it. Blocking PLC-to-OLTC can stop regulation while the CC link remains healthy. Blocking historian ingest can lose archive continuity without interrupting telecontrol.

Report these as distinct outcomes: supply, voltage, supervisory visibility, automatic control and evidence continuity. An unknown/stale voltage must never be plotted as zero. The [security objectives](#eu-security-objectives) and [handover checklist](#eu-acceptance-handover) turn those distinctions into reviewable acceptance criteria.
