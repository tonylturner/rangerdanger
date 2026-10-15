IEC 60870-5-104 carries telecontrol information over TCP. Its framing and acknowledgements establish orderly transport, but an acknowledged frame is not evidence that a switch moved. To interpret a capture, distinguish the connection state, the application data unit and the information object being reported or commanded.

### Frames and addresses

I frames carry application service data units (ASDUs) and send/receive sequence state. S frames acknowledge received I-frame traffic. U frames perform STARTDT, STOPDT and TESTFR functions: data transfer and link supervision, not equipment operations. A completed TCP handshake is therefore weaker evidence than STARTDT followed by a meaningful station exchange.

Within an ASDU, the type identifies the representation or operation, COT explains the cause of transmission, CA identifies the station and IOA identifies the information object. OA is the originator address. This package uses **CA 1**. An IOA only acquires operational meaning through its configured map; the same number in another station need not mean the same equipment.

### The implemented point families

The schema-2 package point map contains five devices, 31 monitored points and five commands. Important groupings are:

| IOAs | Meaning |
| --- | --- |
| 1001, 1002, 2001, 2002 | Breaker position, protection trip, current A, voltage kV |
| 1101–1104 / 1201–1204 | RMU1 / RMU2 position, remote, earth, fault passage |
| 2101–2103 / 2201–2203 | RMU current, section kV, source kV |
| 1301–1306 | OLTC tap, remote, auto, lower limit, upper limit, blocked |
| 2301, 2302 | OLTC LV bus kV and current A |
| 1401, 1402, 2401–2403 | PLC running, input-valid, setpoint kV, deadband %, delay s |

Double-point statuses use M_DP_TB_1 spontaneously and M_DP_NA_1 during GI. Single-point statuses use M_SP_TB_1 and M_SP_NA_1 respectively. Tap position uses M_ST_TB_1 and M_ST_NA_1, preserving both signed position and transient state. Floating-point values use untimed M_ME_NC_1.

Breaker/RMU commands at 3001/3101/3201 use C_DC_NA_1. OLTC step at 3301 uses C_RC_NA_1; mode at 3302 uses C_SC_NA_1. All five are select-before-operate. Station interrogation is C_IC_NA_1 with QOI 20. See [GI and quality](#iec104-gi-cot-quality) and [command sequencing](#iec104-sbo-commands) before interpreting their responses.

### Read an exchange

For an observation exercise, capture the allowed CC-to-RTU flow and identify STARTDT, interrogation activation, confirmation, monitored responses and termination. Then correlate a status change by CA, IOA, type and COT rather than matching only its text label. Compare the decoded value and quality with the CC export, including source time when present.

Configured link parameters are t0/t1 10 seconds, t2/t3 5 seconds, k 12 and w 8. These transport parameters are not the five-second SBO selection timeout or the actuator's movement duration. Mixing those budgets leads to false diagnoses of slow or failed operation.

### Limits

This is an implemented subset, not universal IEC 104 interoperability coverage. CLI support for a type is not evidence that the RTU accepts every operation of that type. The station does not offer an authenticated per-user command policy or IEC 62351 security through this map. Floating reports and GI are untimed; timed status means RTU observation time, not device SOE time. A successful decoder is an analysis tool, not proof of firewall DPI enforcement.

Implementation: package `protocols/points.json`; `services/iec104/src/pointmap.c`, `rtu.c` and `client.c`.
