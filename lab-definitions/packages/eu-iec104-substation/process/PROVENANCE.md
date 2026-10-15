# CIGRE-derived teaching feeder

`feeder.dss` is the active EU circuit: a CIGRE-derived teaching feeder, **not
the CIGRE benchmark**. `profile.json` is schema 2; no schema-1 compatibility
path is supported. Complete BSD-3 notices are retained in `NOTICE.txt`.

`reference_probe.py` runs independent static OpenDSS solves against that same
circuit and prints the measured operating cases in `reference-cases.json`.
It exits nonzero if any contract-v3 operating-point requirement fails.
These are physical operating-point proofs, not PLC timing or wire evidence.

Run with the service's pinned Python requirements in a disposable venv:

```sh
python lab-definitions/packages/eu-iec104-substation/process/reference_probe.py
```

## Parameters and attribution

Sources are BSD-3; their complete notices are retained in `NOTICE.txt`.

- [pandapower cigre_networks.py, revision
  5a5b042fb8935a9c3087909a9d88c7fa34f67920](https://github.com/e2nIEE/pandapower/blob/5a5b042fb8935a9c3087909a9d88c7fa34f67920/pandapower/networks/cigre_networks.py):
  `create_cigre_network_mv`, source 5000 MVA, R/X 0.1; T1 25 MVA,
  110/20 kV, `vkr_percent=0.16`, `vk_percent=12.00107`; cable R1/X1
  0.501/0.716 ohm/km and C1 151.1749 nF/km; overhead 0.510/0.366
  ohm/km and C1 10.09679 nF/km.
- [CIGRE-MV-PSCAD extraction, revision
  cf77b9707ed7d8d383e8172e8b085226b66e4bbd](https://github.com/panas-bhattarai/CIGRE-MV-PSCAD/blob/cf77b9707ed7d8d383e8172e8b085226b66e4bbd/data/cigre_mv_european_tb575.json):
  cable R0/X0 0.817/1.598 ohm/km, C0=C1; overhead R0/X0
  0.658/1.611 ohm/km, C0 4.074 nF/km. Its transformer R is **0.1%**,
  unlike pandapower's **0.16%**; this candidate consistently uses the
  pandapower transformer. These are engineering parameters, not a reproduction
  of the CIGRE brochure.

The reduction uses one shared T1, not the original pair. Section lengths are
the original 1–2 (2.82 km), 2–3 (4.42 km) and 12–13 (4.89 km), rearranged
into two outgoing feeders and a direct normally-open tie. Switches have
negligible impedance and no capacitance. S1 aggregates the unscaled residential
R3, R4 and R5 loads: (0.285 + 0.445 + 0.750) MVA at PF 0.97 =
1435.6 kW. S2 uses CI9: 0.675 MVA at PF 0.85 = 573.75 kW.
S3 uses CI14: 0.390 MVA at PF 0.85 = 331.5 kW. The commercial/industrial
labels are teaching labels; the source groups them together. No DER, load
profiles, compensators or autonomous RegControl are introduced. DSS model 1
uses its standard voltage-dependent fallback outside its constant-power range.

The source setpoint is 1.00 rather than the upstream reference's 1.03.
The tap is on winding 2, `1 + 0.0125 * completed_tap`, with ±8 teaching
limits. This intentionally follows the requested 1.25% steps rather than
the upstream extraction's 0.625% secondary OLTC steps. No gains, voltage
offsets, clipped voltages or tap-dependent loads are used.

## Operating-point evidence (contract v3)

Measured with opendssdirect.py 0.9.4; all 22 sampled solves converged:

| Case | Completed tap | Bus20 kV | Observation |
| --- | ---: | ---: | --- |
| Nominal, 2.34085 MW nameplate | 0 | 19.91346435 | Within ±0.5%, no PLC demand |
| Nominal, one raise | +1 | 20.16281407 | Correct polarity |
| Nominal, one lower | −1 | 19.66412128 | Correct polarity |
| Load multiplier 3.0 | 0 | 19.68483441 | Below the 19.8–20.2 kV band |
| Load multiplier 3.0, one raise | +1 | 19.92818774 | Back inside the band |
| Source 0.90, final demand | +8 | 19.69326520 | Low and at upper limit |
| Source 1.10, final demand | −7 | 20.00359757 | In band at a negative tap |

These are evidence, never tuning targets. Load multiplier 2.0 remained inside
the band in the initial preflight; the acceptance case is now 3.0. At source
1.10 the controller stops at −7: 1.10 × (1 − 7 × 0.0125) is already 1.00375
p.u. before losses. The feeder's impedances, loads and power factors have not
been changed to alter those outcomes.

## Schema-2 runtime and deployment

Keep `OPENDSS_PROFILE=/profiles/profile.json` and the existing read-only mount
of this entire directory at `/profiles`. The process requires HTTP reachability
to all four origins in `devices` (10.50.50.30–33:8080); those entries are
`{id, kind, truth_url}`, where truth_url is the origin, not `/api/truth`.
The profile maps the HV/MV buses, switching elements, transformer and faults.
The application image copies all `eu_*.py` runtime modules. No US service or
legacy US feeder path changes.

The process control routes stay on the existing solver API, port 8080:
`GET /api/electrical`, `POST /api/process/actions`, `GET /api/health`.
Before the first complete truth vector the electrical result has null sampled
values and `truth_stale=true`; it never assumes a switch position. Actions
are serialized with ticks, validated before mutation, and trigger a fresh tick.
On truth loss, sampled bus values and their solve timestamp remain last-known.

Each tick reads all truths concurrently, solves once and delivers all four
measurements concurrently. Each device's delivery result is independent.
A stale-truth conflict, a newer acknowledged revision or an F2 trip uses the
single immediate correction budget: reread every truth and solve again.
A further change during that correction is settled on the next tick, not a
third solve. Health remains degraded while a correction is pending. F2's
latched state is process truth; bus values are samples at `solved_at` and can
precede the resulting trip, just like a device's protection-changing ack.
Measurements expire at the devices after 3 s; delivery success is not a claim
that previously sampled measurements stay valid indefinitely.

Faults are three-phase shunts of 1 micro-ohm per phase (finite numerical
representation of a bolted fault). Fault-current flags follow all simple
closed source-to-fault paths; DSS still computes the actual meshed voltages
and currents if a ring closes. A closed earth switch on either RMU's live
S2 side defensively enables the S2 fault. The F2 head latches open on fault
passage and remains open until `reset_f2` or process `reset`. Resetting a
connected fault trips it again. Opening F2 does **not** forcibly zero S3:
S3 can still be energised through a healthy closed ring. It is dead in the
specified fault-isolation/back-feed case when no alternate supply remains.
