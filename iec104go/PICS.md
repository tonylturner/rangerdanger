# Protocol implementation conformance statement (PICS)

Profile: **iec104go, codec phase 1a**, Apache-2.0. This is an implementation
interoperability profile, **not** certification or a claim to implement a whole
station. Selected editions: IEC 60870-5-104:2006+A1:2016, IEC 60870-5-101:2003.
Test identifiers refer only to the publicly previewed IEC TS 60870-5-604:2016;
**the September 2026 amendment was not consulted**. Sources and limitations are
in [PROVENANCE.md](PROVENANCE.md). Public-profile interpretations, especially
type/element associations, were checked against the supplied research errata.

Legend: X implemented; — not implemented/out of scope; **phase 1b** planned,
not implemented or validated. M = controlled → controlling (monitor direction),
C = controlling → controlled (control direction), with confirmations in reverse.
The codec can encode and decode either direction; it imposes no station role.

## Transport and addressing

| Item | Selected codec capability |
|---|---|
| TCP port / transport security / live connection establishment | phase 1b; no sockets or TLS in this module yet |
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
| SQ=0 / SQ=1 | X both; all object addresses exposed; SQ=1 must be contiguous and not overflow |
| Object count | X 1..127; zero malformed; builder packs until byte/count capacity |
| Truncation / trailing ASDU bytes | Rejected for supported types; unsupported payload opaque |
| Unknown, excluded or private type | X RawASDU retains header and payload plus ErrUnsupportedType |
| COT Test and Negative flags | X independent fields, preserved |
| Value, quality and qualifier policy | Raw bits preserved; station/application decides acceptable states/qualifiers |
| Retention / ownership | X all decoded data owned; builder snapshots own slices; no goroutines |

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
| 13 | file | File transfer cause retained, procedure unsupported |
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

## Session / timers / procedures: phase 1b

**Every row below is phase 1b, not implemented in phase 1a.** Values record the
supplied contract so an encoded feature is not mistaken for a working station.
Timer defaults follow the public Siemens default profile, not its selected t3.

| Item | Planned phase-1b profile |
|---|---|
| Roles | Client and server; plain TCP, default port 2404; no internal reconnect |
| k / w | Defaults 12 / 8; each 1..32767; permit w>k |
| t0 / t1 / t2 / t3 | Defaults 30 / 15 / 10 / 20 seconds; each >0; permit t2>=t1 |
| I/S in STOPPED | Close (strict contract decision) |
| N(S)/N(R) | Modulo 32768 per connection; STOPDT/STARTDT do not reset counters; invalid N(R) closes; bad N(S) sends pending ACK then closes |
| t1 | Outstanding I/U confirmation deadline; restart on partial ACK; expiry closes |
| t2 | Starts on first unacknowledged receive; later receives do not restart it |
| ACK timing | S at w or t2; piggyback whenever an I is sent; either ACK cancels t2 |
| t3 / TESTFR | Restart on send/receive; TESTFR in STOPPED and STARTED; t1 supervises con |
| STARTDT | Idempotent act in STARTED; con without duplicate event |
| STOPDT | ACK receive side, drain sent unacked I frames bounded by t1, then con/STOPPED; local act immediately blocks new Send |
| Server STARTDT/STOPDT role | Never initiates act; unexpected role-specific act/con is protocol error |
| Maximum connections | Default 8; extra accepted then immediately closed and logged |
| Redundancy | One connection per independent group; shared active/standby redundancy groups out of scope |
| Callback/lifetime | Per-connection ordered events/ASDUs; Closed last once; monotonic ConnID never reused |
| Queues and concurrency | Nonblocking bounded Send/Broadcast; full is explicit error, no silent drops; no callback can stall APCI |
| GI, group and counter interrogation | Initiator helpers with deadlines and responses; station/group QOI 20..36 |
| Read, clock, test, reset | Initiator helpers with deadlines; no automatic station policy |
| Direct and select/execute commands | Initiator sequences with ACT-CON/ACT-TERM tracking; station owns acceptance/selection policy |
| Mirror helper / COT44..47 | Preserve OA/CA/IOA/objects; application owns rejection precedence; RawASDU delivered |
| Harness taps | One write per APDU; ordered, synchronous send/receive taps; no TCP normalization inside codec |
| Shutdown / blocked calls | Context on blocking calls, no leaked goroutines, close reason observable |

Thus all §7 contract decisions are recorded: stopped-state strictness, connection
limit accept-and-close, STOPDT drain, S versus piggyback timing, and UTC/DOW/SU.
Only the time encoding decision is implemented now. Protocol violations cannot
be accepted merely because a future differential reference also accepts them.

## Exclusions and conformance evidence

| Item | Status |
|---|---|
| File transfer 120..127 | — outside scope (opaque RawASDU only) |
| CP24 process types, legacy test 104 and delay acquisition 106 | — outside selected 104 profile (opaque RawASDU only) |
| Security extensions, TLS, shared redundancy groups | — no implementation claim |
| IEC TS 60870-5-604:2016 5.3.2.10–39 | Public-preview APCI requirements inform synthetic codec tests; not a certification test execution |
| 5.3.2.50 and procedure test groups listed in preview | phase 1b; full procedures not available from preview |
| Golden evidence | 14 historical first-party APDUs, all exact round-trips; synthetic byte-position tests for all 46 types/SQ modes |
| Differential/live conformance | Later phases; no peer or Docker run in phase 1a |
