IEC 62443 security levels describe increasing requirements for protection against threats with different capabilities. They are one input to a risk-based design, not a score that labels a whole substation “secure.” This overview is explanatory; use the applicable standard for a formal assessment.

### The four levels

| Level | High-level threat capability |
|---|---|
| **SL 1** | Protection against casual or coincidental violations. |
| **SL 2** | Protection against intentional violations using simple means, with limited resources, generic skills and low motivation. |
| **SL 3** | Protection against intentional violations using sophisticated means, moderate resources, IACS-specific skills and moderate motivation. |
| **SL 4** | Protection against intentional violations using sophisticated means, extensive resources, IACS-specific skills and high motivation. |

These are broad threat-capability descriptions, not a list of attacker identities. A level does not predict that a particular actor will attack, nor does it by itself describe the consequence or likelihood of a scenario. The detailed security requirements associated with a level matter; the table is not a substitute for them.

### Target, capability and achieved levels

IEC 62443 distinguishes three uses of an SL designation:

- **SL-T (target)** is the level sought for the defined system scope, informed by risk assessment and security requirements.
- **SL-C (capability)** describes the level a product or system is capable of providing within a defined scope.
- **SL-A (achieved)** describes the level assessed for the implemented and configured system.

Keep the scope attached to each designation: identify the system under consideration (SUC), its zones and conduits, and the requirements being evaluated. An SL-T is a design objective, not evidence that a product can meet it. A product’s SL-C does not establish the SL-A of a deployed environment. Comparing target and achieved protection requires evidence against the relevant requirements; a single number is not a substitute for that analysis.

### Apply the distinction to the EU range

The [EU risk-design workflow](#eu-iec62443-risk-design) starts by defining the SUC and assessing scenarios before selecting target security levels. For example, an unauthorized master and a compromised, permitted Control Centre are different scenarios: source restriction can address who may open a network path, but it does not resolve misuse by a trusted endpoint. The [zone and conduit design](#eu-zone-conduit-design) gives the range’s actual roles and permitted paths.

The improved reference policy source-pins the Control Centre to the RTU on TCP/2404. That is a network boundary rule, not an SL assignment or proof of command authorization. The [policy comparison](#eu-policy-comparison) explains that the EU reference policy disables DPI: it does not filter IEC 104 ASDUs, common addresses or IOAs, and decoded traffic is evidence of observation, not command enforcement. The weak and improved policies are not labeled with numeric SLs. Do not infer that either one achieves SL 1, SL 3 or any other level.

Use the [risk-design process](#eu-iec62443-risk-design) to identify assets and functions, threats, vulnerabilities and consequences; determine tolerable risk with the asset owner; and document requirements, countermeasures and residual risk. A target should follow that analysis, not a rule such as “every field device is SL 3.” If assessed capability or implementation falls short of the target, record the gap and decide on further treatment or explicit risk acceptance. A successful range test is evidence for the tested condition, not a certification or general compliance claim.
