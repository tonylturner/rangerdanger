IEC 62443 defines four Security Levels (SLs) based on the capability of the threat actor a zone must defend against.

### The Four Levels

| Level | Threat | Example |
|-------|--------|---------|
| **SL-1** | Casual or coincidental violation | Accidental access, misconfiguration, untargeted malware |
| **SL-2** | Intentional violation with simple means | Disgruntled employee, attacker using public tools |
| **SL-3** | Sophisticated attack with moderate resources | Skilled attacker with ICS-specific knowledge |
| **SL-4** | State-sponsored with extended resources | Nation-state actor with deep domain expertise |

### Three Types of SL Designation

The standard distinguishes between:

- **SL-T (target)**: the desired security level from risk assessment
- **SL-C (capability)**: the level a system or component can actually provide
- **SL-A (achieved)**: the actual measured level in the deployed environment

The goal is to configure systems so SL-A meets or exceeds SL-T for every zone. When SL-A falls short, the gap is residual risk that needs compensating controls or explicit risk acceptance.

### Assigning Levels in Practice

For each zone, you consider:

1. **Consequences of compromise** (safety, environmental, financial, operational)
2. **Threat landscape** (who targets this zone and what capabilities they have)
3. **Existing vulnerabilities** in the deployed systems

A distribution substation field device zone might get SL-T of 3 because compromise could affect public safety (overcurrent protection disabled), sophisticated actors target utilities, and the devices run unauthenticated protocols like Modbus.

### How This Connects to the Lab

The conduit connecting two zones must enforce the higher SL. If the enterprise zone is SL-1 and the field device zone is SL-3, the firewall rules between them must enforce SL-3 controls. That means more than simple port filtering. You need protocol-aware inspection, logging, and potentially application-layer authentication.

In the lab, the **weak policy** provides roughly SL-1 conduit security: traffic flows freely with no protocol inspection. The **hardened policy** approaches SL-3: cross-zone traffic is restricted to specific source-destination pairs, Modbus function codes are filtered, and DNP3 command traffic is limited to the RTAC.