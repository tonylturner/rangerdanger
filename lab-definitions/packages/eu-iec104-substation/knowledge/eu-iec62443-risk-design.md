IEC 62443-3-2 provides a way to turn “protect the substation” into a documented risk-based design. Its subject is the industrial automation and control system under consideration (SUC), its zones and conduits, their risks, target security levels and requirements. It does not prescribe one security level for every substation or declare a VLAN secure.

This article uses the public scope of [IEC 62443-3-2:2020](https://webstore.iec.ch/en/publication/30727) and the [ISA public table of contents](https://www.isa.org/getmedia/4cfc5f8a-1f19-4e97-9382-c7d46e3b04a5/isa-62443-3-2-toc.pdf). The purchased normative text was not reviewed. The mapping below is a teaching interpretation, not a compliance assessment, certification or substitute for the standard.

### Define the system before choosing a firewall

ZCR 1 defines the SUC and access points. Here, inventory the CC, RTU, breaker, two RMUs, OLTC, OpenPLC, clock and historian. Include engineering access and enterprise archive consumption. Identify the management and simulated-process networks explicitly; silently omitting the paths used by the portal would make the scope misleading.

Record what the assets do. The PLC requests adjacent tap positions; the CC switches auto/manual and issues manual steps; RMUs isolate or restore sections. An inventory of operating systems alone misses the functions whose misuse creates consequences.

### Assess and partition

ZCR 2 is the initial cyber risk assessment. Describe threats, exposed functions and consequences before assuming the reference policy is the answer. An unauthorized master opening a feeder and an authorized but compromised CC opening that feeder are distinct scenarios.

ZCR 3 partitions the system into zones and conduits. The [implemented zone map](#eu-zone-conduit-design) is a starting point. Justify why the historian belongs in a separate Operations DMZ and why engineering gets a maintenance path rather than general field access. Note that the RTU and PLC share the station subnet: their exchange is not firewall-enforced.

ZCR 4 compares initial risk with tolerable risk. The asset owner must supply the acceptance criteria. The range cannot determine a utility's acceptable outage duration, likelihood scale, safety obligations or customer impact.

### Iterate countermeasures and residual risk

ZCR 5 covers detailed assessment: threats, vulnerabilities, consequences, likelihood, target security levels (SL-T), countermeasures and residual risk. Avoid assigning a numerical SL-T merely because the asset is a substation.

For the unauthorized-master scenario, source-pin TCP/2404 to the CC, deny other initiators and test new connections. For the compromised-CC scenario, that same rule is insufficient: the malicious traffic has the allowed source. Discuss endpoint protection, command authorization, operator procedures and monitoring as further treatments, without claiming they are implemented here.

Also assess availability costs. Removing PLC-to-OLTC Modbus reduces connectivity but removes automatic voltage regulation. A countermeasure that creates an unacceptable operating failure needs redesign, not a favorable security score.

### Requirements and approval

ZCR 6 documents requirements, assumptions and constraints. Produce a communications matrix, command inventory, measurable positive and negative tests, rollback criteria and a residual-risk register. State plainly that this range has no command-aware IEC 104 firewall rule and no demonstrated IEC 62351 deployment.

ZCR 7 is asset-owner approval. A successful lab validator is technical evidence, not owner acceptance of production risk. The handover should identify who accepts the remaining trusted-controller and same-subnet risks, and when the assessment must be revisited. Use the [change plan](#eu-change-resource-plan) to turn that discussion into an executable, reviewable work package.
