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

Conduit requirements come from risk assessment and the security needs of the connected zones, not an automatic maximum of two zone labels. The map's illustrative SL numbers are design targets, not measured capability or achievement.

The **weak policy** and **hardened policy** demonstrate different segmentation choices. Neither establishes an achieved Security Level. The hardened reference restricts cross-zone source/destination pairs and source-pins field access to the RTAC. The L4 source pin enforces; DPI detects and logs. A policy label or a range PASS is not an IEC 62443 SL assessment.
