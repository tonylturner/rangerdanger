Scope note: capacitor banks below are general power-system background. The EU range does not model or simulate a capacitor bank or its controller; its voltage-regulation exercises use the primary on-load tap changer (OLTC).

Capacitor banks provide reactive-power compensation on distribution feeders. Inductive loads such as motors and transformers draw lagging reactive power. A shunt capacitor supplies leading reactive power locally, which can reduce the reactive component of current carried from upstream and support voltage along the feeder. It does not supply the real energy consumed by the load. The size and direction of the voltage effect depend on network conditions; compensation that is useful at high load can be excessive at light load.

### Fixed and switched banks

A fixed bank stays connected whenever its circuit is energised. A switched bank is connected or disconnected by switchgear under a controller or operating procedure. Depending on the installation, control decisions can use voltage, reactive power, power factor, a schedule or a combination. The equipment's ratings, switching duty, protection and operating limits must come from its actual design; they cannot be inferred from a generic bank description.

Switching is not electrically neutral. Capacitors draw inrush current when energised, and repeated operations impose duty on switching equipment and capacitor units. A design may limit operation frequency or block switching after certain conditions. Engineers also assess overvoltage, unbalance, harmonics and possible resonance as part of the specific installation. A switching command or an indicated “in” state alone does not establish that the bank is healthy or that compensation is appropriate.

### Voltage support is not the same as voltage regulation

Reactive compensation can reduce upstream reactive current and may help limit voltage drop, but its effect is local and depends on feeder impedance and load. At light load, leaving too much capacitance connected can raise voltage beyond the desired range. The operating plan therefore needs to account for load changes, bank steps, switching constraints and measurements at relevant locations.

Do not confuse a capacitor-bank controller with the EU range's [primary OLTC](#eu-primary-oltc). The OLTC changes the transformer's ratio in discrete steps; a capacitor bank changes reactive-power injection at its connection point. The range's [voltage-regulation loop](#eu-voltage-regulation) is specifically an OLTC control example, not coordinated volt/VAR control or capacitor-bank switching. Its [feeder model](#eu-mv-feeder) does not provide a capacitor-bank device or bank-switching exercise.

### Operational and security considerations

For a real installation, document who may request switching, which local interlocks and protection remain authoritative, how automatic and manual modes interact, and what evidence confirms the completed physical state. Unexpected repeated switching, operation outside an approved envelope, or loss of trustworthy feedback warrants investigation; an old supervisory indication is not proof of present equipment state.

Network segmentation can limit which systems reach a bank controller, but reachability is not proof that a permitted request is operationally safe. Review the actual controller interface, required services, operating procedure and independent protections for the installation. This article describes general risks only: it does not claim that the EU range has a capacitor-bank endpoint, control path, telemetry point or command filter.
