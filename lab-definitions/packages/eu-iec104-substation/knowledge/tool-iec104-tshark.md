Capture filters and display filters act at different stages: `-f 'tcp port 2404'` limits capture; `-Y` selects decoded packets.

Useful display filters:

```text
tcp.port == 2404
iec60870_104.type == 3
iec60870_asdu.typeid == 100
iec60870_asdu.causetx == 20
iec60870_asdu.causetx == 3 || iec60870_asdu.causetx == 11
iec60870_asdu.typeid == 46 && iec60870_asdu.addr == 1 && iec60870_asdu.ioa == 3001
iec60870_asdu.dco.se == 1
iec60870_asdu.dco.se == 0
iec60870_asdu.diq.iv == 1 || iec60870_asdu.siq.iv == 1 || iec60870_asdu.qds.iv == 1
iec60870_asdu.cp56time.iv == 1
```

These identify U frames, GI, GI data, spontaneous/command feedback, the CA 1 / IOA 3001 double command, select/execute, value invalidity and timestamp invalidity respectively. Combine the command filter with the select/execute predicate.

Extract `frame.number`, `ip.src`, `ip.dst`, `tcp.stream`, `iec60870_asdu.typeid`, `iec60870_asdu.causetx`, `iec60870_asdu.addr`, `iec60870_asdu.ioa`, `iec60870_asdu.dco.se` and `iec60870_asdu.dco.on` using `-T fields -e ...`. NT fields are `diq.nt`, `siq.nt` and `qds.nt` under `iec60870_asdu`. Compare boolean values explicitly; field presence is not truth.

Multiple ASDUs or IOAs can share a frame: fields output is not one-row-per-point proof. Isolate CC and Kali using `tcp.stream`. Verify field names with the shipped Kali `tshark -G fields` before using them as evidence.

Host tshark 4.6.7 decoded historical samples; that does not validate shipped Kali or current-slice packets. Historical captures use spike-era point maps and timers, with regeneration deferred. Decoding packets is not firewall semantic enforcement. Pair captures with [temporal/session evidence](#eu-established-sessions).

Sources: recon host dissector inspection; `services/iec104/captures/README.md` lines 10–25, 60–88. Current map: package `protocols/points.json` lines 3–107.
