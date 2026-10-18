# CIGRE-derived teaching feeder

`feeder.dss` is the active EU circuit: a CIGRE-derived teaching feeder, **not
the CIGRE benchmark**. `profile.json` is schema 3; older schemas are rejected.
Complete BSD-3 notices are retained in `NOTICE.txt`.

`reference_probe.py` runs independent static OpenDSS solves against that same
circuit and prints the measured operating cases in `reference-cases.json`.
It exits nonzero if any EU realism operating-point requirement fails.
The profile is disabled for every reference case; traces start at tap 0.
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
labels are teaching labels; the source groups them together. No DER,
compensators or autonomous RegControl are introduced. The runtime teaching
profiles below are not CIGRE benchmark data. DSS model 1
uses its standard voltage-dependent fallback outside its constant-power range.

The source setpoint is 1.00 rather than the upstream reference's 1.03.
The tap is on **HV winding 1**, `1 - 0.0125 * completed_tap`, with ±8
teaching limits. Positive operator tap raises MV voltage. The ideal MV effect
is asymmetric: +8 gives 1/0.9 = +11.1%; −8 gives 1/1.1 = −9.1%, before
network voltage drops. This follows the requested 1.25% HV steps rather than
the upstream extraction's 0.625% secondary steps. No voltage offsets, clipped
voltages or tap-dependent loads are used.

## Operating-point evidence (EU realism contract rev 2)

Measured with opendssdirect.py 0.9.4; all 24 sampled solves converged, with the
profile disabled. The PLC setpoint is 20.4 kV, deadband ±1% =
**20.196–20.604 kV**, configured delay 30 s and actuator motion 2 s.

| Case | Completed tap | Bus20 kV | Observation |
| --- | ---: | ---: | --- |
| Nominal, 2.34085 MW nameplate | 0 | 19.91346435 | Below the new PLC band |
| Nominal, one raise | +1 | 20.16808162 | Still below band |
| Nominal, steady state | +2 | 20.42919735 | In band, close to 20.4 kV |
| Nominal, one lower | −1 | 19.66508827 | Correct polarity |
| Load multiplier 3.0 | 0 | 19.68483441 | Below band |
| Load multiplier 3.0, final demand | +2 | 20.20260102 | In band after two steps |
| Source 0.90, final demand | +8 | 19.91245230 | Low and at upper limit |
| Source 1.10, final demand | −6 | 20.38198681 | In band after six steps |

These are evidence, never tuning targets. `reference-cases.json` contains the
complete nominal and disturbance traces, the measured `steady_state`,
`initial_tap`, band, configured delay and motion time for gate consumers.
Each trace starts at tap 0, not at the nominal settled tap. Gate budgets must
allow steps × (configured delay + motion) plus scheduling/protocol margin.
The feeder's impedances, base loads and power factors have not been changed
to alter these outcomes. The traces prove static power flow and adjacent
quasi-static deadband decisions, not PLC timing or wire behaviour.

## Runtime teaching profiles

The category mapping is exhaustive and fixed for this circuit:

| DSS load | Teaching category | Base kW | Base PF |
| --- | --- | ---: | ---: |
| Load.Residential (S1) | residential | 1435.6 | 0.97 |
| Load.Commercial (S2) | commercial | 573.75 | 0.85 |
| Load.Industrial (S3) | industrial | 331.5 | 0.85 |

Piecewise-linear daily factors use local time in `Europe/Berlin`, including
DST. Residential peaks at 20:00 (1.0), bottoms at 05:00 (0.5); commercial
peaks at noon (1.0), bottoms overnight (0.5); industrial stays at 0.9 before
noise. Exact anchors are in `eu_dynamics.py`. All category factors, including
noise, are bounded to 0.5–1.0 of base kW/kvar; PF is preserved.

Independent load-category Ornstein–Uhlenbeck noise has stationary sigma 0.02
and tau 120 s. The source uses the same sigma, mean 1.0 and tau 600 s,
with its output clamped to 0.985–1.025 pu (not its latent OU state).
The exact OU transition uses elapsed wall-clock
seconds, not a draw per solve; zero elapsed time consumes no random draws.
Backward wall-clock steps advance no noise and cannot double-count time on
recovery. Seed null uses system entropy; integer seeds reproduce draws when
clocks and solve intervals match. These are teaching assumptions, not measured
load curves or a calibrated utility source-voltage model.

Effective load = category factor × process `load_multiplier` × base kW/kvar.
Effective source pu = profile source pu × process `source_pu`. Controls reset
to 1.0 without restarting the profile. `set_profile` with `enabled: false`
on the existing process-actions endpoint gives every category factor 1.0,
source 1.0 and no noise. Reset does **not** re-enable it; `set_profile` with
`enabled: true` resumes it. Disabling clears latent noise. Deterministic tests
and gates must disable the profile explicitly.

## Schema-3 runtime and deployment

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

Measurements are three-phase average current magnitudes and total terminal
P/Q (kW/kvar converted to MW/Mvar). Positive power flows from the configured
source side to section side. Switches use DSS terminal 1; T1 uses negated MV
terminal-2 powers. Reverse power stays negative; open switches report exact
zero P/Q. There is no topological fault-current flag or fault-path authority.
Solve sequence and UTC `solved_at` strictly increase within an epoch; timestamp
collisions or backward wall time are advanced to the previous timestamp +1 ms.
The electrical snapshot includes the last POST's current/P/Q per device,
effective profile factors and F2 protection, with new sampled numbers null
before the first solve.

Faults are three-phase shunts of 1 micro-ohm per phase (finite numerical
representation of a bolted fault). A closed earth switch on either RMU enables
the S2 DSS fault; whether current actually flows is exclusively a DSS result.

F2 protection samples **solved F2 terminal-1 current**: pickup 400 A,
delay 1000 ms, instantaneous 3000 A. The first picked-up solve starts the timer;
the first subsequent sample at or beyond delay trips. Thus effective operate
time is the delay rounded up to the solve cadence (and may be later with
scheduling jitter). A correction solve is a sample, not an elapsed second.
Drop-off, epoch change, unavailable truth/nonconverged solve, reset, or a gap
**greater than 2500 ms** clears the pickup timer. F2 has no separate field-device
generation: process restart changes its epoch; process reset/reset_f2 restarts
its timer and clears last-trip evidence. Field generation/revision changes do
not reset this independently process-owned relay. Last trip current persists
after drop-off until reset. An already open F2 cannot trip again.

The F2 head stays latched open until `reset_f2` or process `reset`. Resetting
a connected fault starts a new pickup period, not an immediate Boolean trip.
In the disabled-profile baseline, S1/S2 radial faults draw about 2701/1438 A
at CB1; an isolated F2 back-feed into the S2 fault draws about 2535 A at F2.
These are below the 3000 A instantaneous setting, so delayed operation is
expected. Opening F2 does **not** forcibly zero S3: DSS may supply it through
a healthy closed ring. It is dead when no alternate supply remains.
