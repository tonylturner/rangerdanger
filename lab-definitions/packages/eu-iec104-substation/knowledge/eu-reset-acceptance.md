A lab reset establishes a reproducible teaching baseline. It is not an operating restoration order: it can bypass the interlocks that ordinary remote commands must respect. Successful reset also needs more than successful HTTP calls. The electrical process must have calculated and delivered measurements for the new device states before the next exercise can rely on them.

### Ordered EU truth reset

The backend performs the package-specific sequence in this order:

1. Process `reset`: clear faults (not field trip latches), restore load multiplier 1.0 and source 1.00 p.u.
2. OLTC `reset`.
3. RMU2 `reset`, then RMU1 `reset`, then CB1 breaker `reset`, then F2 breaker `reset`.
4. GPS `reset_offset`.
5. Wait up to five seconds for post-reset measurement delivery.

Each field reset returns schema-3 state with the expected device id and revision. The wait requires non-stale process truth, every expected device delivered, equal `solved_revision` and `revision`, and revisions no older than the recorded post-reset values. A prior successful solve is not sufficient evidence for a newer reset state.

The restored device baseline is both breakers closed and remote, lockout/trip clear; RMU1 closed, RMU2 open, both remote with earths open and fault passage clear; OLTC completed/target tap 0, idle, remote, auto, unblocked and initially unarmed. The PLC's zero-before-request handshake arms the OLTC; there is no PLC reset call. Arming can therefore advance the revision beyond the immediate reset response without violating the acceptance rule. The tap can then move under automatic regulation: zero is the immediate reset value, not the steady-state baseline.

### Shared cleanup follows

After truth restoration and delivery confirmation, the handler applies the package's default firewall policy and performs shared capture housekeeping. The package baseline is weak. With Docker cleanup available it clears stored captures; preserve them before reset. Every action has a success/detail result. Any failed EU reset/cleanup action makes the response 502 with the full action list, rather than concealing a partial result behind an unconditional success.

The historian is not cleared, and the CC is not restarted. Resetting GPS offset does not claim to restore every possible GPS setting. In particular, the authored reset vocabulary resets offset, not a complete satellite/holdover configuration. Keep those distinctions visible in the handover notes.

### Acceptance after reset

In `/substation`, compare physical baseline positions and flags with fresh CC feedback. Check process `truth_stale`, delivery revisions and section voltages; then check CC link/GI, quality and receipt times. Quiet GOOD status does not expire; for validator acceptance require a retained current-connection GI inside the ten-minute attempt window and points received since its start. If missing, press **Interrogate station** in the **Control Centre** and validate again; no forced reconnect is needed. Read PLC running/input-valid/heartbeat evidence before calling automatic control ready. With the daily profile disabled, tap zero is below the 20.196–20.604 kV [regulation band](#eu-voltage-regulation); the 30 s delay and 2 s motions lead to nominal steady state +2 (about 20.429 kV). With the default daily profile running, the operating point varies. Reset leaves the profile enabled state unchanged; after deterministic checks, explicitly re-enable it.

Verify policy identity separately. Restoring weak intentionally reopens its teaching exposure; it is not a production hardening action. If the next test requires improved, apply it deliberately and establish fresh positive and negative protocol evidence inside that test's window.

### Limits and failure handling

Do not repeat reset blindly after a failed step: retain the action list and inspect the named upstream. The reset's delivery check proves physical/process convergence, not fresh northbound observation, full PLC timing or historian continuity. A disconnected CC can remain stale after a successful physical reset. Likewise, old historian samples remain valid historical evidence, not proof of the current baseline.

For ordinary fault recovery, distinguish `clear_fault`, breaker `reset_lockout` and a subsequent commanded close. Full reset bypasses operating interlocks and must not be used to “prove” that an [operational switching order](#eu-rmu-switching-orders) was safe or correctly sequenced.

Implementation: backend `eu_reset.go`, `reset.go`; package `topology.yml`; `services/devices/fielddevice/actions.go` and `model.go`.
