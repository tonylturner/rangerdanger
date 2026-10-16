# containd milestone B amendment: match IEC104 command state

Handoff spec for the agents that work on the containd `iec104` branch.
It amends `containd-milestone-b.md` §6 (rule predicates). Same owner and
rules: no push, no merge to containd `main`, no image publish.

- **Decision (Tony, 2026-10-16):** EU lab 2.3 denies breaker control on
  the control-centre (CC) session now, and containd gains a command-value
  predicate so a later course can allow breaker CLOSE and deny OPEN.
- **Priority:** after `containd-iec104-monitor-types.md`. That fix gates
  the first EU course (2026-11-07). This one does not; RangerDanger ships
  with IOA-level denial until it lands.
- **References:** `C:` = containd `iec104` at **4eb73bf1**. **V** = read
  from source, **P** = proposed.

## 1. Why

EU control centres legitimately switch feeder breakers, so IEC104 DPI
that denies the whole breaker command point (IOA 3001) also stops the
operator closing the breaker after a fault. The canonical EU attack is an
unauthorized breaker OPEN (C_DC_NA_1, DCS = 1). A rule that allows
`typeIds [46], ioa 3001, commandStates [2]` (close only) would keep the
operator's restore path and still stop the attack, even from the CC.

| Fact | Evidence |
|---|---|
| The decoder already records the command state as the event `value` for SCO/DCO/RCO, with the select bit. | **V** `C:pkg/dp/ics/iec104/asdu.go:180-190,208-213` |
| The rule context has no value field. | **V** `C:pkg/dp/rules/iec104.go:19-29` |
| The matcher checks direction, readOnly, typeIds, classes, CA and IOA only. | **V** `C:pkg/dp/rules/iec104.go:122-148` |
| The config predicate has no value field. | **V** `C:pkg/cp/config/config.go:814-818` |

## 2. Required change (P)

- **Config:** `ICSPredicate.CommandStates []int` with JSON key
  `commandStates`, valid only for protocol `iec104`.
- **Values:** SCS 0/1 for single commands (45, 58); DCS 1 = OFF/open,
  2 = ON/close for double commands (46, 59); RCS 1 = lower, 2 = higher
  for regulating step (47, 60). Reject other values.
- **Validation:** `commandStates` requires `typeIds` and every listed
  type must be in {45, 46, 47, 58, 59, 60}. Mixing single-command (0/1)
  and double/regulating (1/2) types in one rule is rejected, because the
  value spaces differ. Reject it in `validate_iec104.go` with a message
  naming the rule.
- **Context:** `IEC104Context.CommandState *uint8`. Populate it from the
  event `value` only for those six types.
- **Matcher:** when the rule lists states, require
  `CommandState != nil` and membership. An object without a state never
  matches a state-constrained rule.
- **Select and execute:** both carry the state, so a denied OPEN is
  dropped at the select. No actuation and no SBO reservation reach the
  RTU (milestone B first-command prevention).
- **Confirmations:** ACT-CON and ACT-TERM echo the DCO. A rule in the
  `response` direction with the same states lets the operator's approved
  confirmations through. Document this in the predicate doc comment and
  the UI help text.
- **UI:** `C:ui/app/ics/iec104-fields.tsx` and `iec104.ts` get a
  command-state multi-select. It is shown only when every selected type
  is one of the six, and it is labelled per type (open/close,
  lower/raise, off/on).

## 3. Acceptance

- Unit: config validation (valid, mixed value spaces, non-command type,
  out-of-range value), matcher (state match, mismatch, missing state),
  and decoder-to-context population for all six types.
- Integration (containd harness): with
  `allow typeIds [46] ca [1] ioa 3001-3001 commandStates [2]` on
  CC→RTU, enforce mode:
  - a CC select-OPEN at IOA 3001 is dropped, the session resets, the
    breaker does not move, and GI recovers;
  - a CC select-then-execute CLOSE at IOA 3001 completes, with
    ACT-CON and ACT-TERM delivered.
- The US gates pass unchanged.

## 4. Out of scope

Setpoint value ranges (48-50, 61-63) and bitstring masks (51, 64).
