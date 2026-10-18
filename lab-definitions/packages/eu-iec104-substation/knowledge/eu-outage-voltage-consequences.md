A control action matters because it changes an electrical process, not because a packet counter increased. The EU range links switching, fault location, tap control and observation quality so that a security decision can be explained in operating terms.

The model is a reduced CIGRE-derived teaching feeder, not a full utility study or the complete benchmark. Its results illustrate consequences under its stated assumptions; they are not equipment ratings, protection settings or customer-impact forecasts for a real station.

### Loss of supply depends on topology

The 110/20 kV transformer supplies a 20 kV bus. CB1 feeds S1, then normally closed RMU1 connects S2. Normally open RMU2 is the tie from S2 to S3 on feeder 2. The normally-open point matters: indiscriminate closing can join live sources rather than simply “restore power.”

For the reference S1 fault, CB1 trips instantaneously (about 2.7 kA above I>> 2000 A) while RMU1 remains below its 300 A FPI pickup. The lab's isolation/back-feed exercise can restore S2 from feeder 2 after opening RMU1 and using the tie. S1 remains isolated.

For the reference S2 fault (about 1.4 kA), RMU1 latches at 300 A with zero delay; CB1 trips after about 1 s on I> 400 A / 1000 ms, below I>> 2000 A. Merely opening RMU1 and closing RMU2 still connects feeder 2 to the fault. The modeled F2 head sees about 2.5 kA, above I>> 2000 A, and trips instantaneously, potentially losing S3 as well. The same apparent restoration action can therefore enlarge the outage when fault location is misunderstood.

The F2 head is an indication-only field breaker. The RTU telemeters position, protection trip, current and MW; S3 energization still requires RMU2's source-voltage point, not position alone. Do not fill the operator diagram from process truth to make it look complete.

### Interlocks do not replace an operating order

Live-live closes and closes without valid measurements are refused. RMU opening is also refused during fault current or invalid measurements. These checks prevent specific unsafe transitions but do not prove the operator chose the correct isolation boundary.

Fault passage is latched history, not proof that fault current still flows. A position change bumps device revision, temporarily invalidating measurements until the solver delivers a consistent new result. Wait for valid evidence rather than treating a transient refusal as a reason to bypass the interlock.

### Voltage consequences need time

Positive OLTC steps raise the modeled LV bus. The range uses taps −8 through +8, 1.25% per step and two seconds of motion. The OLTC is on the 110 kV winding, giving an asymmetric MV range +11.1% / −9.1%. Automatic regulation uses a 20.4 kV setpoint, ±1% band (20.196–20.604 kV) and 30 s continuously out of band before an adjacent request.

With the daily profile disabled and initial tap zero, nominal settles at +2 (20.429 kV), and load multiplier 3.0 also settles at +2 (20.203 kV). From a settled +2, load ×3 may need no further correction. Source 0.90 p.u. reaches the +8 limit while voltage remains low: actuator saturation is not successful regulation. Source 1.10 p.u. settles negative, measured at −6 (20.382 kV), rather than necessarily reaching −8.

Use the [reference cases](#eu-process-reference-cases) and measured attempt evidence, not those descriptions as invented telemetry. A completed tap and target tap differ during motion.

### Visibility is a separate consequence

Cutting IEC 104 can leave the CC showing an old closed breaker while local protection has tripped it. Blocking PLC-to-OLTC can stop regulation while the CC link remains healthy. Blocking historian ingest can lose archive continuity without interrupting telecontrol.

Report these as distinct outcomes: supply, voltage, supervisory visibility, automatic control and evidence continuity. An unknown/stale voltage must never be plotted as zero. The [security objectives](#eu-security-objectives) and [handover checklist](#eu-acceptance-handover) turn those distinctions into reviewable acceptance criteria.
