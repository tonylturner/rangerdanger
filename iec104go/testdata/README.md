# Golden packet vectors

`golden.json` contains 14 APDUs extracted on 2026-10-23 from our Apache-2.0
captures using `/opt/homebrew/bin/tshark` as an opaque decoder. No packet
implementation source was consulted. The JSON records frame number, pcap
SHA256, exact TCP payload, and independently decoded type/COT/OA/CA/IOAs.
The Go tests additionally assert edge floats, SBO flags, COT47 mirroring and
a CP56 calendar, then re-encode every golden byte for byte at both layers.

| Pcap in `services/iec104/captures/` | Frames extracted | SHA256 (matches that directory's SHA256SUMS) |
|---|---|---|
| edge-addressing.pcap | 11, 43, 44 | ded8efc24842159fca63244f78f01b72108c7fff7d5705ca745f27ac9354fa91 |
| sbo-double-command.pcap | 8, 9, 10, 11, 13 | 9dc5206f1e395453369118c687cc970e82c4027e3fa2e0c45fe79644c287e4c7 |
| startup-gi-monitor.pcap | 33 | b71b0f9c9b838d5037af73c65496d61bb6407b37cbc40310413a39303bf578e1 |
| session-management.pcap | 4, 6, 23, 53, 54 | 88246b0994b67cc7a630e0510aaf71be6469d57fefe9af6a7a69e6bd43871087 |

**Corrections to the phase-1a design's frame references:** startup-gi-monitor
frame 14 is a TCP SYN, not a CP56 ASDU; frame 33 supplies the requested CP56
vector instead. sbo-double-command frame 12 is a pure TCP ACK and has no APDU;
frames 8–11 and 13 contain select/confirm/execute/confirm/feedback. No ACT-TERM
is invented from these historical captures. Session frames 53–54 are STOPDT;
synthetic literal vectors separately cover every U function, including TESTFR.

Regenerate from the repository root (no Docker; each tshark call has a 30 s bound):

```sh
python3 iec104go/testdata/extract_goldens.py
```

The extractor first verifies each pcap against SHA256SUMS, then selects the
listed frames with `-Y 'frame.number==…' -T fields -e frame.number -e
tcp.payload -e iec60870_asdu.typeid -e iec60870_asdu.causetx -e
iec60870_asdu.oa -e iec60870_asdu.addr -e iec60870_asdu.ioa`.
Tests do not run this script or require the original pcaps/tshark. Synthetic
position-dependent vectors cover every other codec type and both SQ modes.
