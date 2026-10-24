# Protocol implementation conformance statement (PICS)

Profile: **iec104go, phases 1a/1b/1c**, Apache-2.0. This is an implementation
interoperability profile, **not** certification or a claim to implement a whole
station. Selected editions: IEC 60870-5-104:2006+A1:2016, IEC 60870-5-101:2003.
Test identifiers refer only to the publicly previewed IEC TS 60870-5-604:2016;
**the September 2026 amendment was not consulted**. Sources and limitations are
in [PROVENANCE.md](PROVENANCE.md). Public-profile interpretations, especially
type/element associations, were checked against the supplied research errata.

Legend: X implemented; — not implemented/out of scope. M = controlled → controlling (monitor direction),
C = controlling → controlled (control direction), with confirmations in reverse.
The codec can encode and decode either direction; it imposes no station role.

## Transport and addressing

| Item | Selected codec capability |
|---|---|
| TCP port / transport security / connection establishment | X plain TCP; caller supplies host:port (normally 2404); t0 and optional source bind; no TLS |
| I, S, U frame encoding and parsing | X; 15-bit unshifted sequence API, wire little-endian |
| U functions | X STARTDT act/con, STOPDT act/con, TESTFR act/con |
| Length | X control+ASDU 4..253; S/U exactly 4; I must carry payload; ASDU <=249 |
| Reserved APCI bits | X reject nonzero; exactly one U function, no payload in S/U |
| Stream fragmentation/coalescing | X exact-boundary APDUReader; no resynchronization after corruption |
| COT width / originator | X selected 2 octets; OA explicit, no injected default |
| Common address | X selected 2 octets, raw range 0..65535 |
| Information-object address | X selected 3 octets, raw range 0..16777215 |
| Alternative address widths | X COT 1/2, CA 1/2, IOA 1/2/3; codec convenience, not selected 104 profile |
| Nonzero OA with one-octet COT | Rejected rather than silently lost |
| SQ=0 / SQ=1 | X both; all object addresses exposed; generic SQ=1 contiguous/nonoverflowing; directory126 shares one IOA |
| Object count | X 1..127; zero malformed; builder packs until byte/count capacity |
| Truncation / trailing ASDU bytes | Rejected for supported types; unsupported payload opaque |
| Unknown, excluded or private type | X RawASDU retains header and payload plus ErrUnsupportedType |
| COT Test and Negative flags | X independent fields, preserved |
| Value, quality and qualifier policy | Raw bits preserved; station/application decides acceptable states/qualifiers |
| Retention / ownership | X all decoded data owned; builder snapshots own slices; session workers joined on closure |

For known types, payload length and SQ overflow are structural malformation,
not silently converted to opaque data. Unsupported types cannot have their
object layout validated, so only their envelope/size/count is checked. The
codec performs no CA/IOA acceptance, COT/type pairing or procedure policy.

## Process information: monitor direction

All listed rows: **X encode, X decode**. Sizes exclude IOA and include time
where applicable. See the COT capability table below for causes/flags.

| ID | Standard type | Direction | Element layout | Bytes |
|---:|---|:---:|---|---:|
| 1 | M_SP_NA_1 | M | SIQ | 1 |
| 3 | M_DP_NA_1 | M | DIQ | 1 |
| 5 | M_ST_NA_1 | M | VTI, QDS | 2 |
| 7 | M_BO_NA_1 | M | BSI32, QDS | 5 |
| 9 | M_ME_NA_1 | M | NVA, QDS | 3 |
| 11 | M_ME_NB_1 | M | SVA, QDS | 3 |
| 13 | M_ME_NC_1 | M | IEEE-754 float32, QDS | 5 |
| 15 | M_IT_NA_1 | M | BCR (signed int32, sequence/CY/CA/IV) | 5 |
| 20 | M_PS_NA_1 | M | SCD (status16, change16), QDS | 5 |
| 21 | M_ME_ND_1 | M | NVA, no quality | 2 |
| 30 | M_SP_TB_1 | M | SIQ, CP56 | 8 |
| 31 | M_DP_TB_1 | M | DIQ, CP56 | 8 |
| 32 | M_ST_TB_1 | M | VTI, QDS, CP56 | 9 |
| 33 | M_BO_TB_1 | M | BSI32, QDS, CP56 | 12 |
| 34 | M_ME_TD_1 | M | NVA, QDS, CP56 | 10 |
| 35 | M_ME_TE_1 | M | SVA, QDS, CP56 | 10 |
| 36 | M_ME_TF_1 | M | IEEE-754 float32, QDS, CP56 | 12 |
| 37 | M_IT_TB_1 | M | BCR, CP56 | 12 |
| 38 | M_EP_TD_1 | M | SEP, elapsed CP16, CP56 | 10 |
| 39 | M_EP_TE_1 | M | SPE, QDP, elapsed CP16, CP56 | 11 |
| 40 | M_EP_TF_1 | M | OCI, QDP, elapsed CP16, CP56 | 11 |

Quality: IV/NT/SB/BL retained; QDS also OV. SIQ/DIQ state bits remain separate
from quality. SEP/QDP elapsed-invalid bits and all reserved bits are retained.
VTI is signed −64..63 with transient indication. NVA is exact signed Q15.
Float32 NaN/Inf/signed-zero bits are not sanitized by the codec.

## Process information: control direction

All listed rows: **X encode, X decode**, including reverse-direction mirrored
confirmations. QU/QL and SE are retained without enforcing application policy.

| ID | Standard type | Direction | Element layout | Bytes |
|---:|---|:---:|---|---:|
| 45 | C_SC_NA_1 | C | SCO | 1 |
| 46 | C_DC_NA_1 | C | DCO | 1 |
| 47 | C_RC_NA_1 | C | RCO | 1 |
| 48 | C_SE_NA_1 | C | NVA, QOS | 3 |
| 49 | C_SE_NB_1 | C | SVA, QOS | 3 |
| 50 | C_SE_NC_1 | C | IEEE-754 float32, QOS | 5 |
| 51 | C_BO_NA_1 | C | BSI32 (no QDS) | 4 |
| 58 | C_SC_TA_1 | C | SCO, CP56 | 8 |
| 59 | C_DC_TA_1 | C | DCO, CP56 | 8 |
| 60 | C_RC_TA_1 | C | RCO, CP56 | 8 |
| 61 | C_SE_TA_1 | C | NVA, QOS, CP56 | 10 |
| 62 | C_SE_TB_1 | C | SVA, QOS, CP56 | 10 |
| 63 | C_SE_TC_1 | C | IEEE-754 float32, QOS, CP56 | 12 |
| 64 | C_BO_TA_1 | C | BSI32, CP56 | 11 |

## System and parameter information

All listed rows: **X encode, X decode**. IOA remains present even when there
are no element bytes (type 102). Clock-sync encoding does not imply a station
accepts clock synchronization or changes its clock.

| ID | Standard type | Direction | Element layout | Bytes |
|---:|---|:---:|---|---:|
| 70 | M_EI_NA_1 | M | COI | 1 |
| 100 | C_IC_NA_1 | C | QOI | 1 |
| 101 | C_CI_NA_1 | C | QCC | 1 |
| 102 | C_RD_NA_1 | C | none (IOA only) | 0 |
| 103 | C_CS_NA_1 | C | CP56 | 7 |
| 105 | C_RP_NA_1 | C | QRP | 1 |
| 107 | C_TS_TA_1 | C | test sequence uint16, CP56 | 9 |
| 110 | P_ME_NA_1 | C | NVA, QPM | 3 |
| 111 | P_ME_NB_1 | C | SVA, QPM | 3 |
| 112 | P_ME_NC_1 | C | IEEE-754 float32, QPM | 5 |
| 113 | P_AC_NA_1 | C | QPA | 1 |

## Cause of transmission

Codec: **X encode/decode every six-bit cause (0..63)**, including unknown or
reserved values, with independent P/N and Test flags and explicit OA. Mnemonics
are supplied for the standard causes below; unknown values have numeric names.
No codec-level type/COT or station-direction restriction is imposed. The
nominal usages here are descriptive, not procedure support claims.

| Cause(s) | Mnemonic(s) | Nominal use |
|---|---|---|
| 1, 2, 3 | per/cyc, back, spont | Monitoring data |
| 4 | init | End of initialization |
| 5 | req | Read/interrogated response |
| 6, 7 | act, actcon | Activation / confirmation |
| 8, 9 | deact, deactcon | Deactivation / confirmation |
| 10 | actterm | Activation termination |
| 11, 12 | retrem, retloc | Remote/local command feedback |
| 13 | file | Transparent-file procedure; see file profile below |
| 20, 21..36 | inrogen, inro1..16 | Station/group interrogation data |
| 37, 38..41 | reqcogen, reqco1..4 | Station/group counter data |
| 44..47 | unknown_type, unknown_cause, unknown_asdu_address, unknown_object_address | Application rejection, never automatically generated |

## Time profile decisions

| Item | Implemented decision / source |
|---|---|
| CP56 raw encoding | X 7 octets; every field, IV, RES1/GEN, SU, weekday and reserved bits round-trips; public CP56 diagrams and contract |
| Invalid calendar | ASDU decode succeeds; Time() returns false without normalizing dates |
| IV vs calendar validity | Independent: Time() reports calendar validity, caller separately inspects Invalid |
| CP56 generation | UTC, DOW=0, SU=0, year modulo 100, millisecond truncation; contract profile |
| CP56 interpretation | UTC, years 2000..2099; independent profile choice, raw century-ambiguous field retained |
| CP16 | X unsigned elapsed milliseconds, 0..65535, used by types 38–40 |
| CP24 | — legacy CP24 ASDU types not supported in selected 104 profile |

## Session / timers / procedures

All rows below are implemented and exercised by deterministic `net.Pipe`/fake-
clock tests, with loopback TCP reserved for connection establishment/admission.
The sources are the supplied contract, -604 public-preview research synthesis,
Siemens default timer profile and the procedure sources in PROVENANCE.md.
These are tested profile decisions, not a certification claim.

| Item | Selected implemented profile |
|---|---|
| Roles | X client/server, explicit STARTDT, plain TCP, optional local source bind; no reconnect |
| k / w | X defaults 12 / 8, each 1..32767; permit w>k |
| t0 / t1 / t2 / t3 | X defaults 30 / 15 / 10 / 20 seconds, each >0; permit t2>=t1 |
| t0 clock | net.Dialer context/timeout for TCP establishment; injected concurrent-safe Clock uses absolute deadlines for t1/t2/t3 to avoid registration/advance races |
| I/S in STOPPED | Close with ErrProtocol (strict contract choice); in-flight traffic remains legal while STOPDT is draining |
| N(S)/N(R) | Modulo 32768 per connection; no reset across STOPDT/STARTDT; N(R) outside outstanding interval closes |
| Bad N(S) | Queue pending S-ACK then close after its single Write, bounded by t1; cannot wait forever for a nonreading peer |
| t1 | Outstanding I or expected U confirmation; partial ACK restarts remaining-I deadline; independent U deadlines are not postponed by I acknowledgements |
| t2 | Anchored to first unacknowledged receive; later receives do not restart; S/piggyback ACK clears it |
| ACK timing | S at w or t2; one pending cumulative S ACK and one pending TESTFR con coalesce under slow writes; I/S N(R) refreshed at the write boundary to avoid regression behind a coalesced ACK |
| t3 / TESTFR | Reset on frame send/receive; TESTFR in STOPPED and STARTED; repeated t3 while awaiting con does not retransmit or extend t1 |
| STARTDT | Act while STARTED returns con with no repeated Started event |
| STOPDT | Freeze new admission; drain previously admitted sends before local act, and drain admitted/outstanding sends before peer-requested con; t1 bounds draining; flush final receive ACK before entering STOPPED |
| STOPDT counters | Never reset; reject local STOPDT con before our outstanding I frames are acknowledged |
| Control roles | Server never sends STARTDT/STOPDT act; unsolicited con and client-received STARTDT/STOPDT act close; TESTFR act is legal in both roles/states |
| Maximum connections | Default 8, configurable positive; extra TCP connection accepted then immediately closed and logged |
| Redundancy | Independent connection groups only; shared active/standby groups out of scope |
| Identity | Process-wide monotonic uint64, never reused; exhaustion fails instead of wrapping; retired Send returns ErrConnectionGone |
| Callbacks | One serial goroutine per connection; ordered Opened/Started/ASDUReceived/Stopped/Closed; Closed exactly once and last |
| Send queue | Default 64 pending ASDUs, configurable positive; full returns ErrQueueFull without admission; k gates transmission, never handler progress |
| Broadcast | Snapshot started set once, enqueue in ConnID order, return per-recipient errors; no slow-recipient wait |
| Callback backlog | Default 256 pending events; one reserved overflow-trigger slot plus Closed. Overflow closes explicitly with ErrEventQueueFull without dropping the triggering decoded ASDU |
| Callback cancellation | Handler receives connection context; Close/Shutdown join it. A handler ignoring cancellation can exceed shutdown ctx; Go cannot forcibly kill application code |
| GI / group interrogation | X QOI20..36, ACT-CON then image then ACT-TERM; preserve OA/CA and exact command echo |
| Counter interrogation | X type101, raw QCC request/freeze fields, COT37..41 image, CON then TERM |
| Read | X type102 Request -> requested-address data with Request; no fictitious ACT-CON/ACT-TERM |
| Clock / test / reset | X types103/107/105, CON only; no mandatory ACT-TERM |
| Direct / select-execute | X single/double/regulating and setpoint commands, including CP56; bitstring direct-only (no SE bit); select waits CON only, execute waits CON then TERM; never retry |
| Command policy | Station acceptance, SBO owner/expiry, cancellation policy, actual field writes and feedback remain application responsibility |
| Transactions | One initiating procedure per connection; caller deadline required; each request/SBO leg matches only after its write boundary and cannot complete until its full Write succeeds; earlier responses ignored; EventQueue response limit; recorded overflow overrides success; atomic detach/fence |
| Mirror / COT44..47 | X independent copy retaining OA/CA/IOA/type/SQ/test/objects/raw payload; no automatic rejection, application owns precedence |
| Harness taps | Serialized per connection; Receive after full APDU decode, Transmit immediately before one Write attempt; owned bytes; short Write closes without retry |
| Close / Shutdown | Context-bounded join of transport and callback workers; RequestClose is the nonblocking handler-safe variant; Done closes after Closed |

### Strict interpretations and contract corrections

- “Client receiving an act” means STARTDT/STOPDT, **not TESTFR**: the same
  contract explicitly requires TESTFR replies in both states/roles.
- The blanket procedure requirement to wait for ACT-CON/ACT-TERM is not valid
  for read, clock, test, reset, or the select leg of SBO. Those exchanges use
  the completion conditions above; only GI/counters and execute require TERM.
  This profile deliberately requires execute termination even for variants
  whose public vendor profiles make termination optional.
- The contract does not specify previously admitted, not-yet-transmitted data
  during STOPDT. Both directions drain that data with a t1 bound, rather than
  silently discarding successful admissions. STOPDT con before the initiator's
  outstanding sends are acknowledged is a protocol error.
- A synchronous tap necessarily participates in wire progress. Taps/loggers must
  return promptly. User callbacks, unlike taps, are isolated from APCI progress.
  A blocked callback cannot be forcibly killed: canceled contexts and bounded
  shutdown errors make this explicit, and tests verify eventual joining.
- The callback backlog is bounded to avoid unbounded memory on a slow handler.
  Its triggering event still precedes Closed; subsequent transport input is
  rejected by closure. Pending accepted events drain in order with canceled ctx.
- After a timed-out/canceled, overflowed or unordered procedure, later helpers
  return ErrProcedureUncertain until reconnect: otherwise a late identical echo
  could falsely confirm a new transaction. Manual sends/callbacks remain usable;
  the library does not choose the application's disconnect/retry policy.

All §7 decisions are implemented: stopped-state strictness, max-connection
accept-and-close, STOPDT drain, S versus piggyback timing, UTC/DOW/SU encoding.
Protocol violations are never allowed merely because a differential peer also
accepts them.

## File transfer (phase 1c)

**X encode/decode all eight types.** Source labels N/G/I refer to the
URLs and exact qualifications in [PROVENANCE.md](PROVENANCE.md). Codec
structure follows N/G §§7.3.6.1–7; type127 follows I §8.9, figure31.
Widths/qualifiers follow N/G §§7.2.6.28–38. This is transparent byte transfer,
not a structured disturbance/event/archive implementation or certification.

| ID / element | Ordered layout after IOA | Bytes | SQ/count | Nominal COT |
|---|---|---:|---|---|
| 120 / FileReady | NOF16, LOF24, FRQ8 | 6 | 0/1 | 13 |
| 121 / SectionReady | NOF16, NOS8, section LOF24, SRQ8 | 7 | 0/1 | 13 |
| 122 / FileCall | NOF16, NOS8, SCQ8 | 4 | 0/1 | 5 directory; 13 otherwise |
| 123 / FileLast | NOF16, NOS8, LSQ8, CHS8 | 5 | 0/1 | 13 |
| 124 / FileACK | NOF16, NOS8, AFQ8 | 4 | 0/1 | 13 |
| 125 / FileSegment | NOF16, NOS8, LOS8, data[LOS] | 4+LOS | 0/1 | 13 |
| 126 / FileDirectory | NOF16, LOF24, SOF8, CP56 | 13 per entry | 1/1..127; shared IOA | 3 or 5 |
| 127 / QueryLog | NOF16, Start CP56, Stop CP56 | 16 | 0/1 | 13 |

Sources G §§7.3.6.1–7 and I §8.9 also list COT44–47 for120–125/127;
no automatic generic diagnostics are generated. Direction is deliberately
not codec-enforced. The download sends120/121/123/125/126 in monitor direction,
122 and124 in control direction; replies/errors may reverse those directions.
The codec preserves every qualifier/reserved octet and zero lengths losslessly;
procedure policy below is stricter than the codec.
The SQ/count column describes **encoding**. Decoding accepts either single-object
VSQ form for120–127 and normalizes SQ to that sending profile; multiple-object
directory decoding still requires SQ1. All other bytes remain lossless.

### Decisions requiring peer/profile agreement

Every **choice** below is an application profile decision, not an assertion
that the public sources normatively settle it. Explicit sources distinguish
verified wire meanings from our choices for the research report's §7 gaps.

| ID | Implemented decision and evidence boundary |
|---|---|
| F1 | Directory call122 uses COT5, NOF=NOS=SCQ=0 at explicit CA/OA/IOA. This exact tuple is a **choice** because G's directory service does not settle all placeholders. The receiver accepts arbitrary NOF/NOS placeholders with SCQ0/COT5; nonzero SCQ receives unexpected-service error3 (owner-selected liberal acceptance). |
| F2 | Directory126 is SQ1 with **the same IOA on every exposed Object**, not incrementing synthetic addresses. Multiple ASDUs complete only on SOF.LFD (0x20), never ACT-TERM; G §7.3.6.7 / §7.4.11.2.6. Provider LFD bits are replaced with one final LFD; extra entries after LFD in one ASDU are rejected. |
| F3 | Empty/unavailable/over-limit directories receive122/COT5 with SCQ error1 (requested memory unavailable), not a fabricated empty entry or terminator. This pairing is a **choice** using the verified SCQ error table. LOF0 directory entries are accepted and served, including subdirectory/unknown-size metadata; zero-content transfer restrictions do not apply to directory metadata. |
| F4 | Zero-length files/sections and duplicate/zero section numbers are not served. Selection returns120 FRQ=0x80, no accepted zero-content sequence. Received zero LOF/LOS in an otherwise positive download fails ordering. N/G mark zero lengths unused; rejection choice is conservative. Zero fields in negative confirmations remain encodable. |
| F5 | NOF is an opaque16-bit application identifier, not forced to G's structured categories1–4 or a vendor running counter. Sections use unique nonzero8-bit numbers in provider order (not necessarily contiguous); calls accept only the currently offered section. Earlier/later/unknown section calls receive SCQ error5. These ordering restrictions are choices. |
| F6 | Select122/op1 → ready120; call-file122/op2 → ready121; call-section122/op6 → segments125 → last123/LSQ3 → ACK124/AFQ3; next section or file-end123/LSQ1 → ACK124/AFQ1. N §7.4.11.1 mapped to104 by I §7.11; no101 polls, link-layer ACKs or invented ACT-CON/TERM. Final file-end NOS uses/requires the last section number (**choice**, exact tuple not settled by handoff). |
| F7 | CHS is sum(content) mod256, separate section/file scopes, N/G §7.2.6.37. A bad section gets AFQ0x24; a bad complete file gets AFQ0x22. Accepted sections alone contribute to total length/checksum. The whole advertised length must match, and malformed/duplicate/unordered indications are rejected rather than guessed. |
| F8 | `Options.Files.SectionRetries` defaults0, range0..10. Only checksum-failed sections can be recalled: AFQ0x24 then122/op6 for that section; server retains it only within its configured retry budget. Both peers must agree. No automatic whole-file, timeout, duplicate-message or uncertain-control retry. G §7.4.11.2.7 permits recalls, but this precise budget/sequence is a **choice**. |
| F9 | `Options.Files.Timeout` defaults30s (>0), independent of t0–t3. Client bounds each send/wait (directory: whole directory exchange); server bounds provider/admission work and idle selected state. Caller deadline bounds the entire initiating helper. This numeric/default/reset policy is **not** derived from104 timers. Controlled idle supervision uses the injected Options.Clock. Provider/send/client context deadlines remain wall-clock application bounds, not APCI timer values. |
| F10 | Server worker waits for a broadcast admission-capacity/state change, bounded by its context. There is no polling timer or QueuePoll option; only **local unadmitted entries** are retried, never wire APDUs. Generation validation and admission share the session mutex, preventing stale admission across STOPDT/STARTDT. Client helper admission failure remains explicit ErrQueueFull; it does not silently wait/retry a request. These are choices. |
| F11 | `MaxFileSize` defaults16777215 bytes, range1..16777215; `DirectoryLimit` defaults4096 entries (>0). Zero options select defaults. Whole snapshots are bounded, not filesystem-backed streams. Provider errors/invalid snapshots reject selection via FRQ0x80; directory failures use F3. Limits are choices; uint24 width is verified. |
| F12 | Deactivate-file122/op3 for the active identity clears serving state and returns120/FRQ0. Idle/unknown-file calls receive SCQ error4; selecting any file while busy receives unexpected-service error3; unknown selection receives120/FRQ0x80 (G §§7.4.11.2.2–3). These exact state/error combinations are conservative choices from verified operations/errors. |
| F13 | Controlled idle timeout sends123/LSQ2; client failure best-effort admits122/op3. Real serving I/O/provider deadline failures request transport closure rather than leak a worker or pretend success. STOPDT cancels the transfer generation at the admission boundary, discards its results and resets state without closing TCP; immediate STARTDT creates a fresh generation. Already-admitted ASDUs drain under the existing STOPDT rules. Link loss cancels and clears file work; an actually lost transport cannot be retained. G §7.4.11.2.7 establishes interruption/deactivation in the cited download scope; timing/error-delivery details here are choices. |
| F14 | Delete, select-section, deactivate-section, unsolicited upload offers/data and unsupported ordered services receive SCQ error3. No upload or deletion engine is fabricated. N §7.4.11.2 documents an upload success path, but task's provider/download boundary and incomplete negative states do not establish an upload receiver. |
| F15 | QueryLog codec retains inclusive endpoints, all-zero begin/end sentinels and both-zero all-records semantics (I §8.9). The transparent-file handler does **not** accept positive QueryLog or interpret archives. It rejects all queries, including reversed/empty/unsupported/malformed-calendar ranges, with120 FRQ0x80 at the query's NOF. This conservative negative file-request response is a **profile choice**, not a verified normative type127 acceptance/rejection exchange. Applications implementing logs must route127 to their own handler instead. No fabricated archive, date normalization or positive continuation. |
| F16 | File negative replies use COT5/13 with header P/N=false and qualifiers carrying the error; they retain CA/OA/IOA/NOF, use the requested NOS for SCQ errors and NOS0 for unsupported non-SCQ services (**echo choices**). Mismatched124 acknowledgements receive122/SCQ errors4 (file),5 (section), or3 (ordering), never an acknowledgement-of-acknowledgement; no automatic COT44–47. The handoff does not settle every P/N case, so this is a choice. Client recognizes peer header negatives/generic diagnostics as ErrNegativeResponse. Test-marked/negative requests are logged without executing providers; application chooses generic diagnostics with Mirror. |
| F17 | A file failure after admission, including an explicit negative, atomically fences all later helpers with ErrProcedureUncertain; reconnect. Successful final ACK requires full Write, not queue admission. All file legs use the existing reader-tagged write boundary; early confirmations cannot satisfy queued requests. Conservative correlation choice; no transaction ID exists. |
| F18 | File responses are streamed out of the transaction buffer as consumed, bounded by EventQueue pending responses; ordinary helpers retain their existing bounded history. Recorded overflow dominates success and finalization is atomic. File helpers return bounded content/entries, not an unbounded raw response history. |
| F19 | Provider snapshots transfer slice ownership, must honor ctx and permit cross-connection calls. One serial worker per connection uses an EventQueue-sized inbox; overflow logs and RequestClose, never silent loss. Closed cancels/joins it. Handler synchronously clones file messages before enqueueing, so the composed application retains independent callback ownership. All normal ordered events/ASDUs still reach it. Segment decoding, Mirror, Builder admission and snapshots deep-copy data; Session.Send remains nonblocking. These are original API/lifetime choices. |
| F20 | Segment ceiling defaults236 data bytes, recomputed from249−header−IOA−4 for alternate Params. Raw codec retains invalid calendars/reserved bits; existing UTC/2000–2099 interpretation is unchanged. Directory timestamps and QueryLog sentinels do not add a new calendar policy. No archive/zip/payload structure is imposed. I figure4 + N/G layout give the size arithmetic; remaining choices preserve the established codec. |

### Evidence boundary

Eight independent synthetic wire-position goldens and both fuzz targets cover
120–127. Go controller/provider peers cover multi-ASDU directories, multi-section
downloads larger than the response history, checksum errors/recalls, unknown
file/section, aborts, unsupported requests, timeouts and cancellation/joining.
The supplied §3a lacks the detailed layouts and state table; choices F1–F20
make its remaining ambiguities explicit rather than inventing normative rules.

The generated capture is **not** a reference implementation trace: Go-peer
APDUs plus synthetic TCP envelopes and a separate eight-golden stream.
tshark4.6.7 parses APCI/ASDU envelopes and names120–127 but leaves their
information elements as **Raw Data**. It does not verify file fields, qualifiers,
content checksums, directory/QueryLog times or procedure ordering. A zero
malformed count proves framing/envelope dissection only; there is no C-reference
differential or independent positive QueryLog interoperability claim.

## Exclusions and conformance evidence

| Item | Status |
|---|---|
| File transfer 120..127 | X typed codec; transparent download/directory helpers in both station roles; exclusions below |
| CP24 process types, legacy test 104 and delay acquisition 106 | — outside selected 104 profile (opaque RawASDU only) |
| Security extensions, TLS, shared redundancy groups | — no implementation claim |
| IEC TS 60870-5-604:2016 5.3.2.10–39 | Public-preview APCI requirements inform synthetic codec tests; not a certification test execution |
| 5.3.2.50 and procedure test groups listed in preview | Deterministic session/procedure tests informed by the public evidence; full normative test procedures not available from preview |
| Golden evidence | 14 historical first-party APDUs, all exact round-trips; synthetic byte-position tests for all 54 types with their permitted SQ/count layouts |
| Differential/live conformance | Later phases; no Docker or reference peer run in phases 1a/1b/1c |
