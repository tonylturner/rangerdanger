**Living off the Land (LOTL)** is the attacker tradecraft of using tools and credentials that already exist in the target environment rather than introducing custom malware. In OT specifically, this means using the legitimate OT tools that engineering workstations and vendor jump hosts already have installed - `mbpoll`, `dnp3poll`, vendor configuration utilities, RDP, native SCADA functions - to accomplish the attacker's goals without ever dropping a file that an antivirus might catch.

### Why OT Attackers Love LOTL

Several reasons converge:

- **Detection.** Antivirus and endpoint detection are vastly weaker in OT than in IT. But the things that DO exist (network monitoring, anomalous-traffic detection) are even more allergic to *new* binaries than to anomalous network use of existing binaries. Using `mbpoll` does not flag any AV alert anywhere.
- **Persistence.** Custom malware needs persistence mechanisms. Legitimate tools are already persistent - they are part of the engineering workstation's image.
- **Plausible deniability.** A protocol-level attack that uses `mbpoll` and a real RTU's source IP looks indistinguishable from legitimate operator activity. Attribution is harder.
- **Lateral movement.** A compromised engineering workstation has the credentials, network paths, and tools to reach everything an engineer reaches. The attacker inherits the engineer's authorization scope without needing to escalate.

### What the Tools Look Like

Some of the legitimate tools commonly abused in OT environments:

- **`mbpoll`, `mbtget`, `MBASE`** - Modbus clients shipped with most engineering Linux distros and many Windows ICS toolkits.
- **`dnp3poll`, `dnp3cmd`, OpenDNP3, SEL AcSELerator`** - DNP3 clients, some commercial, some open-source.
- **`xfreerdp`, `mstsc`, VNC clients** - used for vendor jump-host pivots (Lab 2.3-bonus).
- **`psexec`, `wmiexec`, `PowerShell Remoting`** - Windows admin tools that attackers use to lateralize after reaching an engineering workstation.
- **Vendor configuration utilities** - SEL AcSELerator Architect, ABB MicroSCADA, Siemens TIA Portal, Rockwell Studio 5000, Wonderware InTouch. Each is a legitimate engineering tool that, in attacker hands, becomes a control-system command-line interface.

### Why Behavioral Detection Beats Signature Detection in OT

Signature-based detection (this hash is bad, this string is bad, this binary is malware) does not work on LOTL because the binary is legitimate. What works is **behavioral detection** - flagging traffic patterns and command sequences that legitimate activity should not exhibit.

In OT specifically, behavioral detection is plausible *because* legitimate behavior is so constrained. The RTAC polls the recloser every 5 seconds via DNP3 reads. Nothing else should be sending DNP3 to the recloser. A single DNP3 packet from the engineering workstation to the recloser at 02:47 on a Tuesday is anomalous on its face. You do not need to know whether the engineering workstation's binary is malicious to know that this packet is.

This is why the lab's defense-in-depth lesson stresses both segmentation (source-pin: only the RTAC can reach the field) AND monitoring (every cross-zone packet shows up in the Live DPI Events strip). Segmentation removes the easy paths; monitoring catches what slips through. Antivirus does not enter the conversation.

### How the Lab's Attacks Use LOTL

Every attack in Lab 2.3 and 2.3-bonus uses LOTL by design. `mbpoll` and `dnp3cmd` are real OT tools that any engineering workstation has. `xfreerdp` is a legitimate desktop client. `sshpass` is a legitimate scripting tool. None of these are malware. They are exactly what a vendor's engineer would use to do legitimate work - and exactly what an attacker who has compromised that engineer's workstation would use to do illegitimate work.

The pedagogical point: when you tell students "the firewall has to deny enterprise → field on Modbus," it is not because there is a particular piece of malware to block. It is because the *legitimate* tool, used from the *wrong* place, is the attack. Network policy is the right place to draw the line; endpoint malware detection is not where this fight is fought.

### Mitigation Posture

For OT environments specifically, the LOTL-mitigation posture looks like:

- **Strict network segmentation** with per-conduit allow rules (no broad zone allows)
- **Source-pinning** so a tool used from the wrong place is denied at L4
- **DPI for protocol surfaces** so the right tool used the wrong way is detected and logged
- **Behavioral monitoring** for traffic that does not match the baseline operational pattern
- **Just-in-time vendor access** (open the path for a maintenance window, close it after)
- **Per-session audit** so an investigation can pin actions to a specific human and timeframe

The lab exercises the first three. The L4 source pin enforces; DPI detects and logs. The remaining items are organizational practice that no docker compose can simulate, but they are where the real defense lives.
