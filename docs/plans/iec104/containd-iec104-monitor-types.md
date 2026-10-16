# containd milestone B amendment: single-point and step-position monitoring

Handoff spec for the agents that work on the containd `iec104` branch.
It amends `containd-milestone-b.md` §7.2. Same owner, deadline and rules
(no push, no merge to containd `main`, no image publish).

- **Deadline:** the first EU course is **2026-11-07**. Without this
  amendment, enforce mode cannot be turned on for the EU range.
- **References:** `C:` = containd `iec104` branch at **4eb73bf1**.
  `RD:` = RangerDanger branch `iec104` (integration tree `inc4-integ`).
  Re-check line numbers before you edit.
- **Evidence:** **V** = read from source. **P** = proposed.

## 1. The wall

The RangerDanger EU point map grew after milestone B was specified
(RD commit 74d78c4, "extend EU points"). The RTU now sends four monitor
types that have no B descriptor. In enforce mode containd denies an
`unsupported` APDU without a rule and aborts the session, so the first
general interrogation (GI) after STARTDT kills the control-centre link.

| Fact | Evidence |
|---|---|
| B descriptors cover monitor types 3, 13, 31 only. | **V** `C:pkg/dp/ics/iec104/types.go:89-92` |
| Validation allows only described types in an enforce ALLOW rule. | **V** `C:pkg/cp/config/validate_iec104.go:24-28,156-160` |
| An `unsupported` event is denied without a rule; in enforce it aborts the session. | **V** `C:pkg/dp/engine/iec_decide.go:38-46,66-70` |
| The UI type table lists 3, 13, 31 as the monitor types. | **V** `C:ui/app/ics/iec104.ts:35-37` |
| The RTU sends M_SP_NA_1 (1) and M_ST_NA_1 (5) in GI, and M_SP_TB_1 (30) and M_ST_TB_1 (32) spontaneously. | **V** `RD:services/iec104/src/rtu_server.c:51-85` |
| GI sends one ASDU per point: SQ = 0, count = 1. | **V** `RD:services/iec104/src/rtu_server.c:215-226` |
| The EU point map has 14 single-point and 1 step-position monitor points. | **V** `RD:lab-definitions/packages/eu-iec104-substation/protocols/points.json` (`kind` counts) |
| Milestone B §7.2 says "The first course uses only 3, 13, 31, 100 and 46". This is now false: the course also uses 1, 5, 30, 32 and commands 45 and 47. | **V** `RD:docs/plans/iec104/containd-milestone-b.md:417-424` |

## 2. Required change (P)

Add four monitor descriptors. Everything else in milestone B stays.

| Type ID | Name | Class | Element after IOA | SQ |
|---|---|---|---|---|
| 1 | `M_SP_NA_1` | monitor | SIQ (1) | SQ = 1 accepted |
| 5 | `M_ST_NA_1` | monitor | VTI (1) + QDS (1) | SQ = 1 accepted |
| 30 | `M_SP_TB_1` | monitor | SIQ (1) + CP56Time2a (7) | SQ = 0 only |
| 32 | `M_ST_TB_1` | monitor | VTI (1) + QDS (1) + CP56Time2a (7) | SQ = 0 only |

Wire rules (IEC 60870-5-101 §7.2.6; verify against your edition and
record the clause in the doc comment, as for the other types):

- **SIQ:** SPI = bit 0; bits 1-3 reserved (0); BL, SB, NT, IV = bits 4-7.
  Event `value` = SPI (0/1), `quality` = bits 4-7. A set reserved bit is
  `malformed`.
- **VTI:** value = bits 0-6 as a signed 7-bit integer (-64..+63);
  transient = bit 7. **QDS:** OV = bit 0; bits 1-3 reserved; BL, SB, NT,
  IV = bits 4-7. Event `value` = signed step, `transient` = bool,
  `quality` = QDS. Add `transient` to `eventfields` if no key exists.
- SQ/count rule: the untimed forms (1, 5) accept SQ = 1 like 3 and 13.
  The timed forms (30, 32) need SQ = 0 like 31. Update the §7.2 "B
  profile SQ/count" sentence to list 1, 3, 5, 13 and 30, 31, 32.
- Type 1, 5, 30 and 32 are read-only monitoring: they join the
  read-only set wherever 3, 13 and 31 are members.

## 3. Touch points (P; agents verify)

1. `C:pkg/dp/ics/iec104/types.go`: two element kinds (SIQ, VTI+QDS),
   their sizes, four descriptors.
2. `C:pkg/dp/ics/iec104/asdu.go` `decodeValue`: decode SIQ and VTI+QDS.
3. `C:pkg/cp/config/validate_iec104.go:24-28`: add 1, 5, 30, 32 to
   `iec104DescribedTypes`.
4. `C:ui/app/ics/iec104.ts`: four rows, class `monitor`,
   `descriptor: true`.
5. Synthetic fixtures `C:pkg/dp/ics/iec104/testdata/synthetic.tsv`:
   line 58 (`unsupported_m_sp_na_1`) becomes a valid monitor row. Add
   rows for 5, 30, 32; SQ = 1 for 1 and 5; reserved-bit `malformed`
   rows; truncated rows.
6. Any rule-matcher or read-only test that enumerates monitor types.

## 4. Acceptance

- Decoder unit tests pass for every new row in §3.5.
- A config with an enforce ALLOW rule naming `typeIds: [1, 3, 5, 13, 30, 31, 32]`
  validates; `[2]` (`M_SP_TA_1`) is still rejected.
- **End to end (RD harness):** the EU range runs the containd `iec104`
  image with an enforce rule that allows the control centre
  (10.60.60.20) to read the RTU (10.30.30.20:2404), class `monitor` and
  `system` safe reads. GI completes, all 15 single-point and
  step-position points arrive at the control centre, spontaneous timed
  updates arrive after a field change, and the session stays up for
  5 minutes with zero `unsupported` denials in the IEC104 health
  counters.
- The US gates pass unchanged (milestone B D5).

## 5. Out of scope

- Other monitor types (2, 4, 6-12, 14-21, 33-40). They stay unsupported.
- Any change to the command (45-64) or system (70-107) descriptors.
