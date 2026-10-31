Capacitor banks provide reactive power compensation on distribution feeders. As inductive loads (motors, transformers) draw reactive power, the voltage at the end of the feeder drops. Shunt capacitor banks inject reactive power locally, offsetting the inductive demand and reducing voltage drop. This lets the utility serve more load without upgrading conductors or transformers.

### Fixed vs. Switched

Capacitor banks come in two flavors:

- **Fixed**: always connected, providing constant reactive support
- **Switched**: controlled by a capacitor bank controller that monitors voltage, reactive power, or time of day

Switched banks use vacuum or oil switches rated for capacitor inrush current. Each switching operation stresses the contacts and capacitor cells, so controllers implement switch-count lockout protection to prevent excessive cycling.

### Cybersecurity Risks

An attacker who can communicate with the capacitor bank controller could:

- **Rapidly switch the bank in and out**, exceeding the switch-count limit and triggering lockout
- **Switch it in during light load**, causing overvoltage
- **Prevent switching during heavy load**, causing undervoltage

The switch-count lockout is a protective feature, but an attacker who understands it can deliberately trigger it to disable the bank entirely.

### Relevance to This Lab

RangerDanger includes a dedicated capacitor bank simulator (**capbank-sim**, a 300 kVAR switched bank on the field network at 10.40.40.23). It appears in the substation HMI: the One-Line shows its switched-in/out state, **Supervisory Control** lets you Switch In / Switch Out and toggle Auto/Manual - the same Modbus and DNP3 paths an attacker would use, restricted by the containd firewall to the RTAC - and **Electrical Detail** shows its full status. Because the bank is wired into the OpenDSS power flow, switching it in raises the local voltage, drops feeder current, and corrects the power factor; switching it out reverses that. The switch-count lockout is a real protective feature an attacker can deliberately trip to disable the bank, exactly as described above. The same segmentation principle as the other field devices applies: restrict direct field-device access to the RTAC only.