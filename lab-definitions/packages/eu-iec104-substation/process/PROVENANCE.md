# CIGRE-derived teaching feeder: blocked operating-point preflight

`reference-candidate.dss` is an **inactive candidate**, not the CIGRE benchmark.
`reference_probe.py` performs converged static OpenDSS solves and prints the
observed cases. It exits **1** when the operating-point requirements fail.
`reference-cases.json` records those failures, not successful acceptance proofs.
The active `profile.json`, `feeder.dss` and process service remain unchanged.

Run with the service's Python requirements installed in a disposable venv:

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

## Why implementation stopped

Measured with opendssdirect.py 0.9.4; all 23 sampled solves converged:

| Case | Completed tap | Bus20 kV | Observation |
| --- | ---: | ---: | --- |
| Nominal, 2.34085 MW nameplate | 0 | 19.91346435 | Within ±0.5% |
| Nominal, one raise | +1 | 20.16281407 | Correct polarity |
| Nominal, one lower | −1 | 19.66412128 | Correct polarity |
| Load multiplier 2.0 | 0 | 19.80224868 | Still inside ±1%; no PLC demand |
| Load multiplier 2.0, forced raise | +1 | 20.05046184 | Diagnostic only, not automatic recovery |
| Source 0.90, final demand | +8 | 19.69326520 | Low and at upper limit |
| Source 1.10, final demand | −7 | 20.00359757 | In band before lower limit |
| Source 1.10, forced lower limit | −8 | 19.72919095 | Below band; PLC would request a raise |

At source 1.10 the deadband controller stops at **−7**, already inside
19.8–20.2 kV. Forcing another lower step is not the specified controller.
Even without load, 1.10 × (1 − 7 × 0.0125) = **1.00375 p.u.**:
an ordinary downstream LV-winding tap has already compensated the disturbance.
Changing to an HV-winding reciprocal convention is not a fix for both ends:
0.90 / (1 − 8 × 0.0125) = 1.00 p.u. before losses.

The selected unscaled load subset also leaves doubled load just **inside**
the ±1% band. That failure is specific to this candidate, not a proof that
every possible honest load subset fails. The numerical probe has not been
used to tune impedances, loads or power factors to the requested outcomes.

The source-limit requirement needs a product/contract decision (for example,
a larger source excursion or narrower tap authority), followed by a fresh
operating-point review. This candidate must not be selected in deployment
until that decision and the schema-2 process implementation are complete.
