IEC104 I frames carry ASDUs (application service data units) with sequence and acknowledgment state. S frames acknowledge traffic. U frames perform STARTDT, STOPDT and TESTFR. They manage the link, not breaker switching.

CA is the common address of the station, OA the originator address, and IOA an information object's address. The ASDU type identifies the representation or operation. COT (cause of transmission) explains why it was sent. This station uses CA 1.

| IOA | Point | Representation |
| --- | --- | --- |
| 1001 | Breaker position | Double point |
| 1002 | Protection trip | Single point |
| 2001 | Current | Floating point, A |
| 2002 | Voltage | Floating point, kV |
| 3001 | Breaker command | Double command |

GI uses M_SP_NA_1 (type 1), M_DP_NA_1 (type 3) and M_ME_NC_1 (type 13). Timed status events use M_SP_TB_1 (30) and M_DP_TB_1 (31). The control command is C_DC_NA_1 (46). GI is C_IC_NA_1 (100). The CLI's generic single-command/type-45 support does not mean this RTU implements it.

Configured timers are t0/t1 = 10 seconds, t2/t3 = 5 seconds, with k = 12 and w = 8. See [GI and quality](#iec104-gi-cot-quality) and [SBO commands](#iec104-sbo-commands).
