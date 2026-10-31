The important lesson from Industroyer is not that IEC 104 needs an exotic exploit. An intruder able to act as a master can misuse legitimate protocol operations. Separate what malware analysis establishes from what is known about an incident, then map the defensive lesson to this range without treating it as a reproduction of a real attack.

### Industroyer / CRASHOVERRIDE

[ESET's June 2017 analysis](https://www.welivesecurity.com/wp-content/uploads/2017/06/Win32_Industroyer.pdf), especially pages 7–10, describes an IEC 104 component configured with station IP/port, common address and IOA ranges or sequences. It could send single-command select/execute operations, retain successful command addresses and repeat operations. Optional configuration could terminate legitimate communications software.

That IOA probing was state-changing, not harmless asset inventory. The report describes capabilities of analyzed samples; it is not a packet-for-packet reconstruction of the December 2016 Ukrenergo outage. Do not import later double-command functionality into the earlier component's history.

[Dragos's CRASHOVERRIDE report](https://nsarchive.gwu.edu/sites/default/files/documents/3869008/Dragos-CRASHOVERRIDE-Analyzing-the-Threat-to.pdf) similarly describes the payload acting as a master using station configuration. Its recommendations include baselining protocol use, investigating new endpoints and increased traffic, host hunting, tested offline backups and rehearsed recovery. It does not establish that a firewall alone would have prevented the outage.

### The 2022 attempt

[ESET's Industroyer2 account](https://www.welivesecurity.com/2022/04/12/industroyer2-industroyer-reloaded/) describes a self-contained IEC 104 executable with embedded destination, ASDU, IOA and timeout configuration. Execution was scheduled for 8 April 2022 alongside destructive payloads. It killed and renamed legitimate communications software to impede restart.

This was a reported attack attempt against high-voltage substations, not evidence of a repeat blackout. ESET anonymized the provider and did not know initial entry or IT-to-ICS movement at publication. The CERT-UA article body was not independently accessible in the source research, so no additional incident details are attributed directly to it here.

[Mandiant's reverse engineering](https://cloud.google.com/blog/topics/threat-intelligence/industroyer-v2-old-malware-new-tricks) found configurations with eight and three targets, configurable single/double commands and select/execute behavior. Range-based IOA operation existed in code but was not enabled in its recovered samples. Capability is not proof of operational use.

### Map the lessons, not the malware

This range has one configured common address, named command IOAs and a reachable RTU under weak policy. The lab's unauthorized-master demonstration illustrates exposure of valid protocol control, not the historical intrusion path or destructive malware.

Improved policy removes enterprise/vendor initiation of new IEC 104 sessions. It reduces reachable attack surface but leaves the allowed CC able to issue legitimate commands. Neither a common address nor an IOA is an authentication secret; [SBO](#iec104-sbo-commands) is sequencing, not identity.

Do not transfer historical IOAs into this package. A common address identifies the addressed station or sector, while an IOA identifies a point within that context. Mandiant's worked IOA 1104 command example is not this range's IOA 1104, which represents RMU1 fault-passage telemetry. Identical numbers across independent point maps do not identify the same function.

Defensive evidence should distinguish a new master connection, an unusual command pattern, a protocol rejection and a real device operation. Monitor endpoint/configuration changes as well as network traffic. The historian's snapshots help explain observation history but cannot guarantee complete event capture.

Keep attack discussion within the labs' existing exercises. Do not enumerate unknown control IOAs, reproduce wipers or kill communications processes. Use [trusted-host misuse](#eu-trusted-host-misuse) to discuss residual risk and [acceptance/handover](#eu-acceptance-handover) to record recovery evidence.
