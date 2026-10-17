Power factor helps explain why a feeder can carry substantial current even when its useful real-power delivery is modest. The EU teaching feeder includes loads with configured power factors, so reactive demand contributes to its solved electrical behaviour. It does not include switched capacitor banks or a dedicated volt/VAR controller.

### Real, reactive and apparent power

- **Real power (P)**, in kilowatts (kW), is the average rate of energy transfer doing useful work.
- **Reactive power (Q)**, in kilovolt-amperes reactive (kvar), describes energy exchanged between the source and electric or magnetic fields in equipment such as capacitors, motors and transformers. It is not net useful work, but the current associated with it still flows through network equipment.
- **Apparent power (S)**, in kilovolt-amperes (kVA), represents the combined loading seen by conductors and transformers.

For a balanced sinusoidal AC load, these quantities form a power triangle: **S² = P² + Q²**. In that model, power factor is **P/S**. Lower power factor means more apparent power and current are needed for the same real power. With distorted waveforms or unbalanced phases, the simple triangle is not a complete description; displacement power factor and true power factor can differ.

For a balanced three-phase circuit, **S = √3 × V(line-to-line) × I(line)**. Thus, at fixed voltage and real power, a lower power factor requires more line current. That can increase conductor losses and use more of a transformer's or cable's current capacity. In a simplified feeder, voltage drop depends on both real and reactive current interacting with line resistance and reactance; low power factor can therefore contribute to voltage drop, but it is not the only possible cause.

### Inductive and capacitive demand

Inductive equipment stores energy in magnetic fields; capacitive equipment stores it in electric fields. Their reactive current has opposite phase directions under the usual convention. A suitably designed capacitor can supply leading reactive power near an inductive load, reducing the reactive component drawn from upstream. This may reduce upstream current and affect voltage, but the result depends on network conditions, sizing and operating state. Excess or poorly placed compensation can create overvoltage or resonance concerns; it is not a universal remedy.

### What the EU feeder represents

The package's reduced, CIGRE-derived 20 kV feeder configures its nominal load groups with these real-power and power-factor inputs:

| Feeder group | Real power input | Power factor | Implied apparent power at nominal voltage |
| --- | ---: | ---: | ---: |
| Residential (S1) | 1435.6 kW | 0.97 | 1480 kVA |
| Commercial (S2) | 573.75 kW | 0.85 | 675 kVA |
| Industrial (S3) | 331.5 kW | 0.85 | 390 kVA |

The apparent-power values follow from **S = P / PF** and are derived from the checked-in feeder inputs; they are not separate telemetry points. The circuit solves feeder voltage and current from its modelled loads and equipment. See the [feeder model](#eu-mv-feeder) and its [reference cases](#eu-process-reference-cases) for the model boundary and solved examples.

Voltage regulation in this range is through the primary transformer's on-load tap changer (OLTC). A tap changes the transformer ratio and bus voltage; it does not change the configured load power factor or act as reactive-power compensation. The range's [voltage-regulation loop](#eu-voltage-regulation) is not capacitor switching or coordinated volt/VAR control. Do not infer a capacitor-bank action, a live power-factor measurement or a measured reactive-power point from a tap movement or a voltage change.

### Interpreting a low power factor

Power factor is one part of an electrical explanation, not a stand-alone diagnosis. Check the load, voltage, current, topology and measurement quality together. In the EU range, distinguish model inputs and solver results from what the Control Centre actually observes; stale or absent observations do not establish a physical value. The [primary OLTC description](#eu-primary-oltc) explains the modeled voltage-control equipment and its limits.
