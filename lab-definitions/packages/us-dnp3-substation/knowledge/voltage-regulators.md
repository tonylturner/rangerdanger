Voltage regulators maintain distribution feeder voltage within acceptable limits as load varies throughout the day. The standard is ANSI C84.1 Range A: 114-126 V on a 120 V base. They work by adjusting a tap position on an autotransformer, with each tap step representing about 0.625% voltage change.

### Tap Range and Operation

A standard 32-step regulator uses positions **-16 to +16**, giving plus or minus 10% regulation range. The controller continuously monitors load-side voltage and compares it to a setpoint. When voltage drifts outside the bandwidth for longer than a configurable time delay, the controller initiates a tap change.

Each tap change involves mechanical switching under load, which causes a brief transient. Tap changers have a finite mechanical life (hundreds of thousands of operations), so unnecessary tap changes are a maintenance concern.

### Attack Scenarios

Unauthorized tap manipulation can cause real harm:

- **Driving to extreme position** (+16 or -16) creates overvoltage or undervoltage on the feeder
- **Overvoltage** damages customer equipment, especially sensitive electronics
- **Undervoltage** causes motors to draw excessive current and overheat
- **Rapid cycling** back and forth accelerates mechanical wear and can cause tap changer failure

An attacker who can write to the regulator tap position register via Modbus or DNP3 can execute all of these.

### Lab Implementation

The regulator simulator (`regulator-sim`) runs at **10.40.40.22** on `field_net`. It models a load tap changer with a plus or minus 16 tap range and voltage regulation logic.

- **Modbus**: holding register read/write on port 502
- **DNP3**: analog output commands to outstation address **3** on port 20000

The physics engine adjusts the reported feeder voltage based on tap position. You can observe the electrical impact of unauthorized tap changes in real time on the HMI.