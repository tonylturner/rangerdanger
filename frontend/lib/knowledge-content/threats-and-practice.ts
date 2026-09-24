import type { Section } from "../knowledge-types";

export const threatsAndPractice: Section =
  /* ====== ICS Threats & Operational Practice ====== */
  {
    heading: "ICS Threats & Operational Practice",
    description:
      "Real ICS incidents, the OT kill chain, living off the land, change management, and what outages actually cost. Why this matters beyond the lab.",
    icon: "target",
    accent: "rose",
    articles: [
      {
        id: "ot-kill-chain",
        title: "The OT Kill Chain (Mitre ATT&CK for ICS + Real Incidents)",
        body: `The exercises in this lab are not hypothetical attack patterns. They mirror real ICS incidents that have happened to real utilities. This article walks through the framework that organizes those incidents (Mitre ATT&CK for ICS) and the specific historical events that map most directly to the lab.

### Mitre ATT&CK for ICS

[Mitre ATT&CK for ICS](https://attack.mitre.org/matrices/ics/) is a knowledge base of adversary techniques observed in industrial control system intrusions. It is the OT counterpart to the more widely-known Enterprise ATT&CK and is structured as a matrix of **tactics** (the attacker's goals) and **techniques** (how they achieve those goals).

The tactics in the ICS matrix include: Initial Access, Execution, Persistence, Privilege Escalation, Evasion, Discovery, Lateral Movement, Collection, Command and Control, **Inhibit Response Function**, **Impair Process Control**, and **Impact** (loss of view, loss of control, loss of availability, denial of safety, damage to property). The Inhibit Response, Impair Process Control, and Impact tactics are the ICS-specific column the matrix adds beyond Enterprise ATT&CK - these are the things an attacker can do *only* if they have reached the OT environment.

### The Real Incidents the Lab Mirrors

**Industroyer (CRASHOVERRIDE) - Ukrainian Substation, December 2016.** Sandworm operators used stolen credentials to pivot from IT into a Kyiv-area transmission substation's OT network, then ran custom malware (Industroyer) that spoke IEC 60870-5-101 / -104, IEC 61850, and OPC DA natively. The malware opened breakers, causing a brief regional outage. The attack pattern - IT-to-OT lateral movement, then a protocol-aware command against field devices - is the same shape as the lab's Lab 2.3 (DNP3 Direct Operate against the recloser).

**Industroyer2 - Ukrainian Substation, April 2022.** A follow-on Sandworm campaign targeted Ukrainian high-voltage substations with a refined version of Industroyer focused on IEC 60870-5-104. Intercepted and disrupted before causing significant impact. Same shape, same lesson, with the addition that ICS-aware perimeter monitoring (which the targeted utility had deployed by then) was a meaningful part of the defense.

**Triton / TRISIS - Petrochemical SIS, Saudi Arabia, 2017.** The TEMP.Veles operators (sometimes attributed to Russia's TsNIIKhM) used a long IT compromise to reach a petrochemical plant's safety instrumented system (SIS), then deployed Triton, the first publicly-known malware designed to manipulate a safety controller (Triconex). The attack was discovered when the Triconex tripped the plant - possibly due to a Triton bug. This is an "Impact: Denial of Safety" event in the ATT&CK matrix. The lab does not simulate SIS specifically, but the IT-to-OT lateral path leading to a protocol-aware controller attack is the same pattern.

**Stuxnet - Iranian Centrifuge Cascades, ~2009–2010.** The original public ICS attack. US/Israeli operators (according to the public attribution) used USB-borne malware to reach Siemens S7-315 PLCs controlling uranium centrifuges, then issued protocol-level write commands that subtly damaged the centrifuges over months. The Inhibit Response Function tactic - Stuxnet manipulated the HMI to show normal operations while damage was occurring - is one of the most influential moves in the ATT&CK matrix. The lab does not simulate HMI manipulation directly, but the "by the time the field device sees the command, it looks legitimate" lesson from Lab 2.3-bonus is the same observation.

**Volt Typhoon - Critical Infrastructure Pre-positioning, 2023–2024.** PRC-linked operators pre-positioned access to US critical-infrastructure OT networks (water, electric, transportation) without immediately causing impact. CISA and partner agencies published advisories detailing living-off-the-land tradecraft (LOTL TTPs) - the threat surface is not "attackers run novel malware" but "attackers use legitimate Windows admin tools to live inside OT networks for months." This is the lab's [Living off the Land in OT](#living-off-the-land-ot) lesson made historical.

:::warning Industroyer and Lab 2.3 are the same attack shape
The 2016 Kyiv substation outage was Sandworm sending IEC 60870-5-104 / DNP3 command sequences from a compromised IT host into the OT zone. The Lab 2.3 primary attack - kali firing \`dnp3cmd ... crob 0 trip\` against the recloser from the enterprise zone - is the same shape with the same root cause: cross-zone control surface that was not denied at the perimeter. When a student asks "is this a real thing?" the answer is yes, with a CISA advisory and an attributed threat actor.
:::

### How the Lab Exercises Map to ATT&CK

| Lab step | ATT&CK technique |
|---|---|
| Lab 2.3 unauthorized DNP3 Direct Operate against recloser | T0859 Valid Accounts (RTAC source spoofing) + T0855 Unauthorized Command Message |
| Lab 2.3 Modbus FC5 breaker trip | T0855 Unauthorized Command Message + T0879 Damage to Property (if it persisted) |
| Lab 2.3 Modbus FC6 regulator tap override | T0836 Modify Parameter |
| Lab 2.3-bonus RDP pivot through vendor-jump | T0817 Drive-by Compromise → T0822 External Remote Services → T0859 Valid Accounts |
| Lab 1.2 baseline traffic capture (defender side) | The defender-side counterpart to T0801 Monitoring (which an attacker also does for reconnaissance) |

When you walk a workshop attendee through Lab 2.3 and they ask "is this a real thing?" - yes. The Industroyer pattern. Show them the ATT&CK matrix entry for [Impair Process Control / Unauthorized Command Message](https://attack.mitre.org/techniques/T0855/) and the [Industroyer threat group page](https://attack.mitre.org/software/S0604/). That contextualization changes how seriously students take the exercise.

### See Also

- [Living off the Land in OT](#living-off-the-land-ot) - Volt Typhoon's signature pattern explained
- [Vendor Remote Access Patterns](#vendor-remote-access) - the entry vector for several real incidents
- [What is ICS DPI?](#ics-dpi) - the defensive control that closes the unauthorized-command-message attack
- [Change Management for Substation Firewall Rules](#change-management-firewall-rules) - how the defensive change actually gets approved at a utility`,
      },
      {
        id: "living-off-the-land-ot",
        title: "Living off the Land in OT",
        body: `**Living off the Land (LOTL)** is the attacker tradecraft of using tools and credentials that already exist in the target environment rather than introducing custom malware. In OT specifically, this means using the legitimate OT tools that engineering workstations and vendor jump hosts already have installed - \`mbpoll\`, \`dnp3poll\`, vendor configuration utilities, RDP, native SCADA functions - to accomplish the attacker's goals without ever dropping a file that an antivirus might catch.

### Why OT Attackers Love LOTL

Several reasons converge:

- **Detection.** Antivirus and endpoint detection are vastly weaker in OT than in IT. But the things that DO exist (network monitoring, anomalous-traffic detection) are even more allergic to *new* binaries than to anomalous network use of existing binaries. Using \`mbpoll\` does not flag any AV alert anywhere.
- **Persistence.** Custom malware needs persistence mechanisms. Legitimate tools are already persistent - they are part of the engineering workstation's image.
- **Plausible deniability.** A protocol-level attack that uses \`mbpoll\` and a real RTU's source IP looks indistinguishable from legitimate operator activity. Attribution is harder.
- **Lateral movement.** A compromised engineering workstation has the credentials, network paths, and tools to reach everything an engineer reaches. The attacker inherits the engineer's authorization scope without needing to escalate.

### What the Tools Look Like

Some of the legitimate tools commonly abused in OT environments:

- **\`mbpoll\`, \`mbtget\`, \`MBASE\`** - Modbus clients shipped with most engineering Linux distros and many Windows ICS toolkits.
- **\`dnp3poll\`, \`dnp3cmd\`, OpenDNP3, SEL AcSELerator\`** - DNP3 clients, some commercial, some open-source.
- **\`xfreerdp\`, \`mstsc\`, VNC clients** - used for vendor jump-host pivots (Lab 2.3-bonus).
- **\`psexec\`, \`wmiexec\`, \`PowerShell Remoting\`** - Windows admin tools that attackers use to lateralize after reaching an engineering workstation.
- **Vendor configuration utilities** - SEL AcSELerator Architect, ABB MicroSCADA, Siemens TIA Portal, Rockwell Studio 5000, Wonderware InTouch. Each is a legitimate engineering tool that, in attacker hands, becomes a control-system command-line interface.

### Why Behavioral Detection Beats Signature Detection in OT

Signature-based detection (this hash is bad, this string is bad, this binary is malware) does not work on LOTL because the binary is legitimate. What works is **behavioral detection** - flagging traffic patterns and command sequences that legitimate activity should not exhibit.

In OT specifically, behavioral detection is plausible *because* legitimate behavior is so constrained. The RTAC polls the recloser every 5 seconds via DNP3 reads. Nothing else should be sending DNP3 to the recloser. A single DNP3 packet from the engineering workstation to the recloser at 02:47 on a Tuesday is anomalous on its face. You do not need to know whether the engineering workstation's binary is malicious to know that this packet is.

This is why the lab's defense-in-depth lesson stresses both segmentation (source-pin: only the RTAC can reach the field) AND monitoring (every cross-zone packet shows up in the Live DPI Events strip). Segmentation removes the easy paths; monitoring catches what slips through. Antivirus does not enter the conversation.

### How the Lab's Attacks Use LOTL

Every attack in Lab 2.3 and 2.3-bonus uses LOTL by design. \`mbpoll\` and \`dnp3cmd\` are real OT tools that any engineering workstation has. \`xfreerdp\` is a legitimate desktop client. \`sshpass\` is a legitimate scripting tool. None of these are malware. They are exactly what a vendor's engineer would use to do legitimate work - and exactly what an attacker who has compromised that engineer's workstation would use to do illegitimate work.

The pedagogical point: when you tell students "the firewall has to deny enterprise → field on Modbus," it is not because there is a particular piece of malware to block. It is because the *legitimate* tool, used from the *wrong* place, is the attack. Network policy is the right place to draw the line; endpoint malware detection is not where this fight is fought.

### Mitigation Posture

For OT environments specifically, the LOTL-mitigation posture looks like:

- **Strict network segmentation** with per-conduit allow rules (no broad zone allows)
- **Source-pinning** so a tool used from the wrong place is denied at L4
- **DPI for protocol surfaces** so the right tool used the wrong way is denied at L7
- **Behavioral monitoring** for traffic that does not match the baseline operational pattern
- **Just-in-time vendor access** (open the path for a maintenance window, close it after)
- **Per-session audit** so an investigation can pin actions to a specific human and timeframe

The lab exercises the first three. The remaining items are organizational practice that no docker compose can simulate, but they are where the real defense lives.`,
      },
      {
        id: "change-management-firewall-rules",
        title: "Change Management for Substation Firewall Rules",
        body: `Lab 2.4's evidence package is not a lab artifact for its own sake. It is a deliberately compressed version of what a real utility *change board* expects to see before approving a segmentation change. This article explains what a change board actually is, what they want, and how the lab's outputs map to that.

### What a Utility Change Board Is

A change board (sometimes called CAB - Change Advisory Board) is the operational governance body that reviews and approves changes to production systems. In OT environments these reviews are more stringent than in IT because:

- **Outages cost money and reputation** (the customer-service tile in the lab's HMI is the operational version of this - minutes of outage, customers affected, regulatory implications).
- **Some changes can damage equipment** (a misconfigured firewall rule that prevents the RTAC from reaching the recloser will fail to recover from a fault, which can damage the recloser or the upstream feeder).
- **Some are regulated.** NERC CIP-005 R1 requires utilities to document and review electronic-security-perimeter changes; CIP-010 requires baseline configurations and change-management evidence. A change board's records are part of the audit trail the regulator will eventually inspect.

A typical change board for a substation firewall change includes: a control-room operator (verifies operations impact), a protection engineer (verifies the change does not break protection coordination), a cybersecurity engineer (verifies the security posture is improved or unchanged), and an IT/network engineer (verifies the firewall configuration itself). The board reviews the proposed change package, asks questions, and votes to approve, defer, or reject.

### What the Change Package Has to Contain

A complete substation-firewall change package usually includes:

1. **Statement of intent.** What problem is this change solving? What is the operational or security gap? Why now?
2. **Configuration diff.** The current policy, the proposed policy, and the line-by-line difference. For a containd policy, this is the JSON diff plus the human-readable summary of "rule X added, rule Y removed, rule Z modified."
3. **Test evidence.** Proof that the proposed policy was tested before being proposed for production. For each new or modified rule: a positive test (the legitimate traffic still works) and a negative test (the previously-allowed bad traffic is now blocked). The evidence is typically a combination of PCAP captures, firewall logs, and screenshots of the test result.
4. **Rollback plan.** If the change goes badly in production, how do you revert? For a containd change, this is "restore the prior policy file, run \`containd cli> import config\`, verify the active policy hash matches the previous version."
5. **Maintenance window.** When will the change be applied? What other activities are scheduled in the same window? What is the impact on operations during the change itself (does the firewall need to be restarted)?
6. **Post-change monitoring plan.** What will you watch for in the hours after the change to confirm operations are healthy? Which dashboards, which alarms, which log queries?

### How the Lab Maps to This

Lab 2.4's evidence-assembly step produces almost exactly this package:

- **Statement of intent** - Lab 1.3's design verdicts and Lab 1.4's plan together are the *intent* document. The student wrote them.
- **Configuration diff** - \`containd cli> export config > student-policy.json\` is the proposed policy. The weak baseline is the current. The diff is \`diff substation-weak.json student-policy.json\`.
- **Test evidence** - Lab 2.4's positive-tests step is the legitimate-traffic-works half. The negative-tests step is the bad-traffic-blocked half. Both produce log entries on the Live DPI Events strip that constitute the firewall-log evidence.
- **PCAP** - Lab 2.4's PCAP-capture step writes \`/data/captures/validation.pcap\` showing only RTAC sources reaching field on Modbus/DNP3 after the policy is applied. This is direct network-level evidence.
- **Audit log** - \`containd cli> show audit\` snapshots the \`config.commit\` entries that prove the policy was actually applied.

A real change board would expect more (rollback plan, maintenance window, monitoring plan), but the lab covers the *technical* portion of the evidence package end to end. The operational portions (when, who, what else) are organizational practice no lab can simulate.

### Why This Matters Beyond the Lab

Most cyber-trained workshop attendees never see a change-board package and have never had to defend a firewall change against operators who would rather not change anything. The lab compresses this experience into a 15-minute exercise on purpose. Walking out of the workshop with one assembled evidence package in your Exercise Notes gives you a tangible template to point at when your real organization asks "what would a good change package look like?"

The \`scripts/validation-report.sh\` helper produces a markdown deliverable equivalent to the manually-assembled package - operator-facing rather than student-facing, suitable for attaching directly to a change request.`,
      },
      {
        id: "outage-costs-saidi-saifi",
        title: "What Outages Cost (SAIDI, SAIFI, and the Customer-Service Tile)",
        body: `The customer-service tile on the lab's Feeder HMI shows "ALL CUSTOMERS WITHOUT POWER" or "N kW serving M customers" depending on the feeder state. This is not just flavor. It is the lab's representation of the metrics utilities actually report to their regulators - and the metrics a cyber attack against a substation would actually move.

### The Standard Reliability Metrics

Utilities report distribution-system reliability using two widely-used indices:

- **SAIDI** (System Average Interruption Duration Index) - the *average* total outage time per customer per year. If a utility serves a million customers and the total outage-customer-minutes across the year was 100 million, SAIDI = 100 minutes per customer.
- **SAIFI** (System Average Interruption Frequency Index) - the *average* number of outage events per customer per year. SAIFI = 1.5 means the average customer experiences 1.5 outage events per year.

A third commonly-tracked metric is **CAIDI** (Customer Average Interruption Duration Index) - the average outage duration per outage event = SAIDI / SAIFI. It is what your average outage feels like to a customer, in minutes.

These are reported to **state-level public utility commissions** in the US (each state has its own) and equivalent regulators in other countries. They are public information. They are tracked over time. They feed rate-case decisions, performance-based ratemaking, and utility executive bonuses. They are not abstract.

### Typical Values

For a North American distribution utility:

- SAIDI around **90 to 150 minutes per year** is normal in good weather years.
- Major-event days (hurricanes, ice storms) push SAIDI higher; sometimes the regulator excludes them from the official reporting figure to avoid penalizing for weather.
- The best-performing utilities in fair-weather climates run SAIDI under 60 minutes. Long-rural utilities in harsh climates can be 200+.
- SAIFI typically runs **0.7 to 1.5 events per year per customer**.

A single substation event that takes 1,000 customers offline for 60 minutes contributes 60,000 customer-minutes - visible in the SAIDI calculation. A whole feeder event taking 10,000 customers offline for 4 hours is 2.4 million customer-minutes - the kind of event that triggers an executive after-action review.

### How Outage Costs Get Quantified

Beyond the regulatory reporting, utilities calculate the economic cost of outages for cost-benefit analysis of grid investments. The standard reference is the **DOE Interruption Cost Estimator (ICE) Calculator** (Berkeley Lab). It uses customer-survey-derived willingness-to-pay-to-avoid-outage data to produce per-event cost estimates:

- A 1-hour outage for a typical small commercial customer: roughly **\\$200 to \\$500**
- A 1-hour outage for a medium industrial customer: **\\$5,000 to \\$30,000** depending on production sensitivity
- A 1-hour outage for a hospital or fire station: **incalculable in the direct dollar sense**, which is why the lab calls these out separately on the customer-service tile

For a 1,000-customer-hour outage event across a typical mix, the all-in economic cost is in the **\\$200,000 to \\$1,000,000** range. This is before regulatory penalties.

### Cyber Attacks vs. Weather as Outage Causes

For context: about **70% of customer-minutes of outage** at a typical distribution utility comes from weather (storms, ice, lightning, vegetation contact). The next-largest categories are equipment failure, animal contact, and human error. Cyber attack is, today, a tiny fraction of the actual outage tally.

But the cyber-attack risk profile is different from the weather risk profile in two important ways:

1. **Concurrency.** A targeted cyber attack can take down many substations *simultaneously*, in a way that weather events at this scale (regional ice storm, hurricane) usually require luck or a multi-day weather pattern. A coordinated Industroyer-class attack could in principle affect tens of substations in a window of minutes.
2. **Cascading.** A weather event is bounded by the geography of the weather. A cyber event is bounded by the attacker's reach - which, if the IT-OT boundary is porous, can be region-wide or larger.

These two characteristics are why utility executives and regulators care about cyber attacks against distribution far in excess of the historical-outage-share argument. The risk is not what cyber attacks *have* done; it is what cyber attacks *could* do under a coordinated campaign.

### How the Customer-Service Tile Maps to All This

The customer-service tile is the lab's representation of the operator's awareness of outage impact. When the tile flips from "120 kW serving ~200 customers" to "ALL CUSTOMERS WITHOUT POWER," that is the operational visibility a real control-room operator has. The lab's "hospital and fire station without power" annotation is the *critical-load* category - the loads whose outage triggers an immediate emergency response.

When you tell a workshop attendee "this single packet caused a complete feeder outage," you can put a dollar figure on it: 200 customers × 1 hour at typical mix ≈ **\\$50,000 to \\$200,000** of economic impact, plus the regulatory reporting visibility, plus the (incalculable) critical-services consequences. The cyber-attack-against-substation threat is not theoretical. The lab does not simulate the economics directly, but the customer-service tile is the bridge to that framing.`,
      },
    ],
  }
;
