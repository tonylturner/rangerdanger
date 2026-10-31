A reference case records an expected engineering result under explicit inputs. It helps distinguish an electrical-model problem from a controller, communications or display problem. It does not replace a live measurement, and a static calculation cannot prove that a PLC waited the correct time or that a command traversed the intended firewall boundary.

### What the package records

The package's `process/reference-cases.json` records converged static OpenDSS solves for the circuit selected by `process/profile.json`. It identifies schema 3, engine 0.9.4 and `feeder.dss`, with nominal connected real load 2340.85 kW. The recorded acceptance band is 20.196–20.604 kV at the primary LV bus.

The feeder is a reduced, **CIGRE-derived teaching model**, not the CIGRE benchmark. Provenance and notices accompany the circuit. The values are acceptance evidence from the implemented parameters, not targets achieved by secretly scaling the feeder.

| Case | Recorded result |
| --- | --- |
| Nominal tap 0, load 1.0, source 1.00 p.u. | Bus 19.91346435 kV, below PLC band |
| One lower from nominal | Tap −1, bus 19.66508827 kV |
| One raise from nominal | Tap +1, bus 20.16808162 kV |
| Triple load, tap 0 | Bus 19.68483441 kV, outside band |
| Nominal settled, tap +2 | Bus 20.42919735 kV, inside band and within ±0.5% of 20.4 kV |
| Triple load, tap +2 | Bus 20.20260102 kV, back inside band |
| Source 0.90 p.u., tap +8 | Bus 19.91245230 kV, still below band |
| Source 1.10 p.u., tap −6 | Bus 20.38198681 kV, inside band |

The reference samples also distinguish the CB1 line-side VT at f1 from S1:
at nominal tap 0 they are 19.91346435 kV and 19.70587545 kV, respectively;
at the settled tap +2 they are 20.42919735 kV and 20.22759677 kV. The S1
reading is RMU1's source-side measurement.

These schema-3 reference cases start at tap zero with the daily profile disabled. Positive tap raises solved voltage through the 110 kV winding tap ratio, giving an asymmetric +11.1% / −9.1% MV range. The high-source case settles at −6; requiring it to reach −8 would be the wrong acceptance criterion. The low-source case genuinely exhausts the +8 range without returning to the band.

### Match inputs before comparing outputs

The process supports `set_load` from 0.2 to 3.0 and `set_source` from 0.90 to 1.10. The authored lab steps apply those actions; student hosts should not need direct solver control. Process `reset` restores both to 1.0, clears the fault and recloses F2, and leaves the profile enabled state unchanged. Effective inputs normally include the Europe/Berlin daily category factors and source profile; disable these explicitly for a deterministic comparison, and re-enable them afterwards. Field device resets are separately ordered by [Reset Lab](#eu-reset-acceptance).

Before comparing with a reference row, confirm tap, normal switch configuration, fault absence, F2 status, load multiplier and source setting. Check `truth_stale`, `solver_epoch`, `solve_seq` and delivery revisions. An output retained after truth loss is not a new solve under your requested inputs.

Use the [feeder article](#eu-mv-feeder) for topology and [voltage regulation](#eu-voltage-regulation) for live control expectations. Static intermediate rows describe what the circuit would solve at each tap; they do not assert that the PLC has already moved through those taps.

### Evidence for a live acceptance claim

Starting from tap zero, a live triple-load test should show the disturbance, sustained low voltage, the PLC delay, an accepted request, completed motion and good post-motion voltage. Capture CC-observed evidence separately from physical/process evidence. If the CC conduit is denied, regulation may continue locally while the operator cannot establish the new voltage through telemetry.

Save the relevant historian interval with quality and source/receipt times, but use protocol/audit evidence for an individual operation. The [historian](#eu-historian) cannot supply a missing SOE trace or independently verify field truth.

### Limits

These are static power-flow results, not dynamic stability, protection grading, thermal capacity or regulatory voltage-compliance studies. Small numeric differences require checking engine, circuit and inputs before diagnosing a regression. A recorded `passed` status is not evidence that the current range has run the case. Never adjust feeder parameters merely to make a failing test resemble this table; report the measured value and the input vector.

Implementation: package `process/reference-cases.json`, `profile.json`, `feeder.dss`; `services/opendss-sim/tests/test_eu_circuit.py`.
