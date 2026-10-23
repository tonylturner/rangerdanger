# Clean-room provenance

Written 2026-10-23 for the IEC 104 replacement, phases 1a/1b. No source from
lib60870, c104, j60870, OpenMUC, any other IEC 104 library, Wireshark's decoder,
or `services/iec104/src/` was read, fetched or copied. No implementation code
or paywalled standards text is bundled. Public references below are behavioral
sources, not normative conformance certification.

## Public sources relied on

| Public source | What it informed / qualification |
|---|---|
| Petr Matoušek, *Description and analysis of IEC 104 Protocol*, FIT-TR-2017-12, updated February 2024: https://www.fit.vut.cz/research/result-file/c168651/279814/TR-IEC104v2.pdf | Directly read as prose/diagrams with pdftotext. APCI formats, length/counters, ASDU envelopes, SQ, COT names/catalogue, element orders/sizes, quality and qualifier fields; sections 2.1–2.2 and appendices C.1–C.4. **Not followed blindly:** appendix C.3 conflates CP24/CP56 associations; 30–40 and 58–64 use CP56, legacy CP24 types are unsupported. Type 38 includes SEP before CP16+CP56 despite its incomplete C.1 row; type 39 uses SPE (not SEP). No ACK algorithm is implemented from its prose. |
| RST utility interoperability description: https://www.eso.lt/public/uploads/832_29.2-pdf.pdf | Via supplied public-source research synthesis, pp.37–61: bit positions/ranges for SIQ, DIQ, VTI, QDS, command/measurement fields and QOI; corroboration of CP56 type associations. |
| Beckhoff set-point layout: https://infosys.beckhoff.com/content/1033/tf6500_tc3_iec60870_5_10x/984501643.html | Via research synthesis: NVA+QOS, QL bits0–6 and SE bit7; applied to normalized/scaled/float setpoints and their CP56 variants. No code examples consulted. |
| IEC 62361-2:2013 public excerpt, section 6.2: https://standards.iteh.ai/catalog/standards/iec/d250f6a2-4020-423d-9e01-ceb308b41ecb/iec-62361-2-2013 | Via research synthesis: CP56 field/range/flag treatment; retain the minute bit6 (RES1/GEN), SU, IV, weekday and all reserved fields, rather than normalizing them. |
| IEC TS 60870-5-604:2016 public preview: https://preview.sist.si/sist-preview/22911/6866bf75c89b458aad0dcc431cfefdf4/IEC-TS-60870-5-604-2016.pdf | Via research synthesis, printed pp.10–13: 5.3.2.10–39 APCI tests, control length <=253 and reserved bits zero. 5.3.2.50 and contents identify future session/procedure tests, not a claim they pass. Full standard and September 2026 amendment not consulted. |
| Siemens RTU3000C interoperability profile: https://cache.industry.siemens.com/dl/files/299/109481299/att_1297197/v1/FH_RTU3000C_V60-IECinteroperability_76.pdf | Via research synthesis, p.16: profile-table conventions and k/w/t0–t3 defaults. Vendor choices are not universal normative requirements. |
| ProSoft PLX32 manual: https://www.prosoft-technology.com/prosoft/download/14015/317164/file/PLX32_104S_Protocol_UM.pdf | Via supplied synthesis, pp.25–26,69–72: t1 failure closes, transfer-control exchanges and configurable command termination. This profile requires termination for execute helpers; no peer code read. |
| EDP utility PIXIT: https://www.e-redes.pt/sites/eredes/files/normative_docs/DEF-C98-422N.pdf | Via supplied synthesis, section 3.1 pp.19–25: GI ACT/CON/data/TERM, clock and select/execute exchanges. Read and clock/test/reset are not forced into a generic GI-style ACT-TERM exchange. |

## First-party evidence and independent choices

- Supplied `clean-room.md`, `design.md`, `recon-features.md` §3 and
  `iec104go-clean-room-research.md` establish the requirements and the above
  indirect public-source evidence. The codec lane did not follow recon's links
  to our C application source. `dnp3go/` informed flat-module and test conventions
  only, not IEC behavior. LICENSE is copied from the repository root.
- `services/iec104/captures/{edge-addressing,sbo-double-command,
  startup-gi-monitor,session-management}.pcap` are first-party Apache-2.0
  protocol data. [testdata/README.md](testdata/README.md) records hashes and
  frame numbers. tshark was used only as a black-box executable (`-T fields`,
  `-V`, field/protocol discovery); its source was never opened.
- Public descriptions do not choose the CP56 century or application timezone.
  The selected profile emits UTC, DOW=SU=0 and interprets decoded calendars as
  2000–2099. Invalid calendars and all flags still round-trip as raw fields.
- The exported value-element API, strict malformed-frame behavior, atomic
  packing builder, ownership rules and error classification are original
  design choices under the supplied contract. No vendor API was imitated.
- Session sequence arithmetic, ACK windows, t1 partial-ACK restart, t2 first-
  receive anchoring and STOPDT draining implement the supplied design and the
  -604 public-preview research evidence, not Matoušek's ambiguous ACK prose.
  The isolated state machine, injectable clock, bounded admission/delivery,
  callback cancellation contract and serialized wire taps are original designs.
- This module is Apache-2.0. Running against a sealed GPL reference peer later
  does not authorize importing that peer's source into this module.
