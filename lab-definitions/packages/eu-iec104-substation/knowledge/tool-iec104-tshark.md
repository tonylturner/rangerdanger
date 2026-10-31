`tshark` decodes the packets a capture actually contains. It cannot establish what an unseen endpoint did, and decoding an IEC 104 command does not mean the firewall enforced its contents. Kali and engineering explicitly install `tshark`; the vendor image does not.

### Capture your own client session

In one Kali terminal, create the output directory and run a bounded capture:

```sh
mkdir -p /home/kali/workspace
tshark -i any -f 'host 10.30.30.20 and tcp port 2404' \
  -a duration:15 -w /home/kali/workspace/iec104.pcapng
```

In another, run the short monitor from [iec104cmd recipes](#tool-iec104cmd) while weak policy is active. The capture sees Kali's connection, not all CC traffic on another Docker network. Under improved policy it may contain only unsuccessful connection attempts.

Capture filters (`-f`) decide which packets are saved. Display filters (`-Y`) select from packets already saved. No display filter can recover packets excluded during capture.

### Check the installed dissector

Before relying on a field name, inspect the installed field registry:

```sh
tshark -G fields > /home/kali/workspace/tshark-fields.txt
tshark -r /home/kali/workspace/iec104.pcapng \
  -Y 'tcp.port == 2404' -V
```

The verbose decode is a useful first check when a packaged Wireshark version differs from a reference screenshot. Do not label an empty field column as “no command” until you know the field exists and the capture includes application traffic.

Useful IEC 104 display predicates are:

```text
iec60870_104.type == 3
iec60870_asdu.typeid == 100
iec60870_asdu.causetx == 20
iec60870_asdu.causetx == 3 || iec60870_asdu.causetx == 11
iec60870_asdu.typeid == 46 && iec60870_asdu.addr == 1 && iec60870_asdu.ioa == 3001
iec60870_asdu.dco.se == 1
iec60870_asdu.dco.se == 0
```

These identify U frames, GI, GI data, spontaneous/command-return information, the configured breaker double command, select and execute respectively. Combine the breaker predicate with select/execute to avoid mixing other commands.

The OLTC adds two different command types: type 47 at IOA 3301 is the regulating step, and type 45 at IOA 3302 selects auto/manual. Its completed tap is step-position information, type 5 in GI or type 32 with a time tag. A filter limited to double commands or double-point positions would miss these operations even in a complete capture. Use the common address, IOA and type together, then inspect the decoded qualifiers and response direction.

### Export a compact index

```sh
tshark -r /home/kali/workspace/iec104.pcapng \
  -Y 'iec60870_asdu' -T fields \
  -e frame.number -e frame.time_epoch -e ip.src -e ip.dst \
  -e tcp.stream -e iec60870_asdu.typeid -e iec60870_asdu.causetx \
  -e iec60870_asdu.addr -e iec60870_asdu.ioa \
  > /home/kali/workspace/iec104-fields.tsv
```

Keep the original capture. Multiple ASDUs or IOAs may share one frame, so a fields row is not necessarily one point or one protocol operation. Use `tcp.stream` and endpoints to separate a Kali test from the CC link in a firewall-side capture.

### Interpret quality and limits

Invalid-value fields include `iec60870_asdu.diq.iv`, `iec60870_asdu.siq.iv` and `iec60870_asdu.qds.iv`; not-topical uses the corresponding `.nt` fields. Timestamp invalidity is `iec60870_asdu.cp56time.iv`. Compare flags explicitly with `== 1`: presence does not mean true, and invalid timestamp does not automatically mean invalid value.

For a switching claim, correlate select, execute, confirmation, termination and fresh feedback with the CC journal and device audit. A retransmitted packet is not a second accepted operation. Historical captures may use different IOAs or timers; only this package's point map describes this range. Pair packet interpretation with [temporal proof](#eu-established-sessions) and [evidence exports](#eu-tool-curl-exports).
