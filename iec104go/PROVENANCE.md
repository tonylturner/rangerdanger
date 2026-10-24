# Clean-room provenance

Written 2026-10-23 for the IEC 104 replacement, phases 1a/1b/1c. No source from
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

## Phase 1c: file-transfer evidence and decisions

The supplied 2026-10-23 research handoff, `iec104-file-transfer-sources.md`,
was read in full. The following **public prose**, as established by that
handoff, is the only new protocol evidence used in phase 1c. No SDK manual,
implementation, header, code example or implementation-derived API was used.
The handoff is paraphrased evidence, not independent certification. Its
qualifications about a third-party transcription and a draft are retained.

| Label / public source | Exact evidence used |
|---|---|
| N: Nortegas/COSINOR, *Definición Protocolo CEI-870-5-101*, v3.3 (2013), https://www.nortegas.es/assets/uploads/2018/03/definicion-protocolo.pdf | §§7.2.6.28–38, printed pp30–34: NOF16, NOS8, LOF24, LOS8, qualifiers and content checksum. §§7.3.6.1–7, pp75–81: layouts 120–126. §7.4.11.1, pp102–103: download dialogue. §7.2.6.18, pp27–28: CP56 byte layout. Viewer page = printed page +5. N's contradictory SCQ/AFQ/SOF diagrams are not blindly implemented. |
| G: GOST R IEC 60870-5-101-2006 public transcription, https://allgosts.ru/33/200/gost_r_mek_60870-5-101-2006 | §§7.2.6.28–38: operation/ack low nibble, error high nibble, LSQ and SOF. §§7.3.6.1–7 (figures83–89): count/SQ/COT, especially directory's shared IOA. §7.4.11.2.6: LFD directory completion. §§7.4.11.2.2–3: FRQ negative selection. §7.4.11.2.7: timeout interruption, deactivation, section/file recalls. Mirror has transcription errors, not treated as infallible. |
| I: IMANOR PNM IEC 60870-5-104, IC06.4.141 (2022 **draft**), https://www.imanor.gov.ma/wp-content/uploads/2022/04/06.4.141-iec60870-5-104ed2.1fr.pdf | §8.9, pp47–48 figure31: QueryLog127 = NOF + two CP56 times, inclusive bounds and zero sentinels, continuation into preselected file transfer. §7.11: map file services onto TCP, omit 101 polls/link ACKs. P14 figure4: 249-octet ASDU ceiling. Not described as an enacted national standard. |

### Explicit conflicts resolved

- **NOS:** Matoušek appendix C.3 p37 says two octets. N §7.2.6.34 p33
  and G §7.2.6.34 independently specify **one octet**. Types121–125 use
  that one-octet field. Matoušek p34 does not establish type127's layout.
- N's SCQ/AFQ diagram labels conflict with G's numbered bits: use G's
  low-operation/high-error nibble definitions. G's repeated SRQ “ready”
  wording is resolved by N's explicit negative flag. G's NOF “UI6” typo
  is not used in place of the agreed two-octet layout.
- N's SOF bit6/range prose is not followed: G establishes LFD/FOR/FA
  and the five-bit STATUS allocation (therefore0–31, not the transcription's32).
  CP56 reserved fields retain the existing raw codec; do not import G's suspect month-width transcription.
- The 236-byte default segment limit is **derived**: 249−6−3−2−1−1.
  It is not a quoted vendor maximum; alternate Params recompute the bound.

### Original profile/API choices, not normative rules

[PICS.md](PICS.md#file-transfer-phase-1c) decisions F1–F20 specify every
remaining choice: the directory placeholder tuple and negative empty result;
unused zero lengths; naming/section ordering; unsupported services/QueryLog;
P/N policy; explicit application timeouts, bounded retries, memory/backlog
limits; callback isolation and ownership; uncertainty/abort behavior. They
are conservative implementation choices under the task, not claims derived
from missing text. In particular no upload or positive QueryLog state machine
is invented, and no vendor archive packaging is imposed.

The eight synthetic byte-position goldens were authored from N/G/I's layouts,
not from a reference implementation. The generated pcap combines our Go peers'
actual APDUs with synthetic TCP envelopes and a separate eight-golden stream.
tshark4.6.7 was executed only as a black-box decoder. It names all eight IDs
and parses APCI/ASDU envelopes, but leaves file elements as Raw Data; therefore
zero malformed frames do **not** independently verify these element layouts,
qualifiers, checksums, QueryLog times or the application state machine.

### Owner-selected review corrections (2026-10-23)

No additional public protocol source or implementation was consulted. The owner
selected liberal acceptance for unambiguous single-object SQ (normalized on
decode), directory placeholders/zero-length metadata and file-end NOS. Encoding
retains the strict forms. Clean correlated negatives and file deactivation no
longer impose uncertain-outcome fencing; overflow, ordering failures and unresolved
called transfers still do. PICS F1–F3/F6/F9–F13/F16–F20 record the revised
profile and original generation/callback-ownership/admission APIs.
The positive directory golden/capture now uses COT5; deliberately arbitrary-cause
preservation is a separate named test. F8's checksum-NACK-only recall policy,
F14's unsupported services and F15's archive boundary are unchanged.

### Graceful local close acknowledgement (2026-10-24)

First-party black-box evidence: phase-4a `cmd-rtu/comparison/report.json` under
`build/iec104-differential-go/{all-go,go-clients}/` in the supplied
`rangerdanger-b07fafb5` worktree. The sealed reference emits a final S frame
before TCP FIN where the previous Go stream did not (including
`680401000800` and `680401000a00`). Only the reports were inspected; no
reference implementation source was read.

The owner requires final cumulative acknowledgement on graceful local close,
without STOPDT or changes to running acknowledgement cadence. Counting accepted
receives and successful wire acknowledgements separately prevents queued or
blocked writes from losing that final ACK, and avoids duplicates when an
in-flight piggyback/S write completes. A 100 ms cap, limited further by t2 and
driven by the injected Clock, is an original best-effort shutdown profile
choice. Peer EOF, observed transport failure, cancellation and protocol errors
do not initiate this graceful flush. This is not a new normative APCI timer.
